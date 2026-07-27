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

### Transports during migration

Both transports drain the same outbox, so the durability story is one story:

- **NATS** publishes a dispatch envelope built from the row.
- **Asynq** (legacy) needs the whole job, so an adapter reloads the `Result` and
  `Scenario` at publication time and calls the existing scheduler. Retiring asynq
  in [task 015](../../docs/review-backlog/tasks/015-retire-asynq-transport.md)
  deletes that adapter rather than a second way of dispatching.
