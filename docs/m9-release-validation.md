# M9 release validation

This record identifies the merged source and validation evidence on 2026-10-03.
It does not authorize a release. The maintainer selects product versions and
records the disposition of remaining risks. M9 source acceptance is complete
through Urth `1e37393` and Exp-Bench `929f574`. Product packaging, version
selection and deployment validation remain separate. No shared package release
is needed for the validated changes.

Urth packaging PR [117](https://github.com/sre-norns/urth/pull/117) merges at
`d8f200bf0f252bde73459c8de5f30cd7adc90f49`. Its tree matches the tested PR head,
but the [merged-main audit](https://github.com/sre-norns/urth/actions/runs/37121669063)
detects an intermittent race in the embedded NATS route-reload test. The
[fixture correction](m9-broker-operations.md#route-reload-race-in-the-test-dependency)
retains the race detector and all rotation assertions. Verify the correction's
merged-head checks before selecting the first product tag. No shared package
or production dependency update is required for this test-only correction.

## Installation and dependency contract

Use a fresh PostgreSQL database and fresh NATS storage. Deploy API, Worker, CLI
and website from one validated source set. No existing-resource reader,
enrollment fallback, resource migration or NATS subject migration is supported.
Historical ADR and backlog migration text describes earlier design work.

| Dependency | Validated published pin | Evidence |
| --- | --- | --- |
| wyrd | `v0.7.0` in both products | `GOWORK=off`, no `replace`; module verification passes |
| wyrd identity | `v0.7.2` in both products | Product identity regressions and fresh identity workflows pass |
| components | `0.5.0` in both product lockfiles | Authenticated package install and product builds pass; source checks below |
| Vitest | Urth `4.1.11`; Exp-Bench `5.0.2` | Urth receives the minimal advisory patch; [fresh-stack record](m9-fresh-stack-validation.md#development-dependency-correction) |
| PostgreSQL | 18 | Disposable task databases and the merged Urth CI service |
| NATS | Fresh loopback container `2.10`; secured embedded server `2.15.0` | Development execution and secured operational tests remain distinct environments |

M9 requires a shared patch only if a reproduced shared defect needs one. Record
and validate any new published pin in both consumers before release selection.

## Final merged acceptance checkpoint

Urth PRs [113](https://github.com/sre-norns/urth/pull/113),
[114](https://github.com/sre-norns/urth/pull/114) and
[115](https://github.com/sre-norns/urth/pull/115) merge as `0453da8`, `65f34d7`
and `1e37393dc8ea2b3d69b8d98dcdda42f14aaf3c1f`. All checks at the final combined
commit pass: [PostgreSQL/race/static/build](https://github.com/sre-norns/urth/actions/runs/37116653885),
[website](https://github.com/sre-norns/urth/actions/runs/37116653883) and
[CodeQL](https://github.com/sre-norns/urth/actions/runs/37116653809). The earlier
two main-branch runs are cancelled when later merges supersede them; they are
not counted as passes.

The two new backend test files match their reviewed PR heads. CLI and website
files match tested PR 115 head `ad1bd16`; the other changes from that head are
backend tests and documentation. The exact merged CI runs all backend cases
together. The prior live client passes remain attributed to their actual
candidate source, not to a new live run at the merge commit.

The [client record](m9-enrollment-client-validation.md) proves reciprocal
CLI/browser issue and revoke, optional expiry, replay redaction, one-time dialog
dismissal, secret-free ordinary reads/errors/logs/storage and compatibility of
`runners token`. The final live case passes twice, in 9.4 and 9.0 seconds.
Its full audit and uncached race suite pass (integration 125.791 seconds).
The backend enrollment and executor matrix records retain their independent
full audits and fault controls. Evidence lives in
`~/workspace/m9-review/enrollment-followup/`, `executor-binding-followup/`
and `enrollment-clients/`.

Exp-Bench `929f5743bd1c4ca91a3c7d6dec87377faafe1381` also passes exact merged
[CI and image publication](https://github.com/sre-norns/exp-bench/actions/runs/37112147342)
and [website checks](https://github.com/sre-norns/exp-bench/actions/runs/37112147447).
Its change from the validated `dc0069a` is the release record only. The earlier
race and live evidence below keeps its original source attribution.

Tasks 004, 005, 006, 009 and 024 are complete against their current acceptance
criteria. This closes the source-validation work; it does not claim that a
production deployment or a tagged product release exists.

## Validated source and checks

| Repository | Source identity | Local evidence | Exact merged-head CI | Disposition |
| --- | --- | --- | --- | --- |
| Urth earlier baseline | `d60d9021dcf52a89e9c8aaba680f5ae403b1671f`; tree `1e86a529399ccfe68b0182dcaaa502f4333e5d60` is identical to tested combined commit `c9720b465ae108abb61a57bf7dc633d175fac377` | Full PostgreSQL/race/static audit for the run-capability change; combined API/NATS/Worker/integration pass at `37cb719`; frontend 33 unit tests, build, eight fixture browser/axe cases, and two real-stack tests pass | [Website](https://github.com/sre-norns/urth/actions/runs/37108548817), [CodeQL](https://github.com/sre-norns/urth/actions/runs/37108548872), [Build And Verify](https://github.com/sre-norns/urth/actions/runs/37108548882) pass at `d60d902` | Baseline evidence retained; final follow-up acceptance is recorded above |
| Exp-Bench | `dc0069ad65a31e9b3127948e5afb933fbd7b43ac`; tree `45d3224cc0821be467cfbfd255b2ced00f82d603` is identical to tested `1f87384` | Fresh five-role browser/CLI/MCP verifier passes; 13 real-system browser tests pass in 2.1 minutes; 223 fixture browser tests pass in 8.3 minutes; 122 unit tests, formatting/lint/build and focused race/static checks pass | [Website](https://github.com/sre-norns/exp-bench/actions/runs/37108587549) passes; [CI](https://github.com/sre-norns/exp-bench/actions/runs/37108587526) passes, including image publication | Complete merged-source race gate passes in aggregate; verify/vet/static pass |
| Urth broker follow-up | Source `e4c3483b5f33cc1de3bca52de315840a32c07070`; final head `82b23384bf689e4efc5bb494e583e0c2fe069830` adds reviewed runbook wording only; base `d60d902` | Route rollover race count 10 passes in 287.249 seconds; complete PostgreSQL audit passes, including broker 64.745 seconds and integration 144.609 seconds; final sensitivity controls fail as intended | [PR 111](https://github.com/sre-norns/urth/pull/111) merges as `f93767a`; subsequent docs merge `4990b229` passes [Build And Verify](https://github.com/sre-norns/urth/actions/runs/37112080233), [Website](https://github.com/sre-norns/urth/actions/runs/37112080193) and [CodeQL](https://github.com/sre-norns/urth/actions/runs/37112080155) | Tests/docs only; no production code or dependency change |
| components | `49c16562b9de8c5dd4906cd54ba22cd591341717`; package `0.5.0` tag `224bb6c5aca62a4fa3b7baf726bc19573372dacb`; only README differs | 177 unit/theme tests in 21 files pass in 68.31 seconds; lint/typecheck/build pass; 82 browser/axe/snapshot tests pass in 2.3 minutes; package has 81 expected files | [CI](https://github.com/sre-norns/norns-components/actions/runs/37085957314) passes at `49c1656` | No package source change or new version required |

The Urth merged backend workflow runs `make audit/postgres` with PostgreSQL 18.
It includes module verification, vet, staticcheck and the complete race test
suite. Its formatting/tidy and binary build jobs also pass. The merged-source pinned
`govulncheck` scans pass with zero reachable vulnerable symbols. Urth still
reports one advisory in an imported package and a required module; Exp-Bench
reports none in imported packages and one in a required module. This does not
claim an advisory-free dependency graph. The exact scan logs are
`m9-urth-merged-vuln.log` and `m9-exp-merged-vuln.log` in the preserved
review directory below.
The local combined
run is a selected-package check, not a second full audit: API 3.060 seconds,
NATS 42.317 seconds, Worker 10.791 seconds and integration 351.006 seconds.

The final Urth live run uses API, Worker and CLI binaries built at
`37cb719c0eb9e3a3efa83075b408d62b947c32c0`, with `vcs.modified=false`, plus the
frontend from [PR 110](https://github.com/sre-norns/urth/pull/110). The combined
source tree `c9720b4` contains that backend and frontend and equals merged
`d60d902`. This source equivalence does not change the recorded runtime SHA or
claim a second live run at the merge commit. The temporary combined branch is
removed; the commit/tree identities remain the evidence.

Detailed local evidence is preserved under
`/home/soultaker/workspace/m9-review/followup-2026-10-03`, including
`m9-capability-audit.log`, `m9-combined-integration.log`, broker race logs,
`urth-fresh/`, `m9-exp-canonical.log`, `m9-exp-system-canonical-full.log`,
`m9-exp-browser-final.log` and `m9-components-evidence/validation.json`.
These paths identify maintainer review evidence; they are not portable CI links.
Earlier failed attempts remain recorded. Components first reports 171 passes
and six failures under concurrent load, then passes unchanged with two workers.
The Urth record retains its earlier timeout attempts and explicit fixture skips.
Exp-Bench's `GOWORK=off go test -race -count=1 ./...` at `dc0069a` passes
all packages except `internal/server`, which hits the default ten-minute package
timeout near its final web UI test. There is no assertion or race failure. The
failed complete attempt is retained as `m9-exp-merged-race.log`.
`GOWORK=off go test -race -count=1 -timeout=20m ./internal/server` then passes
unchanged source in 562.996 seconds, recorded in
`m9-exp-merged-server-race.log`. The complete race gate passes in aggregate
across those two runs; this is not one successful whole-suite invocation.
The API package passes in the initial run in 246.069 seconds. Module
verification, vet and pinned static checks also pass at the merged source.
Exp-Bench's CI test command does not enable race detection; the independent
local race evidence closes that check separately.

## Enrollment backend follow-up

`enrollment_lifecycle_test.go` runs against the mounted API and a private
PostgreSQL schema. All six Runner disable/delete or identity-suspension pairs
first prove successful enrollment and refresh. Each case then obtains an
unused proof, changes state through the API and receives 401 from both challenge
and enrollment. Worker records remain unchanged after refusal.

The storage case checks the published identity `v0.7.2` SHA-256 verifier and
plaintext exclusion in credential/token rows. Stored verifiers, IDs and rows
all fail as enrollment authority. The original secret and same unused proof
still enroll. Serialized Runner/Worker resources and normal identity/token
reads exclude the secret. No shared implementation or dependency change is
required. CLI/UI lifecycle and log secrecy now have merged evidence from
PR 115, linked above.

Focused race checks pass in 14.032 seconds. The complete
`GOWORK=off make audit/postgres` passes with module verification, vet,
Staticcheck 2026.2.1 and race tests. Broker and integration packages pass in
66.334 and 152.776 seconds. This local evidence is on base `4990b229`;
PR 113 is now merged and final `1e37393` CI passes. Logs are preserved
in `~/workspace/m9-review/enrollment-followup/m9-enrollment-focused.log` and
`m9-enrollment-audit.log`.

## Security criterion matrix

| Area | Evidenced behavior | Remaining criterion |
| --- | --- | --- |
| Enrollment / [005](review-backlog/tasks/005-secure-runner-enrollment.md) | Paired Runner identity; authenticated one-time issue and replay/read redaction; revoked token cannot obtain a challenge or enroll; replacement token refreshes the same UID; an existing session retains its own authority | Acceptance complete: all six state/operation pairs, stored verifier replay refusal, serialized resource secrecy and live CLI/UI issue/revoke/output checks pass. Exact merged CI passes; deployment-specific checks remain |
| Broker / [004](review-backlog/tasks/004-runner-scoped-nats-credentials.md) | Own consumer/ack/log/presence works; foreign consumer/inbox, own and foreign job publication, event subscription, other-Worker presence, stream/consumer deletion fail; live expiry disconnects; renewal and new-UID reconnect work; session and five-minute lifetime bounds; mTLS/config fail-closed tests; delegated signer overlap/retirement, client/server CA rollover and secured three-node failover pass | Merged PR 111 proves concrete foreign-log, stream create/update, consumer create and service-role denials plus cluster-route certificate rollover. Exact merged-head CI passes; deployment verification remains |
| Worker / [009](review-backlog/tasks/009-worker-identity-and-blocklist.md) | Private persistent Ed25519 key; verified two-minute single-use proof; same-name different-key conflict; versioned block/unblock; immediate next-claim denial; bounded broker disconnection; independent in-flight report authority; CLI/UI equivalence | Acceptance evidence complete; deployment-specific key provisioning and revocation timing remain release operations |
| Run capability / [006](review-backlog/tasks/006-harden-run-capabilities.md) | Exact algorithm/key/issuer/audience/time/tenant/executor/dispatch/scope checks; current Result state; bounded artifact grace; multi-replica key overlap/retirement; server-derived linkage; duplicate reporting and race tests | Acceptance evidence complete; operator key distribution and overlap settings remain deployment responsibilities |
| Composed authorization / [024](review-backlog/tasks/024-authorization-integration-scenarios.md) | Shared PostgreSQL/HTTP/JetStream harness covers token revoke/replacement, active block/delete, session expiry, broker expiry/renewal, bounded reports, wrong Result/dispatch and expired status/artifact authority; project grant revocation preserves claimed work | Acceptance complete: production-issued capabilities fail both mounted status paths and artifact creation after stored Runner/Worker binding changes, with same-token restored-binding positives. Exact merged CI passes |
| OAuth and HTTP tenancy | Mounted credential-purpose, route/catalogue isolation, trusted proxy, Origin, limiter failure/retry and stream membership/session-removal/expiry regressions pass | Production issuer, redirect, proxy trust and provider/mail configuration need deployment review |
| UI foundation | Both brands, wide/narrow layouts, populated routes, axe, theme/unit and package checks pass; real-stack screenshots reviewed | Published images and deployment-specific UI configuration remain untested |

[PR 105](https://github.com/sre-norns/urth/pull/105),
[PR 106](https://github.com/sre-norns/urth/pull/106),
[PR 108](https://github.com/sre-norns/urth/pull/108),
[PR 109](https://github.com/sre-norns/urth/pull/109) and
[PR 110](https://github.com/sre-norns/urth/pull/110) and follow-ups 111–115 are merged in the source set
above. Task closure records apply to their acceptance criteria. They do not
close the product release or unrelated backlog.

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
   secrets consistently across API replicas. Configure key IDs and verification
   overlap before rotation. Unset keys are ephemeral. See the
   [signing settings](../cmd/api-server/README.md#worker-and-run-signing-keys).
   Keep API service NATS credentials and account signing seeds separate from
   Worker files. Require TLS for clients and cluster routes.
6. Trigger a Scenario. Verify dispatch, claim, execution, authenticated live logs,
   stored logs and artifacts. Review classified secret-bearing artifact access
   and retention; automatic retention remains feature backlog.
7. Verify API outbox/JetStream/dead-letter and Worker metrics. Choose the Worker
   metrics listener and access explicitly. Verify mail, relay and reconciler
   failures produce observable signals.
8. Exercise token, grant and session revocation, Worker blocking and unblock,
   with stale-version rejection. Record next-claim denial, active broker expiry
   and already-claimed reporting separately.

## Candidate gates

- [x] Record exact source trees, published pins and module verification.
- [x] Complete Urth PostgreSQL/race/static audit. Merged CI runs the database
  suite; skipped unconfigured local database tests are not counted as passes.
- [x] Run Urth website units/build/fixture Playwright/axe and real-stack tests;
  review wide/narrow screenshots.
- [x] Run focused run-capability, Worker proof and secured broker lifecycle tests.
- [x] Complete the enrollment and composed broker/HTTP matrix, including client
  lifecycle and executor-binding follow-ups. PRs 113/114/115 are merged and all
  exact-head checks pass at `1e37393`.
- [x] Validate fresh Urth registration/mail/password/fake-IdP/device login,
  project/Runner/token/grant/Scenario/execution/log/artifact flow.
- [x] Validate Exp-Bench's fresh browser/CLI/MCP five-role canonical workflow and
  real-system identity suite on its own task database.
- [x] Run components unit/theme, lint/typecheck/build and browser/axe checks;
  review both brands and verify product package installs.
- [x] Complete Exp-Bench merged-source race, module verification, vet and static
  checks; retain the initial timeout and the separate package rerun.
- [x] Verify all required CI jobs on both exact merged product commits, including
  the Exp-Bench image job; components source-head CI passes as linked above.
- [ ] Record maintainer disposition of unresolved criteria and deployment risks
  before selecting versions.
- [x] The previous fresh-stack and components runs remove their task-owned stacks
  and private token/session/key/mail files and preserve non-secret evidence.
  User databases and brokers stay running.
- [x] The Exp-Bench follow-up race database and task services are removed; final
  non-secret logs are preserved.
- [x] Complete the broker follow-up full PostgreSQL/race/static audit, ten-run
  route rollover check and final-source sensitivity controls.
- [x] Remove the broker task database, private test directories and worktree;
  preserve sanitized evidence. User NATS/PostgreSQL remain intact.
- [x] Complete broker follow-up merge and exact merged-head CI at `4990b229`.

## Outstanding risks and ownership

| Risk / remaining work | Owner | Release impact |
| --- | --- | --- |
| M9 source acceptance | Complete | Tasks 004/005/006/009/024 have mapped acceptance evidence and passing combined merged CI. Product release and deployment remain separate |
| Cluster-route certificate rollover | Broker validation owner | PR 111 passes rolling three-node route leaf/trust reload and current delivery/acks; merged CI passes; deployment-specific operations remain |
| Production deployment profile | Maintainer / operator | HTTPS/authenticated client and route TLS, private signing keys, replica configuration, external mail/provider, images, metrics and failure alerts require deployment review |
| Product version selection | Maintainer | No versions or releases are authorized by this record |
| Shared identity provider confirmation copy | Identity owner | Urth intermediate provider page uses Exp-Bench wording; workflow passes; separate cosmetic correction |
| Runner channel policy (008) | Runner-policy owner | Separate P0 product backlog; outside this Worker-proof/broker change |
| Scheduled execution | Scheduler design owner | Required for v1.0; M9 does not implement it |
| Prober defaults (017), artifact retention and other operator features | Relevant backlog owners | Separate work; no automatic closure from M9 |

## Broker follow-up provenance

[PR 111](https://github.com/sre-norns/urth/pull/111) adds tests and the operation
runbook on base `d60d902`. It changes no production code, resource format,
default or dependency. Source `e4c3483` passes the complete
`GOWORK=off make audit/postgres` on a disposable PostgreSQL database, including
repository vet, Staticcheck 2026.2.1, module verification and race tests. The
final head `82b2338` changes reviewed runbook wording only. Ten route rollover
race repetitions pass in 287.249 seconds. Full-suite broker and integration
packages pass in 64.745 and 144.609 seconds.

The route test rolls three leaf certificates with overlapping old/new trust,
forces fresh pooled and system routes, checks current replicas and confirms
cross-node delivery/acks. It rejects retired-root incoming and outgoing TLS.
Established routes do not authenticate again merely because TLS files reload.
Final-source controls that omit leaf reload or retain the old root fail at the
intended assertions. Earlier simultaneous route-disruption readiness failures
remain recorded; rolling convergence corrects that fixture. The new service
and Worker tests attempt concrete foreign-log, stream create/update, consumer
create and cross-role forbidden operations with positive controls. Consumer
update uses the same `CONSUMER.CREATE` subject; no separate update endpoint or
explicit consumer-update positive is claimed.

Evidence is preserved under
`/home/soultaker/workspace/m9-review/cluster-route-followup/`.
PR 111 is merged and exact merged-head checks pass at `4990b229`. This evidence
does not prove a production deployment. See [the candidate broker runbook](https://github.com/sre-norns/urth/blob/82b23384bf689e4efc5bb494e583e0c2fe069830/docs/m9-broker-operations.md).
