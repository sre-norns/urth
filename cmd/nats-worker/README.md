# nats-worker

Executes Urth scenarios, taking its jobs from a NATS JetStream queue belonging
to one runner.

This is the worker described by [ADR 0004](../../docs/adr/0004-nats-communication-backbone.md).
`cmd/asynq-runner` is the earlier prototype and still works; both can run against
the same API server while the migration proceeds.

Everything below is implemented in [`pkg/worker`](../../pkg/worker): registration,
session renewal, the pull/claim/ack handshake, execution and reporting. This
command is the process around it — flags, the enrolment secret, an API client and
a signal-aware context. The handshake is exercised against a real API server and a
real broker in [`test/integration`](../../test/integration), which is the only
arrangement that can see a disagreement between the two halves of the claim
contract.

## What is different from asynq-runner

The broker is the least of it.

**It authenticates.** The worker exchanges its enrolment token for a *session*
credential, and presents that session on every job claim. The API derives which
worker and which runner is asking from the token. `asynq-runner` sends worker
and runner IDs in the request body against an endpoint with no authentication
at all, so the server has no way to check them.

**It is told what to run only after it is allowed to.** The queue message
carries a Result UID, a version, and a dispatch ID — no script, no prob spec, no
credentials. The scenario to execute comes back in the claim response. Under
asynq, the whole job including its script sat in a shared Redis queue that every
worker read.

**It only sees its own runner's work.** Each runner has a durable pull consumer
filtered to `urth.v1.jobs.<runner-uid>`, and workers of that runner share it. The
prototype had one queue that every worker competed on, so a scenario's placement
requirements were computed and then discarded.

