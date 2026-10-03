# M9 release validation

This document is a release checklist, not a completed security audit. Record the
exact release candidate and evidence for each gate. A successful PR check does
not prove the merged commit passes. The maintainer reviews and merges PRs and
selects product versions after validation. No release is authorized by this file.

## Installation and dependency contract

Use a fresh PostgreSQL database and fresh NATS storage. Deploy API, Worker, CLI
and website from one validated release set. No existing-resource reader,
enrollment fallback, resource migration or NATS subject migration is supported.
Historical ADR and backlog migration text describes earlier design work.

| Dependency | Published baseline | Required verification |
| --- | --- | --- |
| wyrd | `v0.7.0` | `GOWORK=off`, no `replace`, module verification |
| wyrd identity | `v0.7.2` | Same published version in Urth and Exp-Bench; rerun consumer regressions after a shared patch |
| components | `0.5.0` | Lockfile and package access in both product repositories |
| PostgreSQL | 18 | Task-owned database; use no development data |

M9 requires a shared patch only if a reproduced shared defect needs one. Record
and validate any new published pin in both consumers before closing the gate.

## Evidence matrix

| Area | Completed baseline | Superseded requirement | Outstanding release evidence |
| --- | --- | --- | --- |
| Enrollment | Paired Runner machine identity; shared token issue/revoke; canonical one-time secret/replay redaction | Separate enrollment store, public token route and automatic token on Runner creation | Mounted unauthorized issuance, token revocation/refresh, disable/delete and storage/secrecy checks; task 005 |
| Broker permissions | Scoped Worker JWTs; secured own-consumer and cross-account denials in `pkg/natsq/credentials_test.go` | Sharing the API service credentials path with Workers | Full subject/admin denial matrix, Worker/session expiry bound, live expiry, renew/reconnect, TLS and service-role least privilege; task 004 |
| Worker identity | Machine token proves Runner enrollment authority | Display name as identity evidence | Persistent installation key, nonce proof, fingerprint, versioned block/unblock, next-claim denial and bounded broker revocation; task 009 |
| HTTP tenancy | Existing `test/integration/tenancy_test.go` and `runlogs_test.go` | Flat resource adapters and unscoped product routes | Credential/route negatives, UID/name routes, catalogues, SSE, artifacts and internal authority; task 024 and authorization review |
| Claimed execution | Grant revocation preserves a previously issued bounded run capability | Immediate cancellation of all actions on grant revocation | Worker deletion/block, session expiry, wrong capability bindings and lease expiry; task 024 |
| OAuth | Shared database-backed limiter is mounted | Adding a second product-local limiter | Product route coverage, peer/proxy trust, Origin checks, retry and store-failure behavior |
| UI foundation | Theme roles, contrast checks, browser cascade and axe tests exist | Building new primitives for each product | Both product screenshots, narrow layouts, populated/error states and package access |

The named tests are source evidence. Rerun them on the candidate and record the
result. Do not mark task 004/005/009/024 done from this table alone.

## M9 candidate additions

