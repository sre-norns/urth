# 003: Reconcile Dispatch and Execution Lifecycle

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `done` |
| Priority | `P0` |
| Workstream | Durability / Claim lifecycle |
| Depends on | 002 (done) |
| Likely conflicts | 001, 011, 012 |
| Owner | `feat/reconcile-dispatch-execution` |

## Why This Matters

The implementation records an execution deadline and configures queue-message
expiry, but nothing acts on either. A Worker that dies after acknowledging leaves
a running Result forever. A message aged out by JetStream leaves a pending Result
forever. Administrative consumer deletion and old terminal messages create the
same drift between authoritative resources and transport state.

The execution lease and outbox only become reliable when a periodic process uses
them to repair or explicitly terminate inconsistent state.

## Evidence

- `pkg/urth/types.go:191-196`: `Deadline` is recorded as a lease for a future reconciler.
- `pkg/natsq/assets.go:37`: JetStream may expire unclaimed jobs at `MaxJobAge`.
- `pkg/urth/service.go:873-886`: running/terminal Results are rejected at claim,
  but expired running state is never transitioned.
- `docs/adr/0004-nats-communication-backbone.md:100-108,373-384`: required
  reconciliation cases and failure behavior.

## Required Outcome

A horizontally safe reconciler periodically detects and handles:

- unpublished or abandoned outbox rows by returning them to relay ownership;
- pending Results whose dispatch is absent, expired, or administratively removed;
- running Results whose execution lease has expired;
- terminal Results with stale live dispatches; and
- Runner consumers missing or inconsistent with current Runner resources.

Expired running Results become `JobExpired` with immutable attempt history. The
reconciler never reopens that Result. If retry policy exists, it creates a new
pending Result and outbox entry; until retry policy is implemented, expiry is
recorded without an automatic retry.

## Implementation Constraints

- Postgres lifecycle state remains authoritative; broker inspection is evidence,
  not permission to rewrite history without version guards.
- Multiple reconcilers may run. Claim work with database locks/leases and use
  optimistic versions on every Result transition.
- Result expiry and any retry Result/outbox creation must be atomic.
- Reconciliation is safe to repeat and safe after partial failure.
- Do not infer a missing message solely from consumer delivery counts while a
  relay may still own an unpublished outbox row.
- Expose last-success time, scan age, repaired counts, failures, and oldest
  inconsistent Result as metrics/logging.

## Suggested Implementation Sequence

1. Define reconciliation state queries and outcomes in `pkg/urth` with store tests.
2. Implement expired-running transition first; it is independent of JetStream lookup.
3. Add outbox-aware pending-dispatch reconciliation.
4. Add terminal-message cleanup and Runner consumer reconciliation.
5. Add one composition-owned periodic loop with leaderless row claiming.
6. Exercise each crash boundary and document operational controls.

## Non-Goals

- Choosing retry/backoff policy for Scenarios beyond creating a new Result when
  an existing explicit policy requests it.
- Dead-letter presentation and operator retry controls (task 012).
- Capacity-aware Runner placement (task 014).

## Acceptance Criteria / Definition of Done

- [x] Expired execution leases leave no Result indefinitely `running`.
- [x] Missing/expired dispatches leave no Result indefinitely `pending`.
- [x] Terminal Results do not retain live work-queue messages indefinitely.
- [x] Missing Runner consumers are recreated by the control plane, never Workers.
- [x] Repeated and concurrent reconciliation is idempotent and version-safe.
- [x] Repair activity and failures are observable.

## Required Tests

- ACKed claim followed by Worker death: lease expiry marks that Result expired.
- Job expires at `MaxJobAge`: pending Result is explicitly expired or republished
  according to the documented policy.
- Reconciler crashes after state transition but before cleanup: next scan converges.
- Two reconcilers race the same Result: only one versioned transition succeeds.
- Deleted consumer for an active Runner is restored without Worker admin rights.

## Validation

```sh
go test -race -count=1 ./pkg/urth ./pkg/natsq ./cmd/api-server
go test -race -count=1 ./...
go vet ./...
git diff --check
```

## Completion Record

