# NNN: Task title

Shared context: [`CONTEXT.md`](CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P0` / `P1` / `P2` |
| Workstream | Claim lifecycle / Durability / Authentication / Runner contract / Migration |
| Depends on | Task IDs or — |
| Likely conflicts | Task IDs or — |
| Owner | Unclaimed |

## Why This Matters

Describe the observable bug, safety risk, or architectural friction and its user
impact. Use the domain and architecture language from [`CONTEXT.md`](CONTEXT.md).
When copying this template under `tasks/`, change that link to `../CONTEXT.md`.

## Evidence

- `path/to/file.go:line`: current behavior and why it is relevant.
- Include a reproduction or failure sequence when one exists.

Line numbers are starting points. Revalidate them against the current branch.

## Required Outcome

State externally observable behavior and important invariants. This section must
be specific enough that an implementer does not need to make a product decision.

## Implementation Constraints

- Required compatibility or package dependency direction.
- Trust, durability, and resource-lifecycle constraints.
- Relevant ADR rules that must not be weakened.

## Suggested Implementation Sequence

1. Add a failing regression or integration test through the affected interface.
2. Make the smallest coherent behavior change.
3. Exercise the relevant failure boundary, not only the success path.
4. Update user-facing and operator documentation.

## Non-Goals

- List tempting adjacent changes that must not be folded into the task.

## Acceptance Criteria / Definition of Done

- [ ] Observable required behavior is implemented.
- [ ] Regression tests cover success and failure/edge behavior.
- [ ] Public and operator documentation matches the behavior.
- [ ] No unrelated changes are included.
- [ ] Targeted and full validation pass.

## Required Tests

- Name concrete scenarios and the most appropriate existing or new test module.

## Validation

```sh
# Add targeted commands first.
go test -race -count=1 ./...
go vet ./...
git diff --check
(cd website && npm test) # when Web UI code changes
```

## Completion Record

Fill this in before marking the task `done`:

- **Implemented:**
- **Tests added/updated:**
- **Documentation updated:**
- **Validation evidence:**
- **Follow-ups:**