[Authorization PR 105](https://github.com/sre-norns/urth/pull/105) adds stored
Result bindings, strict run-capability claims and state checks, artifact row
locking, live-log access checks, and trusted-proxy configuration. It is merged
as `f97750a83cd813bae4232e100d2aacd728ac79ea`. Its full
`GOWORK=off make audit/postgres` run passes at `200bf30`; `806c35c` adds
clarifications to its evidence document. The suite includes mounted credential,
OAuth, item/catalogue, run-state and open-stream regressions with PostgreSQL.
Exact merged-head CI and fresh-stack release checks remain open.

[Worker PR 106](https://github.com/sre-norns/urth/pull/106), reviewed at
`609e1294d35d04abd8c3fa2090a91ab9604b206b`, adds installation-key proof, a persistent
verified fingerprint, versioned Runner block/unblock and separate provisioner,
publisher and observer NATS identities. Focused proof/blocklist tests pass. The
secured PostgreSQL/HTTPS/mutual-TLS broker tests pass in 13.524 seconds. They
cover block/delete claim denial, bounded completion after deletion,
claim/block commit order, same-UID renewal and new-UID transport replacement.
Final focused tests also prove status and artifact reporting after deletion,
revoked-token proof/enrollment denial, old-session claims and replacement-token
renewal with the same UID.

The running Worker proves new-UID presence, a second claim/report and completion
of its original run. Focused CLI/session-expiry checks also pass. The Worker
guide applies to this candidate. The typed Block/Unblock UI passes all 33 unit
tests and its build. All eight desktop/mobile Playwright and axe cases pass;
wide/narrow screenshots are reviewed. The dialogs use versioned writes and
preserve other blocks after a stale-draft recovery. The implementation owner
reports the complete PostgreSQL/race suite and verify/vet/static checks pass at
`7cfdbbf`; rebasing to `609e129` changes upstream documentation only. Its Go/UI
source is identical to that validated source. Operational rotation/failover,
Worker merge, exact merged-head CI and fresh-stack gates remain open.

The evidence matrix above records the merged M8 baseline and remaining release
requirements. Candidate source changes alone do not close those requirements.

## Provision and operate a fresh stack

1. Install the published package versions. GitHub Packages needs `read:packages`
   locally. Product Actions workflows use `packages: read`; grant each repository
   package Actions access. Keep tokens outside source and image layers.
2. Start a new PostgreSQL database and authenticated JetStream broker. Use
   persistent storage and configured limits for the production profile.
3. Set the public HTTPS identity issuer, exact browser redirect URI and mail
   provider. Provision the first account owner. Remove bootstrap passwords after
   provisioning. Verify registration email and password/provider sign-in.
4. Complete `urthctl auth login`. Set a project context. Create a Runner and its
   project grant, issue a machine token, and put it in a private Worker token file.
   See the [quick start](../README.md#quick-start) and
   [Worker setup](../cmd/nats-worker/README.md#running-it).
5. Configure independent persistent Worker-session and run-capability signing
   secrets consistently across API replicas. Unset keys are ephemeral. Configure
   the run key ID and verification overlap before rotation. See the
   [signing settings](../cmd/api-server/README.md#worker-and-run-signing-keys).
   Keep API service NATS credentials and its account signing seed separate from
   Worker files. Configure the resolver and service permissions. Require TLS for
   client and cluster routes. Explicit insecure mode is local development only.
6. Trigger a Scenario. Verify dispatch, claim, execution, authenticated live logs,
   stored logs and artifacts. Check that classified secret-bearing artifacts
   receive the documented access and retention treatment; automatic retention
   remains feature backlog.
7. Observe API outbox/JetStream/dead-letter metrics and Worker claim, ack and run
   metrics. Worker metrics open no listener by default; choose its address and
   access explicitly. Verify mail, relay and reconciler failures are observable.
8. Exercise token revocation, project-grant revocation, session revocation,
   Worker blocking and unblock with stale-version rejection. Record exact next-
   claim, active broker and already-claimed reporting behavior separately.

## Candidate gates

- [ ] Record Git SHAs, package versions, lockfiles and module verification.
- [ ] Run `GOWORK=off make audit/postgres store-url="$URTH_RELEASE_DATABASE"`
  on a disposable database. Run the complete PostgreSQL/race/static suite. A
  skipped database suite is not a pass.
- [ ] Run website unit/build/Playwright/axe checks and the real-stack test in
  [website/README.md](../website/README.md). Inspect both wide and narrow layouts.
- [ ] Run focused authorization, Worker proof and secured broker lifecycle tests.
  Attach denial assertions and positive controls to each outstanding matrix row.
- [ ] Validate the fresh Urth registration/mail/password/fake-IdP/device-login,
  project/Runner/token/grant/Scenario/execution/log/artifact flow.
- [ ] Rerun Exp-Bench's checked-in browser/CLI/MCP five-role canonical verifier on
  its own fresh database. See its `website/README.md`.
- [ ] Run components unit/theme, lint/typecheck/build and Playwright/axe checks.
  Inspect Urth and Exp-Bench brands visually.
- [ ] Verify all required CI jobs on each exact merged commit. Record run URLs.
- [ ] Record unresolved risks and maintainer disposition before version selection.
- [ ] Stop only task-owned services. Remove private token/session files and task
  databases. Preserve review evidence without credentials.

## Candidate record

Leave an incomplete field explicit. Do not fill a result from a different head.

| Repository | Git SHA | Published pins | Local test evidence | Merged-head CI | Result |
| --- | --- | --- | --- | --- | --- |
| Urth | pending | baseline above | pending | pending | open |
| Exp-Bench | pending | baseline above | pending | pending | open |
| components | pending | `0.5.0` baseline | pending | pending | open |

## Outstanding risks and ownership

| Risk / remaining work | Owner | Release impact |
| --- | --- | --- |
| Tasks 004/005/009/024, remaining run-capability criteria (006) and composed authorization gaps | Security implementation owners; maintainer validates evidence | Record dispatch binding and key rotation separately; do not close the full tasks from candidate tests |
| Runner channel policy (008) | Runner-policy owner, assigned by maintainer | Separate P0 product backlog; outside M9 Worker-proof/broker implementation |
| Exact merged-head CI and full fresh-stack validation | Maintainer/release validator | Product version and release remain pending |
| Scheduled execution | Scheduler design owner, assigned by maintainer | Required for v1.0; M9 does not implement it |
| Prober defaults (017), retention and unrelated operator features | Relevant backlog owner, assigned by maintainer | Record separately; no automatic closure from M9 |
