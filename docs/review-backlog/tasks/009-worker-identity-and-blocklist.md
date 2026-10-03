# 009: Add Stable Worker Identity and Runner Blocklists

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `blocked` |
| Priority | `P0` |
| Workstream | Authentication / Runner contract |
| Depends on | 005 |
| Likely conflicts | 004, 008 |
| Owner | Unclaimed |

## Current evidence and remaining criteria (2026-10-03)

Shared machine-token enrollment exists. The dependency on task 005 means its
current enrollment contract and tests; it does not require the historical
standalone token store. Stable Worker identity is still a distinct requirement.

| Classification | Evidence / remaining requirement |
| --- | --- |
| Completed prerequisite | Paired Runner machine identities and revocable shared machine tokens. |
| Outstanding baseline | `admitWorker` in `pkg/urth/service.go` matches the display name. This does not prove possession of a persistent installation key. |
| Outstanding | Persistent private key, verified fingerprint, expiring single-use challenge and explicit same-name/different-key denial. |
| Outstanding | Versioned Runner blocklist with equivalent CLI/UI controls, enrollment/refresh/next-claim enforcement and secrecy tests. |
| Outstanding | Bounded broker credential revocation and independent in-flight run reporting evidence. |

Fresh installations only. Do not add a legacy enrollment fallback or migration
window. Current completion needs evidence for every acceptance criterion below.
See the [release checklist](../../m9-release-validation.md).

## M9 candidate evidence under validation

The implementation candidate adds persistent Ed25519 keys, two-minute single-use
challenge proof, server-derived fingerprints and a versioned Runner blocklist.
The implementation owner reports focused proof/blocklist tests pass, including
`TestWorkerProofIdentityReplayAndBlocklist`,
`TestConcurrentWorkerChallengeReplayHasOneWinner`,
`TestWorkerChallengeCapacityIsBounded` and the private installation-key test.
CLI block/unblock and the website Block/Unblock dialogs edit the same
`blockedWorkers` resource fields. Worker detail exposes `status.fingerprint`.

Secured PostgreSQL/HTTPS/mutual-TLS broker checks pass for blocked/deleted
session denial, bounded completion after deletion, claim/block commit order and
same/new-UID credential renewal. Focused CLI block/unblock and session-expiry
checks pass. The typed Block/Unblock UI passes 33 unit tests/build and eight
desktop/mobile Playwright/axe cases, with reviewed narrow/wide screenshots.
Versioned writes and stale-draft recovery preserve concurrent blocks.
[Worker PR 106](https://github.com/sre-norns/urth/pull/106) at `609e129` has the
same Go/UI source as locally validated `7cfdbbf`. The complete PostgreSQL/race
suite and verify/vet/static checks pass per the implementation owner.
Operational rotation/failover, merge, exact merged-head CI and fresh-stack
validation remain open. Do not mark this task done from focused tests alone. The historical name-based diagnosis
applies to the pre-M9 baseline.

## Historical review baseline and retained requirements

## Why This Matters

Workers currently choose a display name and the server uses that name to find an
existing WorkerInstance during refresh. A process holding the shared Runner
enrollment secret can present another Worker's name and assume that control-plane
identity. There is no stable security identity or blocklist field to revoke one
known physical Worker independently.

Display names and server-assigned registration UIDs are useful metadata, but they
are not proof that the same physical identity returned.

## Evidence

- `cmd/nats-worker/main.go:51,96-98`: Worker name is locally selected/generated.
- `pkg/urth/service.go:1503-1506`: enrollment expands instances by submitted metadata name.
- `pkg/urth/service.go:1552-1567`: matching name is treated as reauthentication
  and receives the existing WorkerInstance UID/session.
- `pkg/urth/types.go:17-45`: WorkerInstance carries no verified security identity.
- `pkg/urth/types.go:47-60`: RunnerSpec carries no blocklist.
- `docs/adr/0003-runner-worker-model.md:215-237`: accepted stable identity and
  blocklist semantics.

## Required Outcome

- Each Worker installation has a stable signing key generated/provisioned by the
  operator and stored in secret-aware local configuration.
- Enrollment proves possession by signing a server nonce/challenge. The API stores
  the public-key fingerprint as the stable Worker security identity.
- Display name remains mutable metadata; WorkerInstance UID remains the registration
  resource identity. Neither is accepted as proof.
- RunnerSpec contains versioned `blockedWorkers` entries keyed by verified fingerprint
  with optional reason/audit metadata.
- Registration, refresh, and every new claim check the current blocklist.
- Adding a block entry prevents new claims immediately and causes active NATS
  authority to expire/disconnect where task 004's mechanism supports it. Existing
  run capabilities retain ADR 0002's bounded in-flight semantics.

## Implementation Constraints

- A fingerprint is derived from a verified public key and cannot be self-asserted
  without proof of possession.
- Challenge tokens are short-lived, single-use, audience-bound, and replay-safe.
- Never store Worker private keys in Urth resources or API responses.
- Re-registration with the same verified identity may refresh the same WorkerInstance;
  a new identity using the same display name creates/conflicts explicitly rather
  than impersonating it.
- Blocklist updates use normal versioned Runner resource semantics and are visible
  equivalently in API, CLI, and UI.

## Suggested Implementation Sequence

1. Define stable identity/fingerprint and challenge-response protocol.
2. Add Worker key-file generation/loading with safe permissions and documentation.
3. Persist verified identity on WorkerInstance and use it for refresh lookup.
4. Add Runner blocklist schema, validation, CLI/UI management, and audit display.
5. Enforce at enrollment/refresh/claim and integrate NATS revocation.
6. Add impersonation, replay, and live-block tests.

## Non-Goals

- Hardware-backed attestation as a baseline requirement.
- Preventing an enrollment-secret holder from creating an entirely new valid
  Worker identity; rotate enrollment for that broader compromise.
- Cancelling already-running Results solely because a Worker was blocklisted.

## Acceptance Criteria / Definition of Done

- [ ] A Worker proves possession of a stable key during enrollment.
- [ ] Reusing another Worker's display name cannot assume its UID/session.
- [ ] Blocklisted identity cannot register, refresh, or claim.
- [ ] Blocking an active identity takes effect without waiting for session expiry.
- [ ] Existing bounded run capability semantics are preserved.
- [ ] CLI and UI expose the same blocklist resource fields/actions.

## Required Tests

- Same name plus different key cannot refresh the original WorkerInstance.
- Valid challenge proof registers; forged, expired, and replayed proof fails.
- Add identity to blocklist while session is active: next claim fails.
- Remove block entry with correct Runner version: enrollment/claim works again.
- Private key never appears in manifests, responses, or logs.

## Validation

```sh
go test -race -count=1 ./pkg/urth ./cmd/api-server ./cmd/nats-worker ./cmd/urthctl
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
