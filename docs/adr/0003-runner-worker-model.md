# ADR 0003: Schedule to Runner channels and execute on Worker instances

- **Status:** Accepted
- **Date:** 2026-07-21

## Context

Urth separates the stable intent of where and how probes should run from the physical
processes that happen to be available at a given moment.

Operators deploy Worker binaries into the networks they want to observe. Those binaries
can differ in version, operating system, privileges, installed runtimes, probe plugins,
and local configuration. For example, one Worker may be able to execute ICMP probes but
not Node.js scenarios; another may have Node.js and npm but lack the privileges needed
for ICMP. Processes are restarted, upgraded, scaled, and replaced over time.

Scheduling directly to those processes would make Scenario placement depend on
short-lived infrastructure. A process disappearing between scheduling and dispatch
would require the scheduler to choose again, and operators would have no stable object
on which to express a probing vantage point, execution policy, or authentication
boundary.

Urth therefore needs two distinct concepts:

- a stable, operator-managed scheduling destination; and
- a physical executor that can join that destination when it satisfies its contract.

The distinction must be reflected consistently in resources, authentication, queues,
labels, Results, the UI, and `urthctl`.

## Decision

### 1. A Runner is a logical scheduling channel

A **Runner** is a versioned API resource created and managed by an operator through the
REST API, Web UI, or `urthctl`. It is a logical channel, not a process and not a machine.
It defines:

- the labels and network vantage point exposed to Scenario placement;
- which jobs may be scheduled to the channel;
- which Workers may join the channel;
- labels that the server propagates to queued Results and their Artifacts;
- capacity and lifecycle policy;
- a blocklist of Worker identities; and
- the authentication boundary for the channel.

Creating a Runner also creates its initial enrollment credential. The credential is
returned and handled according to
[ADR 0002](./0002-worker-authentication.md); it is not stored in the Runner manifest.
Rotating the credential changes who may establish new sessions without changing the
Runner's resource identity or queued work.

Each Runner owns one logical job queue. The queue is identified by the Runner's immutable
UID, so deleting and recreating a Runner with the same name does not attach new Workers
to an old channel or expose old queued jobs. A logical queue may be implemented as a
broker queue, routing key, partition, database lease set, or API stream. The backend is
replaceable; the per-Runner isolation semantics are not. NATS and JetStream are selected
as the initial implementation in
[ADR 0004](./0004-nats-communication-backbone.md).

### 2. A Worker is a physical executor

A **Worker** is a running process deployed and configured by an operator. Urth does not
create or place that process. Its binary and local configuration determine its actual
capabilities, including:

- Urth Worker version;
- operating system and architecture;
- available probe kinds and plugin versions;
- installed runtimes and tools such as Node.js, npm, Python, or a browser;
- privileges needed for operations such as ICMP; and
- configured minimum and maximum execution durations.

When a Worker authenticates, the control plane represents that live registration as a
**WorkerInstance** resource. The terms have deliberately different meanings:

| Term | Meaning | Lifetime |
|---|---|---|
| **Runner** | Operator-created scheduling channel and policy | Stable across Worker churn |
| **Worker** | Physical operating-system process | One process lifetime |
| **WorkerInstance** | Control-plane resource and session for a registered Worker | One active registration, renewable across reconnects |

A Worker connects to exactly one Runner channel at a time and assumes that Runner's
scheduling identity for work received from the channel. It still retains its own
identity for admission, session revocation, blocklisting, audit, and Result attribution.
Assuming a Runner identity does not collapse several Workers into one indistinguishable
principal.

There is a one-to-many relationship:

```text
Runner team-a                         Runner dmz
┌───────────────────────────┐         ┌─────────────────────┐
│ logical job queue         │         │ logical job queue   │
│                           │         │                     │
│ WorkerInstance a1         │         │ WorkerInstance d1   │
│ WorkerInstance a2         │         │ WorkerInstance d2   │
│ WorkerInstance a3         │         │                     │
└───────────────────────────┘         └─────────────────────┘
```

Running one process against multiple Runners is not supported. An operator who wants one
host to serve several channels runs separate Worker processes, each with its own Runner
enrollment, identity, configuration, and session. This keeps failure, policy, and audit
boundaries explicit.

