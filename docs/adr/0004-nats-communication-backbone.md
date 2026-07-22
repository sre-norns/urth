# ADR 0004: Use NATS and JetStream as the communication backbone

- **Status:** Accepted
- **Date:** 2026-07-21

## Context

Urth has three distributed control-plane participants:

- API server replicas own resource validation and state transitions;
- a scheduler, still to be implemented, turns Scenario schedules and manual triggers
  into pending Results bound to Runners; and
- Workers connect from user-controlled networks, receive jobs for one Runner, claim
  them, execute probes, and report Results and Artifacts.

[ADR 0001](./0001-resource-oriented-monitoring-platform.md) makes Postgres-backed API
resources the system contract. [ADR 0002](./0002-worker-authentication.md) requires
authenticated, instance-bound job claims and Result-scoped capabilities.
[ADR 0003](./0003-runner-worker-model.md) defines a Runner as a logical channel with one
queue and multiple physical Workers.

The current prototype uses Redis and Asynq as one shared job queue. That implementation
does not provide the accepted Runner-channel topology, durable control-plane events, or
subject-level Worker isolation. Urth needs a communication layer that supports:

- durable jobs when no Worker is currently connected;
- one logical queue per Runner with competing Workers;
- backpressure, acknowledgements, redelivery, expiry, and observable backlog;
- durable resource notifications between API servers and the scheduler;
- lightweight request/reply for internal operations where appropriate;
- outbound-only Worker connections from private and segmented networks;
- per-Worker permission to consume only one Runner's work; and
- clustering and persistence without introducing a separate system for every messaging
  pattern.

NATS provides subject-based publish/subscribe, queue groups, and request/reply through
Core NATS. JetStream, built into `nats-server`, adds persistent streams, durable
consumers, acknowledgements, replay, work-queue retention, deduplication, and replicated
storage. NATS is a CNCF Incubating project in the Streaming & Messaging category.

The legacy NATS Streaming Server, also known as STAN, is not considered. JetStream has
replaced it.

## Decision

Urth adopts **NATS with JetStream** as its communication backbone and eventual
replacement for Redis/Asynq.

This is a transport decision, not a state-ownership decision:

- **Postgres and Urth resources remain authoritative.**
- **JetStream carries durable jobs and durable resource notifications.**
- **Core NATS may carry transient notifications and internal request/reply.**
- **The REST API remains the public interface used by the UI, `urthctl`, and external
  automation.**

NATS availability or message state never overrides the lifecycle or version of an Urth
resource.

### 1. Postgres remains the source of truth

Scenario schedules, Runner placement, Worker registration, Result state, executor
identity, deadlines, and Artifact metadata remain resources in Postgres. NATS messages
notify participants that work or state exists; they are not independent copies of the
domain model.

A job message contains a small, versioned dispatch envelope:

- Result UID and expected resource version;
- selected Runner UID;
- a unique claim or dispatch ID; and
- transport schema version and tracing metadata.

It does not contain credentials, secret values, Artifacts, or an independently mutable
Scenario definition. After authenticating the claim, the API returns the immutable
execution snapshot associated with the Result and the Result-scoped run capability.
This prevents possession of a queue message from revealing or granting the job.

Resource-event messages likewise identify the resource kind, UID, version, and event
type. Consumers fetch authoritative state or reconcile from the database/API. A message
payload is never used to bypass resource validation or optimistic concurrency.

### 2. Database changes and publications use a transactional outbox

Urth must not perform an uncoordinated Postgres write followed by a NATS publish. That
creates a dual-write failure in which a Result exists without a job, or a job is visible
before its Result commits.

The transaction that creates or changes a resource also writes an outbox entry in
Postgres. An outbox relay publishes that entry through the JetStream publish API, waits
for the storage acknowledgement, and then marks the entry published. The relay may run
inside API server replicas or as a separate process, but competing relays must claim
outbox rows safely.

