# 012: Implement an Operational Dead-Letter Workflow

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `blocked` |
| Priority | `P1` |
| Workstream | Durability / Claim lifecycle |
| Depends on | 003 |
| Likely conflicts | 002, 003, 013 |
| Owner | Unclaimed |

## Why This Matters

The consumer config stops redelivery after `MaxDeliver`, and Workers call `Term`
for malformed or permanently refused messages. Those actions currently produce
only process logs. No authoritative resource records why a Result stopped making
progress, no alert identifies the affected Runner/dispatch, and no operator can
inspect or deliberately retry it.

JetStream does not automatically move a WorkQueue message to a useful dead-letter
queue merely because delivery count was exhausted.

## Evidence

- `pkg/natsq/assets.go:62-67`: consumer config sets `MaxDeliver` without a failure path.
- `cmd/nats-worker/consume.go:112-132`: malformed/misrouted messages are terminated.
- `cmd/nats-worker/consume.go:153-158`: permanent claim refusal terminates and logs.
- `docs/adr/0004-nats-communication-backbone.md:178-187,311-328`: poison and
  maximum-delivery failures must enter an operational workflow and affect Result state.

## Required Outcome

- Poison, permanent-policy, misroute, and max-delivery exhaustion create an
  authoritative Postgres dispatch-failure record linked to Result, Runner,
  dispatch ID, reason category, delivery metadata, timestamps, and redacted detail.
- The affected pending Result transitions version-safely to an explicit terminal
  error/expiry state unless it is already running or terminal.
- Control-plane monitoring consumes JetStream max-delivery advisories; Workers
  report Term reasons through an authenticated API before terminating.
- Operators can list/get failures through the resource API, CLI, and UI and can
  request a retry. Retry creates a new Result/outbox entry; it never reopens the
  failed historical Result.
- Metrics/alerts expose failure rate, oldest unresolved failure, and reason counts.

## Implementation Constraints

- Do not put full probe definitions, credentials, or unredacted payloads into
  advisory/log/dead-letter records.
- Failure recording is idempotent by dispatch ID and failure category.
- If failure reporting itself is unavailable, leave the message/reconciliation
  evidence recoverable rather than silently ACKing it away.
- Worker NATS permissions remain narrow; Workers do not publish jobs or administer
  a DLQ stream. Prefer authenticated API reporting plus control-plane advisories.
- Reconciliation owns convergence when Result and message failure state diverge.

## Suggested Implementation Sequence

1. Define dispatch-failure resource, categories, idempotency, and API surface.
2. Add authenticated Worker failure reporting for explicit Term cases.
3. Subscribe a control-plane component to max-delivery advisories.
4. Implement versioned Result transition and reconciler integration.
5. Add operator list/get/retry in CLI and UI plus metrics/alerts.
6. Exercise unavailable-reporting and duplicate-advisory failure cases.

## Non-Goals

- Automatically retrying arbitrary poison messages indefinitely.
- Storing full raw NATS payloads as Artifacts.
- General incident-management integration or paging provider selection.

## Acceptance Criteria / Definition of Done

- [ ] Every Term and max-delivery outcome is visible as an authoritative resource.
- [ ] Affected pending Results do not remain pending indefinitely.
- [ ] Duplicate reports/advisories do not duplicate failure history.
- [ ] Operator retry creates a new Result with traceable relation to the failure.
- [ ] Failure details are redacted and metrics/alerts are available.
- [ ] Reporting outages converge through reconciliation rather than losing evidence.

## Required Tests

- Malformed envelope, misroute, policy refusal, and max-delivery each record the
  correct category and terminal Result outcome.
- Duplicate Worker report/advisory creates one failure record.
- Failure arrives after Result claimed/terminal: history is recorded without
  corrupting current state.
- Retry produces a new Result and dispatch; original remains immutable.
- API unavailable during Term path: reconciler/advisory later records the failure.

## Validation

```sh
go test -race -count=1 ./pkg/urth ./pkg/natsq ./cmd/api-server ./cmd/nats-worker ./cmd/urthctl
go test -race -count=1 ./...
go vet ./...
git diff --check
(cd website && npm test)
```

## Completion Record

- **Implemented:**
- **Tests added/updated:**
- **Documentation updated:**
- **Validation evidence:**
- **Follow-ups:**
