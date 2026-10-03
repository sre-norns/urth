# 024: Exercise Authorization End to End

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `blocked` |
| Priority | `P1` |
| Workstream | Authentication |
| Depends on | 004, 005, 006, 009, 011 |
| Likely conflicts | 004, 005, 006, 009 |
| Owner | Unclaimed |

## Current evidence and remaining criteria (2026-10-03)

The shared task 011 PostgreSQL/HTTP/JetStream harness runs the M9 composed tests.
Urth `d60d902` passes the complete merged backend gate. Keep this task open for
an explicit mounted capability-binding matrix and broker follow-up merged validation below.

| Scenario | Evidence / exact remaining check |
| --- | --- |
| Enrollment revocation and replacement | `TestMachineTokenRevocationDeniesFreshProofButPreservesSession` attempts challenge/enrollment with an unused valid proof after revocation and receives 401; prior session claim succeeds; replacement token renews the same UID. Individually revoked shared tokens supersede generation rotation. |
| Worker block/delete during claimed work | `TestWorkerBlockAndDeleteComposeWithSecuredBroker` attempts next claim after block and deletion and receives 403; already issued capability completes status and artifact reporting. A blocked connected broker identity closes by issued expiry. This preserves the selected bounded reporting contract. |
| Worker session expires during execution | `TestExpiredWorkerSessionKeepsOnlyBoundedInFlightAuthority` claims before expiry, attempts a later claim with the expired session and receives 403, then completes the original Result with its independent capability. |
| Renewal and new registration UID | Runtime renewal/rebind tests keep a held probe running, reconnect with current credentials, publish new-UID presence, claim/report a second run and complete the original run. |
| Runner broker boundaries | Secured own consumer/ack/log/presence controls and foreign consumer/inbox/jobs/events/deletion negatives pass. PR 111 proves valid concrete foreign-log, stream create/update, consumer create and service-role denials; its full audit passes, pending merge/exact merged-head CI. |
| Result/executor/deadline/scope bindings | Mounted tests attempt wrong Result status, changed dispatch on status/artifact, credential-purpose confusion and expired lease/status/artifact reporting. Validator tests reject wrong Runner/Worker/tenant and missing scope. Open: use a production-issued capability after stored Runner/Worker bindings change; attempt mounted status and artifact operations with denial status classes and unchanged-binding positive controls. Unit claims mutations are not this composed route evidence. |
| Grant and log revocation | Tenancy tests deny new work after grant removal while claimed work completes. Mounted open-stream tests close after membership/session removal or session expiry. |

Each negative above records an attempted protected operation. The mounted HTTP
checks assert response status classes; broker checks assert permission refusal
and confirm permitted operations. The harness shares production issuance,
fixtures and diagnostic mechanisms with task 011. Complete merged CI executes
these tests against PostgreSQL 18; the secured scenarios use real HTTPS and
mutual-TLS NATS, not canned claim responses.

Historical deletion text below proposes blanket status refusal. The current
contract supersedes it: block/delete deny new claims, broker authority expires
within its bound, and previously claimed reporting remains authorized only
within Result state/deadline. The session-expiry scenario has actual mounted
claim denial and successful bounded completion; it is no longer an open claim.

[The release record](../../m9-release-validation.md) attributes PRs 105, 106,
108, 109 and 110, exact merged CI and source equivalence. The fresh-stack tests
provide independent process/browser execution evidence. Deployment-specific
rotation/transport configuration still requires operator review. These facts
do not fill the missing executor-binding route attempts.

## Historical review baseline and requirements under reconciliation

## Why This Matters

Split out of [task 011](011-nats-worker-failure-integration-tests.md) on
2026-07-29. That task carried both halves of the ADR 0004 failure matrix — crash
boundaries and authorization boundaries — and so was blocked on the entire P0
security workstream. The crash boundaries never needed it, and holding them
behind work that has not started put the project's largest validation gap last in
the queue. This task is the half that genuinely does need it.

