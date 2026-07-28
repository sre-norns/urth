# 018: Fail Unplaceable Runs Instead of Queueing Them Forever

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `done` |
| Priority | `P1` |
| Workstream | Runner contract |
| Depends on | 002 (done), 003 (done), 012 (done) |
| Likely conflicts | 014, 016 |
| Owner | Ivan Ryabov |

## Why This Matters

A run of a Scenario whose requirements matched no active Runner was created
`pending`, given an outbox row no routing transport would accept, and left there
permanently. Observed on a live stack: three manual runs of
`basic-rest-self-prober-http` sat `pending` for seventeen minutes and would have
sat there indefinitely, while the api-server logged one line per run per hour.

Nothing in the system could repair it. `placeRun` reported "not placed" and the
creation path ignored it; the relay mapped the transport's refusal to a one-hour
backoff and retried forever; and the reconciler leaves an unpublished outbox row
alone by design, because inferring a lost dispatch there would expire runs the
relay is about to deliver.

The second half is that nothing told an operator beforehand. The Web UI offered
"Run now" for a scenario that could only produce stuck runs, and the only way to
discover a fleet had been decommissioned was to trigger one and watch it hang.

## Evidence

- `pkg/urth/service.go`: `resultsAPIImpl.Create` ignored `placeRun`'s
  not-placed report; `shouldDispatch` wrote an outbox row regardless.
- `pkg/natsq/publisher.go`: refuses an entry with no `RunnerUID`, wrapped in
  `urth.ErrPermanentDispatch`.
- `pkg/urth/outbox_relay.go`: `PermanentDispatchBackoff` held such an entry for
  an hour and retried it; its own comment deferred the fix to task 012.
- `pkg/urth/reconcile.go`: `reconcilePendingDispatches` skips a pending run whose
  entry is unpublished and unretired.
- `website/src/pages/ScenarioDetail.jsx`: gated "Run now" on `spec.active` and a
  prob body only.

## Required Outcome

1. A run that cannot be placed is terminal at creation: `errored`,
   `urth/result.unschedulable=no-eligible-runner`, an end time, and **no** outbox
   row. An unparseable selector produces the same with
   `invalid-requirements`, and the Scenario API refuses such a selector on write.
2. `POST /scenarios/{id}/results` returns 201 with that terminal run. A future
   scheduler has no caller to hand a refusal to, and the record that a run was
   wanted is the point.
3. Unplaceable runs are **not** dead letters. A selector matching nothing is an
   ordinary operational state; once a Scenario is scheduled per minute, one
   record per tick would bury the failures that need a human.
4. The relay settles what it can never publish, through a
   `PermanentDispatchSink`: an unplaced dispatch strands its run with
   `no-eligible-runner` and files nothing; every other permanent failure becomes
   an `undeliverable-dispatch` dead letter. The outbox row is left to the
   reconciler's stale-dispatch sweep.
5. `GET /scenarios/{id}/placement` reports matching and eligible Runners,
   registered and ready Workers, whether a run would be schedulable, and the
   reason it would not be.
6. The Web UI disables "Run now" when nothing is eligible and names the
   requirement that matched nothing; shows capacity when it is; warns, without
   refusing, when eligible Runners have no Workers connected; and explains a
   terminal run that never executed. The same gate applies to the run control on
   the scenario *list*, which is the other place a run can be triggered.

## Implementation Constraints

- Eligible Runners with no Workers still queue the dispatch
  ([ADR 0003](../../adr/0003-runner-worker-model.md) durable channels, task 014).
  "No workers" is never a refusal.
- Placement and the preview share one implementation, so a UI cannot offer a run
  the server then refuses.
- The requirement string is not label grammar and never becomes a label value:
  only the slug is written to `urth/result.unschedulable`.
- `pkg/urth` gains no transport dependency: `ErrDispatchUnplaced` is the domain's
  sentinel and `natsq.ErrNoRunner` aliases it.
- Selection among eligible Runners is unchanged (task 014 owns that).

## Non-Goals

- Capacity-aware selection among eligible Runners (014).
- Per-Runner pipeline visibility (016).
- Automatically re-placing a stranded run when a Runner appears; a retry is a new
  `Result`, by ADR 0004.
- A cron scheduler, or a policy for a Scenario that is unplaceable for a long
  time.

## Acceptance Criteria / Definition of Done

- [x] An unplaceable run is terminal and writes no outbox row.
- [x] An unparseable selector is refused on scenario write and terminal on run.
- [x] The relay stops retrying what can never be published.
- [x] Unplaceable runs file no dead letter; other permanent failures do.
- [x] The placement preview is served and consumed by the Web UI.
- [x] Regression tests cover success and failure behaviour.
- [x] Operator documentation matches the behaviour.

## Required Tests

- `pkg/urth/service_placement_test.go`: no matching Runner, matching but
  disabled, unparseable requirements, the placed regression case, scenario
  validation, and the four preview cases.
- `pkg/urth/outbox_backstop_test.go`: unplaced dispatch strands without a
  record; another permanent failure dead-letters and strands; recording is
  idempotent across two scans.
- `website/src/pages/ScenarioDetail.test.jsx`: button disabled with the named
  requirement, all-disabled runners phrased differently, no-workers warning that
  still offers the run, capacity tile, and the still-loading case.
- `website/src/pages/RunDetail.test.jsx`: a run that was never scheduled explains
  itself.
- `website/src/containers/Scenario.test.jsx`: the list row's run control is
  disabled with the same hint, still offered when only workers are missing, and
  offered while the preview is loading.

## Validation

```sh
make audit/postgres
cd website && npm test
```

Plus a live stack: the already-stuck runs go terminal on the relay's next pass,
a new trigger is terminal immediately with no dead letter, and the same scenario
runs normally once a matching Runner is applied.

## Completion Record

- **Implemented:** `pkg/urth/placement.go` (new), `pkg/urth/undeliverable.go`
  (new), `pkg/urth/service.go`, `pkg/urth/outbox_relay.go`,
  `pkg/urth/dispatch_failure{,_store}.go`, `pkg/urth/labels.go`,
  `pkg/urth/outbox.go`, `pkg/urth/client.go`, `pkg/natsq/scheduler.go`,
  `pkg/controllers/dispatch.go`, `cmd/api-server/main.go`, and the Web UI
  (`utils/placement.js`, `actions/fetchScenarioPlacement.js`,
  `reducers/scenarioPlacement.js`, `ScenarioDetail.jsx`, `RunDetail.jsx`,
  `containers/Scenario.jsx`).
- **Tests added/updated:** `pkg/urth/service_placement_test.go`,
  `pkg/urth/outbox_backstop_test.go`, `website/src/pages/ScenarioDetail.test.jsx`,
  `website/src/pages/RunDetail.test.jsx`,
  `website/src/containers/Scenario.test.jsx`.
- **Documentation updated:** `cmd/api-server/README.md`, `CLAUDE.md`, `TODO.md`.
- **Validation evidence:** `make audit/postgres` exit 0; `npm test` 178 passing;
  live stack reproduction of the original three stuck runs.
- **Follow-ups:** a scheduler that stops creating runs for a Scenario that has
  been unplaceable for a while (there is no cron loop yet, so every unplaceable
  run today is one somebody asked for).
