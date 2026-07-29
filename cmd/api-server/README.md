# Urth API server

Serves the main API objects: Scenarios and Script Run Results. For management
purposes it also manages registration of async workers aka Runners.

Everything below is implemented in [`pkg/apiserver`](../../pkg/apiserver): the
route table, the status mapping, and the composition of store, transport, signing
keys and control loops. This command is the process around it — flags, a database
connection, a listener and a shutdown — and is deliberately thin, because a
`main` can only be tested by starting it. The dispatch path is exercised whole in
[`test/integration`](../../test/integration).

## The dispatch outbox

Creating a run is two durable writes: the `Result` row in Postgres, and the job
message in the broker. Doing them one after the other has a window — a crash, a
lost connection, a deployment — where authoritative state says a run is pending
and no message exists to wake a Worker. Broker-side deduplication does not help:
it suppresses a repeated publication, and here there was never a first one.

So the API server does not publish. It commits the `Result` and a
`dispatch_outbox` row **in one transaction**, and a *relay* carries committed
rows to the transport:

```
POST /results ──▶ ┌──────────── one transaction ────────────┐
                  │ INSERT results …                        │
                  │ INSERT dispatch_outbox … (event UID)    │
                  └─────────────────────────────────────────┘
                                    │
                       relay leases the row ──▶ JetStream (Nats-Msg-Id = event UID)
                                    │
                       UPDATE dispatch_outbox SET published_at = now()
```

Consequences worth knowing:

- **A broker outage does not fail a run request.** `POST /results` returns 201,
  the `Result` stays `pending`, and the dispatch is published when the broker
  returns. It is never rewritten as `errored` — nothing executed, so there is no
  execution failure to report.
- **Publication is at-least-once.** A relay can die between the broker accepting
  a message and the row being marked. It republishes under the same event UID,
  and JetStream's duplicate window collapses the two. That window
  (`--nats.duplicate-window`, default 30m) must comfortably exceed how long a
  relay can be down and still retry, or a retry lands as a second job.
- **Every replica relays by default.** Competing relays are expected: rows are
  leased with `SELECT ... FOR UPDATE SKIP LOCKED`, and a lease that outlives its
  relay expires so another can take the row over.

### Configuration

| Flag | Default | Meaning |
|---|---|---|
| `--relay-enabled` / `--no-relay-enabled` | `true` | Run the relay in this process. Disable only where a dedicated relay process is deployed. |
| `--relay-poll-interval` | `250ms` | How often an idle relay polls. This is the added latency of a manually triggered run. |
| `--relay-batch-size` | `32` | Rows leased per poll. A full batch polls again immediately. |
| `--relay-lease` | `30s` | How long a claim survives its relay. Must exceed how long a publication can take. |
| `--nats.duplicate-window` | `30m` | How long JetStream suppresses a republished message ID. |

### Operating

Hand-written SQL is the current answer and a temporary one:
[task 016](../../docs/review-backlog/tasks/016-runner-queue-operator-visibility.md)
puts each Runner's queue stages in the Web UI and `urthctl`, so an operator does
not need database access to find out why a Runner stopped running probes.

Until then, the backlog is a table and can be inspected directly:

```sql
-- Anything not yet published, oldest first.
SELECT event_uid, result_uid, runner_uid, attempts, not_before, last_error
  FROM dispatch_outbox
 WHERE published_at IS NULL
 ORDER BY created_at;
```

The number to alert on is the **age of the oldest unpublished row**. It stays
near zero while the relay keeps up, regardless of throughput, whereas a row count
tracks how busy the system is rather than whether it is healthy. The same figures
are available in code from `urth.DispatchOutbox.Stats`, which reports pending
count, failing count, oldest age, highest attempt count, and the last error.

A row with a high `attempts` and a `not_before` an hour out is a *permanent*
failure — usually a run that was never placed on a Runner, so there is no subject
to publish it to. Those are parked rather than retried on the fast loop. Retiring
them properly is the dead-letter workflow in
[task 012](../../docs/review-backlog/tasks/012-dead-letter-workflow.md).

## The reconciler

The outbox makes a dispatch durable and the execution lease makes an abandoned
run *detectable*. Neither, on its own, makes anything happen. The reconciler is
what acts on them, and without it the two most common failures in this system are
invisible:

- a Worker that acknowledges its message and then dies leaves a `Result` in
  `running` forever — it acked, so the broker will not redeliver, and it is gone,
  so it will never report;