Every outbox entry has a stable event UID. The relay uses that value as `Nats-Msg-Id` so
JetStream can suppress duplicate publications within its configured duplicate window.
Consumers remain idempotent because retries outside that window, restore operations, or
operator replay can still produce a duplicate.

A periodic reconciler detects:

- unpublished outbox entries;
- pending Results with no corresponding live queue message;
- running Results whose execution lease has expired; and
- terminal Results that still have stale job messages.

The outbox and reconciler, rather than an assumption of exactly-once messaging, close the
gap between Postgres and NATS.

### 3. Runner jobs use one JetStream work-queue stream

Urth creates one primary work-queue stream:

| Property | Decision |
|---|---|
| Stream | `URTH_JOBS` |
| Subjects | `urth.v1.jobs.*` |
| Per-Runner subject | `urth.v1.jobs.<runner-uid>` |
| Retention | `WorkQueuePolicy` |
| Storage | File in production; memory is acceptable for disposable development |
| Replication | Three replicas in the production profile; one in local development |
| Discard | `DiscardNew`, so reaching a limit fails publication instead of evicting unconsumed work |

Each Runner has one durable **pull consumer** with an exact filter for its subject. All
WorkerInstances connected to that Runner bind to and share the same consumer. A message
is therefore delivered to one available Worker, while pull-based delivery lets each
Worker request work only when it has local execution capacity.

Runner subject filters are disjoint. This is important because a JetStream stream using
`WorkQueuePolicy` does not allow overlapping consumers for the same subject. Workers do
not create, modify, or delete streams and consumers; the Urth control plane reconciles
those assets from Runner resources.

Urth does **not** create one stream per Runner. Streams are the heavier persistence and
replication unit. One stream with durable, filtered consumers preserves logical queue
isolation while keeping stream count bounded. Consumer count still grows with Runner
count and must be included in capacity testing and JetStream high-availability asset
limits.

A Runner without Workers must not consume the entire shared stream. The stream therefore
uses a per-subject message limit with per-subject discard-new behaviour as well as a
global storage limit. If installations eventually require different retention or
replication classes, Urth may shard Runners across a small bounded set of streams; it
still does not create a stream for every Runner.

The logical queue is addressed by immutable Runner UID, not Runner name. Recreating a
Runner with the same name cannot expose the old Runner's queued messages or consumer.

### 4. Job acknowledgement follows the authoritative claim

Receiving a JetStream message is not a claim. The Worker performs this sequence:

1. Pull one message for an available local execution slot.
2. Present its Worker session and dispatch ID to the API.
3. Let the API enforce Runner membership, blocklist, capability, Result state, and
   deadline rules from ADRs 0002 and 0003.
4. Let the API atomically change the Result from pending to running, record the
   WorkerInstance and lease, and return the immutable execution snapshot with a run
   capability.
5. Acknowledge the JetStream message synchronously after that durable claim succeeds.
6. Execute the probe and report status and Artifacts through the authenticated API.

The Worker must not acknowledge before the claim, because a failed claim could lose the
job. It also must not hold the NATS acknowledgement until a potentially long-running
probe finishes. Once the API accepts a claim, the Result lifecycle—not a broker
acknowledgement timer—tracks execution.

The claim operation is idempotent for the same Result, dispatch ID, WorkerInstance, and
active session. If the API commits the claim but its response is lost, the same Worker
can retry and recover the same execution authorization without starting a second Result.
A different Worker cannot take over an unexpired claim.

If a Worker dies after the message is acknowledged, its Result lease eventually expires
and the Result records that failed attempt. If policy calls for a retry, the scheduler
creates a new Result and dispatch message rather than erasing or reopening historical
execution state.

Message disposition follows the claim outcome:

- transient API or network failure leaves the message unacknowledged or negatively
  acknowledges it with a bounded delay;
- an already terminal or validly claimed Result causes the stale message to be
  acknowledged and discarded;
