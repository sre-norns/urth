# 002: Add the Transactional Dispatch Outbox

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `done` |
| Priority | `P0` |
| Workstream | Durability |
| Depends on | — |
| Likely conflicts | 003, 007, 011, 012 |
| Owner | `feat/dispatch-outbox` |

## Why This Matters

Creating a Result in Postgres and publishing its dispatch to JetStream are two
separate durable writes. The current request commits the Result first and then
publishes directly. A process crash, lost connection, or deployment between those
steps leaves authoritative state saying work is pending with no message that can
wake a Worker.

JetStream deduplication does not close this gap. It handles repeated publication;
it cannot publish a database change it never learned about.

## Evidence

- `pkg/urth/service.go:638-645`: Result creation commits before scheduling.
- `pkg/natsq/scheduler.go:100-106`: scheduling publishes directly to JetStream.
- `pkg/natsq/scheduler.go:79-83`: the current message ID is stable only for one
  Result version, which is useful input to the outbox but is not an outbox.
- `docs/adr/0004-nats-communication-backbone.md:83-108`: accepted transactional
  outbox and relay design.

## Required Outcome

- The transaction that creates a pending, Runner-bound Result also creates one
  dispatch outbox row with a stable event UID and versioned envelope data.
- No JetStream publication occurs before that transaction commits.
- One or more relay processes safely claim unpublished rows, publish through the
  JetStream persistence API using the event UID as `Nats-Msg-Id`, wait for the
  storage acknowledgement, and mark the row published.
- A relay crash before or after publication is safe: the row remains retryable
  and duplicate publication is tolerated.
- Publication failure leaves the Result pending and the outbox row visible for
  retry; it does not rewrite the Result as a fictional execution failure.
- Outbox age, attempt count, last error, and publish timestamp are observable.

## Implementation Constraints

- Result and outbox writes must use one real Postgres transaction. Sequential
  calls wrapped in comments are not sufficient. Extend the `wyrd/dbstore` seam
  or add a narrow Urth transaction adapter if the current interface cannot do it.
- Keep `pkg/urth` independent of `pkg/natsq`. The outbox stores a transport-neutral
  dispatch record or serialized versioned envelope; a relay adapter performs NATS
  publication.
- Competing relays must use row locking/leases such as `FOR UPDATE SKIP LOCKED`
  and must recover abandoned claims.
- The stable event UID is persisted, not regenerated on every retry.
- Preserve the Asynq migration path until task 015; document how the selected
  transport consumes outbox entries during transition.

## Suggested Implementation Sequence

1. Add an `OutboxEntry` resource/model and transaction-focused store tests.
2. Refactor Result creation so placement, Result, and dispatch entry commit together.
3. Split the current scheduler into dispatch publication and relay orchestration.
4. Implement safe row claiming, retry metadata, and JetStream `Nats-Msg-Id`.
5. Add crash-point integration tests using Postgres and embedded `nats-server`.
6. Add relay configuration, metrics, and operator documentation.

## Non-Goals

- Full pending/running/terminal reconciliation (task 003).
- Dead-letter processing after Worker delivery (task 012).
- `URTH_EVENTS` resource-event stream or the future scheduling loop.

## Acceptance Criteria / Definition of Done

- [x] Result and dispatch outbox row are atomic under commit and rollback.
- [x] A committed unpublished row is eventually published after NATS recovers.
- [x] Relay crashes before and after publish do not lose or duplicate a Result.
- [x] Multiple relays do not concurrently own the same row.
- [x] Publication failure remains observable without changing Result execution state.
- [x] Metrics and documentation expose stuck/old outbox rows.

## Required Tests

- Force rollback after constructing both models: neither persists.
- Commit while NATS is unavailable, start NATS later: dispatch arrives once and
  the row becomes published.
- Publish succeeds, relay dies before marking: retry uses the same message ID and
  one logical dispatch is delivered.
- Two relay instances compete for a batch without double-processing rows.

## Validation

```sh
go test -race -count=1 ./pkg/urth ./pkg/natsq ./cmd/api-server
go test -race -count=1 ./...
go vet ./...
git diff --check
```

## Completion Record

