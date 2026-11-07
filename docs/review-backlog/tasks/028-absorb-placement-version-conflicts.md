# 028: Absorb Runner Version Conflicts During Run Creation

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P2` |
| Workstream | Runner contract |
| Depends on | 008 (done) |
| Likely conflicts | 014, 018, 031 |
| Owner | Unclaimed |

Found in the review of the Runner channel policy implementation (task 008, PR #129).

## Why This Matters

Run creation selects a Runner, then commits the Result and its dispatch in one
transaction. The transaction reads the selected Runner again under a share lock.
If an operator edits the Runner between placement and commit, the Runner version
changes and creation returns a version conflict.

The guard is correct. But the conflict reaches the user as HTTP 409 for "Run now",
and a future scheduler will receive the same error. The caller did not edit
anything and cannot fix the conflict. Placement can run again and usually succeed.

## Evidence

- `pkg/urth/service.go:690` (`resultsAPIImpl.Create`): places the run, then calls
  `createWithDispatch`.
- `pkg/urth/service.go:846-866` (`createWithDispatch`): returns
  `bark.ErrResourceVersionConflict` when the Runner version changed, when the
  Runner no longer accepts the job, or when the grant is gone.

Line numbers are starting points. Revalidate them against the current branch.

## Required Outcome

- `Create` repeats placement a small, fixed number of times (for example 2) when
  `createWithDispatch` returns a version conflict caused by the selected Runner.
- If every attempt conflicts, `Create` returns the conflict as before.
- A run that has no eligible Runner after the new placement follows the existing
  unplaceable path (task 018).

## Implementation Constraints

- Keep the share lock and the version check. The retry is outside the transaction.
- Each attempt is a complete placement. Do not reuse a stale placement decision.
- Do not retry conflicts that do not come from the selected Runner.

## Suggested Implementation Sequence

1. Add a test that edits the selected Runner between placement and commit.
2. Add the bounded retry in `Create`.
3. Add a test where every attempt conflicts.

## Non-Goals

- A scheduler or cron trigger.
- Changes to the transaction or to the lock strength.

## Acceptance Criteria / Definition of Done

- [ ] A single concurrent Runner edit does not surface as 409 for "Run now".
- [ ] Repeated conflicts still return the conflict after the retry limit.
- [ ] Targeted and full validation pass.

## Required Tests

- A Runner edit during placement: the run is created on the second attempt.
- A Runner that changes on every attempt: `Create` returns the conflict.
- A Runner that stops accepting the job: the run follows the unplaceable path.

## Validation

```sh
go test -race -count=1 ./pkg/urth ./test/integration
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
