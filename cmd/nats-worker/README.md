# nats-worker

Executes Urth scenarios, taking its jobs from a NATS JetStream queue belonging
to one runner.

This is the worker described by [ADR 0004](../../docs/adr/0004-nats-communication-backbone.md).
`cmd/asynq-runner` is the earlier prototype and still works; both can run against
the same API server while the migration proceeds.

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

**It acknowledges after the claim commits, never before or after execution.**
Acking early loses the run if the claim fails; holding the ack across the probe
makes the redelivery timer span an arbitrarily long run, and the job gets
executed twice.

## The claim outcome contract

A claim failure is not one situation, and the queue disposition depends on which
one it is. The API classifies the reason and encodes it as an HTTP status *class*
— never the specific reason, which would tell a caller whether a protected run
exists or who holds it. The worker maps that class to exactly one JetStream
action, in one place (`classifyClaimFailure` / `applyDisposition`):

| API status | Meaning | Worker action |
|---|---|---|
| `2xx` | claim granted (or idempotently re-granted) | **Ack**, then execute |
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
