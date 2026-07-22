# 007: Snapshot Immutable Execution Input on Result

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P0` |
| Workstream | Runner contract / Claim lifecycle |
| Depends on | — |
| Likely conflicts | 002, 006, 008, 011 |
| Owner | Unclaimed |

## Why This Matters

A dispatch identifies a Result version, but the Result does not store the probe
definition it was created to execute. Claim authorization reloads the current
Scenario by UID. Editing a Scenario while an older Result waits in a queue changes
what that Result runs without changing its version or audit history.

This violates the promise that a Result is one immutable execution attempt and
that claim authorization returns the snapshot associated with it.

## Evidence

- `pkg/urth/types.go:128-140`: Result spec stores Scenario foreign key and prob
  kind, not the prob definition or Scenario versioned reference.
- `pkg/urth/service.go:581-600`: Result creation loads the then-current Scenario.
- `pkg/urth/service.go:971-983`: claim response reloads the current Scenario.
- `pkg/natsq/envelope.go:45-52`: Result version guards dispatch, while Scenario
  name is explicitly non-authoritative.
- `docs/adr/0004-nats-communication-backbone.md:60-81`: Postgres Result owns the
  immutable execution snapshot returned only after authorization.

## Required Outcome

- Result creation copies the complete executable `prob.Manifest`, effective
  timeout, Scenario UID/name/version, and any typed execution requirements needed
  for claim authorization into an immutable persisted snapshot.
- The snapshot and Result/outbox entry commit atomically once task 002 lands.
- Claim returns only the stored snapshot. Later Scenario edits, disablement, or
  deletion do not change an already scheduled execution attempt.
- Ordinary Result list/get responses do not expose secret-bearing script content;
  the snapshot is disclosed only through an authorized claim or an explicitly
  privileged operator endpoint.
- Legacy pending Results without a snapshot fail closed and are marked with an
  actionable migration reason. They are not silently populated from the latest
  Scenario version.

## Implementation Constraints

- Persist a real value snapshot, not another lazy GORM association.
- Reuse registered prob serialization and preserve the exact typed manifest.
- Snapshot validation happens before persistence; a stored Result must always be
  executable by a compatible Worker or explicitly unschedulable.
- Avoid duplicating mutable Scenario metadata that is irrelevant to execution.
- Version/history displayed in Results must describe the snapshot actually run.
- Consider data classification: probe definitions may contain credentials and
  must not leak through public Result serialization or logs.

## Suggested Implementation Sequence

1. Add a typed execution-snapshot field/value object and serialization tests.
2. Add a regression test: create Result, edit Scenario, claim Result, assert old prob.
3. Populate and validate the snapshot during Result creation.
4. Change `authorizeRun` to use only the Result snapshot.
5. Add safe API serialization and legacy-row migration behavior.
6. Document immutable-at-scheduling semantics.

## Non-Goals

- Scenario revision/history UI beyond showing the version attached to a Result.
- Secret injection at execution time; placeholders may remain in the snapshot.
- Outbox mechanics (task 002) or Runner policy design (task 008).

## Acceptance Criteria / Definition of Done

- [ ] Scenario edits cannot change an existing pending Result's executable input.
- [ ] Claim succeeds after Scenario deletion using the stored snapshot.
- [ ] Result/version/audit metadata identifies the Scenario revision actually run.
- [ ] Snapshot content is absent from ordinary list/get serialization and logs.
- [ ] Legacy pending rows without snapshots fail closed with an actionable state.
- [ ] All registered prob kinds round-trip through snapshot storage.

## Required Tests

- Create Result from probe A, update Scenario to probe B, claim: worker receives A.
- Delete or disable Scenario after scheduling: existing Result still claims A;
  no new Results are scheduled from it.
- Public Result GET/list does not expose A's secret-bearing fields.
- Round-trip HTTP, DNS, TCP, ICMP, gRPC, HAR, REST, and browser manifests.
- Legacy pending Result without snapshot is never executed from current Scenario.

## Validation

```sh
go test -race -count=1 ./pkg/urth ./pkg/natsq ./cmd/api-server ./cmd/nats-worker
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
