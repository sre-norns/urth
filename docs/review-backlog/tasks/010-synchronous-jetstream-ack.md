# 010: Synchronously Acknowledge Claimed Dispatches

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `done` |
| Priority | `P1` |
| Workstream | Claim lifecycle |
| Depends on | — |
| Likely conflicts | 001, 003, 011 |
| Owner | `feat/synchronous-jetstream-ack` |

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

The line numbers below are the `1e13334` baseline. Revalidated at implementation
time against a codebase that had since gained tasks 001, 002, 003 and 012:

- `cmd/nats-worker/consume.go:161-175` is now `applyDisposition`, the single place
  a claim outcome becomes an Ack/Nak/Term (task 001). The `claimAccepted` branch
  was still `msg.Ack()`.
- `pkg/urth/service.go:873-879` is now `ClaimRun` at `service.go:1002`; the
  idempotent re-authorization is the `entry.Status.Status == JobRunning` branch.
- Task 012 added the dead-letter path. An unconfirmed acknowledgement is
  deliberately **not** routed to it: the dispatch was delivered and claimed
  successfully, and filing a `DispatchFailure` would report a failure that did
  not happen.
- Task 003 added the reconciler, which is what makes dropping a duplicate message
  safe: a worker that dies after acknowledging has its lease expired and a retry
  creates a new `Result`.

Original anchors:

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

- [X] The accepted-claim path uses server-confirmed acknowledgement.
- [X] ACK confirmation is bounded and observable.
- [X] A concurrent duplicate delivery does not start a second local execution.
- [X] ACK still occurs before probe execution and only after claim commit.
- [X] Documentation does not overstate delivery guarantees.

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
  - `cmd/nats-worker/ack.go` (new): `confirmAck` — bounded, retried `DoubleAck`
    with a per-attempt timeout and a pause between attempts; `handshakeBudget`,
    which splits the bound consumer's `AckWait` into a claim share and an
    acknowledgement reserve such that `claim + ack ≤ AckWait` for every input.
    `jetstream.ErrMsgAlreadyAckd` counts as confirmed rather than being retried;
    `ErrMsgNoReply` / `ErrMsgNotBound` fail immediately as unretryable.
  - `cmd/nats-worker/inflight.go` (new): the in-process ownership set, acquired
    **before** the claim so two deliveries whose claims are in flight at once are
    deduplicated, and bounded by `--concurrency` through the consume semaphore.
  - `cmd/nats-worker/consume.go`: `applyDisposition` takes a context and an
    `ackConfirmer`; the accepted branch confirms the acknowledgement on a context
    detached from shutdown and executes regardless of the result, because the
    claim has already committed. `handle` drops a duplicate delivery with a plain
    `Ack`. `claim` is bounded by `w.claimBudget` rather than a hardcoded 30s,
    which previously equalled the whole default `AckWait`. `budgetHandshake`
    reads `consumer.CachedInfo().Config.AckWait` and logs the split.
  - `cmd/nats-worker/metrics.go` (new) and `--metrics-address`: an opt-in
    Prometheus endpoint, off by default because the worker runs inside the
    segment it probes. Exports claim outcomes, ack-confirmation latency, retries,
    unconfirmed acknowledgements, deduplicated redeliveries, and run outcomes.
  - Deliberately unchanged: the stale (409) path keeps its plain `Ack`; a claim
    is never re-attempted or rolled back after an unconfirmed acknowledgement.
- **Tests added/updated:**
  - `consume_test.go`: `fakeMsg` now counts `Ack` and `DoubleAck` separately —
    it previously collapsed both into one flag, so no test could tell them apart.
    `TestApplyDisposition` gained a confirmed-ack column;
    `TestHandleAcknowledgesBeforeExecuting` asserts the `claim → double-ack →
    execute` order as a recorded sequence; `TestUnconfirmedAckStillExecutes`,
    `TestAckConfirmationIsDetachedFromShutdown`,
    `TestHandleDoesNotAckOrExecuteAFailedClaim`, and
    `TestHandleDeduplicatesConcurrentRedelivery` cover the rest.
  - `ack_test.go` (new): budget invariant table; retry bounding; the
    already-acknowledged short circuit; and two embedded-NATS tests — a confirmed
    acknowledgement leaves nothing redeliverable past `AckWait`, and confirmation
    against a stopped broker fails inside its budget rather than hanging.
  - `metrics_test.go` (new): claim outcomes, duplicate deliveries and
    unconfirmed acknowledgements are counted; every counter is nil-safe, which is
    the shipped default.
  - Each new assertion was checked against the old behaviour first: reverting to
    `msg.Ack()` fails the three ordering/confirmation tests, and disabling the
    ownership set fails the dedup test with `probe invocations = 2, want 1`.
- **Documentation updated:** `cmd/nats-worker/README.md` (new "claim handshake
  budget" and "metrics" sections, corrected ack claim); ADR 0004 §4 implementation
  note on `DoubleAck` and the ownership set, §5 extended to name the two metrics
  that should be zero; `TODO.md`.
- **Validation evidence:**
  - `make audit` — green (vet, staticcheck `-checks=all`, race tests).
  - `go test -race -count=1 ./cmd/nats-worker ./pkg/natsq ./pkg/urth` — pass.
  - `make audit/postgres` — green.
  - Live stack (Postgres + NATS + api-server `--transport=nats` + worker with
    `--metrics-address=:9101`): the worker logged
    `claim handshake budget: 25s to claim, 5s to confirm the acknowledgement
    (ack-wait 30s)`, a triggered run completed, and `/metrics` reported
    `claims_total{outcome="accepted"} 1`, `ack_confirm_seconds_count 1`
    (~1 ms), `runs_total{result="failed"} 1`, with `ack_unconfirmed_total`,
    `ack_confirm_retries_total` and `duplicate_deliveries_total` all `0`.
- **Follow-ups:**
  - The worker's `/metrics` is a scrape target inside a segmented network, where
    a central Prometheus often cannot reach it. Whether these numbers should also
    reach the control plane belongs with
    [task 016](016-runner-queue-operator-visibility.md), which owns operator
    visibility, rather than here.
  - A live redelivery-during-execution test — killing the broker link between the
    claim and the acknowledgement — is [task 011](011-nats-worker-failure-integration-tests.md)'s
    crash-boundary work. This task proves the behaviour at the unit boundary and
    proves confirmation itself against an embedded broker.
