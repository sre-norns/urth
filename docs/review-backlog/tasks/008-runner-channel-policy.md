# 008: Complete the Runner Channel Policy Contract

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `done` |
| Priority | `P0` |
| Workstream | Runner contract |
| Depends on | — |
| Likely conflicts | 006, 007, 009, 014 |
| Owner | Codex; `feat/runner-channel-policy` (merged in PR #129) |

## Current scope (2026-10-04)

Runner channel policy is separate Urth product work. The implementation merged
in PR #129. M9 Worker identity and broker security
remain in place. The [validation record](../../runner-channel-policy-validation.md)
contains the acceptance matrix and local evidence.

Fresh installations use the explicit policy schema. Obsolete Runner
`requirements` receives a clear error. Existing-resource migration and
compatibility readers are out of scope.

## Historical review baseline and policy requirements

## Why This Matters

The current Runner has one ambiguous label-selector `requirements` field. It is
used for Worker enrollment, while Scenario placement matches Runner metadata
labels and no policy checks which jobs the Runner owner accepts. Claim authorization
does not recheck stored Worker capabilities against the concrete job. Runner
labels are not propagated as authoritative scheduling-time snapshots.

This permits incoherent channels: a job can enter a Runner whose admitted Worker
cannot execute its prob kind or duration, and execution history can describe
current/Worker-supplied labels instead of the selected vantage point.

## Evidence

- `pkg/urth/types.go:47-60`: one `RunnerSpec.Requirements` selector models the
  whole channel policy.
- `pkg/urth/service.go:488-534`: placement checks only Scenario requirements
  against Runner labels.
- `pkg/urth/service.go:1526-1546`: enrollment applies the ambiguous selector to
  self-reported Worker labels.
- `pkg/urth/service.go:928-967`: claim does not check concrete execution needs
  against stored Worker capabilities.
- `pkg/urth/service.go:623-635`: pending Result receives only Runner name/UID,
  not versioned propagated-label snapshots.
- `docs/adr/0003-runner-worker-model.md:133-213`: accepted three-part policy and
  inheritance rules.

## Required Outcome

Replace the ambiguous field with explicit policy sections:

- `jobRequirements`: what Scenarios/Results the Runner accepts, including typed
  prob kinds and maximum/minimum run duration where relevant;
- `workerRequirements`: capabilities every Worker must provide, including typed
  semantic-version/runtime/prob/duration constraints; and
- `propagatedLabels`: operator-controlled vantage-point labels copied to Results
  and Artifacts.

Scheduling succeeds only when Scenario placement and Runner job admission both
accept. Enrollment checks Worker requirements. Claim reloads the stored effective
Worker capability snapshot and verifies it against the concrete Result snapshot.
Artifacts inherit Result labels server-side; Worker labels cannot override them.

## Implementation Constraints

- Version and duration constraints use typed comparison, not lexicographical labels.
- Separate self-reported Worker capabilities from operator-controlled Runner
  labels and server-reserved identity labels.
- Every Worker admitted to a Runner must satisfy every job class the Runner accepts.
  Reject incoherent Runner policy on create/update where it can be proven.
- Reject obsolete Runner `requirements` explicitly. Do not add migration or
  compatibility reads. Scenario placement requirements remain unchanged.
- Runner UID/name/version and propagated labels are snapshotted at Result creation.
- Artifact server code derives inheritance from Result state, not from Worker upload.

## Suggested Implementation Sequence

1. Define typed policy/capability value objects with parser/comparison tests.
2. Add strict fresh-install manifest and validation tests.
3. Split scheduling into placement and Runner job-admission evaluations.
4. Store effective Worker capabilities at enrollment and recheck at claim.
5. Snapshot propagated labels on Result and derive Artifact labels server-side.
6. Update examples, CLI/UI forms, search, and ADR implementation-status notes.

## Non-Goals

- Stable cryptographic Worker identity and blocklist enforcement (task 009).
- Capacity/load-based choice among otherwise eligible Runners (task 014).
- Plugin distribution or dynamic capability installation.

## Acceptance Criteria / Definition of Done

- [x] Placement, Runner job admission, and Worker admission are separately modeled.
- [x] Incoherent policies are rejected or cannot admit an incapable Worker.
- [x] Concrete claim checks use stored capabilities, never claim-body labels.
- [x] Version and duration ranges have domain-correct comparison tests.
- [x] Result and Artifact labels are immutable server-derived Runner snapshots.
- [x] Fresh-install manifests reject obsolete Runner `requirements` with a clear error.
  Existing-resource migration is out of scope.

## Required Tests

- Scenario placement matches Runner labels but job policy rejects its prob kind.
- Worker enrollment meets labels but fails typed version/duration/prob requirement.
- Stored capable Worker claims; incapable/stale-capability Worker is refused.
- Worker upload tries to forge Runner/vantage labels; Result snapshot wins.
- Runner labels change after scheduling; historical Result/Artifact remain unchanged.

## Validation

```sh
go test -race -count=1 ./pkg/urth ./pkg/runner ./cmd/api-server ./cmd/nats-worker
go test -race -count=1 ./...
go vet ./...
git diff --check
(cd website && npm test)
```

## Completion Record

The source baseline is `50aa0d5bea8aa828637bae1e5a6819e3a4538187`.
Merged in PR #129 at `7a3c9cd827125e7057f3351391038a5d492be1c8` on 2026-10-04.
No release or deployment is included.

- **Implemented:** Separate job and Worker policies, validated typed capability
  snapshots, placement/claim enforcement, immutable provenance, strict fresh-install
  decoding, and operator-visible admission rejection state. Workers receive generic
  refusal responses.
- **Tests added/updated:** `test/integration/channel_policy_test.go` covers real
  PostgreSQL, scoped HTTP, NATS and Worker boundaries. Existing domain and M9
  fixtures declare explicit job policies and typed capabilities.
- **Documentation updated:** ADR 0003, Runner manifests, CLI/API guidance and
  this task record describe the fresh-install contract.
- **Validation evidence:** See the [acceptance record](../../runner-channel-policy-validation.md).
  The baseline claim regression fails before enforcement. Real boundary, native
  profile, concurrency, client and race checks pass. The full PostgreSQL audit passes.
  PR #129 head `a6210e8` passed Build And Verify, Website, CodeQL, Nix flake,
  vulnerability scan and release packaging. On the merge commit, Build And Verify,
  Website and CodeQL were cancelled when the next push to `main` started. The next
  `main` commit, `d894a97` (PR #130), contains this change and passes Build And
  Verify (37178497493), Website (37178497481) and CodeQL (37178497528).
- **Follow-ups:** Post-merge review follow-ups are tasks
  [026](026-stale-worker-capability-capacity.md),
  [027](027-report-after-full-budget-run.md),
  [028](028-absorb-placement-version-conflicts.md),
  [029](029-typed-runner-policy-authoring.md),
  [030](030-reject-unexecutable-probe-kinds.md) and
  [031](031-channel-policy-cleanup.md). The `pkg/natsq` test flakes seen during
  review are fixed in PR #130. Release publication requires separate authorization.
