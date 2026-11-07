# 027: Prove Reporting Succeeds After a Run Uses Its Full Budget

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P1` |
| Workstream | Claim lifecycle |
| Depends on | 008 (done) |
| Likely conflicts | 006 |
| Owner | Unclaimed |

Found in the review of the Runner channel policy implementation (task 008, PR #129).

## Why This Matters

Task 008 changed the run lease. The claim now grants exactly the execution
duration of the snapshot. Before, the Worker kept 15 seconds of its budget
(`uploadReserve`) to upload artifacts and the final status. That reserve is
removed. Reporting now depends on the run capability's upload grace period after
the deadline.

No test proves that a probe which uses its whole timeout can still upload its
artifacts and final status. If the grace period does not cover the upload, the
run loses its results and the reconciler expires it.

## Evidence

- `pkg/urth/service.go:1176-1181`: the lease duration is
  `executionDuration(entry.Spec.Execution)`. The deadline is now plus that duration.
- `pkg/urth/service.go:1343-1346`: `artifactUploadGrace` is 5 minutes. The run
  capability expires at the deadline plus this grace (`service.go:1318`).
- `pkg/urth/run_capability.go:54`: the capability check uses the deadline plus
  `artifactUploadGrace`.
- `pkg/urth/reconcile.go:355`: the reconciler uses the same grace as `leaseGrace`.
- `pkg/worker/ack.go:23`: a comment still refers to the removed `uploadReserve`.

Line numbers are starting points. Revalidate them against the current branch.

## Required Outcome

- An integration test runs a probe that uses its whole timeout, then uploads
  artifacts and reports the final status. The Result reaches its terminal state
  with its artifacts.
- The test runs for a short timeout (for example 1 second) and for the default
  timeout (1 minute).
- If the grace period is too short for large uploads, the grace period is sized
  explicitly and documented. Do not add the Worker-side reserve back.
- The stale `uploadReserve` comment in `pkg/worker/ack.go` is corrected.

## Implementation Constraints

- The Worker cannot extend its lease or its capability (task 006).
- The reconciler and the capability must keep one grace value.

## Suggested Implementation Sequence

1. Add the integration test with a probe that blocks for its full timeout.
2. Measure the upload time for a representative artifact set.
3. Change the grace value only if the measurement requires it.

## Non-Goals

- Changing the claim lease model again.
- Streaming or chunked artifact upload.

## Acceptance Criteria / Definition of Done

- [ ] A full-budget run reports its final status and artifacts within the grace period.
- [ ] The test covers a short timeout and the default timeout.
- [ ] The grace value is justified in code comments or documentation.
- [ ] The stale `uploadReserve` comment is corrected.
- [ ] Targeted and full validation pass.

## Required Tests

- `test/integration`: a probe with a 1 second timeout uses all of it, then reports.
- `test/integration`: the same with the default 1 minute timeout. Mark it as a
  long test if needed.
- A report after the grace period is refused.

## Validation

```sh
go test -race -count=1 ./test/integration -run FullBudget
go test -race -count=1 ./...
go vet ./...
git diff --check
```

## Completion Record

Fill this in before marking the task `done`:

- **Implemented:**
- **Tests added/updated:**
- **Documentation updated:**
- **Validation evidence:**
- **Follow-ups:**
