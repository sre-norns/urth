# ADR 0006: Run control loops in-process by default, behind an extractable boundary

- **Status:** Accepted
- **Date:** 2026-07-28

## Context

Urth's control plane has acquired periodic background work that belongs to no request.
[ADR 0004](./0004-nats-communication-backbone.md) requires two such processes: an outbox
relay that publishes committed dispatch rows, and a reconciler that detects and repairs
drift between Postgres resources and JetStream state. Both are now implemented and both
run inside every API server replica.

ADR 0004 §2 explicitly permits the relay to run "inside API server replicas or as a
separate process, but competing relays must claim outbox rows safely." It assigns no home
to the reconciler at all — it says only that "a periodic reconciler detects" four classes
of drift. The reconciler's placement was therefore settled by implementation rather than
by decision, in
[task 003](../review-backlog/tasks/003-reconcile-dispatch-and-execution.md).

That raised two questions worth answering explicitly rather than leaving to the next
person who reads `cmd/api-server/main.go`:

- The API server now serves requests *and* performs periodic maintenance. Has its
  responsibility surface grown in a way that will be regretted?
- Reconciliation decides that a run has failed. Has it absorbed policy that belongs to a
  scheduler?

Three facts constrain the answer.

**There is no scheduler service to move this work into.** `urth.Scheduler` is a
one-method transport publisher — `Schedule(ctx, Result) (RunID, error)` — and not the
component ADR 0004 describes. The scheduler that owns cron evaluation, missed-run policy,
and Runner selection does not exist; ADR 0004's own implementation-status section records
that, and `TODO.md` defers it pending a design pass of its own. Relocating reconciliation
"into the scheduler" would mean building the scheduler now and making reconciliation its
first payload.

**Replica count does not multiply the work.** The reconciler takes a row lease before
each scan and releases it after, so a fleet of API servers performs approximately one scan
per interval in total; the replicas that lose the race perform one indexed `UPDATE` and
report the scan skipped. Every Result transition is version-guarded underneath that, so
the lease is an efficiency measure over an already-correct design.

**The separation already exists where it is expensive to retrofit.** The reconciler is a
package-level component with an interface-shaped dependency set — a store interface and a
transport interface, both owned by `pkg/urth`. It touches no HTTP router, no signing keys,
and no session configuration. What the reconciler shares with the API server is a
*process*, not a responsibility.

The remaining question is therefore about process boundaries specifically: failure
domains, privileges, scaling axes, and operational control. Those are real architectural
properties, not deployment trivia, and this ADR settles how Urth treats them — for the
two loops that exist and the three that are anticipated (the scheduler, Artifact retention
acting on data classification, and the dead-letter workflow in
[task 012](../review-backlog/tasks/012-dead-letter-workflow.md)).

## Decision

### 1. Control loops are a distinct component class

Urth recognises **control loops** as a named component class, separate from request
handlers and from transports. A control loop:

- runs periodically rather than in response to a request;
- reads authoritative Postgres state and drives observed state toward it;
- is safe to run concurrently in several processes, coordinating through a database lease
  and optimistic resource versions rather than through elected leadership;
- is idempotent, safe to repeat, and safe to resume after a partial failure; and
- sits on no request's latency path, so its interval is a detection-resolution choice
  rather than a throughput one.

The dispatch relay and the dispatch/execution reconciler are control loops. The scheduler,
Artifact retention, and dead-letter processing will be. Anything meeting this description
is composed and operated by the rules below rather than being wired ad hoc into whichever
command happened to need it first.

### 2. Control loops are composed in `pkg/controllers`

Loop composition — building a loop from a database handle, a transport, and configuration,
then supervising it — lives in `pkg/controllers` and is callable from any command. It is
not private to `cmd/api-server`.

This is the load-bearing part of the decision. Placement becomes a property of a command's
composition rather than of the loop's implementation, which is what makes §6 a
configuration change instead of a refactor. It also gives the anticipated loops somewhere
to land that is not "another goroutine in `main`."

