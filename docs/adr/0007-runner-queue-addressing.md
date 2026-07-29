# ADR 0007: Address Runner queues by name; keep run entitlement keyed by UID

- **Status:** Accepted
- **Date:** 2026-07-29
- **Supersedes:** [ADR 0004](./0004-nats-communication-backbone.md) §3, on queue addressing only

## Context

[ADR 0004](./0004-nats-communication-backbone.md) §3 addresses a Runner's queue by its
immutable UID: the subject is `urth.v1.jobs.<runner-uid>` and the durable pull consumer is
`runner-<runner-uid>`. Its stated reason is one sentence: "Recreating a Runner with the
same name cannot expose the old Runner's queued messages or consumer."

Two things have changed since that was written, and one thing was never decided at all.

**Nothing removes a queue.** `EnsureRunnerConsumer` is called at dispatch time
(`pkg/natsq/scheduler.go:132`) and by the reconciler for every active Runner
(`pkg/natsq/reconcile.go:19`). There is no counterpart. `runnersAPIImpl.Delete`
(`pkg/urth/service.go:1364`) is a bare store delete with no transport hook, and `grep
DeleteConsumer` outside tests finds nothing. Deleting a Runner therefore leaves its durable
consumer in JetStream permanently and its queued messages until stream `MaxAge` evicts
them. This was observed directly while verifying
[task 013](../review-backlog/tasks/013-bound-and-observe-jetstream.md): after a test run
wiped the resource tables, the broker still held eight consumers for Runners that no longer
existed, and the reconciler correctly created a ninth for the re-applied Runner. Consumers
are a bounded JetStream asset with real memory and metadata cost, so an installation that
churns Runners accumulates queues that nothing will ever read.

**The recreate case is now the common one, and it is not exotic.** A Runner is
infrastructure-as-code: it is applied from a manifest, and the natural way to move it
between environments, rebuild an installation, or restore from a backup is to delete and
re-apply the same manifest. That produces a resource with the same name and a new UID —
what Kubernetes would call a new generation of the same object. Under UID addressing, every
such event silently strands a queue and orphans a consumer, and the operator's mental model
("this is still team-a's runner") disagrees with the broker's ("this is a different
runner").

**The protection ADR 0004 §3 was reaching for is now enforced in Postgres.** When §3 was
written, queue possession was close to the only thing binding a run to the Runner it was
placed on. It no longer is. `resultsAPIImpl.Create` records the placement decision on the
pending Result itself — `Status.Executor.RunnerID` is set at `pkg/urth/service.go:656`,
before the row is committed — and `ClaimRun` refuses any worker whose Runner UID is not
that one (`pkg/urth/service.go:945`), classifying the attempt as `ClaimObsolete`. A worker
holding a message it is not entitled to claim therefore learns so from the API and acks it.

Two further facts constrain the answer.

**Runner names are already immutable.** `runnersAPIImpl.update` refuses a manifest whose
name differs from the stored resource (`pkg/urth/service.go:1319`). A name-addressed queue
cannot move under a live Runner, because the name cannot change without deleting the
resource. This removes the objection that would otherwise be fatal.

**Resource names are not valid NATS subject tokens.** wyrd validates a resource name with
`^[a-z0-9]([a-z0-9\.\-]*[a-z0-9])?$` and a 253-character limit
(`manifest.ValidateSubdomainName`). Dots are legal. A Runner named `team-a.probes` would
produce `urth.v1.jobs.team-a.probes`, which is four tokens after `jobs` — it does not match
the single-token wildcard `urth.v1.jobs.*`, so it would be published into a subject no
consumer covers and no stream captures. This is a silent failure mode and the addressing
scheme has to close it explicitly rather than assume well-behaved names.

## Decision

### 1. A Runner's queue is addressed by its name, not its UID

The subject carrying jobs for a Runner and the name of its durable consumer are derived
from the Runner's `metadata.name`. A Runner's queue is therefore a property of the *name*,
which is the stable operator-facing handle, rather than of a particular resource
generation.

The consequence intended by this decision: deleting a Runner and re-applying the same
manifest reattaches the new generation's Workers to the same queue. The operator's unit of
reasoning and the broker's unit of storage become the same thing.

### 2. Names are encoded into a single subject token, and the encoding is total and injective

A name is not used raw. It is encoded by a function that is:

- **total** over every name `manifest.ValidateSubdomainName` accepts, so no valid Runner
  can fail to get a queue;
- **injective**, so two distinct names can never share a queue;
- **a single NATS subject token**, containing none of `.`, `*`, `>`, or whitespace; and
- **valid as a durable consumer name** and within the broker's length limit.

