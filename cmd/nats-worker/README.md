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

- **No transactional outbox.** The API server publishes to JetStream directly
  after committing the Result. A crash between the two leaves a pending Result
  with no queue message and nothing to detect it. ADR 0004 §2 requires an outbox
  and reconciler; that is separate work.
- **NATS credentials are not issued by Urth.** Registration returns a
  `NATSConnectionInfo` with a credential type of `none` or `creds`. The wire
  contract is final, so Auth Callout or minted NKey/JWT credentials slot in
  behind it without the worker changing — but today subject-level permissions
  are whatever the operator configured on the NATS server.
- **Placement picks the first matching runner** by UID. Least-loaded placement
  needs queue depth per runner, which belongs to the scheduler service that does
  not exist yet.
