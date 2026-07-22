# ADR 0002: Authenticate workers with staged, scoped credentials

- **Status:** Accepted
- **Date:** 2026-07-21

## Context

Urth workers run on infrastructure controlled by users, often inside private or
segmented networks. They connect outward to the control plane, register against a
Runner, claim jobs, execute probes, and upload Results and Artifacts. A worker therefore
needs enough authority to perform a job without receiving general authority over the
Urth API or over other workers' jobs.

There are three different questions in that lifecycle:

1. May this process join a particular Runner pool?
2. Is this registered WorkerInstance allowed to claim this pending Result now?
3. After a successful claim, may it update this Result and attach Artifacts to it?

One reusable token for all three questions would have a large blast radius. Conversely,
receiving a message from the job broker is not proof of identity or permission. Broker
credentials, queue routing, worker-supplied IDs, labels, and timeouts are not an
authentication boundary.

Workers may be restarted, replicated, paused, or removed independently while sharing a
Runner pool. The authentication model must preserve that distinction, support API server
replicas, and keep a compromised worker from writing to unrelated Results.

This ADR covers machine authentication between workers and the Urth control plane. User
and operator authentication is a separate concern, but it is a prerequisite for securely
creating Runners and issuing their enrollment credentials.

## Decision

Urth uses a staged credential exchange. Authority narrows as a worker moves from joining
a pool to claiming one execution.

| Credential | Bound to | Authorises | Lifetime |
|---|---|---|---|
| **Runner enrollment credential** | One Runner | Register or refresh a WorkerInstance under that Runner | Reusable until expiry or rotation |
| **Worker session credential** | One Runner and WorkerInstance | Join that Runner's queue and request job claims | Short-lived and renewable by re-registration |
| **Run capability token** | One Result and its recorded executor | Update that Result's status and create its Artifacts | The server-enforced run deadline plus a small upload grace period |

Possession of a credential grants only the authority in its row. In particular, an
enrollment credential is not a general API token, and a run token is not a reusable
worker identity.

### 1. Runner enrollment bootstraps worker identity

Creating a Runner resource mints its initial enrollment credential as part of the
operator-authenticated operation. The credential is transferred to the worker through an
operator-controlled secret-delivery mechanism. Urth must never put it in the Runner
manifest, logs, command output not explicitly requesting it, or list responses.

The credential delegates permission to join one Runner pool. It may be reused to
autoscale several WorkerInstances under that Runner, subject to the Runner's policy. It
must be independently rotatable and revocable. Disabling a Runner prevents both new
registrations and refreshes.

Reusable enrollment credentials are secrets and must not be stored as plaintext. An
opaque random credential stored as a salted verifier is preferred because it supports
per-Runner revocation. A signed credential is acceptable only when the server also
checks revocable Runner state or a token-generation value; signature and expiry alone
are insufficient for prompt revocation.

The worker presents the credential over an authenticated, encrypted connection together
with its proposed WorkerInstance identity and self-reported capabilities. The API server:

1. validates the credential and resolves the Runner from it;
2. verifies that the Runner is active;
3. validates the WorkerInstance manifest and labels;
4. enforces the Runner's Worker requirements, blocklist, and instance limit;
5. creates or refreshes the WorkerInstance resource; and
6. returns the assigned WorkerInstance identity and a Worker session credential.

The server derives the Runner association from the enrollment credential. A worker
cannot choose a different Runner by submitting another Runner name or UID.

Labels require an explicit trust distinction. Runner labels and registration policy are
operator-controlled. Worker capability labels are self-reported assertions constrained
by that policy. The server stores the effective, validated label set and adds
operator-controlled Runner identity labels itself. It does not trust labels resubmitted
during a later job claim. The channel contract and stable Worker identity are defined in
[ADR 0003](./0003-runner-worker-model.md).

### 2. A Worker session authenticates job claims

A registered WorkerInstance receives jobs only from its Runner's logical queue, through
the configured broker or transport. Receipt of a job grants no authority to execute or
report it. Before starting work, the Worker presents its session credential to the API
and asks to claim the pending Result named by the job.

The API derives the WorkerInstance and Runner identities from the authenticated session,
not from request-body IDs. It then verifies current server state:

- the WorkerInstance still exists and its session has not expired or been revoked;
- the WorkerInstance is not paused;
- its stable identity is not on the Runner's blocklist;
- its Runner exists and is active;
- the Result exists and is still pending;
- the Result was scheduled to the same Runner as the Worker session;
- the Result belongs to the Scenario named by the queued job;
- the stored effective Worker capabilities satisfy the Runner's Worker requirements and
  the concrete job; and
