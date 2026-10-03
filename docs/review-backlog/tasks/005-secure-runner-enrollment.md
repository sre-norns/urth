# 005: Verify Shared Machine-Token Enrollment Lifecycle

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P0` |
| Workstream | Authentication |
| Depends on | — |
| Likely conflicts | 004, 006, 009 |
| Owner | Unclaimed |

## Current evidence and remaining criteria (2026-10-03)

The shared identity contract supersedes the standalone enrollment-store design.
Keep this task open for the precise gaps below. Do not add a second token store,
a legacy enrollment fallback or an operator-authorizer bypass.

| Current criterion | Evidence / remaining work |
| --- | --- |
| Authenticated issuance | The fresh-stack test attempts anonymous machine-token issue and receives 401. An authenticated owner issues the token successfully. |
| One-time secret and normal-read secrecy | `m8_contract_test.go` and the real-stack test prove operation-only issuance, idempotent replay redaction and ordinary token-read redaction. Runner resources contain no enrollment secret. |
| Individual issue/revoke lifecycle | `TestMachineTokenRevocationDeniesFreshProofButPreservesSession` uses an unused valid proof after revocation: challenge and enrollment both return 401. A replacement token renews the same Worker UID. Existing session claims remain authorized independently. The real Worker also fails enrollment with the revoked token. |
| Disable/delete and identity suspension | Open: attempt both fresh enrollment and refresh after Runner disable, Runner deletion and paired identity suspension, with positive controls. `TestRunnerIdentityStaysPairedWithItsRunner` proves suspension is allowed, but does not attempt enrollment after suspension. |
| Stored token protection and secret outputs | Open: record the shared `v0.7.2` token storage representation and prove stored state is unusable as bearer authority. Add explicit secret-exclusion assertions for logs, ordinary CLI reads/errors and serialized manifests; ordinary reads/replays alone do not cover every output. |
| CLI/UI lifecycle equivalence | Open: prove authenticated issue/revoke and one-time secret handling through both clients. Current real-stack issuance/revocation evidence uses the identity endpoint; CLI device login is separate evidence. |

Shared tokens are individually issued and revoked. More than one token can be
active. Issue a replacement, update installations, then revoke the old token.
This replaces the historical single-generation switch and concurrent-rotation
requirement. Runner creation creates its paired identity; it does not return an
automatic initial enrollment secret.

Revocation prevents enrollment or refresh with that token. It does not cancel an
already issued Worker session or claimed run capability. Use the separate
current-state controls for new-claim denial. The mounted and fresh-stack tests
record those operations separately.

The implementation and tests are merged in `d60d902`. The exact merged backend
and website gates pass. See [the release record](../../m9-release-validation.md)
and [the real-stack evidence](../../m9-fresh-stack-validation.md). Passing CI
does not cover the missing lifecycle/storage/client cases above.

## Historical review baseline and superseded design

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

## Current Acceptance Criteria / Definition of Done

- [x] Unauthenticated callers cannot obtain enrollment authority.
- [x] Issuance returns an operation-only secret; normal reads and replay redact it.
- [x] Revocation immediately rejects the old token for proof/enrollment;
  replacement issuance preserves the Runner and Worker UID contracts.
- [ ] Runner disable/delete and identity suspension deny enrollment and refresh.
- [ ] Stored database token state cannot serve as an enrollment bearer secret.
- [ ] CLI and UI provide equivalent authenticated issue/revoke and secret handling.
- [ ] Logs, ordinary CLI reads/errors and serialized manifests exclude secret
  values. Explicit authenticated one-time token issuance may return the secret
  to its caller.

The historical create/rotate and one-active-generation design above is
superseded. Tests must target the current shared issue/revoke contract.

## Validation

```sh
go test -race -count=1 ./pkg/urth ./cmd/api-server ./cmd/urthctl
go test -race -count=1 ./...
go vet ./...
git diff --check
(cd website && npm test)
```

## Completion Record

- **Implemented:** Shared paired machine identity and individually revocable
  tokens; no standalone store or compatibility mechanism.
- **Tests added/updated:** Canonical replay/read redaction, mounted valid-proof
  revocation/replacement and real-stack anonymous issue/revoked Worker checks.
- **Validation evidence:** Merged `d60d902` backend audit and website checks pass;
  detailed source attribution is in the release record.
- **Follow-ups:** The three lifecycle-state pairs, at-rest protection,
  ordinary-read/error/log/manifest secret assertions and CLI/UI equivalence
  listed above.
  The task remains open.
