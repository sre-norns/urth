# 005: Secure Runner Enrollment Issuance and Rotation

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P0` |
| Workstream | Authentication |
| Depends on | — |
| Likely conflicts | 004, 006, 009 |
| Owner | Unclaimed |

## Why This Matters

Anyone able to reach the API can currently request a valid enrollment token for
any named Runner. That token can create or refresh WorkerInstances and obtain a
Worker session. Staged credentials do not provide a boundary when the bootstrap
credential is public.

The current globally signed 23-hour JWT also has no per-Runner rotation generation
or stored verifier, so one Runner's enrollment authority cannot be independently
rotated and revoked as ADR 0002 requires.

## Evidence

- `cmd/api-server/main.go:175-190`: enrollment issuance route has no operator
  authentication middleware; the TODO acknowledges it.
- `pkg/urth/service.go:1381-1406`: token is minted on demand from one global key.
- `pkg/urth/service.go:1485-1513`: enrollment validation accepts that token and
  checks only signature/subject/current Runner activity.
- `docs/adr/0002-worker-authentication.md:51-83`: accepted enrollment lifecycle,
  secrecy, storage, rotation, and revocation rules.

## Required Outcome

- Enrollment issuance and rotation require an authenticated operator decision.
- Runner creation returns the initial enrollment secret exactly once. List/Get
  responses and later ordinary updates never contain it.
- Use an opaque, cryptographically random per-Runner secret stored only as a
  salted verifier and generation metadata. Rotation creates a new secret and
  immediately invalidates the previous generation for registration/refresh.
- Disabling or deleting the Runner prevents registration and session refresh.
- Remove the unauthenticated `GET /auth/runners/:id` issuance behavior. A dedicated
  authenticated rotate action may return a replacement once.
- Until full user/account authentication exists, introduce a narrow
  `OperatorAuthorizer` boundary and require explicit insecure-development mode
  to bypass it. Production must fail closed when no authorizer is configured.

## Implementation Constraints

- Never log enrollment secrets or put them in manifests, labels, URLs, process
  arguments, metrics, or normal CLI output.
- Compare opaque verifiers using a password/secret KDF or keyed verifier designed
  for high-entropy tokens and constant-time comparison.
- Runner creation plus verifier metadata is one database transaction.
- Rotation is version-guarded and auditable. Concurrent rotations leave one
  active generation, not two.
- `urthctl` and UI must expose create/rotate secret handling equivalently without
  persisting the returned secret in resource definitions.
- Existing development flows may use an explicit flag, but documentation must
  label that mode insecure and off by default outside development.

## Suggested Implementation Sequence

1. Define the operator authorization interface and fail-closed configuration.
2. Add per-Runner enrollment verifier/generation storage and service methods.
3. Return the initial secret from authenticated Runner creation and add rotation.
4. Change Worker enrollment validation to use the stored verifier/current state.
5. Remove/deprecate the public token-mint route and update CLI/UI flows.
6. Add secrecy, rotation-race, and revocation tests.

## Non-Goals

- Selecting the complete human identity provider or role model for Urth.
- NATS connection credentials (task 004).
- Stable per-Worker proof and blocklisting (task 009).
- Run capability scopes (task 006).

## Acceptance Criteria / Definition of Done

- [ ] Unauthenticated callers cannot obtain enrollment authority.
- [ ] Initial and rotated secrets are returned once and never appear in resources.
- [ ] Rotation immediately rejects the old credential without changing Runner UID.
- [ ] Disable/delete prevents registration and refresh.
- [ ] Stored database state cannot be used directly as an enrollment bearer secret.
- [ ] CLI and UI offer equivalent authenticated create/rotate workflows.

## Required Tests

- Unauthenticated create/rotate/token request fails without revealing Runner existence.
- Runner creation returns a secret once; Get/List do not.
- Old secret fails immediately after successful rotation; new secret works.
- Concurrent rotations produce one valid generation.
- Logs and serialized Runner manifests do not contain the secret.

## Validation

```sh
go test -race -count=1 ./pkg/urth ./cmd/api-server ./cmd/urthctl
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