- the requested execution deadline does not exceed the server's limit for the Scenario.

The transition from pending to running is atomic and version-guarded. Only one claimant
can win. The Runner was already recorded when the Result was scheduled. At claim time the
server records the WorkerInstance as its physical executor, sets its start time and
expected deadline, and issues a run capability token to the winning Worker.

This claim operation is both authentication and authorisation. Queue access can improve
delivery efficiency, but it must never replace these checks.

### 3. A run capability authorises narrowly scoped writes

The run capability token is a short-lived bearer credential. It identifies the Result
as its subject and is bound to the Runner and WorkerInstance recorded when the claim
succeeded. Its scopes permit only the operations needed to report that execution:

- advance the claimed Result through valid execution status transitions; and
- create Artifacts linked to that Result.

It cannot claim another job, modify the Scenario, manage a Runner or WorkerInstance, or
write to another Result. The API resolves the Result from the validated token and links
uploaded Artifacts server-side; worker-supplied Result or executor labels are not
authoritative.

The server determines the token expiry from the Scenario's maximum run duration, with a
small bounded grace period for final status and Artifact uploads. A worker may request a
shorter timeout for its own execution, but it cannot extend the server deadline. The
same deadline is recorded on the Result so API behaviour and observable resource state
agree.

A signed run token must validate, at minimum:

- an explicit algorithm allowlist;
- signature and signing-key identifier;
- issuer and intended audience;
- issued-at, not-before, and expiry times;
- Result UID;
- Runner and WorkerInstance UIDs; and
- operation scopes.

Authorisation also checks the current Result identity and lifecycle state. A valid token
must not permit a completed, expired, cancelled, or unrelated Result to be rewritten.
Optimistic resource versions protect status transitions from concurrent or replayed
updates.

### 4. Revocation has lifecycle semantics

Revocation acts at the narrowest useful level:

- rotating a Runner enrollment credential prevents new sessions from being established
  with the old credential;
- disabling a Runner prevents registration, session refresh, and new claims by all of
  its WorkerInstances;
- adding a stable Worker identity to the Runner blocklist prevents registration,
  refresh, and new claims by that Worker;
- pausing a WorkerInstance prevents that instance from claiming new work while keeping
  its registration;
- deleting or expiring a WorkerInstance invalidates its session; and
- expiring or cancelling a Result prevents further writes with its run token.

Pausing a WorkerInstance or disabling its Runner does not, by itself, discard an
already-running probe. Its existing run capability remains narrowly valid until the
Result deadline so that the worker can report the outcome. Cancelling or expiring the
Result is the explicit way to reject further writes for that execution.

### 5. Tokens are handled as credentials, not resource data

All production worker-authentication traffic requires TLS. Bearer tokens must be marked
`no-store` in HTTP responses, redacted from logs and telemetry, and supplied through
secret-aware configuration rather than committed manifests or command-line examples
that expose them in process listings.

Enrollment, Worker session, and run-capability signing or verification material must be
separate from user-authentication keys and from one another. Keys are runtime
configuration, support rotation, and have no source-code defaults suitable for a running
server. Short-lived signed tokens should include a key identifier so API server replicas
can rotate keys without interrupting every in-flight run.

The API may use signed session and run tokens to avoid storing each token, but it still
loads the referenced resources when authorising state-sensitive operations. Stateless
signature validation does not override Runner, WorkerInstance, or Result lifecycle
state. NATS connection credentials and subject permissions derived from this identity
are specified in [ADR 0004](./0004-nats-communication-backbone.md).

## Authentication flow

```text
Operator              Worker                    API                 Job broker
   |                     |                       |                       |
   |-- create Runner --------------------------->|                       |
   |<------- Runner + enrollment credential ----|                       |
   |                     |                       |                       |
   |-- deliver secret -->|                       |                       |
   |                     |-- enroll ------------>|                       |
   |                     |<-- identity + session-|                       |
   |                     |                       |                       |
   |                     |<----------------------- receive Runner job ---|
   |                     |-- claim with session->|                       |
   |                     |<-- run capability ----|                       |
   |                     |                       |                       |
   |                     | execute probe         |                       |
   |                     |-- status + artifacts->|                       |
   |                     |   (run capability)    |                       |
```

