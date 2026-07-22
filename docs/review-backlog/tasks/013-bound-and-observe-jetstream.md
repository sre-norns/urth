# 013: Bound and Observe JetStream Assets

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P1` |
| Workstream | Durability |
| Depends on | — |
| Likely conflicts | 004, 012 |
| Owner | Unclaimed |

## Why This Matters

The jobs stream has a per-subject message count and maximum age, but no global
byte/message limit or maximum message size. Consumer configuration omits
`MaxAckPending`, and no application metrics expose stream capacity, Runner lag,
redeliveries, or configuration drift. Enough Runner subjects or publication rate
can therefore consume unbounded disk within the age window, while operators learn
about backlog only after publication or storage failure.

## Evidence

- `pkg/natsq/config.go:75-102`: only replicas, per-Runner messages, age, AckWait,
  and MaxDeliver are configurable.
- `pkg/natsq/assets.go:18-38`: stream lacks global `MaxMsgs`, `MaxBytes`,
  `MaxMsgSize`, and explicit duplicate window.
- `pkg/natsq/assets.go:53-67`: consumer lacks `MaxAckPending` and backoff/limits
  derived from Runner capacity.
- `pkg/natsq/scheduler.go:28-29`: counters exist only in memory and are not exposed.
- `docs/adr/0004-nats-communication-backbone.md:309-332`: accepted limits,
  persistence, replication, and observability requirements.

## Required Outcome

- `URTH_JOBS` has explicit global messages, bytes, per-subject messages, maximum
  message size, age, duplicate window, discard-new behavior, storage, and replicas.
- Each consumer has explicit AckWait, MaxDeliver, MaxAckPending, and any deliberate
  backoff/inactive settings aligned with Runner capacity and claim latency.
- Startup/reconciliation validates unsafe or unsupported combinations and reports
  configuration drift instead of silently accepting broker defaults.
- Prometheus metrics expose stream bytes/messages/capacity, per-Runner pending and
  redelivery counts, oldest message, consumer ACK pending, outbox age, publish
  failures, and reconciliation/dead-letter signals.
- Production documentation specifies persistent volumes, three replicas, TLS/auth,
  alert thresholds, and safe local-development exceptions.

## Implementation Constraints

- Use `DiscardNew` and per-subject discard-new; never evict old unclaimed work to
  admit new work silently.
- Global limits must leave enough headroom for configured Runner count while still
  bounding storage. Validate zero/unlimited values explicitly.
- `MaxAckPending` controls claim-handshake reservations, not probe execution.
- Avoid unbounded metric cardinality. Runner UID metrics require documented
  cardinality limits or aggregated/exported-on-demand strategy.
- Stream updates must be compatible and deliberate; reject changes JetStream
  cannot apply safely rather than deleting/recreating production data.

## Suggested Implementation Sequence

1. Extend config and validation with safe development/production profiles.
2. Add stream/consumer config tests for every explicit field.
3. Reconcile asset drift without destructive recreation.
4. Export bounded-cardinality operational metrics.
5. Add alert/runbook documentation and capacity tests.

## Non-Goals

- Full performance/load characterization of maximum Runner count.
- Dead-letter Result workflow (task 012).
- A general NATS monitoring product or dashboard bundle.

## Acceptance Criteria / Definition of Done

- [ ] Stream storage is globally and per-Runner bounded without silent eviction.
- [ ] Consumer pending delivery is bounded to claim capacity.
- [ ] Unsafe/unlimited production configuration fails validation.
- [ ] Asset drift is detected and safely reconciled or reported.
- [ ] Required metrics and actionable alert guidance exist.
- [ ] One- and three-replica profiles are tested/documented accurately.

## Required Tests

- One Runner reaches per-subject limit without evicting another Runner's messages.
- Global byte/message limit rejects new publication and leaves outbox retryable.
- Oversized envelope is rejected visibly.
- Consumer cannot exceed configured MaxAckPending under concurrent fetches.
- Existing incompatible stream config produces actionable startup/reconcile error.
- Metrics reflect publish, ACK pending, redelivery, and capacity changes.

## Validation

```sh
go test -race -count=1 ./pkg/natsq ./cmd/api-server
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
