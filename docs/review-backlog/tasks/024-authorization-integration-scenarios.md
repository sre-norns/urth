# 024: Exercise Authorization End to End

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `done` |
| Priority | `P1` |
| Workstream | Authentication |
| Depends on | 004, 005, 006, 009, 011 |
| Likely conflicts | 004, 005, 006, 009 |
| Owner | Unclaimed |

## Current evidence and remaining criteria (2026-10-03)

The shared task 011 PostgreSQL/HTTP/JetStream harness runs the M9 composed tests.
Urth `1e37393` passes the complete merged backend gate, including the broker,
enrollment and executor-binding follow-ups. The current acceptance criteria
are complete. Deployment configuration remains a separate release gate.

| Scenario | Evidence / exact remaining check |
| --- | --- |
| Enrollment revocation and replacement | `TestMachineTokenRevocationDeniesFreshProofButPreservesSession` attempts challenge/enrollment with an unused valid proof after revocation and receives 401; prior session claim succeeds; replacement token renews the same UID. Individually revoked shared tokens supersede generation rotation. |
| Worker block/delete during claimed work | `TestWorkerBlockAndDeleteComposeWithSecuredBroker` attempts next claim after block and deletion and receives 403; already issued capability completes status and artifact reporting. A blocked connected broker identity closes by issued expiry. This preserves the selected bounded reporting contract. |
| Worker session expires during execution | `TestExpiredWorkerSessionKeepsOnlyBoundedInFlightAuthority` claims before expiry, attempts a later claim with the expired session and receives 403, then completes the original Result with its independent capability. |
| Renewal and new registration UID | Runtime renewal/rebind tests keep a held probe running, reconnect with current credentials, publish new-UID presence, claim/report a second run and complete the original run. |
| Runner broker boundaries | Secured own consumer/ack/log/presence controls and foreign consumer/inbox/jobs/events/deletion negatives pass. PR 111 proves valid concrete foreign-log, stream create/update, consumer create and service-role denials. It merged as `f93767a`, an ancestor of `4990b229`. Exact merged-head Build And Verify `37112080233`, Website `37112080193` and CodeQL `37112080155` pass. |
| Result/executor/deadline/scope bindings | Mounted tests attempt wrong Result status, changed dispatch on status/artifact, credential-purpose confusion and expired lease/status/artifact reporting. Validator tests reject wrong Runner/Worker/tenant and missing scope. `TestM9RunCapabilityRejectsChangedStoredExecutor` uses production enrollment and claim issuance. It changes only the stored Runner or Worker UID to another valid resource. Both mounted status path forms (Scenario name and UID) and artifact creation return 401. Restoration of the stored binding permits the same original capability to upload an artifact (201) and complete status (200). Denied requests leave Result state/version and artifact count unchanged. Ten race repetitions pass (40 subcases; 88.746s). |
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
are now supplemented by exact merged `1e37393` CI for the executor-binding
matrix from PR 114. Deployment-specific configuration remains a separate
release gate.

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

- [x] Every current scenario and capability-binding variant has real
  PostgreSQL/HTTP/JetStream CI evidence at merged `1e37393`, including the new
  executor-binding matrix. CI links are in the release record.
- [x] Every required matrix entry has attempted denial with a positive control.
  The mounted executor-binding matrix now supplies the remaining attempts.
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
  tests, secured Worker block/delete/expiry/renewal/rebind tests, broker negatives,
  and production-issued Runner/Worker executor-binding attempts for both
  mounted status path forms and artifact creation.
- **Validation evidence:** Full merged `4990b229` PostgreSQL/race/static gate;
  executor-binding focused race run (8.526s) and ten final-source race
  repetitions (88.746s). Full candidate `make audit/postgres` exits 0: all
  backend race tests, vet, staticcheck `2026.2.1`, and module verification pass;
  integration takes 151.001s and `pkg/natsq` takes 64.231s. Isolated fault
  controls remove the Runner comparison,
  Worker comparison, or artifact binding guard. All three fail at the intended
  mounted denial assertion (status 200, status 200, and artifact 201). These
  deliberate failures do not represent candidate defects.
- **Merged validation:** PR 114 merges as `65f34d7`; all exact-head checks pass
  on the combined source at `1e37393`. Its executor test is unchanged from the
  reviewed and locally validated PR head. This test changes stored authority
  as a fixture; it does not add a public executor-change API.
- **Follow-ups:** Deployment-specific verification remains a separate release
  gate. No missing matrix case remains in this task.