## Architectural rules

1. A broker message, resource UID, label set, or network location is never accepted as
   proof of worker identity.
2. The server derives Runner and WorkerInstance identity from validated credentials,
   then loads current resource state before authorising a claim.
3. Enrollment credentials can create sessions but cannot claim jobs or report Results.
4. Worker sessions can request claims but cannot report arbitrary Results.
5. Run capabilities are bound to one Result, one executor, explicit scopes, and a
   server-controlled deadline.
6. Runner membership, blocklist, and capability checks use server-stored state;
   claim-time identities, labels, and timeouts are untrusted input.
7. Every claim is an atomic pending-to-running transition that records its executor.
8. Artifacts are associated with the Result derived from the run token, and security
   labels are derived or overridden by the server.
9. Reusable secrets are revocable and never stored or returned as plaintext after
   issuance.
10. Authentication failures reveal no token, signing, or resource-existence details
    beyond what an authenticated principal is allowed to know.

## Consequences

### Benefits

- Compromise of a run token affects one execution for a short, bounded time.
- Compromise of a Worker session does not grant Runner administration or authority over
  already-owned jobs from other workers.
- A WorkerInstance can be paused or revoked independently from its Runner pool.
- Result history records the identity that passed authorisation at claim time.
- API server replicas can validate short-lived signed capabilities without sharing
  per-request session state.
- Authentication remains independent of Redis, another broker, or a future gRPC job
  transport.

### Costs and constraints

- The control plane must implement enrollment rotation, Worker session issuance and
  expiry, scoped run tokens, and key rotation.
- Workers need a registration and renewal state machine rather than one static API key.
- Every job adds a control-plane claim round trip before execution.
- The server must retain enough current resource state to authorise claims even when
  token verification itself is stateless.
- In-flight work and revocation need explicit semantics; disabling a pool is not the same
  operation as cancelling its running Results.
- Deployments need secure secret delivery and TLS even when workers live on private
  networks.

## Alternatives considered

### One shared API key for every worker

This is operationally simple but cannot identify, pause, or revoke one WorkerInstance.
Its compromise grants broad and long-lived write access. It was rejected.

### Use the Runner enrollment credential for all worker operations

Workers in the same pool would share an identity and a reusable secret. Deleting one
WorkerInstance would not revoke it, and a compromise would affect every job available to
the Runner. It was rejected in favour of instance sessions and per-run capabilities.

### Trust access to the job broker

Broker access controls delivery, not Urth resource ownership. A leaked broker credential
or misrouted message must not grant permission to claim or update a Result. This was
rejected as an authentication mechanism.

### Mutual TLS identities for every worker

mTLS can provide strong transport-level workload identity, but issuing and rotating
client certificates across arbitrary user environments would make it a heavy baseline
requirement. It may be supported later as an alternative authenticator that produces the
same Worker session identity; it does not replace per-run authorisation.

### External workload identity only

Cloud or cluster identities can avoid bootstrap secrets in supported environments, but
Urth runners also operate on bare metal, factory networks, and developer machines. Such
identity providers may be pluggable enrollment mechanisms, not the only supported model.

## Implementation status at acceptance

The code already implements part of this trust chain:

- a Runner-bound, expiring JWT is used to register a WorkerInstance;
- registration checks Runner activity, registration requirements, and maximum instances;
- claiming checks that the WorkerInstance exists, is not paused, and belongs to an active
  Runner;
- the pending-to-running transition is version-guarded and records the executor; and
- a short-lived JWT whose subject is the Result UID authorises status and Artifact writes
  for that Result.

The current development implementation does **not** yet satisfy the full decision:

- the endpoint that issues Runner enrollment JWTs does not authenticate an operator;
- development signing secrets are fixed in source code;
- registration does not issue an instance-bound Worker session credential;
- the job-claim endpoint has no bearer authentication and accepts Worker and Runner IDs
  from its request body;
- claim authorisation does not enforce a scheduled Runner binding, Runner blocklist, or
  the concrete job against stored Worker capabilities;
- the worker supplies the run-token timeout and the server does not cap it from Scenario
  policy or record the expected deadline on the Result;
- run tokens lack explicit issuer, audience, executor, and scope claims; and
- WorkerInstance registration does not yet have an enforced session-expiry lifecycle.

Until those gaps are closed, worker authentication is suitable only for trusted local
development environments. This status section records migration work; it does not weaken
the architectural rules above.
