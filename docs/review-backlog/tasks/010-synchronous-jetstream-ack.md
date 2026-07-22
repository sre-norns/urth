# 010: Synchronously Acknowledge Claimed Dispatches

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P1` |
| Workstream | Claim lifecycle |
| Depends on | — |
| Likely conflicts | 001, 003, 011 |
| Owner | Unclaimed |

## Why This Matters

The Worker comments that it acknowledges synchronously after a durable claim,
but calls `Msg.Ack()`. In the selected JetStream API that only publishes an ACK;
`Msg.DoubleAck(ctx)` waits for server confirmation. A connection loss after the
local publish can redeliver the message while the first execution is already running.

If redelivery reaches the same Worker identity, the idempotent claim returns
authorization and the process can execute the same external probe concurrently.
Exactly-once execution is not promised, but the accepted design explicitly uses
synchronous acknowledgement to reduce this avoidable duplicate window.

## Evidence

- `cmd/nats-worker/consume.go:161-175`: claimed message calls `msg.Ack()` and then executes.
- NATS Go `jetstream.Msg`: `DoubleAck(context.Context)` waits for server acknowledgement;
  `Ack()` does not.
- `pkg/urth/service.go:873-879`: the same Worker/dispatch receives a successful
  idempotent authorization again.
- `docs/adr/0004-nats-communication-backbone.md:149-176,189-205`: required ordering
  and acknowledged at-least-once limitation.

## Required Outcome

- After the API durably accepts a claim, the Worker confirms message removal using
  `DoubleAck` with a short bounded context before starting the probe.
- A transient ACK-confirmation failure is retried within the claim-handshake budget.
- If confirmation remains unknown, the Worker records the condition and still
  honors the already-committed Result ownership; it must not attempt a second claim
  for a different Worker or roll the Result back.
- Concurrent redelivery to the same process is deduplicated by Result/dispatch
  while its original execution is in flight.
- Documentation remains explicit that process/network failure can still cause
  repeated external probe effects; this change does not claim exactly once.

## Implementation Constraints

- ACK remains after successful authoritative claim and before long probe execution.
- Do not extend `AckWait` to cover execution.
- Keep an in-process ownership set bounded by configured concurrency and remove
  entries after reporting/abandonment.
- A Worker restart may lose that memory; Result lease/history remains the durable
  truth and probes must remain side-effect safe.
- Metrics distinguish claim success, ACK confirmation latency/failure, duplicate
  in-flight delivery, and ordinary stale delivery.

## Suggested Implementation Sequence

1. Add a message-adapter unit test proving `DoubleAck` is invoked after claim.
2. Add an embedded-NATS test that interrupts/delays ACK confirmation.
3. Replace `Ack` with bounded `DoubleAck` retry logic.
4. Add in-process Result/dispatch deduplication for concurrent redelivery.
5. Add metrics and update Worker/ADR implementation notes.

## Non-Goals

- Exactly-once external probe execution.
- Lease reconciliation after Worker death (task 003).
- General claim outcome mapping (task 001).

## Acceptance Criteria / Definition of Done

- [ ] The accepted-claim path uses server-confirmed acknowledgement.
- [ ] ACK confirmation is bounded and observable.
- [ ] A concurrent duplicate delivery does not start a second local execution.
- [ ] ACK still occurs before probe execution and only after claim commit.
- [ ] Documentation does not overstate delivery guarantees.

## Required Tests

- Assert event order: claim commits, DoubleAck confirms, execution starts.
- Claim fails: no ACK/DoubleAck and no execution.
- ACK reply is delayed/lost: bounded retries and metric/log signal.
- Duplicate delivery during active execution: one probe invocation.
- Worker dies after confirmed ACK: task 003's lease path remains responsible.

## Validation

```sh
go test -race -count=1 ./cmd/nats-worker ./pkg/natsq ./pkg/urth
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