`pkg/controllers` may depend on `pkg/urth` and on transport packages. Nothing in
`pkg/urth` depends on `pkg/controllers`; the dependency direction of
[CONTEXT.md](../review-backlog/CONTEXT.md) is unchanged — composition points toward the
domain.

### 3. Every control loop runs in every API server replica by default

The default deployment runs each loop in every API server replica, disableable per loop by
flag.

The reasoning is asymmetric failure cost. A deployment where competing loops run is a
deployment where the lease and the version guards do their job — the outcome is one
winner and one no-op. A deployment where *nobody* remembered to start the loop is a
deployment where every run sits pending forever, or where abandoned runs stay `running`
forever, and neither condition announces itself. Urth is self-hosted software whose
premise is that operators deploy it inside their own segmented networks; a fourth
mandatory central process is a real cost paid by every operator, and a silent
correctness failure is a bad thing to make opt-out.

Correctness under concurrency is a property of the loop, not of the deployment. A loop
that is unsafe to run in several replicas does not get a deployment restriction; it gets
fixed.

### 4. A control loop must not be able to take down its host

A loop hosted in a process that also serves requests is a shared failure domain, and
that sharing is only acceptable if it is bounded:

- a panic in a loop is recovered, logged, and the loop restarted with backoff — a defect
  in periodic maintenance must not become an API outage;
- a loop participates in the host's shutdown, so a rolling restart does not hard-kill a
  scan or a publication mid-transaction; and
- a loop's failures are reported and never propagated to the host. A broker that is
  unwell must not stop the API server from recording Results for the relay to publish
  when it returns.

These obligations belong to the supervision in `pkg/controllers` rather than to each loop,
so a new loop inherits them.

### 5. Control-loop liveness is a fleet property, observed in Postgres

The question an operator asks is "is anything reconciling?", and that is a question about
the fleet, not about a process.

In-process status is not a sufficient answer and is actively misleading under §3: a
replica that lost the lease truthfully reports that it skipped its scan, which is
indistinguishable from a fleet where nothing is scanning at all. A healthy loop is also
deliberately quiet — the reconciler logs only when it repaired or failed something — so
absence of log output means both "nothing is wrong" and "nothing is running."

The authoritative liveness signal for a leased loop is therefore **the age of its lease
row in Postgres**, which every replica updates on every successful scan and which no
replica can misreport. Loops expose their in-process status for diagnosis; operators alert
on the lease row. Both belong on the operator surfaces named in `CONTEXT.md`, which is
[task 016](../review-backlog/tasks/016-runner-queue-operator-visibility.md).

### 6. Extraction to a separate process is a supported deployment change

Running control loops in a separate process is a supported topology reached by disabling
them in the API server and starting a command that composes the same loops from
`pkg/controllers`. It is a deployment change, not a redesign, and it does not require a
superseding ADR.

Urth does not ship that command until one of these is true:

- **the scheduler lands.** It needs a host process, and a controller-manager command is
  that host. Reconciliation moves in with it.
- **a third loop appears with no home** — Artifact retention or dead-letter processing.
- **ADR 0004 §8's distinct service identities are implemented.** At that point
  co-location stops being a convenience and becomes a least-privilege compromise (§8).
- **a loop's resource use visibly competes with request handling** — a pass expensive
  enough at Runner or Result scale that sharing the API server's database connection pool
  degrades request latency.

Until then, shipping a second deployable buys separation that nothing yet uses and costs a
duplicated composition path, which is the usual way two deployment shapes drift apart.

### 7. Reconciliation terminates runs; it does not schedule them

The boundary between the reconciler and the scheduler is drawn at *creation*:

