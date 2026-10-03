# nats-worker

Executes Urth scenarios, taking its jobs from a NATS JetStream queue belonging
to one runner.

This is the worker described by [ADR 0004](../../docs/adr/0004-nats-communication-backbone.md).
It replaced the Redis/asynq prototype, which has been retired.

Everything below is implemented in [`pkg/worker`](../../pkg/worker): registration,
session renewal, the pull/claim/ack handshake, execution and reporting. This
command is the process around it — flags, the enrolment secret, an API client and
a signal-aware context. The handshake is exercised against a real API server and a
real broker in [`test/integration`](../../test/integration), which is the only
arrangement that can see a disagreement between the two halves of the claim
contract.

## What changed from the asynq prototype

The broker is the least of it.

**It authenticates.** The worker exchanges its enrolment token for a *session*
credential, and presents that session on every job claim. The API derives which
worker and which runner is asking from the token. The prototype sent worker
and runner IDs in the request body against an endpoint with no authentication
at all, so the server had no way to check them.

**It is told what to run only after it is allowed to.** The queue message
carries a Result UID, a version, and a dispatch ID — no script, no prob spec, no
credentials. The scenario to execute comes back in the claim response. Under
asynq, the whole job including its script sat in a shared Redis queue that every
worker read.

**It only sees its own runner's work.** Each runner has a durable pull consumer
filtered to `urth.v2.jobs.<account-uid>.<encoded-runner-name>`. Workers of that
Runner share it. The
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

Use the authenticated project, Runner, grant and Scenario setup in the
[repository quick start](../../README.md#quick-start). Then issue a token into a
private file and start the Worker:

```bash
RUNNER_TOKEN_FILE=$(mktemp)
chmod 600 "$RUNNER_TOKEN_FILE"
go run ./cmd/urthctl runners token example-runner-yaml > "$RUNNER_TOKEN_FILE"
go run ./cmd/nats-worker --token-file "$RUNNER_TOKEN_FILE" \
  --allow-insecure-api --nats.allow-insecure
```

The token is a shared machine token. It authorizes enrollment for the paired
Runner; a project grant separately authorizes work. Revoke a compromised token
through the identity token controls. Issue a replacement before you revoke an
old token when continuity is required. Multiple tokens can be active; issuance
does not automatically revoke an earlier token.

Keep the file private and remove it when it is no longer needed. `--client.token`
accepts the same secret, but process arguments can expose it to other local users.
The local Makefile uses that argument and is for isolated development.

## Installation key and transport

The M9 Worker candidate uses a persistent Ed25519 key. The default path is the
platform user configuration directory plus `urth/worker.key`. On Linux it uses
`$XDG_CONFIG_HOME/urth/worker.key`, or `$HOME/.config/urth/worker.key` when XDG is
unset. `--identity-key-file` and `URTH_WORKER_IDENTITY_KEY_FILE` select another
path. The Worker creates the seed once as a private regular file and keeps it
across restarts. Do not copy the same seed to separate installations. Give each
installation a separate key file or persistent container volume.

Machine tokens authorize the Runner enrollment. A two-minute single-use server
challenge verifies the installation's key and binds the submitted manifest.
The public fingerprint appears in Worker `status.fingerprint` as `sha256:<hex>`.
A display name does not prove identity. A different key cannot reuse an existing
Worker name to assume its registration. Re-enrollment with the same proven key
can refresh its registration after a display-name change.

Production requires an HTTPS API endpoint and authenticated `tls://` NATS URLs.
Use `--client.api-server-address` for the API. The API-provided broker URLs take
precedence over `--nats.url`. Set `--nats.tlsca-file` for a private broker CA.
If the broker requires mutual TLS, set both `--nats.tls-cert-file` and
`--nats.tls-key-file`. The broker JWT proves Worker authority; the TLS certificate
protects its transport. Do not put credentials in endpoint URLs.

The local command above explicitly permits loopback HTTP and unauthenticated
NATS. These options do not permit a remote insecure endpoint. M9 proof and
transport implementation must merge and pass the release evidence gates before
this candidate configuration is released.

## Block and unblock an installation

An account administrator reads the verified fingerprint from Worker detail in
the website or from `urthctl get worker NAME -o json`. Then use:

```sh
urthctl runners block RUNNER 'sha256:FINGERPRINT' --reason 'Retired installation'
urthctl runners unblock RUNNER 'sha256:FINGERPRINT'
```

Replace the placeholder with the exact lowercase fingerprint. The CLI reads the
Runner version and sends a conditional edit. In the website, open the Runner,
select **Edit scheduling**, and edit **Blocked workers (JSON)**. Entries have
`identity` and an optional `reason`. A stale edit is refused; read the current
Runner before retry. The key stays private on the Worker host.

Blocking denies enrollment, refresh and the next new claim. Broker access has a
bounded credential lifetime. An already issued run capability keeps its separate
bounded reporting authority. Record exact live broker and reporting evidence
before closing [tasks 004/009/024](../../docs/review-backlog/README.md).

## Flags worth knowing

| Flag | Effect |
|---|---|
| `--token-file` | Read the enrollment machine token from a private file |
| `--identity-key-file` | Persistent installation key; one private file per installation |
| `--allow-insecure-api` | Permit HTTP only for loopback development |
| `--nats.allow-insecure` | Permit an unauthenticated/plaintext broker only on loopback |
| `--nats.tlsca-file` | Private CA for broker TLS |
| `--nats.tls-cert-file`, `--nats.tls-key-file` | Client certificate and private key for mutual TLS |
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

- an HTTP heartbeat to `POST /v1/auth/workers/heartbeat`, authenticated by
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
credential lifetime and revocation, production TLS, Runner policy and stable
Worker identity. Runner-scoped JWTs and shared machine-token enrollment exist.
See the [M9 evidence matrix](../../docs/m9-release-validation.md).
The task files are the source of truth for scope and ordering.
