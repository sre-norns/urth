# 029: Author Runner Channel Policy with Typed Website Forms

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P1` |
| Workstream | Runner contract |
| Depends on | 008 (done) |
| Likely conflicts | 030 |
| Owner | Unclaimed |

Found in the review of the Runner channel policy implementation (task 008, PR #129).

## Why This Matters

The website Runner create and edit forms show `jobRequirements`,
`workerRequirements` and `propagatedLabels` as JSON text areas. The task 008
handover asked for typed authoring.

The most urgent problem: a Runner created with the defaults has an empty
`probeKinds` list. The server accepts this Runner, but the Runner admits no jobs.
The only warning is a field hint. An operator can create a Runner that never runs
anything and see no error.

## Evidence

- `website/src/identity/config.ts:65`: the create form parses the three policy
  fields from JSON text.
- `website/src/identity/config.ts:71-73`: the fields are `textarea` inputs with
  hints only.
- `website/src/monitoring/Infrastructure.tsx:90-99`: the edit form uses the same
  JSON text areas.
- `pkg/urth/channel_policy.go:273`: `AcceptsJob` rejects every job when
  `probeKinds` is empty.

Line numbers are starting points. Revalidate them against the current branch.

## Required Outcome

Minimum (do this first):

- The create form starts with the probe kinds that the server supports, or the
  form shows a clear warning before it saves a Runner with an empty `probeKinds`
  list.

Full outcome:

- Probe kinds are a multi-select of the kinds the server supports.
- Privileges, duration bounds and version ranges have typed inputs with inline
  validation that matches the server.
- Propagated labels use a key-value editor.
- The edit form uses the same inputs and keeps the read ETag.

## Implementation Constraints

- The server stays the authority. The form must not accept values the server
  rejects, and it must show the server's error when the two disagree.
- `urthctl` keeps manifest-based authoring. The operator-surface rule in
  `CONTEXT.md` exempts resource authoring forms.
- Use `@sre-norns/components` form primitives where they exist.

## Suggested Implementation Sequence

1. Add the empty `probeKinds` warning or default, with a test.
2. Replace the probe kinds text with a multi-select.
3. Replace the remaining JSON fields with typed inputs.

## Non-Goals

- Server-side policy changes.
- A policy editor in `urthctl`.

## Acceptance Criteria / Definition of Done

- [ ] An operator cannot save a Runner with no probe kinds without a clear warning.
- [ ] Probe kinds, privileges, durations, versions and labels have typed inputs.
- [ ] Create and edit share the inputs. Edit keeps the ETag.
- [ ] Website unit and e2e tests cover the forms. Axe checks pass.
- [ ] Targeted and full validation pass.

## Required Tests

- Create with defaults: the form warns or prefills probe kinds.
- Invalid duration or version range: inline error, no request sent.
- Server rejects a value the form accepted: the server error is shown.
- Edit keeps unchanged fields and the ETag.

## Validation

```sh
(cd website && npm test && npm run build && npm run test:e2e)
git diff --check
```

## Completion Record

Fill this in before marking the task `done`:

- **Implemented:**
- **Tests added/updated:**
- **Documentation updated:**
- **Validation evidence:**
- **Follow-ups:**
