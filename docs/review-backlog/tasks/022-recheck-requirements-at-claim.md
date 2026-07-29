# 022: Recheck execution requirements at claim time

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P1` |
| Workstream | Claim lifecycle / Runner contract |
| Depends on | — |
| Likely conflicts | 008, 014, 018 (done), 021 |
| Owner | Unclaimed |

## Why This Matters

`CONTEXT.md` states the invariant plainly: "The concrete job is rechecked at claim time."
It is not. `ClaimRun` verifies that the claiming worker belongs to the Runner the run was
*placed* on, but never compares the execution snapshot's `Requirements` against that
Runner's *current* labels.

Placement and claim are separated by an unbounded interval — a queued dispatch waits for a
Worker with capacity, and a Runner with no Workers holds its queue indefinitely by design.
A Runner's labels can change in between. `runnersAPIImpl.update` accepts a new label set on
an existing Runner, and labels are exactly how an operator says what a Runner is and where
it sits.

So an operator who relabels a Runner — moving it out of a network segment, dropping a
capability, retiring a role — does not stop the runs already queued for it. Those runs were
placed because the Runner matched a selector it no longer matches, and they will execute
anyway. For a platform whose premise is that a probe runs inside a specific network
segment, "the selector was true when we queued it" is not a strong enough guarantee; the
selector needs to be true when the probe actually runs.

The gap is narrow but it sits on the boundary the product exists to enforce, and it is
cheap to close: the Result already carries the requirements, and the Runner is already
loaded on the claim path.

## Evidence

- `pkg/urth/service.go:901-1010` (`ClaimRun`): checks session validity, dispatch ID,
  Runner membership, dispatch version, placed-Runner UID, execution snapshot presence, and
  Result state. No comparison of requirements against Runner labels.
- `pkg/urth/service.go:1023-1060` (`loadClaimant`): loads the Runner and checks revoked,
  paused, missing, and disabled. The Runner object with its current labels is in hand here.
- `pkg/urth/execution.go:58-63`: `ExecutionSnapshot.Requirements` is "the placement selector
  as it stood when the run was created" — captured precisely so it can be re-evaluated.
- `pkg/urth/placement.go:82` (`selectorFor`): the selector construction the recheck should
  reuse rather than reimplement.
- `pkg/urth/service.go:1329`: `update` replaces `result.Labels` wholesale, so a Runner's
  label set can change under a queued run.
- `docs/review-backlog/CONTEXT.md`, "Runner and Worker model": states the invariant this
  task implements.

## Required Outcome

- `ClaimRun` evaluates the Result's `Spec.Execution.Requirements` against the claiming
  Runner's current labels and refuses a claim that no longer matches.
- The refusal is classified correctly rather than collapsed into an existing disposition by
  convenience. Redelivering to a Worker of the same Runner cannot change the outcome, and
  no other Runner may claim the run either, so the run is terminal — it is marked
  unschedulable with a distinct reason, in the same shape as `ReasonNoExecutionSnapshot`,
  and the dispatch is acknowledged rather than redelivered.
- A snapshot whose requirements do not parse is refused, not treated as matching
  everything. `Create` already records that case as `ReasonInvalidRequirements` at
  placement; a snapshot that reaches claim in that state is a defect and must fail closed.
- An empty requirement set continues to match every Runner. That is the existing meaning of
  "no requirements" at placement and this task does not change it.
- The reason is server-side and label-visible; the Worker learns only a disposition, per
  the existing `ClaimError` contract.
- Operator-visible: the run's terminal reason says the Runner no longer satisfies the
  scenario's requirements, so the operator who relabelled the Runner can see the
  consequence without reading logs.

## Implementation Constraints

- Reuse the selector construction and matching used by placement. Two evaluators that can
  disagree about the same selector is the defect
  [task 020](020-settle-notin-selector-semantics.md) exists to settle; do not add a third.
- The recheck happens before the claim commits, so a Result that fails it is never moved to
  `running` and never has an execution lease spent on it.
- Do not fall back to the Scenario's current requirements. The snapshot is authoritative;
  reading the live Scenario is the failure `ClaimRun` already guards against for the
  snapshot itself.
- Do not weaken the placed-Runner UID check. This is an additional condition, not a
  replacement — [ADR 0007](../../adr/0007-runner-queue-addressing.md) §3 depends on the UID
  check remaining exactly as it is.
- Keep the disposition semantics from [task 001](001-preserve-retryable-claim-failures.md):
  an unclassified or transient failure must never ack a dispatch for a still-pending run.

## Suggested Implementation Sequence

1. Add a failing test: a Result placed on a matching Runner, the Runner relabelled, the
   claim attempted. Confirm it currently succeeds.
2. Add the recheck to `ClaimRun` using the placement selector path, before the commit.
3. Add the unschedulable reason and its label, following `ReasonNoExecutionSnapshot`.
4. Confirm the Worker acknowledges rather than redelivering, through the transport-level
   test.
5. Update `cmd/api-server/README.md` and the claim documentation.

## Non-Goals

- Rechecking Worker capability against the job class (part of
  [task 008](008-runner-channel-policy.md)).
- Re-placing a run onto a Runner that does match. A retry creates a new Result; this task
  does not schedule.
- Queue addressing or inheritance ([task 021](021-name-keyed-runner-queues.md)).
- Changing what a selector *means* ([task 020](020-settle-notin-selector-semantics.md)).

## Acceptance Criteria / Definition of Done

- [ ] A Runner relabelled after placement cannot claim the queued run.
- [ ] The run reaches a terminal state with a distinct, operator-readable reason and label.
- [ ] The dispatch is acknowledged, not redelivered until `MaxDeliver`.
- [ ] An empty requirement set still matches.
- [ ] Unparseable requirements fail closed.
- [ ] The placed-Runner UID check is unchanged.
- [ ] One selector evaluator serves both placement and the recheck.
- [ ] Regression tests fail against the current code before the fix.

## Required Tests

- Placement matches, Runner relabelled, claim refused, Result terminal with the reason.
- Placement matches, Runner unchanged, claim succeeds — the ordinary path is untouched.
- Empty requirements: claim succeeds against any Runner.
- Malformed requirements in the snapshot: claim refused, run terminal, not treated as a
  match.
- Disposition: the transport acknowledges the dispatch rather than retrying it.
- The refusal reason never reaches the Worker's response body.

## Validation

```sh
go test -race -count=1 ./pkg/urth ./pkg/natsq ./cmd/api-server
make audit/postgres
git diff --check
```

Per `CLAUDE.md`, confirm the new test fails against the old code before it passes against
the new.

## Completion Record

- **Implemented:**
- **Tests added/updated:**
- **Documentation updated:**
- **Validation evidence:**
- **Follow-ups:**