**It acknowledges after the claim commits, never before or after execution —
and waits for the server to confirm it.** Acking early loses the run if the
claim fails; holding the ack across the probe makes the redelivery timer span an
arbitrarily long run, and the job gets executed twice. See
[the handshake budget](#the-claim-handshake-budget) for why the confirmation is
not fire-and-forget.

## The claim outcome contract

A claim failure is not one situation, and the queue disposition depends on which
one it is. The API classifies the reason and encodes it as an HTTP status *class*
— never the specific reason, which would tell a caller whether a protected run
exists or who holds it. The worker maps that class to exactly one JetStream
action, in one place (`classifyClaimFailure` / `applyDisposition`):

| API status | Meaning | Worker action |
|---|---|---|
| `2xx` | claim granted (or idempotently re-granted) | **DoubleAck** (server-confirmed), then execute |
| `5xx` | transient store/internal failure; the run may still be pending | **Nak** with delay — redelivered |
| `409` | the run is terminal, superseded, or already validly held | **Ack** and drop |
| `401` / `403` / `400` / `404` | policy refusal or a malformed message that redelivery will not fix | **Term** — stops redelivery, enters the dead-letter path |

A claim interrupted by worker shutdown is a fourth case: it is left
*unacknowledged*, so the broker redelivers it after `AckWait`. It is never a
verdict on the run, so it must not be acked, naked, or terminated.

The prototype flattened every claim failure to `401`, which the worker read as
stale and acknowledged. A momentary Postgres blip could therefore delete the only
live message for a still-pending run — silent, unrecoverable loss on a work-queue
stream. See [review task 001](../../docs/review-backlog/tasks/001-preserve-retryable-claim-failures.md).

## The claim handshake budget

The granted-claim ack is a `DoubleAck`, not an `Ack`. The difference is not
cosmetic: `Ack` publishes the acknowledgement and returns without waiting, so a
connection lost in between leaves a message the broker still considers
outstanding. It is redelivered after `AckWait` — and because the API's claim is
*idempotent for the same worker and dispatch*, the redelivery is authorised
rather than refused, and the same external probe runs twice, concurrently, from
one process. `DoubleAck` is the request/reply form that closes that window.

Because the confirmation is a round trip, it needs time inside the same window
the claim does. The two are budgeted from one number — the bound consumer's
`AckWait`, which the worker reads from the consumer rather than from a flag:

```
AckWait ─────────────────────────────────────────────
├── claim (AckWait − 5s) ──────────────┤├─ ack (5s) ─┤
```

`claim + ack ≤ AckWait` always holds; there is no floor under either half. An
`AckWait` too small to fit a claim is a misconfiguration, and the honest
expression of it is abandoned claims and a dead-lettered dispatch, not a worker
granting itself more of the window than the operator allowed. The split is
logged at startup.

Two things are deliberately *not* what you might expect:

- **An unconfirmed acknowledgement does not stop the run.** The claim is
  committed in Postgres — the Result is `running`, leased, with this worker
  recorded as its executor. Refusing to execute would strand a run the control
  plane already believes is in progress. The worker logs, counts
  `urth_worker_ack_unconfirmed_total`, and proceeds. It never re-claims, never
  hands the run to another worker, and never rolls it back.
- **A stale (409) message keeps the plain `Ack`.** Nothing is executing, so a
  lost stale-ack costs one redelivery and one more cheap 409 — not a duplicate
  probe. A round trip per stale message would be cost without a failure mode.

### The duplicate a confirmed ack cannot prevent

Confirmation shrinks the window; it does not remove it. A claim that runs long
enough, or an ack whose *reply* is lost, still produces a redelivery for a run
this process is executing. So the worker keeps an in-process set of the runs it
currently owns, acquired **before** the claim — the racing case includes two
deliveries whose claims are in flight at once, which a set acquired after the
claim would let through.

A delivery for a run already in the set is acknowledged and dropped. Holding it
would reserve one of the runner's `MaxAckPending` slots for the length of a
probe, which is exactly what the ack must never span; naking it would bring it
back every `AckWait` until `MaxDeliver` filed a dead letter for a dispatch that
was delivered perfectly well.

Nothing about this is durable, deliberately. A restarted worker has forgotten
what it was running; the Result's execution lease is the durable truth and the
reconciler settles a run whose worker died. **None of this makes probe execution
exactly once** — a process can fail after making an external request and before
reporting it, and no broker can prove whether that request happened. ADR 0004 §5
is the contract: probes should be side-effect safe, and scenarios that mutate
must carry their own idempotency.

## Running it

```bash
make run-postgres-podman
make run-nats-podman
make run-api-server-nats

go run ./cmd/urthctl apply ./examples/runner.yaml
go run ./cmd/urthctl apply ./examples/scenario.tcp.yaml

export RUNNER_TOKEN=$(go run ./cmd/urthctl auth-worker -f ./examples/runner.yaml)
make run-nats-worker
```

The enrolment token can come from `--client.token`, or from `--token-file`.
Prefer the file: a secret passed as a command-line argument is visible in the
process table to every user on the host.

## Flags worth knowing

| Flag | Effect |
|---|---|
| `--token-file` | Read the enrolment secret from disk instead of a flag |
| `--concurrency` | Scenarios to execute at once. Defaults to CPU count; this is also the pull batch limit, so the worker never reserves work it cannot start |
| `--timeout` | Per-run ceiling. The server's deadline still wins if it is shorter |
| `--[no-]stream-logs` | Publish run output live. On by default |
| `--nats.url` | Overridden by whatever the API server returns at registration |
| `--heartbeat-interval` | Starting cadence for liveness reports. The server's answer wins |
| `--metrics-address` | Serve Prometheus metrics, e.g. `:9101`. **Empty by default** — this process runs inside the segment it probes, and opening a port is the operator's call |

## Metrics

With `--metrics-address` set, `/metrics` exports what only this process knows —
what happened between pulling a message and starting a probe. From the broker's
side a message that leaves the queue looks the same whether it left cleanly,
after a confirmation that had to be retried, or as the second copy of a run
already in flight.

| Metric | What it answers |
|---|---|
| `urth_worker_claims_total{outcome}` | Are claims being granted, or refused — and refused *how*? `stale` draining steadily is normal; `retry` climbing is an unwell API server |
| `urth_worker_ack_confirm_seconds` | How much of the reserve confirmations are using |
| `urth_worker_ack_confirm_retries_total` | Confirmations that needed a second attempt |
| `urth_worker_ack_unconfirmed_total` | Runs executing on a message that may still be redeliverable. Should be zero |
| `urth_worker_duplicate_deliveries_total` | Redeliveries dropped because the run was already in flight here. Non-zero means acks are not landing |
| `urth_worker_runs_total{result}` | Probes executed, by outcome |

Plus the usual process and Go runtime collectors.

## Reporting that it is alive

The worker reports liveness on both paths it has, every interval, **independently
of each other**:

- an HTTP heartbeat to `POST /api/v1/auth/workers/heartbeat`, authenticated by
  its session; and
- an empty NATS message on `urth.v1.presence.<runner-uid>.<worker-uid>`.

The independence is the point. These two paths fail separately, and the server
combines them into a diagnosis — a worker on its queue but silent to the API
cannot claim the work it is being offered, while one heartbeating but absent from
NATS has nowhere to collect work from. Making the announcement conditional on the
heartbeat succeeding would collapse that back into "absent" and lose it.

Neither failure is fatal here. A worker that cannot report is still a worker that
can run probes; the control plane draws its own conclusion from the silence, and
the attempt repeats next interval. The cadence comes from the heartbeat response
rather than the flag, because the timeout the server judges workers by is derived
from the same number — a worker picking its own could be declared dead while
reporting exactly as often as it meant to.

Claiming a run counts too, so a busy worker is confirmed alive by its work and
never waits out an interval to be believed.

On a clean shutdown it sends one last heartbeat marked `leaving`, so the fleet
view updates at once rather than after the timeout. Best-effort by nature: a
worker killed outright, panicking, or cut off sends nothing.

## Live logs

While a run is executing the worker publishes its log to
`urth.v1.logs.<runner-uid>.<result-uid>` on Core NATS — not JetStream, because a
log tail is worth having while someone is watching and worth nothing afterwards.
The authoritative copy is the log artifact uploaded when the run ends.

With nobody watching, the NATS server drops those messages, so the cost is the
worker's own upstream bandwidth. `--no-stream-logs` turns it off for constrained
links.

## What is not done yet

The remaining production work is tracked in the
[NATS Runner review backlog](../../docs/review-backlog/README.md). It covers
outbox and reconciliation, scoped NATS credentials, authentication and Runner
policy, acknowledgement and failure tests, dead letters, placement, and Asynq
retirement. The task files are the source of truth for scope and ordering.
