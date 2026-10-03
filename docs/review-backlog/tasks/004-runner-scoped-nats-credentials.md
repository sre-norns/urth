# 004: Validate Runner-Scoped NATS Credential Lifecycle

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `done` |
| Priority | `P0` |
| Workstream | Authentication |
| Depends on | — |
| Likely conflicts | 005, 009, 011, 013 |
| Owner | Unclaimed |

## Current evidence and remaining criteria (2026-10-03)

The implementation and broker follow-up are merged through Urth `4990b229`.
All exact-head backend, website and CodeQL checks pass. The acceptance criteria
are complete. Deployment checks remain separate release operations. The
historical source review describes `1e13334`, not the current credential API.

| Criterion | Merged evidence / exact remaining check |
| --- | --- |
| Separate secured API/Worker identities | `credentials_test.go` creates independent provisioner, publisher, observer and Worker credentials against an authenticated mutual-TLS broker. The Worker receives its own JWT/seed, not an API credentials path. |
| Exact Runner consumption/publication | Own consumer fetch/confirmed ack, own log and exact Worker presence publication succeed. Foreign consumer/pull/inbox and other-Worker presence fail. The old merged negative uses a wildcard prefix. PR 111 now attempts valid `LogSubject("b", "result-b")` and asserts denial. |
| Jobs/events/JetStream administration denied | Own and foreign Runner job publication, event subscription and stream/consumer deletion fail. PR 111 now attempts stream create/update, consumer create and cross-role provisioner/publisher/observer denials with positive controls. The follow-up is merged and its exact-head CI passes. |
| Expiry and renewal | Already-connected authority expires/disconnects; expired authority cannot reconnect. Renewal reconnects the existing consumer and survives original expiry. Real Worker runtime tests preserve in-flight reporting and prove presence/claim/report after same/new-UID replacement. Authority ends by Worker session expiry and the default five-minute cap. |
| No server credentials path | Secured registration asserts the legacy `Value` path is empty. The decorated JWT/seed DTO and redacted String method replace the overloaded path. |
| Fail-closed production transport | `tls_test.go` and Worker API transport tests reject missing authentication, accidental plaintext, untrusted/missing client TLS and non-loopback insecure mode. Production deployment configuration still needs operator verification. |

The complete merged PostgreSQL/race/static gate passes. Exact source identities
and CI links are in [the release record](../../m9-release-validation.md).
Loopback [fresh-stack evidence](../../m9-fresh-stack-validation.md) proves actual
execution and revocation controls; it does not replace these secured tests.

