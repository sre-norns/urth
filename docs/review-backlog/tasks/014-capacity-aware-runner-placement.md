# 014: Make Runner Placement Capacity-Aware

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `done` |
| Priority | `P2` |
| Workstream | Runner contract |
| Depends on | — |
| Likely conflicts | 008 |
| Owner | Unclaimed |

## Why This Matters

After filtering eligible active Runners, placement sorts by UID and always picks
the first. Repeated runs for the same broad selector therefore concentrate on one
Runner even when another has idle Workers, while an offline-but-active Runner may
accumulate the entire queue.

Correct Runner-level scheduling is already present; this task improves the choice
among equally valid channels without binding work to a physical Worker.

## Evidence

- `pkg/urth/service.go:477-534`: placement lists eligible Runners and chooses the
  first sorted UID.
- `pkg/natsq/assets.go`: per-Runner consumers expose backlog/ack state through
  JetStream consumer info but placement does not use it.
- `docs/adr/0003-runner-worker-model.md:103-131`: scheduler may use current
  capacity but always binds the Result to a Runner, not a Worker.

## Required Outcome

Among Runners that pass Scenario placement and Runner job admission, select the
lowest-pressure channel using a documented score:

1. exclude disabled or administratively unavailable Runners;
2. prefer Runners with current admitted, unpaused Worker capacity;
3. compare `(pending + ack-pending) / effective worker concurrency` when capacity
   is known;
4. use backlog alone when capacity is unknown; and
5. break equal scores deterministically by Runner UID.

If every eligible Runner is offline, preserve durable-channel semantics: choose
the lowest-backlog eligible Runner and queue the job unless Scenario policy
explicitly requires online capacity. Never schedule directly to a WorkerInstance.

## Implementation Constraints

- Keep placement behind a transport-neutral capacity provider interface owned by
  `pkg/urth`; NATS-specific consumer inspection remains in `pkg/natsq`.
- Capacity is advisory and may change after selection. Database/result uniqueness
  and Runner claim authorization remain authoritative.
- Bound capacity-query latency and define behavior when NATS metrics are unavailable.
- Do not use Worker self-reported claim-body labels as capacity or eligibility.
- Preserve deterministic decisions for equal/unknown scores and make the selected
  score observable in logs/metrics.

## Suggested Implementation Sequence

1. Define Runner pressure/capacity snapshot and provider interface.
2. Add pure placement-score tests covering unknown/offline/tie behavior.
3. Implement JetStream consumer and WorkerInstance capacity adapter.
4. Inject it at composition and add bounded failure fallback.
5. Add placement metrics and operator documentation.

## Non-Goals

- Scheduling one Result to multiple vantage points automatically.
- Direct Worker scheduling or reservation.
- Predictive duration/cost scheduling or global fairness between tenants.
- Implementing the cron scheduling loop.

## Acceptance Criteria / Definition of Done

- [ ] Eligible load is distributed by documented pressure rather than UID alone.
- [ ] Offline channels do not win while equivalent live capacity exists.
- [ ] All-offline channels still retain durable queue semantics.
- [ ] Capacity-provider failure has a deterministic bounded fallback.
- [ ] Result records only the selected Runner until claim.
- [ ] Selection reason/pressure is observable without high-cardinality noise.

## Required Tests

- Two equivalent Runners with different backlog/capacity: lower pressure wins.
- First UID is offline, second live: live Runner wins.
- All offline: deterministic lowest-backlog/UID choice queues the Result.
- Capacity lookup timeout/error: deterministic fallback completes placement.
- Concurrent scheduling never records a Worker on pending Results.

## Validation

```sh
go test -race -count=1 ./pkg/urth ./pkg/natsq ./cmd/api-server
go test -race -count=1 ./...
go vet ./...
git diff --check
```

## Deviations from this task as written

Two, both because worker liveness landed between this task being written and
being done. Recorded here rather than quietly absorbed, since the reasoning is
what a reader will want if either is revisited.

**1. No JetStream inspection. Capacity comes entirely from Postgres.**

This task assumed placement would have to read consumer state, because when it
was written nothing else knew what a runner was carrying. Two things changed:

- placement records the chosen runner on a Result *before* it is persisted
  (`pkg/urth/service.go`, `Create`), so `results` already knows the queued and
  running work of every runner — one `GROUP BY` answers for the whole fleet;
- worker presence (`pkg/urth/presence.go`) says which workers are actually
  reachable, per worker, which is a better measure of usable concurrency than
  "how many registered" ever was.

So `(pending + ack-pending) / effective worker concurrency` is computed from the
database instead of the broker. The constraint this task set — "keep placement
behind a transport-neutral capacity provider interface owned by `pkg/urth`" — is
met more strongly than intended: there is no transport-specific path at all, and
therefore no broker round trip and no new failure mode on the run-creation path.
`RunnerChannelObserver`, which does read JetStream, remains for the runner page
and is not consulted by placement.

**2. Weighted random when every runner is saturated, not a deterministic
lowest-backlog pick.**

This task asks for determinism throughout. The saturated case is the one place
it is actively unhelpful: a deterministic choice sends an unbroken stream to one
runner until its counts move, which is the concentration the task exists to end.
The draw is weighted by online worker count, because a saturated channel drains
in proportion to the workers on it, so distributing new work by the same
proportion equalises expected wait across the fleet.

The deviation is confined to that regime. Whenever any runner has spare capacity
— the ordinary case — selection is fully deterministic and ties break by UID as
required. The draw goes through an injectable source (`placement.pick`) so tests
assert exact outcomes, and candidates are sorted by UID before drawing so a given
source is reproducible.

## Completion Record

- **Implemented:**
  - `pkg/urth/placement.go` — `RunnerCapacity` (online/registered/ready workers,
    queued/running runs; `Committed`, `Spare`, `Pressure`), `PlacementRegime`,
    per-runner `workerCapacity` graded by `WorkerPresenceAt`, and `selectRunner`
    holding both regimes. Single-candidate short-circuit skips capacity entirely.
  - `pkg/urth/placement_store.go` — `RunnerLoadStore` and its gorm
    implementation: one grouped query over unfinished runs per runner.
  - `pkg/urth/types.go` — composite index `idx_results_placement` on
    `(status_executor_runner_id, status_status)`; the query is on the
    run-creation path and `results` grows a row per scenario tick. Also corrects
    the `ExecutorRef` comment, which claimed the field was empty until a claim —
    the runner half has been set at creation since task 018.
  - `pkg/urth/metrics.go` — `PlacementMetrics`, counting decisions by regime.
  - `PlacementPreview` gains `onlineWorkers`, `queuedRuns`, `runningRuns` and
    `spareCapacity`. `readyWorkers` keeps its previous meaning rather than being
    narrowed to the stricter one, so no existing client's field changes under it.
- **Tests added/updated:**
  - `pkg/urth/placement_test.go` — ranking, pure and database-free: capacity over
    UID, pressure as tie-break, queued counted as committed, the weighted draw
    with a fixed source, both weight fallbacks, and unreachable workers
    contributing nothing.
  - `pkg/urth/placement_store_test.go` (Postgres) — successive runs spread across
    equivalent runners, a runner with no reachable workers loses despite sorting
    first, an entirely offline fleet still queues, no worker is recorded on a
    pending run, `InFlightRuns` counts only unfinished placed runs, and a
    capacity-store failure still places the run deterministically.
  - Each behavioural test was confirmed to fail against the previous
    lowest-UID selection before being kept. Runner UIDs are pinned in the
    Postgres tests: with generated ones, "capacity beat UID" passes about half
    the time by luck.
- **Documentation updated:** `cmd/api-server/README.md` ("Choosing a runner"),
  `CLAUDE.md`, this record.
- **Validation evidence:** `make audit` and `make audit/postgres` both exit 0.
  Note that `audit/postgres` must be pointed at a database no api-server is
  attached to — the default `store-url` is the development database, and a
  running relay competes with the tests for outbox rows.
- **Follow-ups:**
  - The new preview fields are not surfaced in the UI yet.
  - Per-scenario policy requiring online capacity (mentioned under Required
    Outcome) is not implemented: it needs a `ScenarioSpec` field and is a
    separate decision about whether a scenario may ever refuse to queue.