- a job JetStream aged out at `--nats.max-job-age` leaves a `Result` in `pending`
  forever — the outbox row says published, the `Result` says pending, and both
  are telling the truth.

To everything else in the system, both look exactly like work still in progress.

A scan runs every `--reconcile-interval` and makes five passes:

| Pass | Finds | Does |
|---|---|---|
| Abandoned leases | outbox rows leased by a relay that is gone | returns them to the pool |
| Expired execution | `running` past `deadline` + upload grace | `Result` → `timeout` |
| Pending dispatch | `pending` past the transport's own job expiry | re-enqueues a missing entry, or expires a lost one |
| Stale dispatch | entries whose `Result` is terminal or deleted | withdraws the queued message, retires the row |
| Runner channels | active `Runner`s | recreates a missing durable consumer |

Things worth knowing:

- **An expired `Result` is never reopened.** The attempt happened; it is history.
  Retry, when there is a policy for it, creates a *new* `Result` — it does not
  erase this one. The executor recorded at claim time survives the expiry, which
  is the first thing anyone diagnosing it will want.
- **An unpublished outbox row is left strictly alone.** It is the relay's work in
  progress, and a broker that has been down for two hours is exactly the case
  where both the backlog and the pending `Result`s are old. Inferring a lost
  dispatch there would expire every run the relay is about to deliver.
- **`--pending-dispatch-grace` is added to `--nats.max-job-age`**, not configured
  independently, so a pending run is always given longer than the broker will
  hold its message.
- **Every replica reconciles by default.** Concurrent scans are settled by a
  `reconcile_leases` row, and every `Result` transition is version-guarded on top
  of that — two reconcilers reaching for one run produce one winner and one no-op.
  A fleet therefore performs about one scan per interval in total, not one per
  replica. [ADR 0006](../../docs/adr/0006-control-loop-placement.md) records why
  this runs here rather than in its own process, and what would change that.
- **A scan cannot take the API server down.** Both loops are supervised by
  `pkg/controllers`: a panic is recovered and the loop restarted with backoff,
  and both stop with the process rather than being hard-killed mid-transaction.
- **Workers never repair anything.** Restoring a Runner's consumer is the control
  plane's job by design (ADR 0004): a Worker allowed to create its own would be
  creating one that overlaps the real one, which a work-queue stream rejects.
- **A retired row is not backlog.** It leaves the relay's view and stops counting
  towards `DispatchOutbox.Stats`, so the alert on oldest-unpublished-age does not
  stay lit for a dispatch that has been deliberately dropped.

### Configuration

| Flag | Default | Meaning |
|---|---|---|
| `--reconcile-enabled` / `--no-reconcile-enabled` | `true` | Run the reconciler in this process. |
| `--reconcile-interval` | `1m` | How often a scan is attempted. This is the resolution at which a stuck run becomes visible. |
| `--reconcile-lease` | `5m` | How long one scan holds the right to run. Must exceed a scan's duration. |
| `--reconcile-batch-size` | `256` | How much one scan repairs. A backlog drains over several scans. |
| `--pending-dispatch-grace` | `30m` | Added to `--nats.max-job-age` to decide when a published dispatch is presumed lost. |

### Operating

Each scan logs a summary when it repaired or failed anything, and stays quiet
otherwise:

```text
reconciler "reconciler-l2Hq0HDK" repaired 3 in 33ms (running-expired=0
  pending-expired=1 redispatched=0 retired=1 dropped=1 leases=0 channels=0
  failures=0 oldest=6.002967577s)
```

`oldest` is the number to alert on: the age of the oldest `Result` found needing
repair. It stays near zero while the reconciler keeps up, however much it is
repairing.

The other figure worth watching is **scan age**, and it is the one thing that
distinguishes "nothing is wrong" from "nothing is scanning" — a healthy
reconciler is deliberately silent, so an empty log means both.

Alert on the **lease row**, not on a process:

```sql
SELECT holder, updated_at, now() - updated_at AS scan_age
  FROM reconcile_leases
 WHERE name = 'dispatch-reconcile';
```

Every replica updates that row on every scan it wins, so its age is a property of
the fleet and no single process can misreport it. Alert when `scan_age` exceeds a
few multiples of `--reconcile-interval`.