The recommended scheme, because it satisfies all four without a hash: map `.` to `_`. The
name charset is `[a-z0-9.-]`, so `_` cannot occur in a valid name and the mapping is
reversible and collision-free. Names whose encoding would exceed the broker's consumer-name
limit take a deterministic truncation-plus-digest suffix; the encoding remains injective.

A name that fails validation never reaches the encoder. Deriving a subject from an
unvalidated name is the failure this section exists to prevent.

### 3. Inheriting a queue is not inheriting an entitlement

A new generation inherits the *messages* in its predecessor's queue. It does not inherit
the right to execute them.

Entitlement remains keyed by UID and remains decided in Postgres. A pending Result records
the UID of the Runner it was placed on, and `ClaimRun` refuses a worker of any other Runner
— including a Runner that merely shares the name. Messages a new generation inherits for
runs placed on the old one are therefore refused as `ClaimObsolete`, acknowledged, and
removed.

This is the whole safety argument for §1, and it must not be weakened. Specifically:

- the placed-Runner check in `ClaimRun` is not relaxed to compare names;
- a Result's placement is never rewritten to point at a new generation; and
- queue possession is never treated as evidence of entitlement, per `CONTEXT.md`.

What §1 buys is that a recreated Runner drains its predecessor's queue promptly instead of
leaving it to age out, and that its own new dispatches arrive in a queue its Workers are
already bound to. What it deliberately does not buy is resumption of the old generation's
work. A run placed on a Runner that no longer exists is a failed run, and it is reported as
one — not silently re-executed somewhere the placement decision never chose.

### 4. Queue lineage is visible, not implicit

Because a queue outlives the resource generation that created it, an operator must be able
to see that. A Runner's queue view reports the generation whose dispatches it currently
carries, and inherited-but-unclaimable messages are counted and named as such rather than
folded into a queue-depth number that reads as backlog.

This is the per-Runner pipeline model owned by
[task 016](../review-backlog/tasks/016-runner-queue-operator-visibility.md); this ADR adds
inherited messages to the stages it must distinguish.

### 5. Queues whose Runner does not exist are garbage-collected

Name addressing makes the recreate case work; it does not address the delete case. A Runner
deleted and not recreated still leaves a consumer nothing will read.

The control plane therefore reaps queue assets that no Runner resource claims. Three rules
make this safe:

- **Absence of the resource is the only trigger.** A *disabled* Runner keeps its queue: it
  is a policy state, not a deletion, and its queued work is meant to survive re-enabling.
- **A failed or empty resource listing is never read as "no Runners exist."** The reaper
  acts on a listing it knows to be complete; a store error aborts the sweep. Without this
  rule one failed query deletes the entire fleet's queues. This is the single most dangerous
  edge in the design.
- **A grace period separates deletion from reaping,** so that a delete-then-re-apply
  performed as two operator commands does not lose the queue in between.

Reaping is a control-loop responsibility under [ADR 0006](./0006-control-loop-placement.md),
alongside the reconciler, and reports what it removed.

### 6. The name-keyed layout is a new transport version

The existing subject prefix carries a major version precisely so an incompatible layout can
coexist (`pkg/natsq/config.go:33-35`). Name addressing uses it: the name-keyed layout is
`urth.v2.jobs.<encoded-name>`.

Bumping rather than reusing `v1` is not ceremony. A Runner may legally be *named* like a
UUID — `550e8400-e29b-41d4-a716-446655440000` satisfies `ValidateSubdomainName` — so a
name-keyed subject under the `v1` prefix could collide with the UID-keyed subject of an
unrelated Runner. The version bump makes the two namespaces disjoint by construction.

Migration is a drain, not a copy. JetStream cannot move a message between subjects, and
republishing dispatches would defeat the `Nats-Msg-Id` deduplication the outbox depends on.
So: publication switches to `v2`; existing `v1` consumers are left to drain and are removed
when empty or when the reconciler's stale-dispatch sweep has accounted for their contents;
the `v1` subject is dropped from the stream once no consumer remains. Both subject patterns
belong to the same stream during migration — a second stream would double the persistence
and replication cost ADR 0004 §3 declined to pay.

## Architectural rules

- A Runner's queue subject and consumer name are derived from `metadata.name` through the
  validated encoder in `pkg/natsq`, never by string concatenation at a call site.
- The encoder is total, injective, and produces one subject token. It is tested as such.
- A name is validated before it is encoded.
- Entitlement to execute a run is decided in Postgres from the Result's placed Runner UID.
  No transport-level fact substitutes for it.
- Runner names remain immutable while the resource exists.
- Queue assets are created and removed only by the control plane. Workers bind.
- The reaper acts only on a complete resource listing, only on absent resources, and only
  after a grace period.

