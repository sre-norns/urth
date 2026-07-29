# 011: Exercise the NATS Worker End to End and at Crash Points

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `done` |
| Priority | `P1` |
| Workstream | Claim lifecycle / Durability |
| Depends on | 001, 002, 003, 007, 010 (all done) |
| Likely conflicts | all runtime tasks |
| Owner | Completed 2026-07-30 |

> **Scope narrowed, 2026-07-29.** This task was `blocked` on "001–010", which put
> the project's largest stated validation gap behind the entire P0 security
> workstream. Most of it never needed that: the harness, the happy path, and the
> claim/ACK and outbox crash boundaries depend only on the durability tasks, which
> are done. The scenarios that genuinely need scoped credentials, enrollment,
> hardened capabilities and blocklists — revocation, rotation, expiry, blocklisting
> — moved to [task 024](024-authorization-integration-scenarios.md), which stays
> blocked on 004/005/006/009. Splitting them costs one shared harness, which 024
> reuses rather than rebuilds.

## Why This Matters

The current suite tests stream topology, envelope parsing, log transport, token
helpers, and individual labels. `cmd/nats-worker` has no tests, and no test runs
the real registration → NATS delivery → authenticated claim → ACK → execution
→ report path. The accepted ADR explicitly calls for integration tests around
lost claim responses, crashes, duplicate publication, expiry, rotation, and
blocklisting because those failures cross package boundaries.

Unit tests passing inside each package cannot show that HTTP status mapping,
JetStream disposition, Postgres versions, and Worker behavior compose safely.

## Evidence

- `cmd/nats-worker/` had no `_test.go` files at the review baseline. It now has
  four (tasks 001, 010, 012), but every one of them stubs the API: `stubResults`
  answers `ClaimRun` from a canned value, so nothing exercises a real HTTP status
  produced by a real `resultsAPIImpl` against a real Postgres row. The two halves
  of the status-class contract are each tested against the other's *assumption*.
- `pkg/natsq/natsq_test.go`: exercises JetStream assets and delivery, but not API
  claim or Worker execution/reporting.
- `pkg/urth/signing_test.go`: token helper coverage does not exercise HTTP routes
  or current resource state.
- `docs/adr/0004-nats-communication-backbone.md:373-384,499-516`: required
  failure matrix and migration completion gate.

## Required Outcome

Create a reusable integration harness that starts:

- real Postgres schema in an isolated test database;
- an embedded or subprocess `nats-server` with JetStream;
- the real Urth service and HTTP router on an ephemeral listener; and
- one or more real Worker loops using a deterministic test prober.

The harness is the deliverable that outlives this task: [task 024](024-authorization-integration-scenarios.md)
adds TLS and scoped credentials to it rather than standing up its own.

It exposes bounded failpoints at each durable boundary and runs the part of the
ADR failure matrix that does not turn on authorization:

- a claim whose response is lost, retried by the same worker, executing once;
- a worker killed after a confirmed ACK, settled by lease expiry and the
  reconciler, with the retry creating a *new* Result;
- a relay killed between the broker accepting a publication and the outbox row
  being marked published, deduplicated by the reused event UID;
- a dispatch redelivered while its run is executing, deduplicated in-process
  ([task 010](010-synchronous-jetstream-ack.md)); and
- a run whose message ages out, expired by the reconciler and not by the relay.

It must be CI-owned, deterministic, parallel-safe, and produce resource/stream
diagnostics on failure.

## Implementation Constraints

- Do not replace the integration path with mocks of the boundaries under test.
  Small fakes may control the prober or inject a failpoint, but Postgres, HTTP,
  and JetStream behavior must be real.
- Use unique database/schema, stream/account, Runner, Worker, and Result IDs per test.
- Every wait has a deadline and on failure prints Result, outbox, consumer, and
  message state. No unbounded sleeps.
- CI provisions Postgres explicitly. A local missing dependency may skip with a
  clear instruction, but the CI job must execute the suite.
- Prefer exported composition helpers over launching `main`; do not copy production
  wiring into tests if a small refactor can share it.

## Suggested Implementation Sequence

1. Extract testable API-server and Worker composition functions.
2. Build isolated Postgres/NATS fixtures with cleanup and diagnostic dumping.
3. Add one happy-path test proving full resource history and Artifact linkage.
4. Add claim/ACK and outbox crash failpoints.
5. Add the redelivery and lease-expiry scenarios above.
6. Add a dedicated CI job and document local invocation.