`urth.Reconciler.Status()` reports the same thing for *one process* and is for
diagnosis, not alerting: under the default where every replica reconciles, the
replicas that lose the lease truthfully report a skipped scan, which looks
identical to a fleet where nothing is scanning at all. Putting both on the
operator surfaces is
[task 016](../../docs/review-backlog/tasks/016-runner-queue-operator-visibility.md);
the reasoning is [ADR 0006](../../docs/adr/0006-control-loop-placement.md) §5.

The same applies to the relay: it holds no scan lease, so its liveness is the age
of the oldest unpublished row in `dispatch_outbox` (`DispatchOutbox.Stats`).

A dispatch dropped rather than delivered says so in the row:

```sql
SELECT event_uid, result_uid, retired_at, retired_reason
  FROM dispatch_outbox
 WHERE retired_at IS NOT NULL
 ORDER BY retired_at DESC;
```

### Transports during migration

Both transports drain the same outbox, so the durability story is one story:

- **NATS** publishes a dispatch envelope built from the row.
- **Asynq** (legacy) needs the whole job, so an adapter reloads the `Result` at
  publication time and calls the existing scheduler. It publishes the `Result`'s
  execution snapshot, never the current `Scenario`. Retiring asynq in
  [task 015](../../docs/review-backlog/tasks/015-retire-asynq-transport.md)
  deletes that adapter rather than a second way of dispatching.

## Dead letters

A dispatch can stop making progress in five ways, and until a `DispatchFailure`
records one, all five leave a `Result` pending forever with the only account of
why in a worker's log:

| Reason | Means | Reported by |
|---|---|---|
| `malformed-envelope` | the message could not be parsed | worker |
| `misrouted-dispatch` | it named a different runner than the consumer that delivered it | worker |
| `policy-refused` | the API permanently refused the claim (4xx) | worker |
| `max-delivery-exhausted` | the broker stopped redelivering | control plane |
| `undeliverable-dispatch` | the relay could never publish the message at all | control plane |

Recording a failure and moving its `Result` to `errored` happen in one
transaction. Either alone is worse than neither: a record without the transition
leaves a run pending next to an explanation nobody is reading, and a transition
without the record loses the reason. `errored` rather than `timeout` because
nothing ran out of time — the dispatch was undeliverable.

Things worth knowing:

- **A worker reports before it terminates.** A terminated message is never
  redelivered, so reporting afterwards means a crash in between destroys the only
  evidence the dispatch existed. A report that cannot be made leaves the message
  queued for another attempt. The exception is a report the API refuses as
  malformed (4xx), which would be refused on every redelivery — those terminate
  anyway and say so loudly.
- **Only a pending run is stranded.** A `running` one was claimed through some
  other delivery and is live work; a terminal one is history. The failure is still
  recorded in both cases, because the delivery really did fail.
- **Recording is idempotent by dispatch and reason.** A worker unsure whether its
  report landed simply sends it again. A dispatch that failed two different ways
  keeps two records, because those tell an operator two different things.
- **The outbox row is left alone.** The reconciler's stale-dispatch sweep is what
  withdraws the queued message once the `Result` is terminal, so this path does
  not grow a second copy of that logic.
- **`max-delivery-exhausted` is not guaranteed.** JetStream advisories are Core
  NATS and at-most-once, so one arriving while nothing is listening is gone. Every
  replica subscribes for that reason, and the reconciler remains the backstop: the
  pending run is expired once its message outlives `--nats.max-job-age`. The
  advisory buys a prompt, specific diagnosis rather than the guarantee.
- **A retry creates a new `Result`.** The failed one is never reopened. The
  execution snapshot is copied rather than re-read from the scenario, and
  placement is inherited — moving the run to another runner would be a scheduling
  decision. The link is recorded in both directions:
  `DispatchFailureStatus.retryResultName` forward, `urth/result.retry-of` back.
- **A run nothing could take is not a dead letter.** See below.

## Choosing a runner

Once a scenario's requirements have narrowed the field to the runners that *may*
take a run, something has to pick one. That used to be `sort by UID, take the
first`: a runner won on its identifier and went on winning however far behind it
fell, while its idle siblings took nothing. An active runner with no workers at
all still won if its UID sorted first, and quietly accumulated a queue nobody was
draining.

Placement now compares what each candidate is actually carrying:

```
online     = workers reachable on both liveness signals, and not paused
committed  = runs placed on this runner that have not finished (queued + running)
spare      = online - committed
pressure   = committed / online
```

`online` is strictly the `online` presence condition, which is where the two
liveness signals earn their keep: a worker that cannot reach this server cannot
claim what it is offered, and one absent from its queue never receives it.
Neither is capacity. A worker that has never reported is not capacity either — it
has demonstrated nothing.