Authorization in Urth is four credentials with narrowing authority — enrollment,
Worker session, NATS connection, run capability — and the property that matters
is not that each one validates, but that **revoking or expiring one stops the
thing it authorizes, at the moment it is revoked, across a process boundary**.
That is not observable from either side alone. A unit test can prove a session
signature is refused; only an end-to-end test can prove that a worker holding a
live NATS connection and a claimed run stops being able to act the instant an
operator drops its WorkerInstance.

The existing tests specifically cannot show this. `cmd/nats-worker`'s tests stub
the API with a canned `ClaimRun` answer, and `pkg/urth`'s tests never build an
HTTP route or a NATS connection. Both halves are tested against the other's
assumption.

## Evidence

- `pkg/urth/service.go`, `loadClaimant` → `resolveSession`: revocation is a
  missing row, and session/runner mismatch is a field comparison. Nothing proves
  a *connected, running* worker is stopped by either.
- `cmd/nats-worker/consume_test.go`: `stubResults.ClaimRun` returns a canned
  value; no test sees a status the API actually produced.
- `docs/adr/0002-worker-authentication.md`: revocation is defined as the
  WorkerInstance disappearing, with no integration coverage.
- `docs/adr/0004-nats-communication-backbone.md:373-384,499-516`: the failure
  matrix and the migration completion gate that requires it.

## Required Outcome

Extend task 011's harness — do not build a second one — with TLS and per-Runner
test credentials, and add the scenarios that turn on authorization:

- an enrolment token that has been rotated no longer registers a new worker,
  while sessions already issued from it remain valid until they expire
  (task 005);
- a WorkerInstance deleted mid-run: the worker's next claim and its status upload
  are both refused, and the in-flight Result is settled by lease expiry rather
  than by the worker;
- a session that expires while a run is executing: the run capability issued
  before expiry still uploads its result, because it is a separate credential
  with its own lifetime (task 006);
- a blocklisted Worker security identity cannot register, and an already
  registered one stops being given work (task 009);
- NATS authority scoped to one Runner: a worker cannot bind another Runner's
  consumer, publish to another Runner's job subject, or administer JetStream
  assets (task 004); and
- a run capability presented for a different Result, a different Runner, or after
  its deadline is refused on every scope it names.

## Implementation Constraints

- Reuse task 011's fixtures, failpoints and diagnostic dumping verbatim. If a
  scenario needs a new seam, add it to the shared harness.
- Assert the *denial*, not just the absence of success: a test that passes because
  the worker never got as far as trying proves nothing. Each case must show the
  worker attempting the action and the server refusing it.
- Refusals must be checked by status class, not by message text — the message is
  deliberately generic (`cmd/api-server/main.go`, `claimHTTPResponse`).
- No credential may be minted by the test except through the production issuing
  path.

## Non-Goals

- Crash and durability boundaries — task 011.
- The authorization mechanisms themselves — tasks 004, 005, 006, 009 own those.
  This task proves they compose.

## Acceptance Criteria / Definition of Done

- [ ] Every current scenario and capability-binding variant has real
  PostgreSQL/HTTP/JetStream CI evidence; exact missing attempts are listed above.
- [ ] Every required matrix entry has attempted denial with a positive control;
  existing cases meet this, the missing entries do not yet have evidence.
- [x] The harness is shared with task 011.
- [x] Revoked/expired enrollment, session and broker authority stop the operation
  each authorizes; independent bounded claimed-run reporting remains valid.

## Validation

```sh
go test -race -count=1 -tags=integration ./test/integration/...
make audit/postgres
```

## Completion Record

- **Implemented:** Shared mounted and secured composition harness; production
  enrollment/claim issuance; explicit credential lifetime and revocation checks.
- **Tests added/updated:** Tenancy/runlogs, mounted M9 authorization and live-log
  tests, secured Worker block/delete/expiry/renewal/rebind tests, broker negatives.
- **Validation evidence:** Full merged `d60d902` PostgreSQL/race/static gate,
  prior complete candidate audits and combined secured integration pass; source
  attribution appears in the release record.
- **Follow-ups:** Mounted Runner/Worker executor-binding denials for both status
  and artifacts, plus PR 111 merge/exact merged-head validation.
  Task remains open; no blanket stale lifecycle gap remains.
