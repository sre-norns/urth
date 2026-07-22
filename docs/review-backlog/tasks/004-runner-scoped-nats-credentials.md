# 004: Issue Runner-Scoped NATS Credentials

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P0` |
| Workstream | Authentication |
| Depends on | — |
| Likely conflicts | 005, 009, 011, 013 |
| Owner | Unclaimed |

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

- [ ] A secured deployment works with API and Worker identities stored separately.
- [ ] A Worker can consume only its exact Runner consumer and publish its log prefix.
- [ ] Cross-Runner reads, job publication, event subscription, and JS admin fail.
- [ ] Credentials expire and renew with the Worker session.
- [ ] No server-local credentials path is sent to a Worker.
- [ ] Production configuration rejects unauthenticated/plaintext NATS accidentally.

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

- **Implemented:**
- **Tests added/updated:**
- **Documentation updated:**
- **Validation evidence:**
- **Follow-ups:**