Counting **queued as well as running** work is what makes placement
self-spreading. A run placed a moment ago has already reduced that runner's
spare, so a burst of runs walks across the fleet instead of piling onto whichever
runner happened to look idle when the burst began.

Both numbers come from Postgres — worker presence, and one `GROUP BY` over
unfinished runs per runner. There is no broker round trip on the run-creation
path, and so no new way for run creation to fail. This is possible because
placement records the chosen runner on the Result *before* it is persisted, which
makes `results` the authority on every runner's queue depth.

### The two regimes

**Somebody has room.** Deterministic: most spare wins, ties broken by lower
pressure, then by UID. Pressure is the second key rather than the only one
because it separates a large runner that is nearly full from a small one that is
empty — the empty one should win, having nothing to do.

**Nothing has room.** A weighted draw, weighted by online worker count; falling
back to registered workers when nothing is online anywhere, and to an even chance
when no runner has any worker at all.

The randomness is deliberate, not a shrug. When every channel is saturated each
drains in proportion to the number of workers on it, so handing out new work in
that same proportion keeps the expected *wait* equal across the fleet. Choosing
deterministically here would send an unbroken stream to one runner until its
counts moved, which is the concentration this change exists to end. Queue depth
is not part of the weight for the same reason: once saturated, a deeper queue on
a larger runner is not worse for a new run — it is being worked off faster.

### What capacity never does

**It never refuses a run.** A scenario whose fleet is entirely offline still gets
a placement and still queues, because the queue is durable and the work waits for
the fleet to come back. Capacity decides *which* queue, never *whether*. The only
things that make a run unschedulable are the ones in the next section.

**It never binds a run to a worker.** A run is placed on a runner; the worker is
recorded when one claims it, which is the only moment the association is certain.

**It is never authoritative.** Capacity is read when the run is created and may be
stale by the time a worker claims. The claim checks in ADR 0002 remain the
authority on whether a given worker may take a given run.

If the capacity numbers cannot be read at all, placement logs once and falls back
to the lowest UID — the behaviour that predates this — rather than failing a run
because a count was unavailable. That case is visible as the `unmeasured` regime.

### Observing it

Every placement logs its regime and the chosen runner's numbers:

```
placed run of "web-check" on runner "syd-dmz" by spare-capacity
  (2 of 3 eligible; online=4 queued=1 running=2 spare=1 pressure=0.75)
```

`urth_placement_decisions_total{regime}` counts decisions by regime only.
Which runner won is deliberately not a label: runner UIDs are operator-created
and unbounded, and putting them in a metric turns a growing fleet into a
cardinality problem. A rising `saturated` rate means the fleet has no spare
capacity; a rising `unmeasured` rate means placement has degraded to its
fallback.

`GET /api/v1/scenarios/:id/placement` reports the same picture before a run is
created — `onlineWorkers`, `queuedRuns`, `runningRuns` and `spareCapacity`
alongside the runner counts. `spareCapacity: 0` means a run would wait, not that
it would be refused.

## Runs that were never placed

A run of a scenario whose requirements match no *active* runner is created
already terminal: `errored`, with `urth/result.unschedulable=no-eligible-runner`
and no outbox row. The same happens, with `invalid-requirements`, when the
scenario's selector does not parse — which the scenario API now refuses on the
way in, so it should only be reachable by a record written before that check.

It is deliberately **not** a `DispatchFailure`. A selector matching nothing is an
ordinary state of a fleet being changed — a runner decommissioned, a scenario
written ahead of the runners that will serve it — and once a scenario is
scheduled every minute, dead-lettering it would file a record per tick and bury
the failures that do need a human. The run itself carries the fact, so this stays
a label query:

```sh
urthctl get results <scenario> -l 'urth/result.unschedulable = no-eligible-runner'
# fleet-wide, since `get results` is scoped to one scenario:
curl -sG localhost:8080/api/v1/results \
  --data-urlencode 'labels=urth/result.unschedulable = no-eligible-runner'
```

`POST /scenarios/{id}/results` still returns 201 with that terminal run rather
than a 4xx. A scheduled trigger has no caller to hand a refusal to, and the
record that a run was wanted and could not happen is the thing worth keeping.

Clients ask *before* offering to trigger one:

