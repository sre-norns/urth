# M9 release validation

This record identifies the merged source and validation evidence on 2026-10-03.
It does not authorize a release. The maintainer selects product versions and
records the disposition of remaining risks. No shared package release is needed
for the validated changes.

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

## Validated source and checks

| Repository | Source identity | Local evidence | Exact merged-head CI | Disposition |
| --- | --- | --- | --- | --- |
| Urth | `d60d9021dcf52a89e9c8aaba680f5ae403b1671f`; tree `1e86a529399ccfe68b0182dcaaa502f4333e5d60` is identical to tested combined commit `c9720b465ae108abb61a57bf7dc633d175fac377` | Full PostgreSQL/race/static audit for the run-capability change; combined API/NATS/Worker/integration pass at `37cb719`; frontend 33 unit tests, build, eight fixture browser/axe cases, and two real-stack tests pass | [Website](https://github.com/sre-norns/urth/actions/runs/37108548817), [CodeQL](https://github.com/sre-norns/urth/actions/runs/37108548872), [Build And Verify](https://github.com/sre-norns/urth/actions/runs/37108548882) pass at `d60d902` | Source validation passes; remaining criteria below stay explicit |
| Exp-Bench | `dc0069ad65a31e9b3127948e5afb933fbd7b43ac`; tree `45d3224cc0821be467cfbfd255b2ced00f82d603` is identical to tested `1f87384` | Fresh five-role browser/CLI/MCP verifier passes; 13 real-system browser tests pass in 2.1 minutes; 223 fixture browser tests pass in 8.3 minutes; 122 unit tests, formatting/lint/build and focused race/static checks pass | [Website](https://github.com/sre-norns/exp-bench/actions/runs/37108587549) passes; [CI](https://github.com/sre-norns/exp-bench/actions/runs/37108587526) passes, including image publication | Complete merged-source race gate passes in aggregate; verify/vet/static pass |
| Urth broker follow-up | Source `e4c3483b5f33cc1de3bca52de315840a32c07070`; final head `82b23384bf689e4efc5bb494e583e0c2fe069830` adds reviewed runbook wording only; base `d60d902` | Route rollover race count 10 passes in 287.249 seconds; complete PostgreSQL audit passes, including broker 64.745 seconds and integration 144.609 seconds; final sensitivity controls fail as intended | [PR 111](https://github.com/sre-norns/urth/pull/111) is open; review passes; merge and exact merged-head CI pending | Tests/docs only; no production code or dependency change |
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

## Security criterion matrix

| Area | Evidenced behavior | Remaining criterion |
| --- | --- | --- |
| Enrollment / [005](review-backlog/tasks/005-secure-runner-enrollment.md) | Paired Runner identity; authenticated one-time issue and replay/read redaction; revoked token cannot obtain a challenge or enroll; replacement token refreshes the same UID; an existing session retains its own authority | Mounted Runner disable/delete and identity-suspension enrollment/refresh denials; shared token at-rest protection; CLI/UI issue/revoke equivalence and ordinary CLI read/error/log/manifest secret checks; authenticated one-time issuance output is allowed |
| Broker / [004](review-backlog/tasks/004-runner-scoped-nats-credentials.md) | Own consumer/ack/log/presence works; foreign consumer/inbox, own and foreign job publication, event subscription, other-Worker presence, stream/consumer deletion fail; live expiry disconnects; renewal and new-UID reconnect work; session and five-minute lifetime bounds; mTLS/config fail-closed tests; delegated signer overlap/retirement, client/server CA rollover and secured three-node failover pass | PR 111 now proves concrete foreign-log, stream create/update, consumer create and service-role denials plus cluster-route certificate rollover; its merge/exact merged-head CI and deployment verification remain |
| Worker / [009](review-backlog/tasks/009-worker-identity-and-blocklist.md) | Private persistent Ed25519 key; verified two-minute single-use proof; same-name different-key conflict; versioned block/unblock; immediate next-claim denial; bounded broker disconnection; independent in-flight report authority; CLI/UI equivalence | Acceptance evidence complete; deployment-specific key provisioning and revocation timing remain release operations |
| Run capability / [006](review-backlog/tasks/006-harden-run-capabilities.md) | Exact algorithm/key/issuer/audience/time/tenant/executor/dispatch/scope checks; current Result state; bounded artifact grace; multi-replica key overlap/retirement; server-derived linkage; duplicate reporting and race tests | Acceptance evidence complete; operator key distribution and overlap settings remain deployment responsibilities |
| Composed authorization / [024](review-backlog/tasks/024-authorization-integration-scenarios.md) | Shared PostgreSQL/HTTP/JetStream harness covers token revoke/replacement, active block/delete, session expiry, broker expiry/renewal, bounded reports, wrong Result/dispatch and expired status/artifact authority; project grant revocation preserves claimed work | Mounted status/artifact denials for changed stored Runner/Worker executor bindings are not separately evidenced; broker follow-up merge/exact merged-head CI remains |
| OAuth and HTTP tenancy | Mounted credential-purpose, route/catalogue isolation, trusted proxy, Origin, limiter failure/retry and stream membership/session-removal/expiry regressions pass | Production issuer, redirect, proxy trust and provider/mail configuration need deployment review |
| UI foundation | Both brands, wide/narrow layouts, populated routes, axe, theme/unit and package checks pass; real-stack screenshots reviewed | Published images and deployment-specific UI configuration remain untested |

[PR 105](https://github.com/sre-norns/urth/pull/105),
[PR 106](https://github.com/sre-norns/urth/pull/106),
[PR 108](https://github.com/sre-norns/urth/pull/108),
[PR 109](https://github.com/sre-norns/urth/pull/109) and
[PR 110](https://github.com/sre-norns/urth/pull/110) are merged in the source set
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
- [ ] Complete the remaining enrollment and composed broker/HTTP negative matrix
  rows identified above. Broker follow-up cases now pass locally; its merge and
  exact merged-head CI remain pending.
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
- [ ] Complete broker follow-up merge and exact merged-head CI.

## Outstanding risks and ownership

| Risk / remaining work | Owner | Release impact |
| --- | --- | --- |
| Task 005/024 missing cases; task 004 follow-up merge gate | Security validation owners | Enrollment/executor-binding cases remain missing; broker cases now pass in PR 111 and need merged validation |
| Cluster-route certificate rollover | Broker validation owner | PR 111 passes rolling three-node route leaf/trust reload and current delivery/acks; merge/CI and deployment-specific operations remain |
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
This candidate does not prove a production deployment or replace its future
exact merged-head gate. See [the candidate broker runbook](https://github.com/sre-norns/urth/blob/82b23384bf689e4efc5bb494e583e0c2fe069830/docs/m9-broker-operations.md).
