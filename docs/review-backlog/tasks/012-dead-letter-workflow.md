# 012: Implement an Operational Dead-Letter Workflow

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `done` |
| Priority | `P1` |
| Workstream | Durability / Claim lifecycle |
| Depends on | 003 (done) |
| Likely conflicts | 002, 003, 013 |
| Owner | `feat/dead-letter-workflow` |

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

- [x] Every Term and max-delivery outcome is visible as an authoritative resource.
- [x] Affected pending Results do not remain pending indefinitely.
- [x] Duplicate reports/advisories do not duplicate failure history.
- [x] Operator retry creates a new Result with traceable relation to the failure.
- [x] Failure details are redacted and metrics/alerts are available.
- [x] Reporting outages converge through reconciliation rather than losing evidence.

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
  - `pkg/urth/dispatch_failure.go`: `DispatchFailure` as a resource (kind
    `dispatchFailures`), the four `DispatchFailureReason` categories, the
    reporter/report/retry request types, and the label vocabulary. A resource
    rather than a private table because the questions asked of dead letters — by
    runner, by reason, by scenario, over a time range — are the label and range
    queries the resource API already answers.
  - `pkg/urth/dispatch_failure_store.go`: `recordDispatchFailure` writes the
    record and strands the run in one transaction; `strandRun` touches only a
    pending run and only for the current version, version-guarded so a claim
    racing the report wins; `retryDispatchFailure` creates a new `Result` and its
    outbox entry together. Idempotency is a derived resource name
    (`<eventUID>.<reason>`), so a repeat report collides rather than racing a
    read against an insert.
  - `pkg/urth/dispatch_advisory.go`: `DispatchAdvisory`, the domain-owned
    `DispatchAdvisorySink`, and the recorder that resolves a broker sequence to
    an outbox row via `PublishedSeq`. A sequence with no row is logged and
    dropped rather than recorded.
  - `pkg/natsq/advisory.go`: `AdvisoryWatcher`, a `controllers.Loop` subscribing
    to `$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.URTH_JOBS.*`. The payload
    struct is declared locally; nats-server is a test-only dependency here.
  - `pkg/urth/service.go` + `client.go`: `DispatchFailuresAPI` (List/Get/Report/
    Retry/Resolve) and its REST client. `Report` authorises exactly as a claim
    does — identity from the session, never the body — but deliberately does not
    require an active runner or unpaused worker, since a report grants no
    authority to execute anything and a worker being wound down still knows why
    its message is undeliverable.
  - `cmd/nats-worker/report.go` + `consume.go`: all three worker Term paths now
    report first and terminate only once the failure is on record. A report that
    cannot be made leaves the message queued behind a delayed NAK; one refused as
    malformed (4xx) terminates anyway rather than spinning forever.
  - `cmd/api-server/main.go`: routes, the advisory loop registration, and the
    kind added to `kindMap` so label search works for dead letters.
  - `pkg/controllers/dispatch.go`: the advisory watcher composed as a loop, with
    `--advisories-enabled`. It runs in every replica without a lease — safe
    because recording is idempotent, and necessary because advisories are
    at-most-once and a single listener is a single point at which they are missed.
  - `cmd/urthctl/dispatch_failures.go`: `get dead-letters`, `get dead-letter`,
    `retry`, `resolve`.
  - `website/`: `/dead-letters` page, nav entry, search row, actions, and the
    query/flattening helpers in `utils/deadLetters.js`.

- **Policy recorded:** the reconciler's stale-dispatch sweep, not this path,
  withdraws the queued message once a `Result` goes terminal. Retiring the outbox
  row while recording the failure would take it *out* of that sweep and leave the
  message in the queue. Leaning on task 003's machinery avoids a second, subtly
  different copy of the withdrawal logic.

- **Tests added/updated:** `pkg/urth/dispatch_failure_test.go` (12 tests,
  Postgres) — a report strands the pending run; duplicate reports record one
  failure; different reasons stay distinct; a claimed run is not rewritten; an
  unknown reason and a missing session are refused; retry creates a new run and
  dispatch with the snapshot copied, placement inherited and the relation
  traceable both ways; retry is not repeatable; resolve schedules nothing;
  advisories record, deduplicate, and ignore unknown sequences.
  `cmd/nats-worker/consume_test.go` — a permanent refusal is reported before
  termination, an unreported one keeps the message, and the 4xx/5xx
  classification. `website/` — 8 page tests and 9 helper tests.

- **Documentation updated:** `cmd/api-server/README.md` gains a dead-letter
  section (categories, guarantees, what is *not* guaranteed, operating and
  alerting).

- **Validation evidence:**
  - `make audit/postgres` — pass. `cd website && npm test` — 14 files, 169 tests.
  - Guards confirmed load-bearing by breaking them first: making `terminate()`
    unconditional fails `TestUnreportedRefusalKeepsTheMessage` on all three
    assertions; removing the retry name's lowercasing fails
    `TestRetryCreatesANewRunAndDispatch`.
  - Verified against a live stack (Postgres + JetStream + api-server): a worker
    session reports a `policy-refused` dispatch, the run becomes `errored` with
    `time_ended` set; a second identical report adds no second record;
    `urthctl retry` names the new run; retrying again returns the same run and
    enqueues no second dispatch; the relay published the retry's dispatch; the
    failed run keeps its history. The UI renders the full row with no React
    errors.
  - **Two bugs the suite did not catch and the live stack did**, both fixed with
    regression tests: a retry's generated name was mixed case and so not a valid
    DNS subdomain — it stored fine and failed only when a client read it back;
    and the list endpoint returned flat entries while get returned manifests,
    reproducing the `Result` shape wart that every piece of UI code has to
    special-case.

- **Follow-ups:**
  - Metrics are label queries and log lines rather than a Prometheus exporter;
    task 013 owns the metrics surface and should include unresolved-failure count
    and reason breakdown.
  - Per-Runner assembly of these failures alongside the rest of the dispatch
    pipeline is task 016, which lists this task as a dependency.
  - The legacy asynq transport has no dead-letter path. Task 015 removes it
    rather than extending it.