### 3. Scheduling chooses a Runner, not a Worker

When a Scenario is triggered, the scheduler selects an eligible Runner and binds the
pending Result to that Runner. It does not select a WorkerInstance. The job is placed on
the selected Runner's logical queue and remains there until one of these events occurs:

- an admitted WorkerInstance claims it;
- its queue or execution deadline expires;
- an operator cancels it; or
- the Runner is removed and the control plane marks the pending Result accordingly.

The absence of a connected Worker does not change the scheduling abstraction. A job can
wait for capacity to join later. The scheduler may use current capacity as an input to
Runner selection, but it must not bind the Result to a particular process.

The pending Result records the selected Runner UID, name, version, and a snapshot of its
propagated labels at scheduling time. The Worker identity is initially empty. When a
WorkerInstance wins the claim, the API records that instance as the executor. This
preserves the distinction between **where the scheduler sent the job** and **which
physical process executed it**.

Each Result is bound to one Runner. If a Scenario must execute from several vantage
points, the scheduler creates a separate Result and queue entry for each selected Runner.
Moving or retrying work on another Runner is a new, explicit scheduling decision; a
Worker from another channel cannot steal the existing job.

### 4. Runner policy forms a channel contract

A Runner sits between jobs and Workers. Its policy has separate, explicitly named parts;
one ambiguous `requirements` field must not serve all of them.

#### Scenario placement requirements

A Scenario declares the properties required of its probing vantage point. The scheduler
matches those selectors against operator-controlled Runner labels such as region,
network, environment, or a guaranteed channel capability.

These labels describe the Runner contract, not whichever Worker is online. If a Runner
advertises that its channel can execute ICMP or Node.js probes, its Worker admission
policy must require every Worker joining that channel to provide that capability.

#### Runner job requirements

A Runner defines which jobs it accepts. Job requirements may constrain Scenario or
Result labels and typed execution properties such as probe kind or maximum duration.
They let the owner of a network vantage point prevent unsuitable work from entering its
queue even when a Scenario selector would otherwise match.

A scheduling decision succeeds only when both directions agree:

1. the Runner's labels satisfy the Scenario's placement requirements; and
2. the job satisfies the Runner's job requirements.

#### Runner Worker requirements

Possession of a Runner enrollment credential is necessary but not sufficient to join
its queue. The registering Worker must also satisfy the Runner's Worker requirements.
Those requirements may include:

- exact or set-based label matches;
- a minimum Urth Worker version;
- minimum Node.js, npm, Python, browser, or plugin versions;
- required probe kinds such as ICMP, DNS, HTTP, or browser execution;
- required operating system, architecture, or privilege declarations; and
- supported minimum and maximum probe durations.

Version ranges and durations are typed constraints, not ordinary string labels. They are
compared using their domain semantics—for example, semantic-version ordering and parsed
durations—rather than lexicographical label comparison. Labels remain appropriate for
identity, equality, membership, selection, and grouping.

The Worker submits a capability document during authenticated registration. The server
validates and stores an effective capability snapshot on the WorkerInstance. Admission
and later claim checks use that server-stored snapshot, not values resubmitted with a
claim.

The Runner's job and Worker requirements must describe a coherent channel: every Worker
admitted to the queue must be able to execute every class of job admitted to it. Different
binary versions and configurations are allowed within one Runner only when they all meet
that common contract. Operators create separate Runners when they need materially
different capability pools.

The API rechecks the claiming Worker's stored capabilities against the concrete job when
it claims work. This is defence in depth against stale policy or incorrectly declared
capabilities; it is not a substitute for coherent Runner admission rules.

### 5. Runner labels are inherited by execution history

Runner labels serve two related purposes:

- they are the stable surface against which Scenarios select a vantage point; and
- they annotate the Results and Artifacts produced from that vantage point.

At scheduling time, the server copies the Runner's configured propagation labels onto
the pending Result and adds reserved identity labels including Runner name, UID, and
version. At claim time it adds the winning Worker identity. Artifacts inherit these
labels from their Result, with security-sensitive and identity labels derived or
overridden server-side.

