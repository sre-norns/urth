# 006: Harden Run Capabilities and Reporting Authorization

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P0` |
| Workstream | Authentication / Claim lifecycle |
| Depends on | — |
| Likely conflicts | 005, 007, 008, 011 |
| Owner | Unclaimed |

## Why This Matters

The new claim path issues a separate run signing key, but the resulting token is
still only a generic JWT whose subject is the Result UID. Status and Artifact
authorization validate a broad HMAC family and subject, without audience, key
rotation identity, operation scopes, recorded executor, dispatch, or current
lifecycle checks required by ADR 0002.

A leaked capability remains narrow by key separation, but its authority is
implicit and inconsistent across endpoints. This weakens revocation, audit, and
future service separation.

## Evidence

- `pkg/urth/service.go:985-1001`: run JWT contains standard issuer/subject/time
  claims only and has no key ID.
- `pkg/urth/service.go:1039-1068`: status validation accepts any HMAC method and
  checks only Result subject after generic JWT validation.
- `pkg/urth/service.go:1662-1697`: Artifact authorization repeats a separate,
  similarly incomplete parser.
- `pkg/urth/service.go:1071-1104`: terminal transition validation does not use
  explicit scopes or compare token executor/dispatch to current Result state.
- `docs/adr/0002-worker-authentication.md:111-146,183-205`: accepted run
  capability claims and lifecycle rules.

## Required Outcome

Introduce one centralized run-capability issuer and validator with:

- explicit HS256 allowlist (or a stronger chosen algorithm), key ID, configured
  issuer, and `urth-api` audience;
- Result UID, Runner UID, WorkerInstance UID, dispatch ID, and explicit scopes
  for status transition and Artifact creation;
- issued-at, not-before, server-controlled deadline, and bounded upload grace;
- verification-key rotation by key ID without breaking in-flight capabilities;
  and
- current Result checks proving the token still describes the recorded executor,
  dispatch, and permitted lifecycle transition.

Status may move a running Result through allowed terminal transitions once. It
cannot reopen or rewrite a terminal Result. Artifact uploads may finish during
the bounded upload grace for the same capability, including when the final status
won a concurrent race; they remain linked server-side to the token's Result.

## Implementation Constraints

- Both status and Artifact APIs consume the same validated capability principal;
  do not maintain two JWT parsers.
- Worker-supplied Result/executor/scenario/security labels are never authoritative.
- API resolves the Result from the token and route ID agreement, not from an
  Artifact manifest association supplied by the Worker.
- Keep signing material separate from enrollment, session, and NATS keys.
- Error responses remain generic; detailed validation causes are safe only in
  redacted server logs/metrics.
- Preserve optimistic versions and define idempotent final-status/artifact retry
  behavior for lost responses.

## Suggested Implementation Sequence

1. Define typed claims, scopes, and a key-ring interface with focused token tests.
2. Replace issuance in `authorizeRun` and add `kid`/audience/executor/dispatch.
3. Centralize parse plus current-Result authorization.
4. Migrate status and Artifact endpoints to the shared principal.
5. Add lifecycle, cross-Result, cross-executor, and key-rotation tests.
6. Document upload grace and operator key rotation.

## Non-Goals

- Runner enrollment credentials (task 005).
- Worker session/NATS credentials (task 004).
- Artifact object-store selection or retention policy.
- General user authorization for reading Results or Artifacts.

## Acceptance Criteria / Definition of Done

- [ ] Capabilities validate algorithm, key ID, issuer, audience, time, identity,
  dispatch, and scopes.
- [ ] Status and Artifact APIs share one validator/authorizer.
- [ ] A token cannot affect another Result or a different recorded executor.
- [ ] Terminal Results cannot be rewritten; bounded final Artifact upload works.
- [ ] Old verification keys support in-flight work during documented rotation.
- [ ] Worker-controlled labels cannot override authoritative linkage/identity.

## Required Tests

- Wrong algorithm, issuer, audience, key ID, scope, Result, Runner, Worker, and
  dispatch are each rejected.
- Expired token and Result lease are rejected according to upload-grace policy.
- Replayed final status cannot rewrite a terminal Result.
- Artifacts racing final status still attach to the correct Result.
- Rotate signing key while an old valid run remains in flight.

## Validation

```sh
go test -race -count=1 ./pkg/urth ./cmd/api-server ./cmd/nats-worker
go test -race -count=1 ./...
go vet ./...
git diff --check
```

## Completion Record

- **Implemented:**
- **Tests added/updated:**
- **Documentation updated:**
- **Validation evidence:**
- **Follow-ups:**