- a policy mismatch that should have been prevented by Runner admission is terminated
  and surfaced as a control-plane error instead of redelivered forever; and
- repeated poison-message delivery is sent to an operational dead-letter workflow and
  reflected on the Result.

### 5. Delivery is at-least-once at the application boundary

Urth designs for duplicate publication and delivery. JetStream deduplication and
synchronous acknowledgements reduce duplicates, but they do not make external probe
execution exactly once.

A process can fail after making an HTTP request, DNS query, or other external side
effect but before reporting completion. No broker can prove whether that side effect
occurred. Result claims, dispatch IDs, resource versions, leases, and explicit attempts
provide application-level idempotency and honest history.

Probe authors should assume that an execution may be retried. Probes intended only for
observation should be side-effect safe. Scenarios that deliberately perform mutations
must supply their own idempotency mechanism where the target protocol permits it.

`Nats-Msg-Id`, JetStream's duplicate window, and synchronous acknowledgement are useful
defences, not substitutes for these rules.

### 6. Durable resource events use a separate stream

Control-plane events use a separate stream from jobs:

| Property | Decision |
|---|---|
| Stream | `URTH_EVENTS` |
| Subjects | `urth.v1.events.<resource-kind>.<event>` |
| Retention | `LimitsPolicy` with an explicit age and size budget |
| Consumers | One durable consumer per independent control-plane projection or service |

The scheduler consumes relevant Scenario, Runner, and Result notifications from this
stream. Other future consumers may build metrics, audit projections, web notifications,
or integrations without competing for the same copy. `WorkQueuePolicy` is therefore not
used for resource events.

Events are wakeups and ordered observations, not the scheduler's only recovery
mechanism. On startup and periodically thereafter, the scheduler reconciles authoritative
Scenario and Result state. If an event is expired, duplicated, delayed, or delivered out
of date, resource UID and version checks make processing safe.

Horizontal scheduler replicas share a durable consumer when an event should be processed
once. Database uniqueness and optimistic concurrency still protect Result creation from
duplicate scheduling.

### 7. Core NATS is reserved for transient service communication

Core NATS provides low-latency pub/sub and request/reply but is at-most-once: a message is
lost when no eligible subscriber is present. Urth uses it only where the caller can
tolerate timeout, retry, or reconstruction from resource state. Examples include:

- internal request/reply between an API server and a currently available service;
- best-effort wakeups when a durable event already exists; and
- ephemeral operational notifications.

Durable jobs, scheduling facts, audit-relevant events, and lifecycle transitions never
depend only on Core NATS.

NATS request/reply may later transport Worker claim or status commands, but the same API
service handlers, authentication, and resource state transitions remain authoritative.
It does not create a privileged messaging-only control plane or replace the public REST
API.

### 8. NATS authorization is derived from Urth identity

All production connections use TLS and authenticated NATS accounts. A default
unauthenticated NATS deployment is suitable only for local development.

Urth's Worker enrollment and session model remains the source of machine identity. A
successful Worker registration yields separate, short-lived NATS connection authority
bound to the same Runner and WorkerInstance. The Runner enrollment credential itself is
not used as a reusable NATS credential.

The initial integration should use either NATS Auth Callout backed by Urth or short-lived
NATS user JWT credentials backed by NKeys and issued by Urth. The precise credential
mechanism may evolve; the permission outcome is fixed. A Worker may:

- bind to and pull from only its Runner's durable consumer;
- use only the reply subjects required by approved request/reply operations; and
- publish only narrowly defined Worker control messages, if any.

A Worker may not:

- subscribe to another Runner subject or consumer;
- publish job messages;
- create or manage streams and consumers;
- subscribe broadly to resource events; or
- treat NATS permission as authorization to mutate an Urth resource.

