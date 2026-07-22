# NATS Runner Review Backlog Context

This document is shared context for the agent-ready tasks in [`tasks/`](tasks/).
Read it before claiming a task. Each task repeats its local requirements, while
this document records cross-cutting language and invariants that must remain
consistent across workstreams.

## Review Baseline

The backlog was created after reviewing the NATS Worker implementation preserved
in commit `1e13334` on branch `feat/nats-worker-jetstream`. The implementation is
a useful development slice, but it is not yet the production-ready realization
of ADRs 0002–0004. In particular, its own migration notes acknowledge missing
outbox, reconciliation, broker authorization, blocklisting, and dead-letter work.

Line numbers in tasks refer to that baseline. They are evidence anchors, not
authority: revalidate them after merging earlier tasks.

## Product and Execution Flow

Urth is a resource-oriented synthetic-monitoring platform:

1. An operator creates a `Runner`, a stable logical scheduling channel.
2. A deployed Worker process enrolls against exactly one Runner and is represented
   by a `WorkerInstance` resource and short-lived session.
3. A manual trigger or future scheduler creates a pending `Result`, selects a
   Runner, and durably dispatches a small envelope to that Runner's queue.
4. One Worker sharing the Runner's durable consumer pulls the envelope and asks
   the API to claim the Result using its Worker session.
5. The API atomically records the physical executor and execution lease, then
   returns the immutable execution snapshot and a Result-scoped capability.
6. The Worker acknowledges the broker message, executes the probe, and uploads
   status and Artifacts through the authoritative REST API.

Postgres resources are authoritative throughout. NATS and JetStream carry
notifications and dispatch envelopes; broker state never replaces resource
validation or ownership.

## Domain Language

- **Runner**: operator-created, long-lived scheduling channel and policy boundary.
- **Worker**: a physical process deployed by an operator.
- **WorkerInstance**: the API resource/session representing one registered Worker.
- **Result**: one immutable execution attempt and its lifecycle history.
- **Dispatch**: the durable notification that one pending Result has work available.
- **Claim**: the atomic pending-to-running transition assigning a WorkerInstance.
- **Execution lease**: the server-controlled deadline recorded on a running Result.
- **Run capability**: short-lived authority to report one claimed Result and its
  Artifacts; it is not a general Worker or Runner credential.
- **Runner queue**: the exact subject `urth.v1.jobs.<runner-uid>` plus one durable
  pull consumer shared by all Workers enrolled in that Runner.
- **Outbox**: a Postgres row written in the same transaction as a resource change,
  later relayed to JetStream using a stable message ID.

## Required Invariants

The accepted ADRs are the source of truth. Tasks implement them; they do not
silently weaken them.

### Resource authority and durability

- Postgres remains authoritative for placement, executor identity, deadlines,
  Result state, and Artifact linkage.
- Every durable resource-to-message transition uses a transactional outbox.
- Duplicate publication and delivery are expected and handled idempotently.
- A reconciler detects unpublished outbox rows, missing or stale dispatches,
  expired execution leases, and terminal Results with leftover messages.
- A retry after an execution lease expires creates a new Result; it never erases
  or reopens historical execution state.

### Runner and Worker model

- Scheduling binds a Result to a Runner, never directly to a Worker process.
- One Worker connects to exactly one Runner; many Workers share one Runner consumer.
- Scenario placement, Runner job admission, and Runner Worker admission are
  separate policy evaluations with explicit types and ownership.
- Every Worker admitted to a Runner must be able to execute every job class that
  Runner accepts. The concrete job is rechecked at claim time.
- Runner and executor labels on Results and Artifacts are server-derived snapshots.
- Blocklists use stable, verifiable Worker security identities, not display names.

### Authentication and authorization

- Runner enrollment, Worker sessions, NATS connection authority, and run
  capabilities are distinct credentials with narrowing authority.
- Operator authentication protects enrollment issuance and rotation.
- NATS authority is short-lived and scoped to one Worker and Runner consumer; a
  Worker cannot publish jobs or administer JetStream assets.
- Claim identity comes only from a validated Worker session. Request-body IDs,
  labels, queue possession, and network location are not identity evidence.
- Run capabilities carry an algorithm allowlist, key ID, issuer, audience,
  Result/Runner/Worker identity, explicit scopes, and bounded expiry.
- State-sensitive authorization reloads current resources; a valid signature
  never overrides pause, disable, cancellation, expiry, or terminal state.

### JetStream behavior

- `URTH_JOBS` is one file-backed WorkQueue stream over `urth.v1.jobs.*`.
- Each Runner has one exact-filter durable pull consumer addressed by immutable UID.
- Workers bind existing assets and never create or update streams or consumers.
- The Worker pulls only when local capacity exists, claims through the API, and
  synchronously acknowledges after the durable claim succeeds.
- Transient claim failures are retried; stale dispatches are acknowledged;
  permanent policy mismatches and poison messages enter an observable operational
  path. These outcomes must not collapse into one HTTP status.
- Production assets have explicit storage, age, delivery, acknowledgement, and
  concurrency limits plus monitoring.

## Package and Component Map

- `pkg/urth/`: resource models, service interfaces, placement, claims, token
  issuance/validation, and authoritative state transitions.
- `pkg/natsq/`: NATS naming, stream/consumer provisioning, dispatch envelope,
  publication, and transient live-log transport.
- `cmd/api-server/`: HTTP status mapping and composition of store, scheduler,
  signing keys, transport provider, and log streaming.
- `cmd/nats-worker/`: enrollment, session renewal, NATS connection, pull/claim/ack,
  execution, and reporting.
- `pkg/runner/`: probe execution and Worker capability/label production.
- `pkg/redqueue/` and `cmd/asynq-runner/`: migration-only legacy transport.
- `website/`: resource UI and live run-log client.

Keep dependencies pointing from composition toward domain interfaces and concrete
adapters. `pkg/urth` must not import `pkg/natsq`; transport-specific implementations
implement interfaces owned by the domain package.

## Agent Workflow

1. Choose a `ready` task whose dependencies are `done`.
2. Read the full task, this context, and ADRs linked by the task.
3. Inspect `git status --short` and the task index for conflicts before editing.
4. Mark the task `in-progress` with an owner and branch.
5. Keep changes within the Required Outcome and Non-Goals.
6. Add tests at the failure boundary named by the task.
7. Fill the completion record, mark the task `done`, and update the index.

When a task makes an enduring choice not settled by an accepted ADR, add or
supersede an ADR rather than burying that choice in implementation comments.

## Validation Baseline

The reviewed implementation passed:

```text
go test -race -count=1 ./...   pass
go vet ./...                   pass
website npm test               12 files, 152 tests passed
git diff --check               pass
```

Those checks do not provide an end-to-end registration/claim/execution test and
do not exercise the crash boundaries named in ADR 0004. Task 011 owns that gap.