Auth revocation, blocklist, rotation and expiry are [task 024](024-authorization-integration-scenarios.md).

## Non-Goals

- Performance/load testing consumer-count scale.
- Browser UI end-to-end testing.
- Testing every prober implementation; one deterministic prober is sufficient
  for distributed lifecycle coverage.

## Acceptance Criteria / Definition of Done

- [x] Real happy-path Worker execution is covered end to end.
- [x] Every non-authorization ADR 0004 failure-table row has an automated scenario.
      The authorization rows moved to [task 024](024-authorization-integration-scenarios.md)
      when this task's scope was narrowed.
- [x] Tests run in CI with bounded waits and useful failure diagnostics.
- [x] The harness supports multiple Workers sharing one Runner and isolated Runners.
- [x] No test relies on public internet or developer machine state.
- [x] Worker behaviour has direct regression coverage (now `pkg/worker`).

## Required Tests

- Postgres commit while NATS unavailable; relay later publishes.
- Relay publishes then dies before marking outbox.
- Worker dies before claim, after claim response loss, and after confirmed ACK.
- Duplicate publish/delivery; different Worker loses claim; same Worker retry recovers.
- Transient API failure NAKs; stale ACKs; permanent policy failure enters DLQ.
- Message expiry and execution-lease expiry reconcile correctly.
- Runner disable/delete, Worker pause/blocklist, session/NATS credential rotation.
- Runner A Worker cannot consume/claim Runner B.

## Validation

```sh
make audit/postgres                                             # the gate; runs the suite for real
URTH_TEST_POSTGRES_URL=… go test -race -count=1 ./test/integration/...
go test -race -count=1 ./pkg/worker/... ./pkg/apiserver/...
go vet ./...
gofmt -l ./cmd ./pkg ./test
git diff --check
```

## Completion Record

- **Implemented:**
  - `pkg/apiserver` (new): the whole route table (`apiRoutes` → exported `Routes`),
    its handler helpers, `runlogs.go`, `metricsRegistry`, `Models()`, and the
    composition that used to live in `cmd/api-server/main.go` — store, signing
    keys, transport, service options, control loops — behind
    `New(ctx, db, cfg, opts…) (*Server, error)` with `Start`/`Wait`/`Close`. The
    prober blank imports moved here with it: the registry belongs to the package
    that decodes prob specs, so a test and a command decode against the same set
    of types. `cmd/api-server/main.go` is down from 978 lines to 95: kong, gorm,
    `AutoMigrate`, `http.Server`, shutdown.
  - `apiserver.WithPublisherDecorator`: the one seam a deployment does not need.
    It is how "the broker accepted this and then the relay died" is expressed
    without a second process, and it needs no production hook because
    `urth.DispatchPublisher` was already an interface.
  - `pkg/worker` (new): `main.go` (minus `main`), `consume.go`, `execute.go`,
    `report.go`, `ack.go`, `inflight.go`, `metrics.go` and all five test files.
    `worker` → `Worker`, `workerConfig` → `Config`; the `main()` defaults became
    `Config.Normalize()`; task 010's `executeJob` seam became the exported
    `WithProbeRunner` option. `cmd/nats-worker/main.go` is 42 lines.
  - **Bug found and fixed — a lost claim response cost a whole run.**
    `resultsAPIImpl.ClaimRun` refused a dispatch whose `ResultVersion` was behind
    the Result's — correct for a rescheduled run, wrong for the retry the
    dispatch ID exists to permit: committing a claim writes the Result, so a
    worker retrying after a lost response presents a version its *own* claim
    bumped. It got 409, correctly acknowledged the message away as stale, and the
    run sat in `running` — leased, to that same worker — until the reconciler
    expired it. `isReclaim` (same worker, same dispatch, still running) now
    exempts it, checked before the version guard as well as after. Every unit
    test on both sides passed against this; only running the two halves together
    could see it.
  - **Second bug found and fixed — a worker could stop consuming and look
    healthy.** `consume` pulled with only `FetchMaxWait(5s)`, and nats.go
    supplies an idle heartbeat only for windows of ten seconds or more. A pull
    request lost to a reconnect therefore waited forever on a reply to an inbox
    the server had forgotten: the loop took no further work and ignored
    cancellation, while the worker stayed connected, re-registered and
    heartbeated — every operator-visible signal saying the fleet was fine. The
    fetch is now bounded by `FetchContext` (prompt shutdown) *and*
    `FetchHeartbeat` (noticing a dead pull while still running), in
    `Worker.pump`. Reproduced by a broker restart mid-suite; the suite's own wall
    time dropped from a variable 28–42s to a steady 18s once workers stopped
    waiting out their pull windows on teardown.
  - Harness bug worth recording because it is the same class of mistake:
    `startWorker` returned once the worker had *registered*, which is before it
    binds its queue. Readiness is now "the runner's consumer has a waiting pull",
    and a `Run` that returned early is reported where it happened rather than as
    an unexplained silence in a later assertion.
