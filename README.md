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

Product resource routes require authentication and account or project authority.
Users sign in through the shared identity service (password, email links,
Google/GitHub/OIDC), and
runners use revocable machine tokens. See the [M4 notes](docs/m4-backend-tenancy.md)
and the [api-server identity settings](cmd/api-server/README.md#identity-issuer-sign-in-mail-and-the-first-user).
`urthctl auth login` signs the CLI in through the same service (the OAuth device grant).

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
# Everything still stored in a project that may carry credentials
urthctl get artifacts -l 'urth/artifact.may-contain-secrets=true'

# Narrower: faithful recordings and unclassified output
urthctl get artifacts -l 'urth/artifact.data-class in (secret-bearing,unknown)'
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

The scheduler binds each pending Result to a Runner, not to a process. The Result and its
dispatch commit in one Postgres transaction, and a relay carries the committed dispatch to
the queue — so a broker outage delays a run rather than losing it. See
[the API server's notes on the dispatch outbox](./cmd/api-server/README.md#the-dispatch-outbox).
The job then waits in that Runner's logical queue until one of its authenticated
WorkerInstances claims it or the job expires. Different Worker versions and configurations can share a Runner only
when they satisfy the channel's Worker requirements. See
[ADR 0003](./docs/adr/0003-runner-worker-model.md) for the full model and
[ADR 0004](./docs/adr/0004-nats-communication-backbone.md) for the NATS transport.

### Components

| Component | Path | Role |
|---|---|---|
| **api-server** | [`cmd/api-server`](./cmd/api-server/README.md) | REST API for all resources; hands out jobs. Run several replicas in production. |
| **nats-worker** | [`cmd/nats-worker`](./cmd/nats-worker/README.md) | The Worker. Shares its Runner's durable JetStream consumer, authenticates claims, executes probes, and uploads Results and Artifacts. |
| **urthctl** | [`cmd/urthctl`](./cmd/urthctl/README.md) | CLI. Apply manifests, inspect resources, run scenarios locally. |
| **Web UI** | [`website`](./website) | React front end on `@sre-norns/components`: identity, account infrastructure, project scenarios, runs, authenticated logs, artifacts and dead letters. |

The architectural commitments behind the resource model and distributed runner design
are recorded in [the architecture decision records](./docs/README.md).

**Dependencies**

- **Database** — Postgres 18, for development as well as production. Earlier versions are
  not supported.
- **Job transport** — NATS with JetStream. It is the only transport; the Redis/asynq
  prototype has been retired.

> **Project status.** Urth is under active development and not yet at a stable release.
> Four things are worth knowing before you start:
>
> - **Postgres is required.** `--store.url` still defaults to `sqlite:test.sqlite`, but
>   the schema relies on Postgres `TIMESTAMPTZ` columns that SQLite cannot read back, so
>   you must pass a Postgres URL explicitly. See [TODO.md](./TODO.md).
> - **There is no scheduling loop yet.** Scenario `schedule` fields are stored and
>   validated, but runs must currently be triggered manually via the API, UI, or
>   `urthctl`.
> - **NATS hardening remains a release gate.** The transactional dispatch outbox,
>   reconciler, dead-letter workflow and Runner-scoped NATS JWTs exist. Credential
>   lifetime, revocation, TLS and complete policy validation require evidence from
>   the [review backlog](./docs/review-backlog/README.md).
> - **Authentication is not yet reviewed for production.** Users sign in through the
>   shared identity service and runner tokens are issued only to account administrators
>   (M4, M5), but the combined auth surface has not had its security review (M9). Run
>   Urth only in a trusted development environment until then.
>
> See [TODO.md](./TODO.md) for the full backlog.

### Worker authentication model

The credential stages for the M9 candidate are:

1. An account administrator creates a **Runner** with its shared machine identity.
2. The administrator issues a shared machine token through the identity service.
   Runner creation does not return an enrollment token. Ordinary resource reads
   never return token secrets.
3. A Worker uses the machine token and proves possession of its persistent
   installation key with a server challenge. It receives a **WorkerInstance**,
   short-lived Worker session and restricted NATS credentials.
4. The Worker presents its session on each claim. The API checks current Runner,
   Worker and project grant state before it assigns the pending Result.
5. The API issues a capability for that execution. It permits only the Result
   status and Artifact writes for the claimed run, within its bounded lifetime.

Shared machine tokens authorize enrollment. Worker sessions authorize claims.
NATS credentials authorize broker operations. Run capabilities authorize one
execution's reporting. Revoking a project grant prevents new claims; an already
claimed run retains its bounded reporting authority.

The [M9 evidence matrix](docs/m9-release-validation.md) distinguishes implemented
controls from outstanding security and release checks. The original enrollment
store proposal is superseded by shared identity. The M9 candidate adds stable
Worker proof and blocklists; full production validation and merge remain open
in the
[review backlog](./docs/review-backlog/README.md).

The channel and executor relationship is defined by
[ADR 0003](./docs/adr/0003-runner-worker-model.md).

---

## Fresh installation

Use a new PostgreSQL database and a new NATS store. Deploy the API, worker, CLI
and website from the same validated release set. No earlier resource format or
existing-resource migration is supported. Keep old ADR migration discussions as
historical design records. Complete the
[release checklist](docs/m9-release-validation.md) before release.

## Quick start

**Prerequisites:** Go (version per [`go.mod`](./go.mod)), Postgres 18, NATS, `jq`, and
Node.js for the Web UI. The Web UI installs `@sre-norns/components` from GitHub Packages,
so `~/.npmrc` needs a GitHub token with `read:packages`
(`//npm.pkg.github.com/:_authToken=…`). Each service below wants its own terminal.

```bash
# 1. Start Postgres and NATS
make run-postgres-podman
make run-nats-podman

# 2. Start the API server on :8080. It creates the user admin@urth.example
#    (password urth-dev-password) owning an account, and writes mail to .dev/mail.
make run-api-server        # override the database with: make run-api-server store-url=...

# 3. Start the Web UI on http://localhost:3001 and sign in as that user.
#    This dev server is the browser's origin: sign-in and emailed links go through it.
cd website && npm install && npm run dev
```

Everything that follows can be done in the Web UI too — **Projects → Create project**,
**Runners → Register runner**, then a token, then **Authorize runner** on the project —
but here it is from the command line.

```bash
# 4. Sign in the CLI: open the printed link, log in as admin@urth.example, approve.
#    The session is kept in a profile and refreshed; `urthctl auth status` shows it.
urthctl() { go run ./cmd/urthctl "$@"; }
urthctl auth login
urthctl projects create quickstart --use    # or: urthctl context use <a project>

# 5. Register a runner, authorize it for the project, and add a scenario
urthctl apply ./examples/runner.yaml
urthctl apply - <<YAML
apiVersion: urth.sre-norns.com/v1
kind: runner-authorizations
metadata:
  name: example-runner-yaml
spec:
  runnerRef: $(urthctl get runner example-runner-yaml -o json | jq -r .metadata.uid)
  roles: [runner]
YAML
urthctl apply ./examples/scenario.tcp.yaml
urthctl get scenarios

# 6. Issue the runner a token, then start a worker with it
RUNNER_TOKEN_FILE=$(mktemp)
chmod 600 "$RUNNER_TOKEN_FILE"
urthctl runners token example-runner-yaml > "$RUNNER_TOKEN_FILE"
go run ./cmd/nats-worker --token-file "$RUNNER_TOKEN_FILE" \
  --allow-insecure-api --nats.allow-insecure
```

The two insecure flags apply only to loopback development endpoints. Production
uses HTTPS and authenticated NATS over TLS. The Worker stores its installation
key at `$XDG_CONFIG_HOME/urth/worker.key` (or the platform configuration directory
when XDG is unset). Use `--identity-key-file` to give each installation its own
private persistent key.

`apply` is quiet on success. Start a run now rather than waiting for the schedule, then
inspect its result:

```bash
urthctl trigger tcp-self-fondle
urthctl get results tcp-self-fondle
```

What `get` prints with `-o yaml` can be edited and applied back, as with kubectl:
`urthctl get scenario tcp-self-fondle -o yaml > s.yaml`, edit, `urthctl apply s.yaml`.
The copy carries the version it was read at, so if someone changed the scenario in
between, the apply is refused rather than undoing their change. Manifests say
`apiVersion: urth.sre-norns.com/v1`; legacy `v1` and missing API versions are rejected.

To sign in through an upstream provider without a real one, see the fake identity
provider in the [api-server README](cmd/api-server/README.md#identity-issuer-sign-in-mail-and-the-first-user).

### Running everything at once

A [`Procfile`](./Procfile) is provided for [foreman](https://github.com/ddollar/foreman)
and its clones ([goreman](https://github.com/mattn/goreman),
[honcho](https://github.com/nickstenning/honcho)):

```bash
export RUNNER_TOKEN=...   # local development only; the Makefile passes a process argument
goreman start
```

It runs the same Makefile targets as the steps above, so Postgres and NATS must already
be up.

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
cmd/           api-server, nats-worker, urthctl
pkg/urth/      domain model: Scenario, Runner, Result, Artifact
pkg/prob/      prob registry and the interface probers implement
pkg/probers/   one package per prob kind
pkg/runner/    job dispatch, run logging, metrics collection
pkg/http-parser/  .http / .rest file parser
pkg/natsq/     NATS/JetStream transport: naming, dispatch, live logs
pkg/worker/    the worker loop: claims, executes, uploads
website/       Web UI (Vite 8, React, TypeScript 6, @sre-norns/components)
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
