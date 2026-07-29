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

- [ ] Every scenario above runs in CI against real Postgres, HTTP and JetStream.
- [ ] Each proves an attempted action and its refusal, by status class.
- [ ] The harness is shared with task 011, not duplicated.
- [ ] A revoked or expired credential is shown to stop work already in flight.

## Validation

```sh
go test -race -count=1 -tags=integration ./test/integration/...
make audit/postgres
```

## Completion Record

- **Implemented:**
- **Tests added/updated:**
- **Documentation updated:**
- **Validation evidence:**
- **Follow-ups:**
