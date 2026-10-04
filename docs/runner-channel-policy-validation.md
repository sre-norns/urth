# Runner channel policy validation

Date: 2026-10-04, Australia/Sydney. Baseline:
`50aa0d5bea8aa828637bae1e5a6819e3a4538187`. Candidate branch:
`feat/runner-channel-policy`. Implementation review remains in progress for
backlog tasks 008 and 022. The maintainer reviews and merges the change.

This work adds an explicit Runner policy for fresh installations. Obsolete Runner
`requirements` receives a clear error. Scenario `requirements` remains the
placement selector. The change has no migration window, shared package release,
release tag or deployment.

## Boundary evidence

`test/integration/channel_policy_test.go` uses PostgreSQL, scoped HTTP, an
embedded NATS broker and the production Worker loop. Positive controls accompany
refusals.

| Boundary | Evidence |
| --- | --- |
| Job admission | A matching Runner rejects a probe kind. Preview and creation report the same refusal. No dispatch reaches the outbox. A duration filter retains the other authorized eligible Runner. |
| Worker enrollment | Valid proof cannot bypass missing probe coverage, version or duration constraints. Compatible enrollment stores effective capabilities. |
| Worker refusal visibility | The Worker receives a generic refusal. An operator reads the fingerprint, reason and timestamp from Runner status. The rejection does not change the policy ETag or create Worker authority. Client uploads cannot forge that status on create or update. |
| Concrete claim | Claims use stored capabilities even when the request supplies forged capabilities. An incapable Worker leaves the Result pending. Another capable Worker receives the dispatch and executes once. |
| Duration | A shorter requested budget receives a retryable refusal without a lease. A larger request receives the immutable job budget. A server ceiling below the required budget records a terminal refusal without a lease. Worker execution retains the full valid budget. |
| Current placement | Relabelling the Runner prevents the pending claim. The terminal reason is visible to operators. The Worker acknowledges the dispatch without executing the probe. Empty and unchanged selectors still work. Malformed snapshots fail closed. HTTP refusal bodies omit the reason. |
| In-flight recovery | An idempotent claim keeps its authorization after the Runner policy changes. Existing two-Worker races, lost-response recovery, grants, revocation, pause, block, expiry and UID-generation regressions remain in the full suite. |
| Concurrency | Five concurrent capability refresh and operator-pause attempts preserve both the refreshed declaration and pause state. |
| Provenance | Result Runner UID, name, version and propagated labels remain the scheduling snapshot. Claim preserves them. Forged Artifact identity and propagated labels lose to server-derived values. |
| Clients | CLI manifests and read-only capabilities use the same schema. The website displays policies, capabilities, provenance and admission refusals. Edits retain the ETag. Live browser tests use the real API, broker and Worker. |

Parser and model tests cover semantic ordering of `1.9.0` and `1.10.0`,
prereleases, unknown versions, inclusive duration bounds, empty policies,
incoherent intervals, reserved propagation keys and strict JSON/YAML fields.
Native profile tests exclude browser execution. Runtime discovery reports only
installed executables and preserves raw versions.

## Baseline control

A clean archive of the baseline runs the real relabelled-Runner claim test. The
claim returns HTTP 200 and starts a job after its required segment changes. The
new test fails on that result. The candidate records a terminal refusal instead.

Additional raw new-schema controls receive HTTP 400 on the baseline because
`jobRequirements` and `workerRequirements` are unknown. This proves the old wire
contract cannot express the new policy. It does not prove policy enforcement on
that unsupported schema.

## Local verification

Go: `go1.27.1 linux/amd64`. Checks use `GOWORK=off` and published dependencies.
PostgreSQL uses task-owned container `policy-postgres-008`, loopback port 15438,
and a fresh `policy` database. Tests create private schemas. Domain storage tests
use the disposable public schema. Live browser tests use a separate `policy_live`
database. User services on 5432, 4222, 8222, 8080 and 3001 remain untouched.

```sh
GOWORK=off make audit/postgres store-url='<task-owned PostgreSQL URL>'
GOWORK=off go test -race -tags urth_native ./pkg/runner ./pkg/worker
GOWORK=off go test -race ./pkg/urth -count=1
GOWORK=off go vet ./pkg/urth ./test/integration
git diff --check
```

The final PostgreSQL audit passes: module verification, vet, staticcheck and all
race suites, including domain 25.608 seconds and integration 245.983 seconds.
A subsequent create-status ownership hardening receives an exact-source full
domain race rerun and scoped HTTP ownership tests. The implementation PR CI
provides the exact committed-source full gate.
The final native race suites pass: Runner 1.144 seconds, Worker 7.905 seconds.
The exact final domain race suite passes in 25.345 seconds. Exact final domain
and integration vet passes. Scoped HTTP rejection-history ownership and
proof-refusal tests pass with race detection in 5.672 seconds.

Website fixture tests, production build, fixture Playwright checks, held-SSE live
Worker checks and live CLI/website enrollment checks pass. Live checks use API
18087, website 13007 and NATS 14227. The UI owner removes its task services and
live database after validation.

## Attempted failures and repairs

The claim transaction initially locks a Worker and updates presence through a
separate connection. Boundary tests expose a self-deadlock. Presence now uses
the claim transaction, and Worker locking prevents concurrent lock upgrades.

Old short-lease fixtures require a default one-minute job under a one-millisecond
server ceiling. Fixtures now use a concrete one-millisecond probe timeout. Old
M9 enrollment fixtures now declare explicit capabilities. A missing Worker keeps
its HTTP 403 refusal after the new lock check.

Staticcheck findings in new policy files are repaired before reruns. One
intermediate audit compiles while an obsolete test is removed and reports a
stale generated test reference. Superseded audits stop after their task-owned
PIDs are verified. Temporary private files from interrupted tests are removed
only from paths recorded by those test processes. Sandboxed broker tests fail
when local listeners are unavailable; approved local-network reruns pass.

## Remaining review actions

Record the implementation commit and PR, verify exact PR CI, and obtain
maintainer review. Keep tasks 008 and 022 in progress until those review actions
are complete. Record merged-main CI after merge. Publication requires separate
authorization. Task 020 selector semantics remain separate and unchanged.