The labels are snapshots. Editing or deleting a Runner later does not rewrite historical
Results or Artifacts. This makes queries such as “all failures observed from the Sydney
DMZ” meaningful across Worker replacements and Runner changes.

A Worker cannot override Runner, Result, executor, or security-classification labels in
an upload. Worker-local labels may be accepted into a separate, clearly identified
namespace when they are useful for diagnosis, but they are never authoritative channel
identity.

### 6. Worker identity and blocklisting are distinct from display names

Each Worker presents a stable identity during enrollment. The Runner blocklist contains
those stable identities and is part of the Runner resource definition. The server checks
the blocklist before creating or refreshing a WorkerInstance and again before allowing a
job claim, so adding an already-connected Worker takes effect without waiting for its
session to expire.

A blocklist identity must be stable and verifiable—for example, a public-key fingerprint
proved during enrollment or an operator-issued per-Worker credential. A hostname,
display name, process-supplied UUID, or server-assigned WorkerInstance UID alone is not a
sufficient security identity because a rejected process could simply present another
value. The exact proof mechanism may evolve with worker authentication, but the
blocklist key must not be self-selected without proof.

The human-friendly Worker name remains metadata for logs and UI. The WorkerInstance UID
identifies one control-plane registration. Neither replaces the stable security identity
used by the blocklist.

A Runner blocklist denies known Worker identities; it cannot protect against an operator
or attacker who holds the shared Runner enrollment credential and can provision an
entirely new, valid Worker identity. Rotating the enrollment credential or disabling the
Runner is the response at that broader trust boundary.

### 7. Queue membership follows authenticated Runner membership

A Worker may consume from a Runner queue only after enrollment and Worker-session
authentication succeed. Queue access is scoped to that Runner UID. A Worker session for
one Runner cannot subscribe to or claim work from another Runner's queue.

The job transport may enforce this with scoped broker credentials, per-Runner streams,
or an authenticated API connection. Regardless of transport, the API performs the final
claim checks from [ADR 0002](./0002-worker-authentication.md). Receiving or reading a
queue message is not authorisation to execute it.

Workers renew their sessions and registration before their TTL expires. A disconnected
or expired WorkerInstance stops counting as channel capacity. Its disappearance does not
delete the Runner or its queued jobs.

## Illustrative Runner contract

The following manifest illustrates the separation of concerns. Field names and exact
constraint syntax remain subject to the versioned API schema; the policy boundaries are
normative.

```yaml
apiVersion: v1
kind: runners
metadata:
  name: syd-dmz
  labels:
    region: ap-southeast-2
    network: dmz
spec:
  active: true

  jobRequirements:
    probKinds: [http, dns]
    maxDuration: 30s

  workerRequirements:
    workerVersion: ">=1.4.0"
    probKinds: [http, dns]
    runtimes:
      node: ">=20.0.0"
      npm: ">=10.0.0"
    maxRunDuration: ">=30s"

  propagatedLabels:
    vantage: syd-dmz
    owner: network-operations

  blockedWorkers:
    - identity: "sha256:example-worker-public-key-fingerprint"
      reason: "retired host"
```

## Architectural rules

1. Operators create Runners; deployed processes create or refresh WorkerInstances only
   through authenticated enrollment.
2. A Runner is a stable logical channel with one logical queue and zero or more
   WorkerInstances.
3. A Worker process has one active Runner membership and consumes only from that
   Runner's queue.
4. Scheduling binds a Result to a Runner, never directly to a WorkerInstance.
5. The selected Runner is recorded while the Result is pending; the Worker is recorded
   only when a claim succeeds.
6. A job remains bound to its selected Runner until completion, expiry, cancellation, or
   an explicit new scheduling decision.
7. Scenario placement, Runner job admission, and Runner Worker admission are separate
   policy evaluations with explicit names and ownership.
8. Enrollment credentials do not bypass Worker requirements or the blocklist.
9. Every Worker admitted to a Runner must satisfy the channel contract for every job
   class that Runner accepts.
10. Version and duration constraints use typed comparison rather than string-label
    ordering.
11. Runner and executor labels on Results and Artifacts are server-derived snapshots and
    cannot be overridden by Workers.
12. Blocklists use stable, verifiable Worker identities rather than display names or
    unproved self-reported IDs.

