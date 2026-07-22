# 001: Preserve Retryable Claim Failures

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `done` |
| Priority | `P0` |
| Workstream | Claim lifecycle |
| Depends on | — |
| Likely conflicts | 003, 010, 011 |
| Owner | main (this change) |

## Why This Matters

A delivered dispatch is removed only when the Worker can prove the authoritative
claim is complete or obsolete. The current HTTP handler turns every service error
into `401 Unauthorized`, including transient database and internal failures. The
Worker treats every 401 as a stale Result and acknowledges the dispatch.

A momentary Postgres failure can therefore delete the only live message for a
still-pending Result. Without a reconciler that Result remains pending forever;
even with reconciliation this creates avoidable loss and latency.

## Evidence

- `cmd/api-server/main.go:162-168`: every `ClaimRun` error is flattened to 401.
- `cmd/nats-worker/consume.go:196-205`: 401/403/404 are classified as stale.
- `cmd/nats-worker/consume.go:146-151`: stale messages are acknowledged.
- `docs/adr/0004-nats-communication-backbone.md:178-187`: transient, stale,
  and policy outcomes require different dispositions.

Failure sequence:

1. A Worker pulls a valid pending Result.
2. Postgres temporarily fails while `loadClaimant`, Result loading, or the
   version-guarded update runs.
3. The API returns 401.
4. The Worker acknowledges the dispatch as stale although no claim committed.

## Required Outcome

Claim responses preserve an opaque but machine-actionable outcome:

- invalid/expired session or permanent policy refusal: generic 401/403; Worker
  terminates the message and records an operational error;
- terminal, missing, or already-validly-claimed Result: generic 409 conflict;
  Worker acknowledges the obsolete message;
- transient store, API, timeout, or internal error: 5xx; Worker NAKs with a
  bounded delay;
- successful or idempotently repeated claim: 2xx; Worker proceeds to ACK.

Bodies must not reveal whether a protected Result exists or which Worker owns it.
Status class and a small stable claim-outcome code are sufficient.

## Implementation Constraints

- Define typed domain errors/outcomes at the `pkg/urth` service boundary; do not
  infer retryability from log text.
- The HTTP adapter maps domain outcome to status. The Worker maps status/outcome
  to JetStream disposition in one place.
- Preserve optimistic concurrency: a lost claim race is stale, not transient.
- A context cancelled by Worker shutdown may leave the message unacknowledged;
  it must not be converted into a terminal outcome.
- Do not leak token validation details or protected resource existence.

## Suggested Implementation Sequence

1. Add handler tests with a fake `RunResultAPI` returning transient, stale, and
   policy outcomes.
2. Add Worker disposition table tests independent of a live NATS server.
3. Introduce typed claim outcomes/errors in `pkg/urth`.
4. Map those outcomes explicitly in the API and Worker.
5. Add a JetStream integration test proving a transient response redelivers.

## Non-Goals

- Transactional outbox or missing-message reconciliation (tasks 002 and 003).
- Changing claim authorization policy itself (tasks 006, 008, and 009).
- Designing the dead-letter operator surface (task 012).

## Acceptance Criteria / Definition of Done

- [ ] A transient service/store failure cannot cause a Worker ACK or Term.
- [ ] Stale dispatches are acknowledged without exposing ownership details.
- [ ] Permanent policy refusals are terminated and surfaced distinctly.
- [ ] Worker shutdown leaves uncommitted deliveries available for redelivery.
- [ ] Unit and JetStream integration tests cover every disposition.
- [ ] Worker/API documentation describes the outcome contract.

## Required Tests

- API service returns a synthetic database error: HTTP 5xx, Worker NAKs, message
  is redelivered.
- Result is already terminal or claimed elsewhere: generic 409, Worker ACKs.
- Session is invalid or policy permanently refuses the Worker: Worker Terms.
- Claim commits but response succeeds: ACK and execute path remains unchanged.

## Validation

```sh
go test -race -count=1 ./cmd/api-server ./cmd/nats-worker ./pkg/urth ./pkg/natsq
go test -race -count=1 ./...
go vet ./...
git diff --check
```

## Completion Record

- **Implemented:**
  - Introduced typed, transport-neutral claim outcomes at the service boundary
    (`pkg/urth/claim.go`): `ClaimError` carrying a `ClaimDisposition` of
    `ClaimUnavailable` (transient), `ClaimObsolete` (stale), or `ClaimForbidden`
    (policy). Retryability is decided here, not re-derived downstream.
  - Reclassified every return in `ClaimRun`, `loadClaimant`, and `authorizeRun`
    (`pkg/urth/service.go`). Fixed the latent bug where a store error on
    `GetByUID`/`Update` was reported as `ErrResourceNotFound`/`Unauthorized` — a
    transient failure masquerading as a terminal one. The version-guarded update
    still loses the race (now surfaced as `ClaimObsolete`, not a raw error).
  - API handler maps disposition to a status *class* with a generic, reason-free
    body: transient → `503`, obsolete → `409`, forbidden → `403`; an unclassified
    error defaults to `503`, never a terminal refusal (`cmd/api-server/main.go`,
    `claimHTTPResponse`/`abortClaim`). Replaces the old flat `401`.
  - Worker maps status class to one JetStream disposition in one place
    (`cmd/nats-worker/consume.go`): `2xx`→Ack+execute, `5xx`→Nak(delay),
    `409`→Ack(drop), `401/403/400/404`→Term. Added a fourth outcome,
    `claimAbandon`, for a claim interrupted by shutdown: the message is left
    unacknowledged for redelivery rather than acked/naked/termed. Extracted
    `applyDisposition` so the Ack/Nak/Term decision is unit-testable.
- **Tests added/updated:**
  - `cmd/api-server/claim_test.go`: `claimHTTPResponse` disposition→status table,
    wrapped-error unwrapping, and a body-does-not-leak-reason guard.
  - `cmd/nats-worker/consume_test.go`: `applyDisposition` outcome→queue-action
    table (incl. abandon-leaves-untouched and delayed-nak), `classifyClaimFailure`
    status→outcome table, and `claim()` shutdown-abandon vs live-classification
    against a stubbed service. These fail against the old code: old `409`→retry
    (now stale), old `403/404`→stale-ack (now term), old shutdown→retry (now
    abandon).
  - `pkg/natsq/natsq_test.go`: `TestNakedJobIsRedelivered` proves on an embedded
    JetStream server that the transient disposition (NAK) actually redelivers on a
    work-queue stream rather than dropping.
- **Documentation updated:** `cmd/nats-worker/README.md` gains a "claim outcome
  contract" table; ADR 0004 already described the disposition split abstractly and
  the implementation now realizes it.
- **Validation evidence:** `go test -race -count=1 ./...` pass; `go vet ./...`
  clean; `git diff --check` clean. (staticcheck still blocked by the pre-existing
  Go 1.25/1.26 toolchain mismatch, deferred to the Go-uplift PR — it fails at
  compile, unrelated to this change.)
- **Follow-ups:** A full API+DB+NATS crash-point integration test is owned by
  task 011. Session-expiry is currently `ClaimForbidden`→Term per this task's
  required outcome; if operators want an expired session to renew-and-retry rather
  than dead-letter, revisit under the auth workstream (004/005). The legacy
  `Auth` claim path (`resultsAPIImpl.Auth`) still uses the old flat error mapping;
  it is asynq-only and removed by task 015.
