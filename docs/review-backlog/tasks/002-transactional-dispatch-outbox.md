# 002: Add the Transactional Dispatch Outbox

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P0` |
| Workstream | Durability |
| Depends on | — |
| Likely conflicts | 003, 007, 011, 012 |
| Owner | Unclaimed |

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

- [ ] Result and dispatch outbox row are atomic under commit and rollback.
- [ ] A committed unpublished row is eventually published after NATS recovers.
- [ ] Relay crashes before and after publish do not lose or duplicate a Result.
- [ ] Multiple relays do not concurrently own the same row.
- [ ] Publication failure remains observable without changing Result execution state.
- [ ] Metrics and documentation expose stuck/old outbox rows.

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
- **Tests added/updated:**
- **Documentation updated:**
- **Validation evidence:**
- **Follow-ups:**
