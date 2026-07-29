# 011: Exercise the NATS Worker End to End and at Crash Points

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P1` |
| Workstream | Claim lifecycle / Durability |
| Depends on | 001, 002, 003, 007, 010 (all done) |
| Likely conflicts | all runtime tasks |
| Owner | Unclaimed |

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

- [ ] Real happy-path Worker execution is covered end to end.
- [ ] Every ADR 0004 failure-table row has an automated scenario.
- [ ] Tests run in CI with bounded waits and useful failure diagnostics.
- [ ] The harness supports multiple Workers sharing one Runner and isolated Runners.
- [ ] No test relies on public internet or developer machine state.
- [ ] `cmd/nats-worker` behavior has direct regression coverage.

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
# Replace with the committed focused integration target/command.
go test -race -count=1 ./integration/...
go test -race -count=1 ./...
go vet ./...
git diff --check
```

## Completion Record

- **Implemented:**
- **Tests added/updated:**
- **Documentation updated:**
- **Validation evidence:**
- **Follow-ups:**
