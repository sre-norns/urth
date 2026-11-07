# 026: Record Claim-Time Capability Refusals and Exclude Stale Workers from Capacity

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P1` |
| Workstream | Runner contract / Claim lifecycle |
| Depends on | 008 (done), 022 (done) |
| Likely conflicts | 014, 016, 021 |
| Owner | Unclaimed |

Found in the review of the Runner channel policy implementation (task 008, PR #129).

## Why This Matters

An operator can edit a Runner's channel policy after Workers enroll. A Worker that
enrolled under the previous policy can stop satisfying the new policy. The claim
path refuses that Worker with a retryable refusal, and the Worker negatively
acknowledges the dispatch and retries after 5 seconds. Nothing makes the Worker
enroll again.

Placement still counts the Worker as `online` capacity. `selectRunner` can choose
a Runner where every Worker refuses the run. The run then stays pending until the
queue age limit and the reconciler expire it.

The operator sees no cause. `Runner.status.lastAdmissionRejection` records only
enrollment denials. The claim refusal is a server log line only.

## Evidence

- `pkg/urth/service.go:1163-1165`: a claim from a Worker whose stored
  `EffectiveCapabilities` fail `AdmitCapabilities` or `CanExecute` returns
  `claimUnavailable("stored worker capabilities do not cover channel", nil)`. No
  state is recorded.
- `pkg/worker/consume.go:326`: the Worker retries a refused claim with
  `NakWithDelay(5 * time.Second)`.
- `pkg/urth/placement.go:604-629`: capacity counts every online, not paused Worker
  of an eligible Runner. It does not check the stored capabilities against the
  current Runner policy.
- `pkg/urth/types.go:109` and `pkg/urth/service.go:1967-1968`:
  `LastAdmissionRejection` is written only on the enrollment path.

Line numbers are starting points. Revalidate them against the current branch.

## Required Outcome

- A claim refused because stored Worker capabilities do not satisfy the current
  Runner policy, or cannot execute the concrete job, is recorded on the Worker,
  for example as `WorkerInstance.status.lastClaimRefusal` with a reason, a time
  and the Result. The record is visible in the Web UI Worker page and in
  `urthctl`, per the operator-surface rule in `CONTEXT.md`.
- Placement counts a Worker as capacity only when its stored capabilities satisfy
  the Runner's current `workerRequirements`. A Runner whose Workers all fail the
  current policy is not selected because of those Workers.
- A Worker that does not satisfy the current policy is visible as such. The
  operator can tell "online but not admissible" apart from "online".

## Implementation Constraints

- Claim checks continue to use stored capabilities, never claim-body labels
  (task 008).
- The refusal stays retryable for the dispatch. Another capable Worker can still
  claim it.
- Do not add automatic re-enrollment in this task. Record the decision if the
  implementation shows that re-enrollment is necessary.
- Recording a refusal must not add a write on every redelivery of the same
  dispatch. Rate-limit or deduplicate the record.

## Suggested Implementation Sequence

1. Add a failing integration test: enroll a Worker, change the Runner policy so
   that the Worker no longer satisfies it, run the Scenario, and assert the
   refusal record and the placement result.
2. Record the claim refusal on the Worker.
3. Filter placement capacity by the current policy.
4. Show the state in the Web UI and `urthctl`.

## Non-Goals

- Automatic Worker re-enrollment or policy migration.
- The per-Runner queue read model (task 016).
- Capacity weighting among eligible Runners (task 014).

## Acceptance Criteria / Definition of Done

- [ ] A claim refused for stored capabilities records an operator-visible reason.
- [ ] Placement does not count Workers that fail the current Runner policy.
- [ ] Both operator surfaces show the refusal and the "not admissible" state.
- [ ] Regression tests cover a policy edit after enrollment.
- [ ] Targeted and full validation pass.

## Required Tests

- A Worker enrolled under policy A claims after an edit to policy B and is refused.
  The refusal is recorded once, not once per redelivery.
- Placement with one admissible and one stale Worker counts only the admissible one.
- Placement with only stale Workers does not report spare capacity for that Runner.

## Validation

```sh
go test -race -count=1 ./pkg/urth ./test/integration
go test -race -count=1 ./...
go vet ./...
git diff --check
(cd website && npm test)
```

## Completion Record

Fill this in before marking the task `done`:

- **Implemented:**
- **Tests added/updated:**
- **Documentation updated:**
- **Validation evidence:**
- **Follow-ups:**
