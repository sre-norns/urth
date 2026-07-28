# Urth API server

Serves the main API objects: Scenarios and Script Run Results. For management
purposes it also manages registration of async workers aka Runners.

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
repairing. The other figure worth watching is *scan age* —
`urth.Reconciler.Status()` reports when a scan last completed without failures,
which is the only thing that distinguishes "nothing is wrong" from "nothing is
scanning". Surfacing both to operators is
[task 016](../../docs/review-backlog/tasks/016-runner-queue-operator-visibility.md).

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
- **Asynq** (legacy) needs the whole job, so an adapter reloads the `Result` and
  `Scenario` at publication time and calls the existing scheduler. Retiring asynq
  in [task 015](../../docs/review-backlog/tasks/015-retire-asynq-transport.md)
  deletes that adapter rather than a second way of dispatching.
