# 013: Bound and Observe JetStream Assets

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `in-progress` |
| Priority | `P1` |
| Workstream | Durability |
| Depends on | — |
| Likely conflicts | 004, 012 |
| Owner | Ivan Ryabov (`feat/bound-jetstream-assets`) |

## Why This Matters

The jobs stream has a per-subject message count and maximum age, but no global
byte/message limit or maximum message size. Consumer configuration omits
`MaxAckPending`, and no application metrics expose stream capacity, Runner lag,
redeliveries, or configuration drift. Enough Runner subjects or publication rate
can therefore consume unbounded disk within the age window, while operators learn
about backlog only after publication or storage failure.

Urth also validates none of its own JetStream settings before handing them to the
broker, so a rejected combination surfaces as the broker's complaint about its own
field names rather than as a statement about the flags an operator set. Setting
`--nats.max-job-age=5s` without lowering `--nats.duplicate-window` from its `30m`
default fails startup with:

```text
failed to connect to NATS: failed to provision stream "URTH_JOBS": nats: API error:
code=500 err_code=10052 description=duplicates window can not be larger then max age
```

Nothing in that names `--nats.duplicate-window`, and the constraint is not
documented. It is a legitimate rejection reached the slow way: the flags are known
at parse time, so this should fail before a connection is attempted, naming both
flags and their values. The same applies to every other cross-field constraint on
these settings.

## Evidence

- `pkg/natsq/config.go:75-102`: only replicas, per-Runner messages, age, AckWait,
  and MaxDeliver are configurable.
- `pkg/natsq/assets.go:18-38`: stream lacks global `MaxMsgs`, `MaxBytes`,
  `MaxMsgSize`, and explicit duplicate window.
- `pkg/natsq/assets.go:53-67`: consumer lacks `MaxAckPending` and backoff/limits
  derived from Runner capacity.
- `pkg/natsq/scheduler.go:28-29`: counters exist only in memory and are not exposed.
- `pkg/natsq/config.go:96,101`: `DuplicateWindow` (`30m`) and `MaxJobAge` (`1h`)
  are independently defaulted and independently settable into a combination
  JetStream rejects. `Config` has no `Validate`, and nothing checks a cross-field
  constraint before `NewScheduler` dials the broker.
- `pkg/natsq/assets.go:37,41`: `MaxAge` and `Duplicates` are passed straight to
  `CreateOrUpdateStream`, which is the first thing that checks them.
- `docs/adr/0004-nats-communication-backbone.md:309-332`: accepted limits,
  persistence, replication, and observability requirements.

## Required Outcome

- `URTH_JOBS` has explicit global messages, bytes, per-subject messages, maximum
  message size, age, duplicate window, discard-new behavior, storage, and replicas.
- Each consumer has explicit AckWait, MaxDeliver, MaxAckPending, and any deliberate
  backoff/inactive settings aligned with Runner capacity and claim latency.
- Startup/reconciliation validates unsafe or unsupported combinations and reports
  configuration drift instead of silently accepting broker defaults.
- **Invalid configuration fails before the broker is contacted.** Every constraint
  Urth can check from its own flags — `DuplicateWindow` not exceeding `MaxJobAge`
  being the known one — is checked at startup, and the process exits rather than
  connecting. A setting that is wrong at parse time should not need a reachable
  NATS server to be discovered, because "is my configuration valid" and "is my
  broker up" are different questions and today they share one error.
- Validation messages name the Urth flag and the value the operator supplied, not
  the broker's internal field. `duplicates window can not be larger then max age`
  does not tell anyone which flag to change.
- Cross-field constraints are documented next to the flags that carry them, so the
  relationship is discoverable before it is violated.
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
- Config validation belongs to the config type — a `Validate` method on
  `natsq.Config`, which kong calls during parsing — rather than to the code that
  provisions assets. Every command that embeds the config then inherits the check
  instead of the api-server being the only place it happens.
- Report every violated constraint in one pass rather than failing on the first.
  An operator tuning several related durations should not rediscover the next
  constraint on the next start.
- `MaxAckPending` controls claim-handshake reservations, not probe execution.
- Avoid unbounded metric cardinality. Runner UID metrics require documented
  cardinality limits or aggregated/exported-on-demand strategy.
- Stream updates must be compatible and deliberate; reject changes JetStream
  cannot apply safely rather than deleting/recreating production data.

## Suggested Implementation Sequence

1. Add `natsq.Config.Validate` covering the cross-field constraints, starting with
   the duplicate-window/max-age one that is already reachable, and confirm it
   fails without a broker running.
2. Extend config and validation with safe development/production profiles.
3. Add stream/consumer config tests for every explicit field.
4. Reconcile asset drift without destructive recreation.
5. Export bounded-cardinality operational metrics.
6. Add alert/runbook documentation and capacity tests.

## Non-Goals

- Full performance/load characterization of maximum Runner count.
- Dead-letter Result workflow (task 012).
- A general NATS monitoring product or dashboard bundle.

## Acceptance Criteria / Definition of Done

- [ ] Stream storage is globally and per-Runner bounded without silent eviction.
- [ ] Consumer pending delivery is bounded to claim capacity.
- [ ] Unsafe/unlimited production configuration fails validation.
- [ ] Configuration Urth can reject from its own flags is rejected at startup,
      before a broker connection is attempted, naming the flags involved.
- [ ] Asset drift is detected and safely reconciled or reported.
- [ ] Required metrics and actionable alert guidance exist.
- [ ] One- and three-replica profiles are tested/documented accurately.

## Required Tests

- One Runner reaches per-subject limit without evicting another Runner's messages.
- Global byte/message limit rejects new publication and leaves outbox retryable.
- Oversized envelope is rejected visibly.
- Consumer cannot exceed configured MaxAckPending under concurrent fetches.
- Existing incompatible stream config produces actionable startup/reconcile error.
- `--nats.duplicate-window` greater than `--nats.max-job-age` is rejected by
  `Config.Validate` alone, with no NATS server reachable, and the message names
  both flags and their values. Confirm the test fails against today's code, which
  reaches the broker before noticing.
- A config violating several constraints reports all of them, not just the first.
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