## Consequences

### Benefits

- A delete-and-re-apply of the same manifest — the ordinary way infrastructure-as-code
  moves — no longer strands a queue or orphans a consumer.
- The operator's unit of reasoning ("team-a's runner") and the broker's unit of storage
  are the same object, so a queue can be found from a name without a UID lookup.
- Stale messages from a previous generation are drained by the new one within its Workers'
  pull cadence, instead of occupying per-subject quota until `MaxAge`.
- Consumer count becomes bounded by the number of Runner *names* an installation has used
  rather than by the number of generations it has created.
- Subjects and consumer names become legible in broker tooling: `runner-team-a` rather than
  `runner-9f2c…`.

### Costs and constraints

- Two addressing schemes exist during migration, and the drain is not instantaneous. An
  installation is in a mixed state for as long as `v1` messages remain.
- The encoding is a new correctness surface. An encoder that is not injective silently
  merges two Runners' queues, which is why §2 states the properties as testable
  requirements rather than describing an implementation.
- A queue now outlives the resource that created it, so "this queue is deep" no longer
  implies "this Runner has a backlog". §4 exists to keep that from misleading an operator.
- The reaper is a destructive control loop operating on a derived view of authoritative
  state. Its safety rests entirely on §5's three rules.
- Name reuse becomes operationally meaningful: reusing a decommissioned team's Runner name
  for an unrelated purpose inherits its queue. Harmless under §3 — the messages are refused
  — but visible, and worth saying in operator documentation.

## Alternatives considered

### Keep UID addressing and only add reaping

The minimal change: leave ADR 0004 §3 alone, add a garbage collector, accept that a
recreated Runner starts with an empty queue and its predecessor's messages age out.

Rejected because it leaves the operator model and the broker model disagreeing about what a
Runner is, and because it makes the common case (re-apply a manifest) permanently lossy in
a way that has no operator-visible explanation. It is, however, the safe fallback if the
migration in §6 proves more disruptive than expected: the reaper in §5 is required either
way and is specified so that it does not depend on §1.

### Address by name and let a new generation claim its predecessor's runs

The maximal reading of "inherit the queue": treat same-name Runners as the same scheduling
channel for entitlement too, so queued work resumes across a recreate.

Rejected. Placement decided that a run belongs on a Runner with particular labels, in a
particular network segment — which is the product's core premise. A new generation may
carry entirely different labels and sit somewhere else entirely; a name is a handle, not a
guarantee of identity or location. Executing a placement decision against a resource the
decision never evaluated would make the label selector advisory. The claim response is also
the one place the execution snapshot — probe definitions, and any credentials in them — is
disclosed, so this alternative is a disclosure path as well as a placement violation.

### Encode the name with a hash rather than a character mapping

A digest of the name is trivially a valid single token of bounded length.

Rejected as the default because it discards the legibility that is one of §1's main
benefits: `runner-3f9a2c…` is no easier to correlate than the UID it replaced. A digest is
retained only as the overflow path for names too long to encode directly.

### Make queue identity a separate, explicitly declared field on the Runner

Let an operator declare `spec.queue: team-a` independently of the resource name, so
inheritance is opt-in.

Rejected as unnecessary indirection. It adds a second identifier operators must keep
consistent, and its only advantage over §1 is the ability to *avoid* inheriting a queue —
which is achievable by choosing a different name, and which §3 already makes safe.

## Implementation status at acceptance

Not implemented. The current code is UID-keyed at `pkg/natsq/config.go:54,59` and performs
no reaping.

- [Task 021](../review-backlog/tasks/021-name-keyed-runner-queues.md) implements §1, §2,
  §5, and §6.
- [Task 022](../review-backlog/tasks/022-recheck-requirements-at-claim.md) closes a related
  gap this ADR relies on being closed: `ClaimRun` verifies the placed Runner UID but never
  rechecks the execution snapshot's `Requirements` against the Runner's *current* labels,
  so label drift on a live Runner can let it execute a run it no longer matches.
- [Task 016](../review-backlog/tasks/016-runner-queue-operator-visibility.md) carries §4.

## References

- [ADR 0003](./0003-runner-worker-model.md) — Runner as a logical scheduling channel.
- [ADR 0004](./0004-nats-communication-backbone.md) — the stream, consumer, and claim model
  this ADR amends.
- [ADR 0006](./0006-control-loop-placement.md) — where the reaper runs.
- `pkg/natsq/config.go`, `pkg/natsq/assets.go` — current addressing and provisioning.
- `pkg/urth/service.go:656,945,1319` — placement recording, the claim's Runner check, and
  name immutability.