API servers, the scheduler, and the outbox relay use distinct service identities with
least-privilege subject permissions. NATS accounts isolate Urth installations or future
tenants; Runner isolation is normally expressed through subjects and per-identity
permissions rather than one NATS account per Runner.

Adding a Worker identity to a Runner blocklist prevents future authentication and job
claims. Urth must also expire or disconnect its active NATS connection where supported.
The API claim check remains mandatory because permission changes and distributed
disconnects are not instantaneous.

### 9. Scheduling remains an Urth responsibility

NATS does not become the Scenario scheduler. Cron expressions, enabled state,
next-scheduled time, missed-run policy, Runner selection, and Result creation remain
Urth resource logic owned by the scheduler and API.

Broker-side delayed or scheduled delivery may be useful as an optimisation, but it is
not the source of the next-run calculation. Updating or disabling a Scenario must be
correct even if a previously published timer message exists. Reconciliation against
Postgres is the recovery path.

### 10. Artifacts do not travel through job or event streams

Screenshots, HAR files, logs, browser output, and other potentially large or
secret-bearing Artifacts are uploaded through the Result-scoped API to the configured
Artifact store. They are not embedded in NATS jobs, resource events, or request/reply
messages.

JetStream Object Store is not adopted as the Artifact store by this decision. Artifact
classification, access control, streaming downloads, retention, and audit need a storage
boundary designed for those resources. A future ADR may select an object store without
changing the job transport.

### 11. Stream limits must fail visibly, not silently lose jobs

Every stream and consumer has explicit storage, age, delivery, and concurrency limits.
Defaults are not accepted blindly.

For `URTH_JOBS`:

- `DiscardNew` rejects publication when a size or message limit is reached; the outbox
  remains pending and Urth alerts instead of silently evicting older unconsumed jobs;
- a per-subject limit and per-subject discard-new policy prevent one offline Runner from
  exhausting the shared stream before the global limit is reached;
- stream maximum age is an upper bound aligned with Result queue expiry, and the
  reconciler marks Results whose messages age out;
- `MaxAckPending` and Worker pull batch sizes reflect the Runner's actual concurrency so
  Workers do not reserve work they cannot claim promptly;
- acknowledgement wait covers only the short claim phase, not probe duration;
- maximum delivery attempts emit an operational signal and enter a deliberate
  dead-letter/reconciliation path; JetStream does not automatically remove a
  work-queue message merely because it reached its delivery limit; and
- stream and consumer lag, redelivery, outbox age, and pending Result age are monitored.

JetStream production storage uses persistent volumes and replication across three NATS
servers. A single non-replicated server is acceptable for local development and tests,
but is not described as highly available.

### 12. Edge NATS topology is optional, not baseline

Workers connect outbound directly to the central NATS service in the baseline
deployment. NATS leaf nodes may later provide local connectivity, traffic locality, or
store-and-forward behaviour for large or intermittently connected sites.

Urth does not require an operator to deploy a NATS server beside every Worker. Leaf-node
topology adds another security, persistence, upgrade, and failure boundary and should be
introduced only for a demonstrated edge requirement.

## End-to-end flow

```text
                  authoritative state
             ┌──────────────────────────┐
UI / CLI ───>│ API server ───> Postgres │
             └──────┬────────────┬──────┘
                    │            │ same transaction
                    │            └── outbox entry
                    │                     │
                    │              JetStream publish + ack
                    │                     │
                    │              URTH_JOBS
                    │         urth.v1.jobs.<runner-uid>
                    │                     │
                    │          durable Runner consumer
                    │              ┌──────┴──────┐
                    │           Worker A      Worker B
                    │              │
                    │<── authenticated claim
                    │──> Result running + capability
                    │              │
                    │         synchronous NATS ack
                    │              │
                    │<── Result status and Artifact uploads
                    │
                    └── URTH_EVENTS ──> scheduler / projections
```

## Failure handling

