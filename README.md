# Urth

[![Build](https://github.com/sre-norns/urth/actions/workflows/go.yml/badge.svg)](https://github.com/sre-norns/urth/actions/workflows/go.yml)
[![Go Report Card](https://goreportcard.com/badge/sre-norns/urth)](https://goreportcard.com/report/github.com/sre-norns/urth)

**Uptime Kuma meets Prometheus Blackbox Exporter and self-hosted CI runners — with
character.**

Urth is synthetic monitoring for networks you cannot reach from the public internet.
Define HTTP, TCP, DNS, ICMP, gRPC, and full-browser checks as versioned,
Kubernetes-inspired **Scenario** resources. Scenarios are designed to run manually or on
a schedule, and each job is routed to a self-hosted runner in the network you want to
observe. Results become pass/fail history, artifacts, and Prometheus-style metrics.

Every durable concept in Urth is a resource exposed by the same resource-oriented REST
API. The Web UI and `urthctl` — a CLI inspired by `kubectl` — are equivalent clients of
that API. Resources use CRD-like manifests, but Urth implements the model directly and
does not require Kubernetes.

---

## Why Urth?

Hosted uptime monitoring works well right up to the point where the thing you need to
monitor lives inside a VPC, behind a VPN, or on a segmented factory network. At that
point you are asked to punch a hole through your perimeter so someone else's prober can
reach in.

Urth inverts that. **Probes execute on Worker processes you host, inside the network
segment being tested.** Workers reach out to the API server; nothing reaches in. The
deployment model is the same as self-hosted CI runners.

That design carries a second benefit. A large organisation is not one network — it's
dozens, each owned by a different team. Urth models this explicitly:

- **Runners advertise labels** describing where they sit and what they can do
  (`os: linux`, `region: eu-west-1`, `urth/capability.prob.puppeteer`).
- **Scenarios declare requirements** as label selectors.
- A job is only dispatched to a runner that satisfies the scenario's requirements.

If you have written a Kubernetes `nodeSelector`, this will feel familiar. Team A's probes
run on Team A's runners, which are the only ones with a route to Team A's infrastructure —
enforced by the control plane rather than by convention.

```yaml
# A scenario that must run on a Linux runner, and never in dev or test environments
spec:
  requirements:
    matchLabels:
      os: "linux"
    matchSelector:
      - { key: "env", operator: "NotIn", values: ["dev", "testing"] }
```

### How it compares

| | Urth | Uptime Kuma | Cronitor | Blackbox Exporter |
|---|---|---|---|---|
| Probes run on your own infrastructure | ✅ | ✅ | ❌ hosted | ✅ |
| Many runner pools, routed by label selector | ✅ | ❌ | ❌ | ❌ |
| Scenario & result history as API resources | ✅ | partial | ✅ | ❌ |
| Browser (Puppeteer) scenarios | ✅ | ❌ | ✅ | ❌ |
| Prometheus metrics per run | ✅ | partial | ✅ | ✅ |

Urth reuses the probe implementations from
[blackbox_exporter](https://github.com/prometheus/blackbox_exporter), so its HTTP, TCP,
DNS and ICMP semantics should be familiar if you already run it.

---

## Concepts

| Concept | What it is |
|---|---|
| **Scenario** | A probe definition: what to test, on what schedule, and which runners may execute it. |
| **Prob** | The executable body of a scenario, typed by `kind` (see below). |
| **Runner** | An operator-created logical scheduling channel: its queue, placement labels, job policy, and Worker admission policy. |
| **Worker** | A physical process deployed by an operator. Its binary and configuration determine which probes it can execute. |
| **WorkerInstance** | The API resource and session representing a live Worker connected to exactly one Runner. |
| **Result** | The record of one execution: status, timing, and who ran it. |
| **Artifact** | Data produced by a run — logs, metrics, HAR files, screenshots. |

Scenarios, Runners, WorkerInstances, Results, and Artifacts are versioned API resources.
Their manifests use the familiar `apiVersion`, `kind`, `metadata`, `spec`, and, where
appropriate, `status` structure. The model is deliberately CRD-like rather than a
Kubernetes API implementation: the concepts transfer, but no Kubernetes cluster is
needed.

The Web UI and `urthctl` operate on those same resources through the REST API. Core
functionality belongs to the API rather than either client, so a resource created in one
is immediately manageable from the other. `urthctl` adopts familiar `kubectl` workflows
such as applying manifests and getting resources, while also providing local probe tools.

### Available prob kinds

`http` · `tcp` · `dns` · `icmp` · `grpc` · `rest` · `har` · `puppeteer` · `pypuppeteer`

- **`rest`** executes `.http`/`.rest` files — the format used by the
  [VS Code REST Client](https://marketplace.visualstudio.com/items?itemName=humao.rest-client)
  and [IntelliJ HTTP Client](https://www.jetbrains.com/help/idea/http-client-in-product-code-editor.html).
- **`har`** replays a [HAR](https://en.wikipedia.org/wiki/HAR_(file_format)) capture from your browser.
- **`puppeteer`** / **`pypuppeteer`** drive a real headless browser (Node and Python).

### Artifact data classification

Probing an authenticated service means handling credentials, and different
artifacts need different treatment. Run logs are read casually and gain nothing
from recording a token, so credentials are stripped from them. A HAR recording is
the opposite: it exists to be replayed and diffed against earlier runs, which
requires a faithful copy of the exchange — redacting it destroys the only reason
to keep it.

Run logs take the conservative side of that split: header values are written
only for an allowlist of headers known to be safe (`Content-Type`, `Server`,
and similar), and everything else is logged by name with its value redacted.
Urth probes services it knows nothing about, so "which headers carry
credentials" is not a knowable set, while "which headers are safe to print" is.

Rather than pretend one policy fits both, every artifact declares what it may
expose, and the API surfaces that as labels:

| Class | Meaning | Produced by |
|---|---|---|
| `clean` | Cannot carry credentials by construction | metrics |
| `redacted` | Derived from a live exchange, credentials removed | run logs |
| `secret-bearing` | Faithful capture; may contain credentials | HAR recordings |
| `unknown` | The prober made no declaration | browser artifacts, anything unclassified |

An artifact that declares nothing is `unknown`, not `clean` — the absence of a
claim is not a claim of safety. Both `unknown` and `secret-bearing` are reported
as `urth/artifact.may-contain-secrets: "true"`.

This makes retention and audit questions ordinary label queries:

```bash
# Everything still stored that may carry credentials
curl -sG 'http://localhost:8080/api/v1/artifacts' \
  --data-urlencode 'labels=urth/artifact.may-contain-secrets=true'

# Narrower: faithful recordings and unclassified output
curl -sG 'http://localhost:8080/api/v1/artifacts' \
  --data-urlencode 'labels=urth/artifact.data-class in (secret-bearing,unknown)'
```

The classification is assigned server-side from the artifact's own declaration,
so a worker cannot relabel its upload as clean.

> Treat `secret-bearing` artifacts as credential material: restrict who can
> download them and keep retention short. Injecting secrets at replay time from a
> secret store — so recordings hold placeholders rather than live credentials —
> is [planned](./TODO.md), not yet implemented.

---

## Architecture

```
                    ┌──────────────┐
                    │    Web UI    │
                    └──────┬───────┘
                           │  REST
   ┌───────────┐    ┌──────┴───────┐    ┌──────────────┐
   │  urthctl  ├────┤  API server  ├────┤   Database   │
   └───────────┘    └──────┬───────┘    │  Postgres   │
                           │            └──────────────┘
                           │ schedule Result to Runner
                ┌──────────┴──────────┐
          ┌─────┴───────┐      ┌─────┴───────┐
          │ Runner      │      │ Runner      │  logical queues
          │ team-a queue│      │ dmz queue   │  (JetStream)
          └───┬─────┬───┘      └─────┬───────┘
              │     │                │
        ┌─────┴──┐ ┌┴───────┐  ┌─────┴─────┐
        │ Worker │ │ Worker │  │  Worker   │
        │ VPC A  │ │ VPC A  │  │   DMZ     │
        └────────┘ └────────┘  └───────────┘
```

Workers only ever make **outbound** connections, so a network segment can be probed
without granting inbound access to it.

The scheduler binds each pending Result to a Runner, not to a process. The job waits in
that Runner's logical queue until one of its authenticated WorkerInstances claims it or
the job expires. Different Worker versions and configurations can share a Runner only
when they satisfy the channel's Worker requirements. See
[ADR 0003](./docs/adr/0003-runner-worker-model.md) for the full model and
[ADR 0004](./docs/adr/0004-nats-communication-backbone.md) for the NATS transport.

### Components

| Component | Path | Role |
|---|---|---|
| **api-server** | [`cmd/api-server`](./cmd/api-server/README.md) | REST API for all resources; hands out jobs. Run several replicas in production. |
| **nats-worker** | [`cmd/nats-worker`](./cmd/nats-worker/README.md) | Target Worker implementation. Shares its Runner's durable JetStream consumer, authenticates claims, executes probes, and uploads Results and Artifacts. |
| **asynq-runner** | [`cmd/asynq-runner`](./cmd/asynq-runner/README.md) | Legacy Redis/Asynq Worker retained temporarily during migration. |
| **urthctl** | [`cmd/urthctl`](./cmd/urthctl/README.md) | CLI. Apply manifests, inspect resources, run scenarios locally. |
| **Web UI** | [`website`](./website) | React front end. |

The architectural commitments behind the resource model and distributed runner design
are recorded in [the architecture decision records](./docs/README.md).

**Dependencies**

- **Database** — a Postgres-compatible database, for development as well as production.
- **Job transport** — NATS with JetStream is the accepted target and has a development
  implementation. Redis via [asynq](https://github.com/hibiken/asynq) remains as the
  legacy path during migration.

> **Project status.** Urth is under active development and not yet at a stable release.
> Four things are worth knowing before you start:
>
> - **Postgres is required.** `--store.url` still defaults to `sqlite:test.sqlite`, but
>   schema migration currently fails on SQLite with `index idx_name already exists`, so
>   you must pass a Postgres URL explicitly. See [TODO.md](./TODO.md).
> - **There is no scheduling loop yet.** Scenario `schedule` fields are stored and
>   validated, but runs must currently be triggered manually via the API, UI, or
>   `urthctl`.
> - **Runner-level NATS queues are a development implementation.** The topology and
>   authenticated claim exist, but the transactional outbox, reconciliation, scoped NATS
>   credentials, complete Runner policy, and failure workflows are not finished. Track
>   the ordered work in the [NATS review backlog](./docs/review-backlog/README.md).
> - **Authentication is not production-ready.** Enrollment issuance still has an
>   unauthenticated route, NATS authority is not derived from Worker identity, run
>   capabilities need stronger claims, and the insecure legacy Asynq claim remains during
>   migration. Run Urth only in a trusted development environment until the P0 backlog is
>   closed.
>
> See [TODO.md](./TODO.md) for the full backlog.

### Worker authentication model

The intended model is:

1. An operator creates a **Runner** resource. Its creation mints the initial Runner
   enrollment credential.
2. A Worker presents that credential to register a **WorkerInstance**. The API checks
   the Runner's policy and returns the instance identity with a short-lived Worker
   session.
3. The WorkerInstance joins that Runner's logical queue and waits.
4. Before executing a job, it authenticates the claim with its Worker session. The API
   checks current Runner and Worker state, blocklist, channel policy, and concrete stored
   Worker capabilities, then atomically assigns the pending Result.
5. The API issues a short-lived capability token scoped to that Result. The worker uses
   it only to post the Result's status and Artifacts.

Enrollment grants permission to join one Runner pool; a Worker session grants permission
to request job claims; a run capability grants permission to report one execution. The
server controls each lifetime, with the run token bounded by the Scenario's maximum
duration plus a small upload margin. [ADR 0002](./docs/adr/0002-worker-authentication.md)
records the trust model, revocation semantics, and known implementation gaps.

The development implementation has the staged Worker session and atomic claim, but the
blocklist, full channel policy, enrollment boundary, and scoped NATS authority remain in
the [review backlog](./docs/review-backlog/README.md). This description is the target
contract, not a claim that every check is already enforced.

The channel and executor relationship is defined by
[ADR 0003](./docs/adr/0003-runner-worker-model.md).

---

## Quick start

**Prerequisites:** Go (version per [`go.mod`](./go.mod)), Redis, Postgres, and Node.js
for the Web UI. Each service below wants its own terminal.

```bash
# 1. Start Redis and Postgres
make run-redis-podman
make run-postgres-podman

# 2. Start the API server on http://localhost:8080
make run-api-server        # override the database with: make run-api-server store-url=...

# 3. Register a runner and create a scenario
go run ./cmd/urthctl apply ./examples/runner.yaml
go run ./cmd/urthctl apply ./examples/scenario.yml
go run ./cmd/urthctl get scenarios -o wide

# 4. Mint a worker token, then start a worker with it
export RUNNER_TOKEN=$(go run ./cmd/urthctl auth-worker -f ./examples/runner.yaml)
go run ./cmd/asynq-runner --client.token="$RUNNER_TOKEN"

# 5. Start the Web UI at http://localhost:3000
make serve-site
```

After step 3, `get scenarios` should list `basic-rest-self-prober-http` with its schedule
and requirements — that confirms the API server and database are wired up correctly.

Trigger the `basic-rest-self-prober-http` scenario from the UI, then inspect the results:

```bash
go run ./cmd/urthctl get results basic-rest-self-prober-http
curl 'http://localhost:8080/api/v1/scenarios/basic-rest-self-prober-http/results'
```

### Running everything at once

A [`Procfile`](./Procfile) is provided for [foreman](https://github.com/ddollar/foreman)
and its clones ([goreman](https://github.com/mattn/goreman),
[honcho](https://github.com/nickstenning/honcho)):

```bash
goreman -b 8080 start
```

---

## Writing scenarios

Start from a manifest — see [`examples/`](./examples/) for one per prob kind — then run it
locally. A local run never uploads results, so it won't pollute a scenario's history:

```bash
go run ./cmd/urthctl run -f ./my-scenario.yaml
```

Keep the working directory around to inspect artifacts while troubleshooting:

```bash
go run ./cmd/urthctl run -f ./my-scenario.yaml --runner.keep-temp
```

Browser scenarios may need extra flags:

```bash
go run ./cmd/urthctl run -f ./examples/scenario.puppeteer.yaml --puppeteer.headless --runner.keep-temp
```

You can also re-run the server's copy of a scenario by name, which is useful when a
scheduled run fails and you want to reproduce it:

```bash
go run ./cmd/urthctl run basic-rest-self-prober-http --runner.keep-temp
```

`urthctl` can also convert a browser HAR capture into a `.http` file:

```bash
go run ./cmd/urthctl convert ./website.har
```

### Schedules

Schedules are crontab expressions, parsed by
[gronx](https://github.com/adhocore/gronx), which also accepts these shorthands:

| Expression | Meaning |
|---|---|
| `@yearly` / `@annually` | every year |
| `@monthly` | every month |
| `@weekly` | every week |
| `@daily` | every day |
| `@hourly` | every hour |
| `@30minutes` / `@15minutes` / `@10minutes` / `@5minutes` | every N minutes |
| `@always` | every minute |
| `@everysecond` | every second |

---

## Development

```bash
make help          # list all targets
make test          # run tests with the race detector
make test/cover    # run tests and open a coverage report
make audit         # go vet + staticcheck + tests (what CI runs)
make tidy          # format code and tidy go.mod
make build         # build all binaries and the Web UI
```

### Repository layout

```
cmd/           api-server, asynq-runner, urthctl
pkg/urth/      domain model: Scenario, Runner, Result, Artifact
pkg/prob/      prob registry and the interface probers implement
pkg/probers/   one package per prob kind
pkg/runner/    job dispatch, run logging, metrics collection
pkg/http-parser/  .http / .rest file parser
pkg/redqueue/  Redis-backed job queue
website/       React Web UI
examples/      example resource manifests
```

Shared, non-domain-specific packages live in separate modules:
[wyrd](https://github.com/sre-norns/wyrd) (labels and selectors) and `grace` (service
lifecycle). See [`pkg/README.md`](./pkg/README.md).

## Contributing

Contributions are welcome. Please make sure `make audit` passes before opening a pull
request. [TODO.md](./TODO.md) tracks the current backlog and is a reasonable place to look
for something to pick up.

## License

See [LICENSE](./LICENSE).
