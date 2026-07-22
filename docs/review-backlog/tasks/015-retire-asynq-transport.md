# 015: Drain Asynq and Retire the Legacy Job Model

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `blocked` |
| Priority | `P1` |
| Workstream | Migration |
| Depends on | 001–013 |
| Likely conflicts | all runtime tasks |
| Owner | Unclaimed |

## Why This Matters

The repository currently carries two incompatible execution paths. The Asynq
path publishes the complete job to one shared Redis queue and claims through a
legacy endpoint that trusts Worker/Runner IDs from the request body. Keeping it
after the NATS path is production-ready preserves an authentication bypass,
duplicates operational dependencies, and forces every domain change to account
for a transport that violates the accepted Runner model.

Removal must be deliberate: simply deleting Redis code can strand already queued
jobs during an upgrade.

## Evidence

- `cmd/api-server/main.go:473-483,525-550`: runtime transport switch retains Asynq.
- `cmd/api-server/main.go:194-207`: legacy claim route remains available.
- `pkg/redqueue/`: Redis/Asynq scheduler publishes the old job representation.
- `cmd/asynq-runner/`: legacy physical Worker implementation.
- `pkg/urth/job.go`: legacy `Job`/topic model remains for that transport.
- `docs/adr/0004-nats-communication-backbone.md:499-516`: migration completes
  only after correctness tests, drain/migration, and Redis removal.

## Required Outcome

- Publish an upgrade procedure that chooses one explicit strategy per deployment:
  drain Asynq fully before cutover, or enumerate/cancel and recreate pending jobs
  as new NATS Results with auditable linkage.
- API startup detects legacy pending/running state and refuses destructive cutover
  unless the operator explicitly resolves it.
- Remove `cmd/asynq-runner`, `pkg/redqueue`, Redis/Asynq dependencies/configuration,
  the transport switch, legacy unauthenticated registration/claim surfaces, and
  the complete-job queue representation.
- Retire `urth.Job` and `RunScenarioTopicName` when no non-legacy caller remains.
- Rename remaining components whose public names expose the old transport only
  when that improves the stable domain interface.
- README, examples, Make targets, deployment manifests, and architecture status
  describe NATS/JetStream as the one supported path.

## Implementation Constraints

- Begin only after tasks 001–013 establish the NATS path's security, durability,
  recovery, and operational failure handling. Task 014 is an optional quality
  improvement and does not block retirement.
- Do not translate an already-running execution into a new Result silently.
- A migrated pending job receives a new Result/dispatch when exact original
  execution snapshot or placement cannot be proven.
- Preserve Result history and record migration/cancellation reason as resources
  or authoritative labels/conditions.
- Removal is one coherent migration commit/series with no hidden fallback to an
  insecure endpoint.

## Suggested Implementation Sequence

1. Inventory every Asynq/Redis/job-model reference and document drain choices.
2. Add startup/preflight reporting for legacy queued and running state.
3. Exercise drain and recreate/cancel procedures on a copy of representative data.
4. Remove legacy HTTP endpoints and Worker/scheduler binaries.
5. Remove packages, dependencies, flags, Make targets, and old job types.
6. Update all documentation and run full integration/upgrade validation.

## Non-Goals

- Capacity-aware placement (task 014).
- Maintaining long-term dual-transport compatibility.
- Translating opaque Redis payloads when their authoritative Result/snapshot
  cannot be verified safely.

## Acceptance Criteria / Definition of Done

- [ ] Operators have a tested drain or explicit recreate/cancel upgrade path.
- [ ] Cutover cannot silently strand legacy pending/running work.
- [ ] No unauthenticated legacy claim/registration endpoint remains.
- [ ] Redis/Asynq code, dependencies, configuration, and docs are removed.
- [ ] `urth.Job`/legacy topic types have no remaining use and are removed.
- [ ] NATS end-to-end and upgrade tests pass from a representative pre-cutover state.

## Required Tests

- Drain path: old queue reaches zero, in-flight Results finish, NATS starts cleanly.
- Recreate/cancel path: every old pending Result has an explicit terminal or linked
  replacement state and no duplicate execution.
- Startup refuses cutover with unresolved legacy state unless operator chooses a path.
- Legacy endpoints return not found and old Worker cannot authenticate after upgrade.
- Dependency/build scan contains no Asynq/Redis runtime references.

## Validation

```sh
rg -n "asynq|redqueue|MessageBrokerURL|RunScenarioTopicName|urth\.Job" . \
  --glob '!docs/review-backlog/**' --glob '!docs/adr/**'
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