| Failure | Required behaviour |
|---|---|
| Postgres commits but NATS is unavailable | Outbox remains pending; relay retries and alerts on age. |
| Relay publishes but fails before marking the outbox row | Stable message ID suppresses near-term duplicates; consumer idempotency handles the rest. |
| Worker receives a message and dies before claim | Message remains unacknowledged and is redelivered. |
| API commits a claim but the response is lost | Same Worker retries the idempotent claim and recovers its authorization. |
| Message is redelivered after another Worker claimed it | API rejects the stale claim; Worker acknowledges the obsolete message. |
| Worker dies after claim and message acknowledgement | Result lease expires; attempt is recorded; retry creates a new Result. |
| NATS loses an expired or administratively removed message | Reconciler detects the pending Result and marks or republishes it according to policy. |
| Postgres is unavailable while NATS is healthy | No authoritative claims or scheduling occur; messages wait or are retried. |

## Architectural rules

1. Postgres resources are authoritative; NATS messages are transport envelopes and
   notifications.
2. Every durable resource-to-message transition uses the Postgres transactional outbox.
3. Runner jobs use JetStream, never Core NATS alone.
4. One `URTH_JOBS` stream carries disjoint Runner subjects; each Runner owns one durable
   pull consumer shared by its Workers.
5. Workers bind to control-plane-created consumers and never receive JetStream
   administration permissions.
6. A Worker acknowledges a job only after the API has durably accepted its atomic claim.
7. A successful broker delivery never substitutes for Worker authentication or Result
   authorisation.
8. Duplicate publication, duplicate delivery, and replay are expected and handled
   idempotently at the resource layer.
9. Exactly-once probe execution is not claimed.
10. Durable resource events use a separate replayable stream from work-queue jobs.
11. Scenario scheduling and retry policy remain explicit Urth resource logic.
12. Large or secret-bearing Artifacts never travel through job or event subjects.
13. Stream limits, expiry, poison messages, outbox lag, and consumer lag are observable
    and reconciled with Result state.
14. NATS identities and subject permissions follow ADR 0002's least-privilege Worker
    session model.

## Consequences

### Benefits

- NATS subjects map naturally to immutable Runner UIDs and logical queues.
- Durable pull consumers support multiple competing Workers and Worker-controlled
  backpressure.
- The same platform supports durable jobs, replayable control-plane events, and
  lightweight request/reply without giving all three the same delivery semantics.
- Subject-level permissions reinforce the one-Runner-per-Worker boundary.
- The outbox and reconciliation model makes service restarts and temporary NATS outages
  recoverable.
- NATS' Go implementation and official Go client fit the existing codebase.
- Leaf nodes provide a possible future path for large edge installations without making
  them part of the baseline architecture.

### Costs and constraints

- NATS with JetStream becomes a required production dependency that must be secured,
  monitored, upgraded, backed by persistent storage, and clustered for availability.
- The control plane must manage and reconcile streams, consumers, permissions, outbox
  rows, dead letters, and expired Result leases.
- One durable consumer per Runner creates a JetStream asset-scaling dimension that must
  be load tested.
- NATS and Postgres cannot participate in one transaction; the outbox and idempotent
  consumers are mandatory complexity.
- Worker claim and message-acknowledgement ordering requires careful integration tests
  around crashes and lost responses.
- Supporting NATS Auth Callout or issued NKey/JWT credentials adds key and connection
  lifecycle work.
- Operators upgrading from the prototype must migrate pending Asynq jobs or drain them
  before cutting Workers over.

## Alternatives considered

### Continue with Redis and Asynq

Asynq already provides basic durable jobs and retries, but the prototype uses one shared
queue and would need additional design for per-Runner isolation, durable resource events,
dynamic Worker permissions, and edge connectivity. Maintaining Redis plus another event
or request/reply system would broaden the operational surface. It was rejected as the
target architecture, though it remains during migration.

### Use Core NATS without JetStream

