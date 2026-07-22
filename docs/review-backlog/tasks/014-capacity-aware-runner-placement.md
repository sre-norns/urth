# 014: Make Runner Placement Capacity-Aware

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
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

## Completion Record

- **Implemented:**
- **Tests added/updated:**
- **Documentation updated:**
- **Validation evidence:**
- **Follow-ups:**
