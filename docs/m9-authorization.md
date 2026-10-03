# M9 authorization boundary

This record covers the authorization changes and tests in M9-01. It does not
close the worker identity, broker lifecycle, or final release gates.

## Credential authority

| Credential | Authorized operation | Boundary |
|---|---|---|
| Account user session | Account and project resource routes, catalogues, stored and live logs | Active session; current account/project membership where scoped |
| System user session | Shared system authority; Urth does not mount system administration routes | Does not grant product account/project authority |
| Machine enrollment token | Worker registration for its authorized Runner | Does not authorize user resource reads, run claims, or result writes |
| Worker session | Its Runner/Worker claim and worker operations | Does not authorize user resource reads or result writes |
| Run capability | Status and artifact reporting for the claimed Result | Stored executor, account, project, purpose, expiry and Result state |
| NATS credential | Broker operations in its issued subject permissions | Separate broker policy; M9 worker/broker work validates its lifecycle |
| Internal control context | Reconciler and controller operations | Constructed by server code; no bearer credential selects this authority |

The shared identity module owns user, system and machine credentials. Product
routes require an account user session. The service binds worker claims and
run reporting to their separate credentials.

## Run capability contract

The server issues an HS256 credential with key ID `run`, issuer `urth`, audience
`urth-run`, Result UID, Runner UID, Worker UID, account, project, issued-at,
not-before, expiry, and operation scopes `run.status` and `run.artifacts`.
Validation requires those claims and compares the binding with the stored Result.
The expiry cannot exceed the stored execution deadline plus the existing
five-minute artifact upload grace period.

Status writes require a running Result and its current version. A successful
terminal write prevents another status write. Completed runs can still receive
final artifacts until the capability expires. Pending, errored and expired runs
cannot receive artifacts. Artifact admission holds the Result row lock through
insertion, so expiry cannot interleave between validation and storage.

Grant or Worker revocation does not cancel a previously issued run capability.
Its limited reporting authority lasts until its expiry unless the Result enters
a state that rejects the operation. Worker/broker changes document the separate
rules for new claims, enrollment and broker connections.

Fresh installations are the target. Old run credentials that lack the required
claims are rejected. No compatibility reader or migration window is provided.

## Live logs

The scoped log route requires a bearer account session. Live streams authenticate
that bearer again and check current project visibility before each data emission.
Idle streams check once per second. A failed lookup closes the stream; each check
has a two-second timeout. Closure can therefore take the polling interval plus
that timeout. Already transmitted bytes cannot be withdrawn.

Tests cover project membership removal, session removal and session expiry on
an open stream. Stored logs retain the ordinary request authorization check.
Both responses use `Cache-Control: no-store`.

## Reverse proxies and OAuth

Urth now ignores forwarded client addresses by default. Configure only trusted
proxy IP addresses or CIDRs with `URTH_TRUSTED_PROXIES` (comma separated) or
`--http.trusted-proxy`. The router walks a forwarded chain from the trusted peer
toward the client and uses the first untrusted address. Invalid configuration
stops server composition. The proxy must replace forwarding headers supplied by
an untrusted client.

The mounted shared OAuth limiter uses this client address. Tests cover repeated
requests with changing untrusted forwarding headers, trusted proxy chains,
issuer-origin requests, foreign origins, clients without Origin, and unavailable
limiter storage. A storage error denies the request without creating a grant.

## Evidence and validation

- `pkg/urth/m9_run_capability_test.go`: required claim, algorithm, purpose,
  expiry and binding checks. These use known test keys; they do not demonstrate
  that an external caller can forge a signature.
- `test/integration/m9_authorization_test.go`: real PostgreSQL and mounted HTTP
  credential separation, terminal rewrite denial, wrong-Result denial, expired
  run denial, concurrent completion, artifact row locking, project item/catalogue
  isolation and OAuth boundaries.
- `test/integration/m9_runlogs_test.go`: open-stream access changes with the
  actual HTTP route, PostgreSQL sessions and NATS log publisher.
- `test/integration/tenancy_test.go`: existing cross-account/project and
  grant-revocation cases, including bounded reporting after a claimed run.
- `test/integration/runlogs_test.go`: existing authorized live/stored log controls.

Before the fixes, the terminal rewrite test changed an already completed Result,
the direct-peer OAuth test bypassed the limit with changed forwarding headers,
and an open log stream delivered data after project membership removal. The
regression tests now require denial with a valid-operation positive control.

Run `make audit/postgres store-url='<disposable PostgreSQL URL>'` with
`GOWORK=off`. It runs module verification, vet, pinned static analysis and the
full race-enabled suite. Use a dedicated database. Published dependency pins
remain unchanged; this change requires no wyrd or components release.