```sh
curl -s localhost:8080/api/v1/scenarios/<name>/placement
{"requirements":"os=linux","matchingRunners":2,"eligibleRunners":1,
 "registeredWorkers":3,"readyWorkers":2,"schedulable":true}
```

`eligibleRunners` is what decides `schedulable`; the worker counts are advisory.
A dispatch to an eligible runner with no workers connected waits in that runner's
queue rather than failing, which is the durable-channel behaviour
[ADR 0003](../../docs/adr/0003-runner-worker-model.md) requires — so the Web UI
warns about it and still offers the run.

### The relay's backstop

The same condition can be reached from the other side: an outbox row written
before this check, or one whose runner was deleted between placement and
publication. A `urth.PermanentDispatchSink` settles those instead of retrying
them forever:

- a dispatch with no runner assigned strands its run with
  `no-eligible-runner`, and files nothing; and
- every other permanent failure — an envelope this build cannot encode, a row
  from a newer schema, a `Result` that has moved on — becomes an
  `undeliverable-dispatch` dead letter, which strands the run in the same
  transaction.

Either way the outbox row is left to the reconciler's stale-dispatch sweep, which
retires it once the `Result` is terminal. Without the sink — a relay assembled
without one — such an entry is retried on `PermanentDispatchBackoff` (1 hour)
for as long as it exists, which is what left runs pending indefinitely.

### Operating

Both operator surfaces show unresolved failures by default:

```sh
urthctl get dead-letters              # unresolved
urthctl get dead-letters --all -o wide
urthctl retry <name>                  # creates a new run, names it
urthctl resolve <name>                # closes it without re-running
```

The Web UI has the same at `/dead-letters`. Everything is a label query:

```sh
urthctl get dead-letters -l 'urth/dispatch-failure.reason = max-delivery-exhausted'
urthctl get dead-letters -l 'urth/runner.name = example-runner'
```

The figure to alert on is the count of unresolved failures, and in particular any
`max-delivery-exhausted`: workers are being handed work they cannot claim.

| Flag | Default | Meaning |
|---|---|---|
| `--advisories-enabled` / `--no-advisories-enabled` | `true` | Record dispatches the broker has stopped redelivering. |

## The execution snapshot

A `Result` is one execution attempt, so it stores what that attempt was asked to
do: `ResultSpec.Execution` holds the scenario's UID, name, version, placement
requirements, and the complete typed `prob.Manifest`, copied at the moment the
run is created and never revisited.

This is what makes a scheduled run immutable. Claim authorization used to reload
the `Scenario` by UID, which had two consequences that no one could see from the
`Result`:

- editing a `Scenario` changed what an already-queued run executed, without
  changing the `Result`'s version and without any trace in its history;
- deleting a `Scenario` destroyed every run already committed to happen — the
  claim answered "obsolete" and the dispatch was discarded.

Both are now impossible: nothing on the claim path reads the `Scenario`.

Things worth knowing:

- **The snapshot is never serialized by the resource API.** A probe definition is
  a script and may carry credentials, so it is disclosed only in the response to
  an authenticated claim (`AuthJobResponse.Prob`). `GET /results` and
  `GET /results/{id}` expose `probKind` and the `urth/scenario.*` labels, not the
  script. There is deliberately no operator endpoint that returns it yet; adding
  one needs the operator authentication of
  [task 005](../../docs/review-backlog/tasks/005-secure-runner-enrollment.md).
- **The labels describe the snapshot.** `urth/scenario.version` on a `Result`
  names the revision that run actually executes, so "which runs used the bad
  script" stays a label query after the scenario has moved on.
- **A run with no snapshot fails closed.** Rows written before this column
  existed read back as NULL. Such a run is refused at claim, its dispatch is
  acknowledged rather than redelivered forever, and the `Result` becomes
  `errored` and labelled `urth/result.unschedulable=missing-execution-snapshot`.
  It is never repaired from the current `Scenario`: that would run something
  nobody asked for and record it as history. The remedy is to trigger the
  scenario again.

```sql
-- Runs stranded by a missing snapshot.
SELECT uid, name, created_at
  FROM results
 WHERE labels::json ->> 'urth/result.unschedulable' = 'missing-execution-snapshot';
```

## JetStream limits

Every limit on `URTH_JOBS` is set explicitly. Nothing is left to a JetStream
default, because most of those defaults are "unlimited" — a stream that grows
until the volume fills is not a limit anybody chose, it is one nobody wrote down.
`natsq.Config.Validate` refuses a zero rather than passing it through, so the
decision has to be made.

