# 008: Complete the Runner Channel Policy Contract

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P0` |
| Workstream | Runner contract |
| Depends on | — |
| Likely conflicts | 006, 007, 009, 014 |
| Owner | Unclaimed |

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
- Preserve compatibility by explicitly migrating/deprecating legacy `requirements`;
  do not silently reinterpret it differently for existing manifests.
- Runner UID/name/version and propagated labels are snapshotted at Result creation.
- Artifact server code derives inheritance from Result state, not from Worker upload.

## Suggested Implementation Sequence

1. Define typed policy/capability value objects with parser/comparison tests.
2. Add manifest compatibility and validation tests.
3. Split scheduling into placement and Runner job-admission evaluations.
4. Store effective Worker capabilities at enrollment and recheck at claim.
5. Snapshot propagated labels on Result and derive Artifact labels server-side.
6. Update examples, CLI/UI forms, search, and ADR implementation-status notes.

## Non-Goals

- Stable cryptographic Worker identity and blocklist enforcement (task 009).
- Capacity/load-based choice among otherwise eligible Runners (task 014).
- Plugin distribution or dynamic capability installation.

## Acceptance Criteria / Definition of Done

- [ ] Placement, Runner job admission, and Worker admission are separately modeled.
- [ ] Incoherent policies are rejected or cannot admit an incapable Worker.
- [ ] Concrete claim checks use stored capabilities, never claim-body labels.
- [ ] Version and duration ranges have domain-correct comparison tests.
- [ ] Result and Artifact labels are immutable server-derived Runner snapshots.
- [ ] Legacy manifests receive a documented migration/error path.

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

- **Implemented:**
- **Tests added/updated:**
- **Documentation updated:**
- **Validation evidence:**
- **Follow-ups:**