| Concern | Owner |
|---|---|
| Whether a Scenario is due, and when | Scheduler |
| Which Runner a run is placed on | Scheduler |
| Creating a `Result` | Scheduler, or a manual trigger |
| Whether a failed attempt is retried | Scheduler (retry policy) |
| Detecting that an attempt cannot complete | Reconciler |
| Recording that attempt as terminal | Reconciler |
| Restoring transport state derived from an existing decision | Reconciler |

The reconciler may re-derive a dispatch for a `Result` that already exists, because the
outbox row is a derived artefact of a placement decision already made and recorded in the
`Result`'s execution snapshot. It may not create a `Result`, select a Runner, evaluate a
schedule, or decide that an attempt is worth repeating.

This ADR records the consequence already reached in task 003: **a pending `Result` whose
dispatch outlived the transport's own job expiry is explicitly expired, not republished.**
Republishing would reopen an attempt, which ADR 0004 forbids — a retry creates a new
`Result`. That choice is correct, but it is a statement about run lifetime and therefore
scheduler policy made in a durability task. **The retry-policy decision, when it is
written, owns it** and may supersede this paragraph; the reconciler implements whatever it
says rather than being the place the policy lives.

When retry policy does arrive, "an expired lease creates a new pending `Result`" is
scheduler work by this table, whichever process executes it.

### 8. Control loops declare their own service identity

ADR 0004 §8 requires that "API servers, the scheduler, and the outbox relay use distinct
service identities with least-privilege subject permissions." The reconciler is absent
from that list because it did not exist when ADR 0004 was accepted. It belongs on it.

Today every loop borrows its host's NATS connection and therefore its host's authority.
The incremental privilege the reconciler adds to an API server is narrow but real: message
deletion, used to withdraw a dispatch that nothing will claim. JetStream asset
administration is *not* incremental — the API server already provisions the jobs stream at
startup and a Runner's durable consumer on the worker-registration path, so co-location
does not widen that.

When ADR 0004 §8 is implemented, each control loop receives its own identity and
permission set rather than inheriting the API server's, and that work is a trigger
condition in §6.

## Architectural rules

1. A control loop is periodic, idempotent, safe to run concurrently, and on no request's
   latency path.
2. Control-loop composition lives in `pkg/controllers` and is callable from any command;
   `pkg/urth` never depends on it.
3. Concurrency safety is a property of the loop — a database lease plus optimistic
   resource versions — never a deployment restriction.
4. Every loop runs in every API server replica by default and is disableable per loop.
5. A loop's panic, failure, or slow dependency never takes down its host or its host's
   other loops.
6. A loop participates in host shutdown rather than being hard-killed.
7. Loop liveness is alerted on from the lease row in Postgres, not from any process's
   in-memory status.
8. Running loops in a separate process is a supported topology reached by configuration,
   not by moving code.
9. A control loop never creates a `Result`, selects a Runner, or evaluates a schedule.
10. Retry policy is scheduler policy wherever it executes.
11. Each control loop gets its own service identity under ADR 0004 §8 rather than
    inheriting the API server's.

## Consequences

### Benefits

- An operator's minimum deployment is unchanged: Postgres, NATS, an API server, and
  Workers. Nothing silently stops working because a maintenance process was never started.
- The failure that co-location genuinely risks — a maintenance defect crashing the API —
  is bounded by an explicit obligation rather than by hoping the loops are correct.
- Alerting on the lease row works identically in every topology, so the observability
  contract does not change when the deployment shape does.
- The anticipated loops have a defined home and inherit supervision, shutdown, and
  liveness reporting instead of each re-deciding them in `main`.
- The scheduler/reconciler boundary is written down before the feature that would blur it,
  and the policy that leaked into task 003 has a named future owner.
- Extraction stays cheap: the loops are already interface-bounded, so the decision is
  reversible at roughly the cost of a new command.

### Costs and constraints

- Loops share the API server's database connection pool. A large repair batch — most
  likely while recovering from a broker outage, which is also when the API is busy —
  competes with request handling, and there is no isolation knob until §6 fires.