| Flag | Default | Bounds |
|---|---|---|
| `--nats.max-jobs` | `100000` | Queued jobs across the whole fleet. |
| `--nats.max-bytes` | `1GiB` | What the stream may occupy — the unit the disk actually runs out of. |
| `--nats.max-jobs-per-runner` | `1024` | One runner's share, so an offline runner cannot consume the stream. |
| `--nats.max-msg-size` | `8KiB` | One dispatch envelope. |
| `--nats.max-job-age` | `1h` | How long a job may sit unclaimed. |
| `--nats.duplicate-window` | `30m` | How long a republished dispatch is suppressed. |
| `--nats.ack-wait` | `30s` | The claim handshake, *not* probe execution. |
| `--nats.max-deliver` | `5` | Redeliveries before the dead-letter path. |
| `--nats.max-ack-pending` | `64` | Claim handshakes one runner may hold at once. |
| `--nats.max-runner-series` | `100` | Runners reported as individual metric series. |
| `--nats.replicas` | `1` | 3 in production. |

Both bounds are needed and neither is sufficient: the per-runner limit stops one
offline runner from filling the stream, and does nothing about a fleet of runners
each inside its own share adding up to the whole volume.

Reaching any of them **refuses the publication**. `DiscardNew` and
`DiscardNewPerSubject` are what make that true; JetStream's default,
`DiscardOld`, would evict the oldest unclaimed job to admit a new one and report
success to both publishers. The outbox row stays unpublished and retryable, so
the work is not lost — it is queued in Postgres instead, where
`urth_dispatch_outbox_pending` shows it.

The envelope size deserves its own note. A dispatch carries identity and nothing
else: the probe definition is disclosed only in the response to an authenticated
claim, by [ADR 0004](../../docs/adr/0004-nats-communication-backbone.md). A
message anywhere near 8 KiB means something is riding on the queue that should
not be, and refusing it is better than storing it.

`MaxAckPending` reserves **claim handshakes, not probe runs**: a worker
acknowledges as soon as its claim commits and then executes under the `Result`'s
own lease. Set too low, a pool of workers pulls in lockstep; too high, a runner
reserves work it cannot claim inside `--nats.ack-wait`, every reservation times
out, and the redelivery storm reads as a broker fault when it is a configuration
one.

`InactiveThreshold` is deliberately **not** set on consumers. It deletes a
consumer nobody has pulled from, and that consumer *is* the runner's queue —
expiring it because a runner's workers are offline would discard exactly the work
they are coming back for.

### Cross-field constraints

These are checked by `natsq.Config.Validate`, which kong runs during flag
parsing. Invalid configuration therefore fails **before a broker connection is
attempted**: "is my configuration valid" and "is my broker up" are different
questions and should not share an error. Every violation is reported in one pass,
so tuning several related durations does not mean rediscovering the next
constraint on the next start.

- `--nats.duplicate-window` ≤ `--nats.max-job-age`. JetStream refuses a duplicate
  window longer than the stream's maximum age. Lowering the job age without
  lowering the window used to fail startup with `duplicates window can not be
  larger then max age`, which names nothing an operator can change.
- `--nats.ack-wait` ≤ `--nats.max-job-age`, or a claim outlives the job it is for.
- `--nats.max-jobs-per-runner` ≤ `--nats.max-jobs`, or the stream stops accepting
  long before any single runner reaches its share.
- `--nats.replicas` ∈ {1, 3, 5}.

### Existing assets

Assets are provisioned on every start, which is how a limit change reaches the
broker. It is also how a stream that is *not* Urth's — an older deployment's, a
hand-made one, a restore taken under different settings — would be quietly
adopted, so the existing stream is inspected first:

- differences JetStream can apply are applied, and logged as
  `stream "URTH_JOBS": applying configuration drift: …`;
- a **retention or storage** policy it cannot is refused with
  `existing stream cannot be reconciled in place`. Resolving those means deleting
  the stream and every job queued in it, which is an operator's decision, not a
  process's at startup.

There is deliberately no equivalent check for consumers: a work-queue stream
refuses any acknowledgement policy but explicit, so that difference cannot arise,
and the subject filter — the field a deployment that keyed consumers on runner
*names* would have wrong — is updated in place without disturbing the queued
messages. Guarding it would block the repair.

## Worker liveness

Nothing used to record that a worker was alive. A `WorkerInstance` row was
written at registration and then sat there, so a worker whose process had been
gone for a week rendered exactly like one running: registered, and therefore
green.

