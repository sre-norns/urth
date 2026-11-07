# 030: Reject Probe Kinds That No Worker Profile Can Execute

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P2` |
| Workstream | Runner contract |
| Depends on | 008 (done) |
| Likely conflicts | 017, 029 |
| Owner | Unclaimed |

Found in the review of the Runner channel policy implementation (task 008, PR #129).

## Why This Matters

The API server registers the `pypuppeteer` prober, so a Runner with
`probeKinds: [pypuppeteer]` passes validation. Workers never advertise
`pypuppeteer`: it is not part of either supported Worker profile. Such a channel
admits no Worker, and the operator gets no error when the Runner is saved.

## Evidence

- `pkg/apiserver/server.go:54`: the API server imports the `pypuppeteer` prober.
- `pkg/urth/channel_policy.go:237-244` (`ValidateProbeKinds`): accepts every kind
  that has a registered prober in the server binary.
- `pkg/runner/dispatch.go:20`: the Worker does not import the `pypuppeteer` prober.
- `pkg/runner/capabilities.go:90-92`: capability discovery skips `pypuppeteer`
  ("not part of either supported Worker profile").
- `cmd/urthctl/run.go:19,129`: `urthctl` still uses the `pypuppeteer` kind for
  local runs.

Line numbers are starting points. Revalidate them against the current branch.

## Required Outcome

Choose one option and record the decision in this task:

- `ValidateProbeKinds` rejects kinds that no supported Worker profile can execute,
  with a clear error; or
- the `pypuppeteer` prober is removed from the API server, and from `urthctl` if
  local runs no longer need it.

In both cases, a Runner can no longer be saved with a probe kind that no Worker
can execute.

## Implementation Constraints

- The list of executable kinds must come from one source that both the server and
  the Worker profiles use. Do not copy the list by hand.
- Keep `ValidatePolicy` free of server-only prober registration (see the comment
  on `ValidateProbeKinds`).

## Suggested Implementation Sequence

1. Add a failing test: create a Runner with `probeKinds: [pypuppeteer]`.
2. Apply the chosen option.
3. Update examples and documentation that list probe kinds.

## Non-Goals

- Adding a Python Puppeteer Worker profile.
- Prober configuration defaults (task 017).

## Acceptance Criteria / Definition of Done

- [ ] A Runner with a kind that no Worker profile can execute is rejected on save.
- [ ] The decision (reject or remove) is recorded.
- [ ] Targeted and full validation pass.

## Required Tests

- Create and update with `probeKinds: [pypuppeteer]` fail with a clear error.
- Supported kinds still pass.

## Validation

```sh
go test -race -count=1 ./pkg/urth ./pkg/apiserver ./cmd/urthctl
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