Core NATS is attractive for its simplicity and queue groups, but offline Workers miss
messages and there is no durable backlog. It was rejected for jobs and authoritative
events; it remains useful for explicitly transient communication.

### Create one JetStream stream per Runner

This makes isolation visually obvious but creates a stream and replication group for
every Runner. It was rejected in favour of one work-queue stream with disjoint subjects
and filtered durable consumers.

### Make NATS the source of truth through event sourcing

Reconstructing resources exclusively from message history would conflict with the
resource-oriented REST model, complicate queries and optimistic updates, and make stream
retention part of database correctness. It was rejected. Events are derived from
committed resources.

### Use Postgres polling and notifications only

This could avoid a broker and keep all durable state in one system. It remains a viable
simpler alternative, but implementing fair competing consumers, per-Runner backpressure,
redelivery, remote Worker transport, and request/reply would move substantial messaging
logic into Urth. NATS is selected for those capabilities.

### Use direct gRPC streams between API servers and Workers

Direct streams provide typed bidirectional communication but require Urth to implement
durable offline queues, load balancing across API replicas, replay, redelivery, and
backpressure. gRPC may still transport API operations, but it is not selected as the
durable job backbone.

### Acknowledge jobs only after probe completion

This delegates running-job recovery to JetStream but requires acknowledgement waits to
span arbitrary probe durations and allows redelivery to race with authoritative Result
state. It was rejected. NATS owns delivery until claim; the Result lease owns execution
after claim.

### Store Artifacts in JetStream Object Store

This would consolidate infrastructure, but Artifact security classification, access
control, retention, and download behaviour deserve an independent storage decision. It
was rejected by this ADR without ruling out a later evaluation.

## Migration from the prototype

1. Introduce NATS and JetStream in local development and integration tests without
   removing the existing scheduler interface.
2. Add the Postgres outbox, idempotent relay, and reconciliation metrics.
3. Create `URTH_JOBS`, publish versioned dispatch envelopes, and provision one filtered
   durable consumer per Runner.
4. Change Workers to authenticate for scoped NATS access, pull only from their Runner,
   and acknowledge immediately after an accepted API claim.
5. Add `URTH_EVENTS` and move scheduler wakeups and internal projections to durable
   consumers.
6. Test crash points, duplicate publication, NATS outages, expired leases, Runner
   deletion, credential rotation, and blocklisting.
7. Drain or explicitly migrate Asynq jobs, remove Redis/Asynq, and rename implementation
   components whose names still expose the old transport.

The migration is complete only when Runner-level routing, authentication, Result state,
and reconciliation remain correct through those failure cases.

## Implementation status at acceptance

NATS is not yet implemented in Urth. The current code publishes complete jobs to one
Redis/Asynq queue, all Workers consume that queue, and API resource writes do not use an
outbox. The scheduler is also not yet a standalone service.

This ADR defines the target transport and the constraints under which implementation may
begin. It does not describe the prototype as already satisfying the decision.

## References

- [NATS CNCF project page](https://www.cncf.io/projects/nats/)
- [What is NATS?](https://docs.nats.io/nats-concepts/what-is-nats)
- [JetStream concepts](https://docs.nats.io/nats-concepts/jetstream)
- [JetStream streams and retention](https://docs.nats.io/nats-concepts/jetstream/streams)
- [JetStream consumers](https://docs.nats.io/nats-concepts/jetstream/consumers)
- [JetStream delivery and deduplication](https://docs.nats.io/using-nats/developer/develop_jetstream/model_deep_dive)
- [NATS subject authorization](https://docs.nats.io/running-a-nats-service/configuration/securing_nats/authorization)
- [NATS Auth Callout](https://docs.nats.io/running-a-nats-service/configuration/securing_nats/auth_callout)
- [NATS request/reply](https://docs.nats.io/nats-concepts/core-nats/reqreply)
- [NATS leaf nodes](https://docs.nats.io/running-a-nats-service/configuration/leafnodes)
