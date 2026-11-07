# 031: Clean Up Channel Policy Code, Example and Test Paths

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P2` |
| Workstream | Runner contract |
| Depends on | 008 (done) |
| Likely conflicts | 026, 028 |
| Owner | Unclaimed |

Found in the review of the Runner channel policy implementation (task 008, PR #129).

## Why This Matters

The review of task 008 found four small problems. Each is cheap to fix. Together
they make the policy code harder to read and let unit tests check a code path that
production does not use.

## Evidence

1. **Reason literal.** `pkg/urth/placement.go:329` and `:523` use the string
   `"job-policy-rejected"`. Other placement reasons are named constants in
   `pkg/urth/labels.go:135-153` (for example `ReasonNoEligibleRunner`).
2. **Example noise.** `examples/runner.yaml` sets
   `workerRequirements.minDuration: 1ns` and `maxDuration: 1m`, which repeat the
   defaults. The previous example also listed the `har` kind, which this example
   dropped.
3. **Two creation paths.** `pkg/urth/service.go:883` onward
   (`createWithDispatch`) keeps a non-transactional path for a nil `identityDB`.
   Unit tests use it. Production uses the transactional path with the Runner
   share lock and the grant check (`service.go:847-866`). The two paths diverge.
4. **Repeated validation.** `AcceptsJob` (`pkg/urth/channel_policy.go:270`) and
   `AdmitCapabilities` (`channel_policy.go:304`) call `ValidatePolicy` on every
   call: once per candidate Runner per placement, and once per claim.

Line numbers are starting points. Revalidate them against the current branch.

## Required Outcome

1. A named reason constant replaces `"job-policy-rejected"`, documented next to
   the other reasons.
2. `examples/runner.yaml` sets only what an operator needs to set. It lists the
   kinds an example operator is expected to use.
3. Unit tests use the transactional creation path, or the non-transactional
   fallback is deleted. Choose one and record the decision.
4. Policy is validated once on write. Read paths trust the stored policy, or the
   decision to keep per-call validation is recorded with a reason.

## Implementation Constraints

- No behavior change for API clients, apart from the decisions in items 3 and 4.
- Stored policies that are invalid must still fail closed (task 008).

## Suggested Implementation Sequence

1. Add the reason constant.
2. Trim the example and check it with `urthctl` against a test server.
3. Move the unit tests to the transactional path, then remove the fallback if
   nothing else needs it.
4. Move validation to the write path and add a test for an invalid stored policy.

## Non-Goals

- The retry on version conflicts (task 028).
- Claim refusal records (task 026).

## Acceptance Criteria / Definition of Done

- [ ] No string literal reason remains in placement.
- [ ] The example contains only meaningful settings.
- [ ] Tests and production use the same creation path, or the decision is recorded.
- [ ] Policy validation runs on write. An invalid stored policy still fails closed.
- [ ] Targeted and full validation pass.

## Required Tests

- Placement and preview report the named reason for a job policy rejection.
- `examples/runner.yaml` applies successfully.
- An invalid stored policy fails closed on placement and on claim.

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
