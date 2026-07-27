# 016: Surface Runner Queue State to Operators

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `blocked` |
| Priority | `P1` |
| Workstream | Operability |
| Depends on | 012, 013 |
| Likely conflicts | 012, 013, 014 |
| Owner | Unclaimed |

## Why This Matters

A Runner is the unit an operator reasons about: "team-a's probes stopped running"
is a question about one Runner's pipeline. That pipeline now has several stages
that can independently stall — a Result committed but not relayed, a job queued
but unclaimed, a job delivered repeatedly and never acknowledged, a dispatch
dead-lettered — and none of them are visible anywhere an operator looks.

Today the only way to answer "why is nothing running on this Runner" is to open
`psql` and the NATS monitoring port. `RunnerDetail.jsx` shows the Runner's workers
and nothing about its work; `urthctl get runner` prints the resource. The
diagnostic information exists — [task 002](002-transactional-dispatch-outbox.md)
records outbox backlog, attempts, and last error per entry;
[task 012](012-dead-letter-workflow.md) makes dispatch failures authoritative
resources; [task 013](013-bound-and-observe-jetstream.md) exposes stream and
consumer state — but each lands in its own subsystem, and nothing assembles them
per Runner.

Assembling them is the point. The stages are only meaningful next to each other:
a deep queue with healthy workers means saturation, a deep queue with no workers
means a Runner nobody is serving, and an empty queue with a growing outbox means
the relay, not the Runner, is the problem. Read one at a time, all three look the
same.

## Evidence

- `website/src/pages/RunnerDetail.jsx:171-210`: the Runner page lists workers only.
- `cmd/urthctl/get.go:218-288`: `get runner`/`get runners` print resource fields
  with no pipeline state.
- `pkg/urth/outbox.go`: `DispatchOutboxStats` is process-wide, not per Runner, and
  is not reachable through the API.
- `cmd/api-server/README.md`: documents inspecting the backlog with hand-written
  SQL, which is the workaround this task removes.

## Required Outcome

- One authoritative read model per Runner covering every stage a dispatch passes
  through: unpublished outbox entries, queued-and-unclaimed jobs, in-flight
  claims, redelivering messages, and dead-lettered dispatches. Each stage reports
  at least a count and the age of its oldest member.
- Age is reported alongside every count. A count says how busy a Runner is; the
  age of the oldest stuck item says whether it is healthy, and only the second is
  worth alerting on.
- The model is served by the API server, which owns both Postgres and the
  JetStream handle. Neither the Web UI nor `urthctl` talks to NATS or the database.
- **Both operator surfaces present it**, per the operator-surface rule in
  `CONTEXT.md`: the Runner detail page gains a pipeline view, and `urthctl` gains
  an equivalent — a per-stage summary and the ability to list the items in a stage.
- Dead-lettered dispatches are inspectable from both surfaces and carry the reason
  category and affected Result recorded by task 012, so an operator can get from
  "this Runner is stuck" to the specific failed dispatch without leaving the tool.
- Stale or unavailable broker data is reported as unavailable, not as zero. A
  Runner whose queue depth could not be read must not render as an empty queue.

## Implementation Constraints

- The read model is derived, never authoritative: it is assembled per request from
  Postgres and JetStream and is not a resource operators can write.
- Keep `pkg/urth` free of `pkg/natsq`. Broker-derived stages arrive through an
  interface the domain owns, as `DispatchPublisher` and `WorkerTransportProvider`
  already do; a deployment on the legacy transport reports those stages as
  unavailable rather than failing.
- Per-Runner outbox statistics need an indexed query on `runner_uid`, not a scan
  of the whole outbox.
- Reading queue depth must not consume, acknowledge, or otherwise disturb messages.
- Do not put probe definitions, credentials, or raw payloads into the view; a
  dispatch identity, a reason, and a Result reference are enough to act on.
- The endpoint is operator-facing and must sit behind the same authorization as
  other Runner administration.

## Suggested Implementation Sequence

1. Define the per-Runner pipeline read model and its stage vocabulary.
2. Add the per-Runner outbox query and index.
3. Add the broker-side stage provider behind a domain-owned interface.
4. Expose the composed model on the Runner API, including the unavailable case.
5. Add `urthctl` commands for the summary and per-stage listings.
6. Add the Runner detail pipeline view and its tests.

## Non-Goals

- Defining dispatch-failure records or the retry action ([task 012](012-dead-letter-workflow.md)).
- Stream/consumer limits and control-plane metrics ([task 013](013-bound-and-observe-jetstream.md)).
- Capacity-aware placement decisions ([task 014](014-capacity-aware-runner-placement.md));
  this task shows the queue, it does not schedule against it.
- A general dashboard, alerting rules, or a metrics backend.

## Acceptance Criteria / Definition of Done

- [ ] Every dispatch stage is visible per Runner with a count and an oldest age.
- [ ] The Web UI and `urthctl` expose the same information; neither is a subset.
- [ ] Dead-lettered dispatches are inspectable and name their reason and Result.
- [ ] A stalled outbox, a queue with no workers, and a saturated Runner are
      distinguishable from one another at a glance.
- [ ] Broker unavailability renders as unavailable, never as zero.
- [ ] Per-Runner queries are indexed rather than scanning the outbox.

## Required Tests

- A Runner with an unpublished outbox entry, a queued job, and a dead-lettered
  dispatch reports all three stages with correct counts and ages.
- Broker unreachable: the response marks broker-derived stages unavailable and
  still reports the Postgres-derived ones.
- A Runner with no workers and a non-empty queue is distinguishable from a Runner
  with busy workers and an equally deep queue.
- Reading the view twice does not change queue contents or delivery counts.
- `urthctl` output covers the same stages as the UI, asserted against one fixture.
- UI test: pipeline view renders stalled, healthy, and unavailable states.

## Validation

```sh
go test -race -count=1 ./pkg/urth ./pkg/natsq ./cmd/api-server ./cmd/urthctl
make audit/postgres
git diff --check
(cd website && npm test)
```

## Completion Record

- **Implemented:**
- **Tests added/updated:**
- **Documentation updated:**
- **Validation evidence:**
- **Follow-ups:**
