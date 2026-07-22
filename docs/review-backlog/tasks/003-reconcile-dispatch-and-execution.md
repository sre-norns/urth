# 003: Reconcile Dispatch and Execution Lifecycle

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `blocked` |
| Priority | `P0` |
| Workstream | Durability / Claim lifecycle |
| Depends on | 002 |
| Likely conflicts | 001, 011, 012 |
| Owner | Unclaimed |

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

- [ ] Expired execution leases leave no Result indefinitely `running`.
- [ ] Missing/expired dispatches leave no Result indefinitely `pending`.
- [ ] Terminal Results do not retain live work-queue messages indefinitely.
- [ ] Missing Runner consumers are recreated by the control plane, never Workers.
- [ ] Repeated and concurrent reconciliation is idempotent and version-safe.
- [ ] Repair activity and failures are observable.

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
- **Tests added/updated:**
- **Documentation updated:**
- **Validation evidence:**
- **Follow-ups:**