- **Implemented:**
  - `pkg/urth/reconcile.go`: `Reconciler`, its five passes, `ReconcileReport` /
    `ReconcileStatus`, the `ReconcileStore` and `RunnerChannelReconciler`
    interfaces, `ReconcileLease`, and `JobStatus.IsTerminal`.
  - `pkg/urth/reconcile_store.go`: the gorm/dbstore-backed store. The scan lease
    is a conditional `UPDATE` plus an `ON CONFLICT DO NOTHING` insert, so a
    takeover is one atomic statement rather than a read followed by a write.
    `ExpireRun` runs the version-guarded transition and the retirement of the
    dispatch in one transaction, rebuilding `dbstore` over the transaction handle
    so the transition keeps one definition.
  - `pkg/urth/outbox.go` / `outbox_store.go` / `outbox_relay.go`:
    `DispatchPublisher` now returns a `DispatchReceipt`, and the entry carries
    `PublishedSeq`, `RetiredAt` and `RetiredReason`. A published dispatch that
    cannot be addressed cannot be withdrawn, so a terminal Result's queued
    message could otherwise only be waited out. Retired rows leave both the
    relay's claim query and `Stats`.
  - `pkg/natsq/reconcile.go`: `EnsureRunnerChannel` binds before creating, so
    "restored" means something; `DropDispatch` deletes the exact stream sequence
    rather than purging a subject that carries every other job for that Runner.
  - `cmd/api-server/main.go`: the reconciler runs in every replica by default,
    alongside the relay, with `--reconcile-*` flags and a pending timeout derived
    from `--nats.max-job-age` rather than configured independently.

- **Policy recorded:** a pending Result whose dispatch was published and has
  outlived the transport's own job expiry is **explicitly expired**, not
  republished. There is no retry policy yet, and ADR 0004 requires a retry to
  create a new Result rather than reopen one; republishing would have been the
  latter in disguise. A pending Result whose outbox entry is *missing* is
  re-enqueued, because the Result is authoritative and still wants to run.

- **Tests added/updated:** `pkg/urth/reconcile_test.go` (16 tests, Postgres) —
  expiry after a real registration/claim handshake, no reopening, a terminal
  Result refused outright rather than merely losing the version race, lost dispatch
  expired, unpublished dispatch left to the relay, missing dispatch re-enqueued
  idempotently, convergence after a crash between transition and cleanup,
  delivered dispatch retired without touching the broker, retired rows invisible
  to the relay, abandoned lease release, four concurrent reconcilers producing
  exactly one versioned transition, scan-lease exclusion and takeover, skipped
  scans reported as skipped, channel restore limited to active Runners, a failed
  pass not cancelling the others, and reconciler health.
  `pkg/natsq/reconcile_test.go` (3 tests, embedded JetStream) — a deleted
  consumer restored by the control plane, withdrawal removing only the stale
  message, and withdrawal of an already-gone message staying quiet.
  Existing outbox tests and doubles updated for the receipt.

- **Documentation updated:** `cmd/api-server/README.md` gains a reconciler
  section (passes, guarantees, flags, what to alert on); `TODO.md` notes the
  reconciler as landed.

- **Validation evidence:**
  - `make audit/postgres` — pass (`go vet`, staticcheck `-checks=all`,
    `go test -race` incl. the Postgres-backed tests): 225 passed, 19 packages.
  - `git diff --check` — clean.
  - Verified against a live stack (Postgres + JetStream + api-server with
    `--nats.max-job-age=5s --pending-dispatch-grace=1s --reconcile-interval=3s`).
    The reconciler restored the Runner's missing consumer, the relay published,
    and six seconds later the pending run was expired, its outbox row retired
    with `queued message withdrawn`, and the JetStream message deleted. Result
    row: `status_status=timeout`, `status_result=timeout`, `time_ended` set,
    `urth/result.state=timeout`.
  - Guards confirmed load-bearing by breaking them first: disabling the
    unpublished-dispatch check fails
    `TestReconcilerLeavesUnpublishedDispatchesToTheRelay`, and disabling the
    expired-lease query fails the expiry tests.

- **Follow-ups:**
  - The legacy asynq publisher returns an empty receipt, so a stale asynq task
    cannot be withdrawn. Task 015 removes that path rather than extending it.
  - `Reconciler.Status()` is in-process only. Putting scan age and repair counts
    on both operator surfaces is task 016.
  - Retired rows are the substrate the dead-letter workflow (task 012) needs;
    presentation and operator retry are still that task's.