Liveness is now reported over **two independent signals**, and they are kept
apart on purpose. A worker reaches Urth over HTTPS to this server and over NATS
to its queue; either can fail on its own, and which one failed is the diagnosis.

| | NATS presence | no NATS presence |
|---|---|---|
| **API contact** | `online` | `nats-unreachable` — the broker is down or its port blocked; the worker is healthy and has nowhere to collect work from |
| **no API contact** | `api-unreachable` — the worker is parked on its queue but cannot reach this server; it will be offered work and can claim none of it | `offline` |

A fifth state, `unknown`, means neither signal has *ever* been seen: a record
written before this existed, or a worker that does not report — `asynq-runner`
does not. It is a distinct state rather than a synonym for offline because
asserting a worker dead on no evidence would be a worse answer than the green dot
this replaced, and because it is what keeps the reconciler from evicting those
registrations.

**API contact** is any authenticated arrival from the worker's session:

- `POST /api/v1/auth/workers/heartbeat`, authenticated by the session bearer
  token. Nothing in the body identifies the caller — the same rule the claim
  route follows, since a request body is not evidence of identity. The response
  carries the interval to report at next (this server owns the cadence, because
  the offline timeout is derived from the same number) and whether an operator
  has paused the worker. A valid session whose registration has been dropped gets
  **404**, which is the worker's cue to register again rather than report into
  nothing.
- **claiming a run.** A worker taking a steady stream of jobs is confirmed alive
  by its work, without a single extra request and without waiting out an
  interval. Recorded as `status.lastSeenVia = claim`, so a reader can tell "the
  process is up" from "the process is up and taking work".

**NATS presence** is an empty message the worker publishes on
`urth.v1.presence.<runner-uid>.<worker-uid>`, recorded by the `worker-presence`
control loop. Core NATS, not JetStream, for the reason run logs are: it is worth
nothing once stale, and a dropped message costs one interval of resolution. The
runner UID precedes the worker UID so publish permission can be scoped to a
runner's own prefix; the recording write additionally requires the worker to be a
member of the runner it announced under, which closes the gap that scoping alone
leaves.

The worker publishes both **unconditionally**. Skipping the announcement when the
heartbeat failed is the obvious economy and would make `api-unreachable`
unobservable — the case the split exists for.

A worker shutting down cleanly sends a final `{"leaving": true}`, so stopping one
shows up at once instead of after the timeout. It is a courtesy, never a
guarantee: a worker killed outright, panicking, or cut off sends nothing, and the
timeout covers those.

Liveness is written straight to its columns rather than through the resource
store — see `urth.WorkerPresenceStore` for why, and CLAUDE.md for the trap.
It is **not** a label: presence changes every interval, and labels are an indexed
search surface rather than a place to write telemetry.

Nothing here affects scheduling. A run placed on a runner whose workers are all
offline still queues, which is the durable-channel behaviour ADR 0004 requires.

### Eviction

The reconciler drops registrations silent on **both** signals for longer than
`--worker.retention`, counted as `evictedWorkers`. Without it the fleet view
accumulates every worker that ever registered — bearable where worker names are
stable, steadily worse where a container mints a new one per restart.

Two things it will not do: it never touches a worker that has reported nothing at
all (`unknown`), and it requires both signals to be quiet, so a worker still
announcing over NATS survives however long its API route has been broken. That
case wants an operator, not a deletion.

Evicting a worker does not disturb its runner or anything queued for it, and the
worker may register again. This drops a registration; it does not bar a worker.

### Queue observation

`GET /api/v1/runners/:id` also reports `status.channel` — how many worker pull
requests are parked on that runner's JetStream consumer, and how much is queued.
It needs no cooperation from any worker, so it is the cross-check on the presence
workers report about themselves: every worker quiet while the queue still shows
waiting pulls means the reporting broke, not the fleet.

Read only on the single-runner fetch, never on the list, which would be one
broker round trip per runner on a page load. It is bounded at two seconds and any
failure degrades to `observed: false` rather than failing the request — a client
must show nothing rather than an authoritative-looking zero.

### Configuration

| Flag | Default | Notes |
|---|---|---|
| `--worker.heartbeat-interval` | `1m` | What workers are told to report at. Affordable at a minute because claims and the goodbye cover the fast cases. |
| `--worker.offline-after` | `0` | How long a signal may go unheard before it counts as offline. Zero derives it as 3× the interval, so the two cannot be configured into contradiction. |
| `--worker.retention` | `24h` | How long a doubly-silent worker is kept. `0` disables eviction. |