- **Tests added/updated:**
  - `test/integration` (new package, `doc.go` plus test files). Postgres schema
    per test (`CREATE SCHEMA urthtest_<rand>` … `DROP SCHEMA … CASCADE`), an
    embedded NATS server per test on a reserved port with a file-backed store so
    it can be stopped and restarted as an outage, the real router on an ephemeral
    listener, and real `pkg/worker` loops. The control loops are composed but
    never started; `relayOnce`/`reconcileOnce` drive them, so nothing races a
    ticker. Every wait is a deadline-bounded `eventually` that dumps Results,
    outbox rows, dead letters, stream state and consumer info on failure. One
    registered prob kind whose behaviour comes from *spec fields*, never a
    global, so the scenarios are parallel-safe.
  - Seventeen scenarios: happy path with artifact linkage and executor identity;
    unplaceable run terminal and unqueued; broker outage then recovery; relay
    crash after publish (asserted against `StreamState.LastSeq`, so "one message
    reached the broker" is a fact rather than an inference); aged pending run
    expired by the reconciler *and not before it is published*; Postgres
    unavailable (the `results` table really is renamed away) NAKed and recovered;
    permanently undeliverable dispatch dead-lettered; lost claim response
    recovered by the same worker; worker dying mid-claim leaving the job for
    another; redelivery during execution; stale redelivery to a second worker;
    misrouted dispatch refused and reported; placement isolation between two
    runners; unreadable envelope reported then terminated; worker dying after a
    confirmed ack settled by lease expiry with the retry creating a new Result;
    an idle worker stopping promptly rather than waiting out its pull window;
    two workers sharing one runner.
  - `pkg/urth/service_execution_test.go`:
    `TestReclaimAfterALostResponseIsNotRefusedAsSuperseded` (fails against the
    old code — verified by reverting the guard) and
    `TestClaimForASupersededVersionIsStillRefused`, so the exemption does not
    read as "the version check is optional".
  - Both fixes were confirmed to fail against the code they repair before being
    counted: the reclaim guard by reverting the exemption, and the pull bounds by
    restoring the bare `FetchMaxWait` (4.5s to stop, against a 2s bound).
- **Documentation updated:** `CLAUDE.md` (layout, verification expectations, and
  a trap entry for the reclaim bug), `docs/review-backlog/CONTEXT.md` (package
  map and validation baseline), `cmd/api-server/README.md`,
  `cmd/nats-worker/README.md`, `TODO.md` (the dev-database trap narrowed to the
  `pkg/urth` fixtures, with the schema-per-test alternative named),
  `.github/workflows/go.yml` (`gofmt` scope now includes `./test`, audit timeout
  20 → 30), this record and the backlog index.
- **Validation evidence:** `make audit/postgres` green, including
  `test/integration` at 28s. The integration suite was run six consecutive times
  under `-race` to shake out flakes; two harness defects (cleanup ordering
  against a restarted broker, and the readiness race above) were found and fixed
  that way. `gofmt -l ./cmd ./pkg ./test` clean; `go vet ./...` clean;
  staticcheck `-checks=all` clean. The real stack was then run end to end against
  Postgres and NATS in containers — `urthctl apply`, worker registration, a
  triggered run reaching `completed` — because a green suite proves the packages
  compose, not that the binaries still work.
- **Follow-ups:**
  - The authorization rows of the failure table remain
    [task 024](024-authorization-integration-scenarios.md), which reuses this
    harness rather than standing up its own.
  - The `pkg/urth` fixtures still `DropTable` against `store-url`; moving them to
    `openTestSchema`'s shape is the open half of the `TODO.md` entry.