- Loops share the API server's deploy cadence. Disabling reconciliation fleet-wide means
  restarting every replica with a flag; there is no runtime kill switch for a component
  whose job includes terminating runs.
- The per-loop disable flags are a footgun: setting one on every replica silently stops
  that loop everywhere. The lease-age alert of §5 is the mitigation, and it is a mitigation
  rather than a guard.
- The API server holds a NATS connection with message-deletion authority it would not
  otherwise need.
- Deciding the trigger conditions in §6 rather than extracting now means accepting that
  the extraction will be done later, under whatever schedule pressure the scheduler design
  brings with it.

## Alternatives considered

### Host reconciliation in the scheduler

Rejected on two grounds. The scheduler does not exist, so this is not a relocation but a
commitment to build it now. More fundamentally it is the wrong seam: reconciliation is
controller work — drive observed state toward declared state — while scheduling is
placement. Putting the component that decides *whether a run should exist* in the same
box as the one that decides *an attempt has failed* is how the §7 boundary gets lost
rather than defended. ADR 0004 §8 already treats them as distinct identities.

If the scheduler and the reconciler eventually share a process, it is as two loops in a
controller-manager under §2 and §6, each keeping its own responsibility — not as
reconciliation becoming part of the scheduler.

### Ship a standalone reconciler command now

The right shape at the wrong time. It buys a clean failure and privilege domain,
independent rollback, an obvious liveness surface, and its own connection pool. But
today's benefit is one NATS verb and a panic risk that recovery addresses for a fraction
of the cost, while the price is a fourth mandatory process for every operator and a second
composition path maintained in parallel with the first. §2 and §6 keep this available for
the moment it pays.

### Elect a leader and run loops in exactly one replica

Leader election would make "which process is reconciling" answerable directly. It replaces
a lease row that already exists and works with a second coordination mechanism to
configure, monitor, and debug, and it converts a lost election into a total stop rather
than a delay. The per-scan lease already single-flights the work; the question leader
election answers well is better answered by §5's lease-age signal.

### Designate one replica by configuration

Running loops only where a flag says so removes concurrent scans but makes correctness
depend on deployment discipline — the exact failure §3 rejects, with the added hazard that
the designated replica's absence is invisible. Rejected.

### Drive loops from external cron or a scheduled job

An external scheduler could invoke a one-shot repair command. It moves a correctness
requirement into the operator's orchestrator, makes the guarantee depend on infrastructure
Urth cannot see, and gives partial failure nowhere to report. It remains a reasonable way
for an operator to run *additional* out-of-band repairs, and `RunOnce` supports that, but
it is not the baseline.

## Implementation status at acceptance

The dispatch relay and the reconciler are implemented and run in every API server replica
by default, with per-loop disable flags. They satisfy §1, §3, and §7.

Not yet satisfied at acceptance, and implemented in the commits that follow this ADR:

- §2: loop composition lives in `cmd/api-server/main.go` rather than `pkg/controllers`.
- §4: both loops are started as bare goroutines with no panic recovery, and the API
  server has no signal handling, so neither participates in shutdown.
- §5: `Reconciler.Status()` is in-process only, and the lease-age signal is neither
  documented nor exposed.

Deferred by design:

- §6: no separate command ships, by decision, until a trigger condition fires.
- §8: control loops borrow their host's NATS identity until ADR 0004 §8 is implemented.

## References

- [ADR 0004: NATS communication backbone](./0004-nats-communication-backbone.md), §2
  (outbox and reconciler), §8 (service identities), §9 (scheduling remains an Urth
  responsibility)
- [Task 003: Reconcile dispatch and execution lifecycle](../review-backlog/tasks/003-reconcile-dispatch-and-execution.md)
- [Task 016: Runner queue operator visibility](../review-backlog/tasks/016-runner-queue-operator-visibility.md)