## Metrics

`GET /metrics`, in Prometheus exposition format. Registered outside `/api/v1`
because bark's content negotiation on that group answers 406 to any Accept header
it does not recognise — the same trap that makes the live run log stream
unreachable from a browser
([task 019](../../docs/review-backlog/tasks/019-serve-run-log-stream.md)).

| Metric | Meaning |
|---|---|
| `urth_jetstream_stream_messages` / `_bytes` | What is queued now. |
| `urth_jetstream_stream_max_messages` / `_max_bytes` | The configured limits, so "how full" is a ratio. |
| `urth_jetstream_stream_oldest_message_age_seconds` | Age of the oldest queued job. |
| `urth_jetstream_pending_messages` | Jobs queued across every runner. Exact at any fleet size. |
| `urth_jetstream_ack_pending_messages` | Claim handshakes outstanding. |
| `urth_jetstream_redelivered_messages` | Jobs handed out again after a failed handshake. |
| `urth_jetstream_runner_*{runner}` | The same three, per runner, capped — see below. |
| `urth_jetstream_runners_unreported` | Runners omitted by the cardinality cap. |
| `urth_jetstream_published_total` / `_publish_failures_total` | This process's publication tally. |
| `urth_dispatch_outbox_pending` / `_failing` | Committed dispatches not yet published. |
| `urth_dispatch_outbox_oldest_age_seconds` | Age of the oldest unpublished dispatch. |
| `urth_dispatch_outbox_max_attempts` | Worst attempt count in the backlog. |
| `urth_dispatch_dead_letters_unresolved` | Dead letters nobody has dealt with. |
| `urth_*_scrape_failures_total` | Scrapes that could not read the broker or the database. |

Runner UID is an unbounded label — a deployment creating a runner per tenant, per
branch or per test run writes a series per runner into Prometheus and keeps it
forever — so past `--nats.max-runner-series` only the busiest runners get their
own series, chosen by pending work and then by name so the choice is
deterministic. The fleet totals are summed *before* the cap, so "is anything
queued" is always exact; only "for which of these two hundred runners" degrades.

### What to alert on

- **`urth_dispatch_outbox_oldest_age_seconds` above a minute or two.** The best
  single signal: it sits near zero while the relay keeps up, whatever the
  throughput, and grows the moment publication stops. Raised backlog with an
  empty stream is the relay; the same backlog with a full stream is the fleet.
- **`urth_jetstream_stream_messages / urth_jetstream_stream_max_messages` above
  ~80%**, and the same for bytes. Past the limit, publications are refused.
- **`urth_jetstream_stream_oldest_message_age_seconds` approaching
  `--nats.max-job-age`.** Jobs are about to expire unclaimed.
- **`urth_jetstream_redelivered_messages` sustained above zero.** Workers are
  being handed work they cannot claim; check `--nats.ack-wait` and worker health
  before blaming the broker.
- **`urth_dispatch_dead_letters_unresolved` above zero**, and any
  `max-delivery-exhausted` among them.
- **`urth_*_scrape_failures_total` increasing.** The metrics are lying by
  omission; fix that before trusting the rest.
- **`urth_dispatch_outbox_pending` flat and non-zero** while the stream is empty
  means no relay is running.

## Deployment profiles

| | Development | Production |
|---|---|---|
| `--nats.replicas` | `1` | `3` |
| NATS storage | container filesystem | persistent volumes, one per server |
| Servers | one, `-js` | three, clustered across failure domains |
| TLS | off | required, both client and route connections |
| Auth | none | credentials file per participant (`--nats.creds-file`) |
| Postgres | container | managed or replicated, with backups |

A single non-replicated NATS server is fine for local development and tests, and
is what `make run-nats-podman` starts. It is **not** highly available and should
not be described as such: it holds every queued job on one disk, and losing that
disk loses the jobs that had been accepted but not yet claimed. Three replicas on
persistent volumes is the production shape ADR 0004 §11 requires.

Urth does not yet issue NATS identities — ADR 0004 leaves the choice between Auth
Callout and minted NKey/JWT open ([task
004](../../docs/review-backlog/tasks/004-runner-scoped-nats-credentials.md)) — so
until then `--nats.creds-file` points at credentials an operator provisioned, and
the same file is handed to workers through the registration response.