- **Implemented:**
  - `pkg/urth/outbox.go`: `DispatchOutboxEntry` model (table `dispatch_outbox`),
    `DispatchEventUID`, the `DispatchOutbox` and `DispatchPublisher` interfaces,
    `DispatchOutboxStats`, and the `ErrPermanentDispatch` sentinel. The entry
    stores dispatch *fields*, not a marshalled transport payload, so the row stays
    queryable and a relay running newer code re-encodes rather than replaying a
    stale wire format. The event UID is the one thing minted once and persisted.
  - `pkg/urth/outbox_store.go`: gorm-backed outbox. Talks to gorm directly rather
    than through `wyrd/dbstore`, because the store interface cannot express
    `SELECT ... FOR UPDATE SKIP LOCKED`, and a claim that cannot skip locked rows
    is a claim two relays can both win. Also holds `NewStoreResultLoader`.
  - `pkg/urth/outbox_relay.go`: `DispatchRelay` (claim → publish → mark, with
    lease, backoff, and per-attempt timeouts) and `SchedulerDispatchPublisher`,
    the adapter that keeps the legacy asynq transport on the same outbox.
  - `pkg/urth/service.go`: `resultsAPIImpl.Create` now commits the Result and its
    outbox entry through one `dbstore` transaction (`createWithDispatch`) and no
    longer publishes inline. The `scheduleRun` path and the `JobErrored` rewrite
    on scheduling failure are both gone — a broker outage is not an execution
    failure, and marking the Result terminal destroyed the only record a retry
    could work from.
  - `pkg/natsq/publisher.go`: `PublishDispatch` is now the sole publication path;
    `scheduler.Schedule` delegates to it. Added `natsq.Transport` so the API
    server composition no longer type-asserts for the transport provider.
  - `pkg/natsq/config.go`, `assets.go`: added `--nats.duplicate-window` (default
    30m) and set `Duplicates` on `URTH_JOBS`. The relay's at-least-once
    republication depends on this window; the stream previously left it default.
  - `cmd/api-server/main.go`: migrates `dispatch_outbox`, builds the transport's
    publisher (native for NATS, adapter for asynq), and runs the relay in-process
    by default with `--relay-*` flags.
- **Tests added/updated:**
  - `pkg/urth/outbox_relay_test.go` (no DB): crash-before-marking reuses the event
    UID; transport failure retries without marking published; permanent failures
    get the long backoff; one bad entry does not stop the batch; the legacy
    adapter dispatches and rejects a stale Result version.
  - `pkg/urth/outbox_store_test.go`: lease and release, expired-lease reclaim with
    the attempt still counted, failure backoff and stats. Postgres-only:
    `TestOutboxCompetingRelaysDoNotDoubleClaim` runs four relays over one backlog.
  - `pkg/urth/service_outbox_test.go` (Postgres): Result and dispatch commit
    together; a failed outbox insert rolls the Result back; a publication failure
    leaves the Result `pending` and the entry observable; the entry publishes once
    the transport recovers; an inactive scenario writes no entry.
  - `pkg/natsq/outbox_test.go` (embedded `nats-server`): dispatch committed while
    the broker is stopped is published after it restarts, and the stream holds
    exactly one message; a relay that publishes then fails to mark republishes and
    JetStream still holds one message; an unplaced dispatch is reported permanent.
  - `.github/workflows/go.yml`: Postgres service plus `URTH_TEST_POSTGRES_URL`.
    Without it CI reports green having skipped every atomicity and leasing test.
  - `Makefile`: `make test/postgres`. Plain `make test` stays container-free.
- **Documentation updated:** `cmd/api-server/README.md` (dispatch outbox: design,
  flags, the SQL to inspect the backlog, what to alert on, transport migration);
  `README.md` architecture and project-status notes; this record.
- **Validation evidence:**
  - `make audit` — pass. `URTH_TEST_POSTGRES_URL=… go test -race -count=1 ./...` —
    207 tests, all packages pass. `gofmt -l ./cmd ./pkg` — clean.
    `go mod tidy` — clean. `actionlint` on the changed workflow — clean.
  - Mutation-checked the two tests that carry the guarantees, because this
    repository has repeatedly shipped tests that passed against the bug they were
    meant to catch: disabling the locking clause makes the competing-relays test
    report duplicate claims; committing the Result before writing the outbox entry
    makes the rollback test find an orphaned Result.
  - Ran the real stack (Postgres + NATS + api-server + nats-worker). Verified end
    to end: an outbox row written on `POST /results`, relayed, executed, marked
    published. Then stopped NATS, triggered a run — `201`, Result stayed
    `pending`, entry unpublished with the failure and a backoff recorded — and
    restarted NATS: the entry published on attempt 3 and the run completed, with
    nobody re-requesting it. Confirmed `duplicate_window=30m` on the live stream
    and `timestamp with time zone` on every outbox time column.
- **Follow-ups:**
  - Dead-lettering parked entries is [task 012](012-dead-letter-workflow.md);
    until it lands, a permanently unpublishable entry retries hourly forever
    rather than being retired.
  - Reconciling pending Results against live queue state remains
    [task 003](003-reconcile-dispatch-and-execution.md), which is now unblocked.
  - Stats are exposed through `DispatchOutbox.Stats` and SQL, not a `/metrics`
    endpoint — the API server has no Prometheus registry yet. Worth adding with
    [task 013](013-bound-and-observe-jetstream.md), which owns broker observability.
  - `natsq.DispatchIDFor` is deprecated in favour of `urth.DispatchEventUID` and
    should go when task 015 removes the legacy paths that still call it.
