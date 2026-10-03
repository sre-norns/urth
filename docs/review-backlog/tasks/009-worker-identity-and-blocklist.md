# 009: Add Stable Worker Identity and Runner Blocklists

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `done` |
| Priority | `P0` |
| Workstream | Authentication / Runner contract |
| Depends on | 005 |
| Likely conflicts | 004, 008 |
| Owner | M9 Worker security workstream |

## Current evidence and completion criteria (2026-10-03)

All six acceptance criteria have evidence in merged Urth `d60d902`. Shared
machine-token enrollment is the prerequisite; the historical name-only lookup
below describes the pre-M9 source. Task 005's remaining lifecycle/storage review
does not remove the proved Worker identity and blocklist controls.

| Criterion | Evidence |
| --- | --- |
| Stable key and verified proof | `TestInstallationKeyPersistsPrivatelyAndRejectsUnsafeFiles` and concurrent creation test; `TestWorkerProofIdentityReplayAndBlocklist` verifies fingerprint, forged/altered/expired/replayed proof denial and secret omission. The concurrent challenge test admits one winner. |
| Display name is not identity | The proof test retains the UID after a same-key rename and rejects a different key with the same name. |
| Block registration/refresh/claim | Secured `TestWorkerBlockAndDeleteComposeWithSecuredBroker` denies fresh proof and an attempted active-session claim with 403. The proof test and claim/block commit-order test enforce current Runner state. |
| Active block takes effect | Next claim fails immediately. The secured broker connection closes by its issued expiry; the default maximum broker authority is five minutes, capped by Worker session expiry. Blocking does not require waiting for Worker session expiry. |
| Preserve bounded reporting | Secured block/delete tests complete an already claimed run and its artifact within the independent run capability. Session-expiry and runtime renewal/rebind tests preserve the same boundary. |
| CLI/UI equivalence | `TestWorkerBlocklistCLIUsesVersionedCanonicalRunner` edits `blockedWorkers`. Typed UI unit/browser checks and the real-stack block/stale-write/403/unblock/success flow use those same versioned fields. |

The website passes 33 unit tests, its build and eight desktop/mobile fixture
Playwright/axe cases. Two real-stack tests pass with the combined backend and
reviewed wide/narrow screenshots. Fixture live skips are not counted as real
workflow evidence. [The fresh-stack record](../../m9-fresh-stack-validation.md)
identifies actual runtime `37cb719` and exact merged-tree equivalence.
[The release record](../../m9-release-validation.md) links the complete merged
PostgreSQL/race/static and website CI gates. Operator key provisioning and
production broker revocation timing remain deployment responsibilities. Task
closure does not authorize M9 or product release.

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

- [x] A Worker proves possession of a stable key during enrollment.
- [x] Reusing another Worker's display name cannot assume its UID/session.
- [x] Blocklisted identity cannot register, refresh, or claim.
- [x] Blocking an active identity takes effect without waiting for session expiry.
- [x] Existing bounded run capability semantics are preserved.
- [x] CLI and UI expose the same blocklist resource fields/actions.

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

- **Implemented:** Private persistent installation key, single-use challenge
  proof, server fingerprint, versioned Runner blocklist and current-state claim
  enforcement. No name-based fallback or migration is supported.
- **Tests added/updated:** Proof/replay/capacity/private-key tests; secured
  block/delete/expiry/renewal/rebind and lock-order tests; CLI blocklist test;
  typed UI and real-stack block/unblock regression.
- **Documentation updated:** Worker security guide, CLI/Worker instructions and
  fresh-stack/release evidence.
- **Validation evidence:** Full merged `d60d902` PostgreSQL/race/static audit and
  website CI pass. Local source and runtime attribution appear in the release
  record. All acceptance criteria above are evidenced.
- **Follow-ups:** Deployment-specific private key provisioning and broker
  lifetime configuration; separate enrollment review in task 005. No remaining
  implementation criterion in this task. Product release remains a maintainer
  decision.