[Broker PR 109](https://github.com/sre-norns/urth/pull/109), source `09547ce` plus
`b01c130`, is merged. Ten consecutive race runs prove delegated signer overlap
and retirement, client/server CA rollover and secured three-node JetStream
failover. The complete broker race suite, vet, staticcheck and module
verification pass. The cluster uses authenticated accounts and mutual TLS on
client and route connections, stops the connected stream leader, and confirms
delivery/acks before and after failure. This test does not rotate route
certificates.

## Additional candidate evidence

[PR 111](https://github.com/sre-norns/urth/pull/111) supplies the remaining
permission attempts and route rollover test. Source is
`e4c3483b5f33cc1de3bca52de315840a32c07070`; final head
`82b23384bf689e4efc5bb494e583e0c2fe069830` adds reviewed runbook wording only.
It changes tests/docs on `d60d902`, with no production or dependency change.
Independent source review passes. PR 111 merges as `f93767a`; the subsequent
documentation merge `4990b229` passes [Build And Verify](https://github.com/sre-norns/urth/actions/runs/37112080233),
[Website](https://github.com/sre-norns/urth/actions/runs/37112080193) and
[CodeQL](https://github.com/sre-norns/urth/actions/runs/37112080155).

The full `GOWORK=off make audit/postgres` passes: broker race 64.745 seconds,
integration 144.609 seconds, repository vet, Staticcheck 2026.2.1 and module
verification. Route rollover passes ten race repetitions in 287.249 seconds.
The three-node test rolls route leaves and trust without broker restart,
forces fresh pooled/system routes, checks current replicas and cross-node
message delivery/confirmed acks, and denies retired-root incoming/outgoing TLS.
Established routes do not authenticate again merely because TLS files reload.
Final-source omitted-leaf-reload and retained-old-root controls fail at the
intended assertions. Earlier simultaneous disruption/readiness fixture failures
remain preserved; the corrected fixture waits for rolling convergence.

Concrete foreign-log, stream create/update, consumer create and cross-role
service denials now have dedicated tests. Consumer update shares the
`CONSUMER.CREATE` endpoint; there is no separate `CONSUMER.UPDATE` API. The
negative sends `{}` and asserts broker permission rejection before parsing.
Service positives update a stream and create/delete a consumer; no explicit
consumer-update positive is claimed.

Evidence is preserved under
`/home/soultaker/workspace/m9-review/cluster-route-followup/`.
Deployment verification still reviews private key files, response `no-store`,
ordinary log/error secret handling, resolver/service identity configuration,
client/route TLS and maximum revocation delay. These operator checks are
separate from local secured tests. See [the candidate runbook](https://github.com/sre-norns/urth/blob/82b23384bf689e4efc5bb494e583e0c2fe069830/docs/m9-broker-operations.md).

## Historical review baseline and requirements

## Why This Matters

The API server currently connects using `Config.CredsFile` and returns that same
server-local path to a registering Worker. On a remote Worker the path normally
does not exist. If the path is deliberately shared, the Worker receives the
control-plane NATS identity that can create consumers and publish jobs.

Consequently there is no production configuration that both authenticates NATS
and gives API servers and Workers appropriately distinct authority.

## Evidence

- `pkg/natsq/config.go:104-119`: one client configuration connects every role.
- `pkg/natsq/scheduler.go:131-141`: API-server credentials-file path is copied
  into Worker registration data.
- `cmd/nats-worker/main.go:220-229`: returned path overrides Worker-local credentials.
- `pkg/urth/worker_session.go:35-46`: JWT credential discriminator exists but is
  not implemented.
- `docs/adr/0004-nats-communication-backbone.md:250-284`: required identity and
  permission outcome.

## Required Outcome

Use short-lived NATS user JWTs backed by an ephemeral Worker NKey as the initial
credential mechanism:

- successful Urth registration issues NATS connection authority bound to that
  WorkerInstance and Runner UID;
- the Worker can pull only from the named Runner durable consumer, use required
  reply/ack subjects, and publish only that Runner's live-log subjects;
- it cannot read another Runner, publish jobs, subscribe to resource events, or
  create/update/delete JetStream assets;
- API server, outbox relay, scheduler, and log subscriber use separate configured
  service identities and least-privilege permissions;
- NATS authority expires no later than the Worker session and is renewed/rotated
  without dropping in-flight runs; and
- production requires TLS and authenticated NATS accounts. An unauthenticated
  mode is explicit local-development configuration only.

If NATS deployment constraints make user JWT/NKey issuance untenable, stop and
record a superseding ADR selecting Auth Callout before implementing another scheme.

## Implementation Constraints

- Never reuse the Runner enrollment credential or Worker session bearer value as
  a NATS password.
- The API may return a short-lived JWT and corresponding ephemeral private seed,
  but must mark the response `no-store`, redact both, and never persist plaintext.
- Extend `NATSCredential` so the worker has everything nats.go needs; a single
  overloaded `Value` string is not a durable contract for JWT plus NKey proof.
- Session renewal must update the NATS connection credential. The current renewal
  path ignores newly returned connection information.
- Worker code continues to bind existing consumers; authorization is tested by
  attempting forbidden operations against a secured embedded/test NATS server.

## Suggested Implementation Sequence

1. Add separate service-role and Worker connection configuration types.
2. Configure a test NATS operator/account and signing key.
3. Extend the registration wire format for JWT/NKey credentials and expiry.
4. Mint Runner/Worker-scoped permissions from authenticated registration state.
5. Teach the Worker to connect and renew using issued authority.
6. Add positive and negative permission integration tests and TLS documentation.

## Non-Goals

- User-facing API authentication beyond Worker enrollment (task 005 owns the
  enrollment operator boundary).
- Runner blocklist identity proof (task 009).
- General multi-tenant account design beyond isolating one Urth installation.

## Acceptance Criteria / Definition of Done

- [x] A secured test deployment uses separate API and Worker identities.
- [x] A Worker consumes only its exact Runner consumer and publishes only its
  log/presence authority; merged PR 111 proves valid concrete foreign-log denial.
- [x] Cross-Runner reads, job/event authority and all required administration
  denials are evidenced in merged PR 111 and pass the exact-head CI.
- [x] Credentials expire and renew within the Worker session bound.
- [x] No server-local credentials path is sent to a Worker.
- [x] Production configuration rejects accidental unauthenticated/plaintext NATS.

## Required Tests

- Runner A credential consumes A and is denied Runner B.
- Worker credential cannot publish `urth.v1.jobs.*` or call stream/consumer admin APIs.
- Worker credential publishes `urth.v1.logs.<runner-a>.*` but not another prefix.
- Session renewal rotates NATS authority while a Worker continues fetching.
- Revoked/expired authority can no longer establish or retain a connection.

## Validation

```sh
go test -race -count=1 ./pkg/natsq ./pkg/urth ./cmd/nats-worker ./cmd/api-server
go test -race -count=1 ./...
go vet ./...
git diff --check
```

## Completion Record

- **Implemented:** Worker JWT/NKey issuance, Runner/Worker permission boundaries,
  separate service-role identities, bounded expiry and dynamic renewal/rebind.
- **Tests added/updated:** Secured credential isolation; runtime expiry/renewal;
  delegated signing rotation, client/server CA rollover and secured failover.
- **Validation evidence:** Full merged `d60d902` backend gate passes. Additional
  PR 111 permission and route-rotation audit passes as attributed above.
- **Follow-ups:** Deployment key/no-store/secrecy/TLS verification remains a
  release operation. Acceptance evidence and the follow-up merged CI pass.
  Task closure does not approve a production deployment.