## Consequences

### Benefits

- Scenarios target durable probing vantage points rather than ephemeral processes.
- Worker binaries can be upgraded, restarted, and horizontally scaled without changing
  Scenario placement.
- Jobs can wait for Runner capacity instead of being coupled to currently connected
  Workers.
- Runner policy gives network owners control over both incoming work and joining
  executors.
- Capability floors make every Worker in a channel interchangeable for its accepted job
  classes.
- Results preserve both the scheduled vantage point and the physical executor.
- Per-Runner queues isolate consumption, failures, and operational backlogs.
- Blocklisting can remove one known Worker without disabling the whole Runner pool.

### Costs and constraints

- The scheduler, broker adapter, and API must preserve a Runner binding throughout the
  Result lifecycle.
- Per-Runner queue isolation requires dynamic queue or partition management.
- Runner policy needs separate schemas for label selectors, semantic versions,
  durations, capabilities, and blocklisted identities.
- Operators must design coherent Runner contracts and may need several Runners on one
  host for different capability or policy classes.
- Worker capability claims are partly self-reported unless backed by attestation; the
  control plane can validate consistency but cannot remotely prove every local property.
- Stable, verifiable Worker identities require key or credential lifecycle management.
- Jobs may remain pending when a Runner has no eligible capacity, so expiry and backlog
  visibility are required operational features.

## Alternatives considered

### Schedule directly to WorkerInstances

This would let the scheduler choose an exact capability set, but it binds durable work to
ephemeral processes and makes restarts, scaling, and failover scheduler concerns. It was
rejected in favour of Runner-level placement and late Worker claims.

### Treat each Runner as one physical Worker

A one-to-one model is superficially simpler but prevents pooling, rolling upgrades, and
horizontal scaling behind a stable vantage point. It also conflates operator intent with
process state. It was rejected.

### Use one global queue and let Workers filter jobs

This weakens channel isolation, permits incompatible Workers to consume work, and can
cause retries, starvation, or head-of-line blocking. It also makes queue access look like
authorisation. It was rejected in favour of logical queues per Runner.

### Allow one Worker process to join several Runners

This saves processes but mixes credentials, policy, backpressure, and failure domains.
It becomes unclear which Runner identity a process is exercising when it claims work. It
was rejected; operators run separate Worker processes when one host serves several
channels.

### Use one generic label selector for every requirement

Label selectors work well for equality and set membership, but version ranges and
duration bounds require typed ordering. Overloading a single `requirements` field also
obscures whether it applies to Scenarios, jobs, or Workers. It was rejected in favour of
separate policy sections and typed constraints.

### Let possession of the Runner credential bypass admission policy

The credential would become both a shared secret and unrestricted queue membership.
One incompatible or retired Worker could then join and consume jobs. It was rejected;
credential validation, Worker requirements, and blocklist checks are cumulative gates.

## Implementation status at acceptance

The current code already provides part of the model:

- Runner and WorkerInstance are separate resources with a one-to-many relationship;
- a Worker registers under exactly one Runner using a Runner-bound token;
- Runner activity, a registration selector, and maximum instances are checked during
  registration;
- Workers report runtime and probe capability labels;
- individual WorkerInstances can be paused or removed; and
- a claimed Result records both Runner and WorkerInstance identity.

The current development implementation does **not** yet satisfy the complete decision:

- all Workers consume a shared Asynq queue rather than the JetStream-backed logical queue
  per Runner selected by ADR 0004;
- scheduling searches current WorkerInstances instead of selecting and recording a
  Runner independently of connected capacity;
- the pending Result does not record its scheduled Runner before claim;
- Runner job requirements, Worker requirements, and propagated labels are not separate
  policy surfaces;
- semantic-version and duration constraints do not have a typed admission schema;
- Runner metadata and propagation labels are not snapshotted consistently onto Results
  and Artifacts by the server;
- Runner creation and enrollment-credential issuance are separate operations;
- stable verifiable Worker security identities and Runner blocklists are not implemented;
  and
- broker access is not scoped by authenticated Runner membership.

These gaps describe migration from the prototype's shared queue and selectors to the
accepted channel model. They do not change the architectural rules above.
