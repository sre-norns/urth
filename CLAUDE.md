# Working on Urth

Context for picking this project up. Written after a long session; the goal is
that the next one does not rediscover the same traps.

## What this is

Synthetic monitoring where **probes run on runners you host, inside the network
segment being tested**. The runner reaches out to the API server; nothing reaches
in. That is the whole point of the design — hosted monitoring can't reach a VPC
or a segmented factory network without punching a hole in the perimeter.

The second half of the idea: an organisation is many networks, each owned by a
different team. Runners advertise labels, scenarios declare label-selector
requirements, and the scheduler only dispatches to a runner that matches. If
you've written a Kubernetes `nodeSelector`, it's that.

Read `README.md` for the user-facing version. It is accurate as of this session.

## Layout

```
cmd/api-server/     REST API, owns all resources, places and dispatches runs.
                    Also hosts the outbox relay and the reconciler.
cmd/nats-worker/    the worker: claims jobs from JetStream, executes, uploads.
                    This is the one ADR 0004 describes.
cmd/asynq-runner/   the Redis/asynq prototype worker. Migration-only; task 015 retires it.
cmd/urthctl/        CLI (kubectl-shaped), co-equal with the Web UI
pkg/urth/           domain model + service impl + REST client. The centre of gravity.
pkg/natsq/          JetStream naming, assets, envelope, publication, live logs
pkg/prob/           prob registry and the interface probers implement
pkg/probers/*/      one package per prob kind (http, tcp, dns, icmp, grpc, rest, har, puppeteer…)
pkg/runner/         probe execution, run logging, worker capability labels
pkg/redqueue/       legacy asynq transport, retiring with cmd/asynq-runner
website/            React UI (webpack, emotion, redux, wouter)
```

`pkg/urth` must not import `pkg/natsq`: transport adapters implement interfaces
the domain package owns.

Shared non-domain packages live in a sibling repo, `github.com/sre-norns/wyrd`
(`manifest`, `dbstore`, `bark`). The user develops it locally at
`~/workspace/wyrd`. Several sharp edges below originate there.

## Running it

**Postgres is required. SQLite does not work.** `--store.url` still defaults to
`sqlite:test.sqlite`, and migration fails there with `index idx_name already
exists` — wyrd's `ResourceMeta.Name` carries a hardcoded `gorm:"index:idx_name"`
and every model embeds it, so the second `CREATE INDEX` collides. Known, in
TODO.md, not fixed because it needs a wyrd change.

```bash
make run-postgres-podman        # or podman run … postgres:15
make run-nats-podman
make run-api-server-nats        # passes a Postgres URL explicitly
go run ./cmd/urthctl apply ./examples/runner.yaml
go run ./cmd/urthctl apply ./examples/scenario.tcp.yaml
export RUNNER_TOKEN=$(go run ./cmd/urthctl auth-worker -f ./examples/runner.yaml)
make run-nats-worker            # reads RUNNER_TOKEN
cd website && npm start         # :3000, proxies /api to :8080
```

The asynq path (`make run-redis-podman`, `run-api-server`, `run-asynq-worker`)
still works and is what `--transport` defaults to, but it is the prototype: no
authentication, no placement, no outbox.

Trigger a run without the UI:

```bash
curl -X POST 'http://localhost:8080/api/v1/scenarios/tcp-self-fondle/results' \
  -H 'Content-Type: application/json' \
  -d '{"apiVersion":"v1","kind":"results","metadata":{},"spec":{}}'
```

## Verification expectations

This project has repeatedly hidden bugs that pass unit tests. The habits that
caught them:

- `make audit` (vet + staticcheck + race tests) must exit 0. CI runs
  `make audit/postgres`, which is the same plus the tests that need a real
  database — see the Postgres note below. Run that one before pushing.
- `cd website && npm test` — vitest, currently 204 tests in 16 files.
- **Run it.** Several of the worst bugs this session — timestamps ten hours in
  the future, workers unable to register, resources that could not be disabled —
  were invisible to the test suite and obvious the moment a real stack ran.
- Screenshot/drive the UI with puppeteer from `worker/node_modules/puppeteer`
  (already installed). Catches React runtime errors that a build will not.
- **When fixing a bug, confirm the new test fails against the old code first.**
  Three times this session a test passed against the bug it was meant to catch.

Watch for a stale `api-server` still holding `:8080` from an earlier run — it
silently swallows requests meant for the one just started. Check with
`ss -ltnp | grep :8080` before concluding something is broken. Stop containers by
name; `podman stop -a` will take out containers that aren't yours.

**`make test/postgres` and `make audit/postgres` run against your dev database
and destroy it.** They pass `store-url` — the same Postgres the api-server uses —
as `URTH_TEST_POSTGRES_URL`, and the tests `DropTable` the whole model set on
setup *and* on cleanup (`service_outbox_test.go`). Every runner, scenario and run
you applied by hand is gone afterwards, silently: the next `urthctl get runners`
just prints an empty table. CI is unaffected because it provisions a database of
its own. Point `store-url` at a second database before running them, or expect to
re-apply the examples every time.

The same sharing bites in the other direction. A live api-server runs a relay and
a reconciler against that database, so one left over from manual testing competes
with the tests for the same outbox rows —
`TestOutboxCompetingRelaysDoNotDoubleClaim` and
`TestPublicationFailureLeavesResultPending` then fail intermittently and read
exactly like a regression in code nobody touched. Kill it first.

## Traps, in rough order of how much time they cost

**`dbstore.Update` silently drops zero values.** It passes the struct to gorm's
`Updates`, which skips zero-valued fields — so no bool can ever be set to
`false`. This defeated *disabling a scenario* and *disabling a runner* entirely;
both returned 200 and changed nothing. Resource edits now use `saveResource()`
(→ `CreateOrUpdate` → gorm `Save`). **Do not blanket-replace the remaining
`store.Update` calls**: `resultsAPIImpl.ClaimRun` (and the legacy `Auth` beside
it) relies on the version-guarded update to lose the race when two workers reach
for the same run.

**Worker liveness must never be written through `saveResource`.** Recording that
a worker is alive is not a resource edit: it happens on a timer, forever, for
every worker. `ObjectMeta.BeforeSave` in wyrd increments `Version` on every gorm
`Save`, so routing a heartbeat through `CreateOrUpdate` would bump a worker's
resource version every interval — the version would stop meaning "this record was
edited", and the version-guarded delete behind the UI's Drop button would fail
against a version that was current a minute ago. `WorkerPresenceStore` writes the
columns with `UpdateColumns`, which also skips the `updated_at` refresh that
`Updates` would do. Both properties have tests; both were measured against a live
Postgres, not assumed. And note the asymmetry that surprised me: `Updates` with a
*map* does **not** bump the version (the version is not a key), so a test using
it as the counter-example proves nothing — `presence_store_test.go` guards
against the resource-save path and against `updated_at` separately.

**Presence is two signals, deliberately not one.** A worker reaches Urth over
HTTPS to the api-server and over NATS to its queue, and either can fail alone.
`status.lastSeenTime` (heartbeat *or* run claim — a busy worker proves itself by
working) and `status.natsLastSeenTime` are stored and reported separately, and
`WorkerPresenceAt` combines them into `online` / `offline` / `api-unreachable` /
`nats-unreachable` / `unknown`. The worker publishes both **unconditionally**: if
the NATS announcement were skipped when the heartbeat failed, `api-unreachable`
could never be observed, which is the case the split exists for. `unknown` is a
real third state — the asynq prototype reports neither signal, and records
predating this feature have neither — and it is what keeps the reconciler's
eviction pass off them.

**Labels have a grammar and violating it is silent or fatal.** Values must match
`^[[:alnum:]]$|^[a-zA-Z0-9][a-zA-Z0-9_.\-]*[a-zA-Z0-9]$`. MIME types (`text/plain`),
file extensions (`.png`) and VCS build versions (`…+dirty`) all fail it. Artifact
labels are merged *after* manifest validation so bad values persisted unnoticed;
worker labels are validated on registration, so a binary built from a dirty tree
could not register at all. Always go through `urth.LabelSafeValue` / `putLabel`.

**Spec is worker-owned; Status is server-owned.** A worker rewrites its whole
`Spec` every time it registers. Anything an operator sets must live in `Status`
or it evaporates on reconnect — that is why `WorkerInstanceStatus.IsPaused` sits
there. Its zero value means *working*, deliberately, so records predating the
field keep taking jobs rather than going dark.

**Run results come back flat.** Unlike every other resource, a `Result` has
`name`/`uid`/`labels` at the top level, not nested under `metadata`. UI code has
to special-case this (`RunResult.jsx`, `runStats.js`).

**Timestamps need `TIMESTAMPTZ`.** `TIMESTAMP` in Postgres is *without* time zone,
so local wall-clock was stored naive and read back as UTC — every run time off by
the server's offset. Fixed for run/artifact times; use `TIMESTAMPTZ` for any new
time column.

**JSX lives in `.jsx` files.** Vite's oxc transform refuses to parse JSX from
`.js` and offers no override. `vitest.config.js` runs the project's babel presets
rather than plugin-react, because `@emotion/babel-plugin` must run or emotion's
component selectors throw at render time.

**Creating a run no longer publishes anything.** `resultsAPIImpl.Create` commits
the `Result` and a `dispatch_outbox` row in one `dbstore` transaction; a relay
(in-process in every api-server by default) publishes committed rows. Two
consequences to keep: a publication failure must leave the Result `pending` —
the old code rewrote it as `errored`, which claimed an execution that never
happened — and the event UID is minted once at enqueue and reused on every
retry, because it is the `Nats-Msg-Id` that suppresses the duplicate. See
`cmd/api-server/README.md`.

**A run that cannot be placed is terminal, not pending.** If no *active* runner
matches the scenario's requirements, `Create` writes the `Result` already
`errored` with `urth/result.unschedulable=no-eligible-runner` and **no outbox
row** — the POST still returns 201, because a future scheduler has no caller to
hand a refusal to. It is deliberately not a dead letter: a selector matching
nothing is ordinary (a runner was decommissioned), and a per-minute scenario
would file one record per tick. The relay is the backstop for rows written
before this check — `urth.ErrDispatchUnplaced` strands the run and files
nothing, while any other `ErrPermanentDispatch` becomes an
`undeliverable-dispatch` dead letter; both leave the row to the reconciler's
stale-dispatch sweep. Eligible runners with *no workers* is **not** unschedulable:
that dispatch waits in the queue. `GET /scenarios/:id/placement` is the preflight
the UI gates "Run now" on.

**Placement reads capacity but can never refuse a run.** Among runners a scenario
matches, `selectRunner` picks by spare capacity — `online workers − (queued +
running) runs`, both counted from Postgres, with no broker round trip on the
run-creation path. When nothing has spare it falls to a weighted random draw by
worker count, which is why placement is *not* fully deterministic and why
`placement.pick` exists to be injected in tests. The rule that must survive any
future edit: **capacity decides which queue, never whether**. A fleet that is
entirely offline still gets a placement and still queues, because the queue is
durable. If you find yourself adding an "insufficient capacity" unschedulable
reason, re-read ADR 0004 first. Only `online` presence counts as capacity — an
`api-unreachable` worker cannot claim what it is offered and a `nats-unreachable`
one never receives it. Queued runs count as committed *deliberately*: it is what
makes a burst of runs spread rather than pile onto whichever runner looked idle
when the burst began. This works only because `Create` records the runner on the
`Result` before persisting it, so `results` knows every runner's queue depth —
`idx_results_placement` is what keeps that query off a sequential scan.

**A claimed job is acknowledged with `DoubleAck`, budgeted from the consumer.**
`Msg.Ack` only publishes the ack; lose the connection before the server records
it and the message is redelivered mid-probe — and since `ClaimRun` is idempotent
for the same worker and dispatch, that redelivery is *authorised*, not refused.
One process, same probe, twice. `handshakeBudget` (`cmd/nats-worker/ack.go`)
splits `consumer.CachedInfo().Config.AckWait` so claim + ack ≤ AckWait always,
with no floor under either half — a worker granting itself more of the window
than the operator allowed would acknowledge messages already offered elsewhere.
Three rules to keep: an **unconfirmed ack never withholds execution** (the claim
is committed and leased; refusing to run strands it, and re-claiming or rolling
back is not the worker's to do); the **in-process ownership set is acquired
before the claim**, because the racing case is two deliveries claiming at once;
and stale (409) messages keep the plain `Ack`, since nothing is executing and a
lost one costs a redelivery and another cheap 409. That set is not durable and
is not meant to be — the execution lease is.

**A scheduled run does not read its Scenario again.** `ResultSpec.Execution` is
an `ExecutionSnapshot` — scenario UID/name/version, requirements, and the whole
typed `prob.Manifest` — copied when the run is created. Claim authorization uses
only that; nothing on the claim or dispatch path loads the `Scenario`, which is
why editing one no longer changes a queued run and deleting one no longer kills
it. The field is `json:"-" yaml:"-"`: a probe definition may carry credentials
and is disclosed only in the claim response. A `Result` whose snapshot is NULL
(written before the column) is refused at claim, marked `errored`, and labelled
`urth/result.unschedulable=missing-execution-snapshot` — never back-filled from
the current scenario. See `cmd/api-server/README.md`.

**Nothing repairs itself; the reconciler does it.** `pkg/urth/reconcile.go` runs
in every api-server beside the relay and is what makes the execution lease and
the outbox mean anything: an abandoned `running` run and a `pending` run whose
message aged out are both invisible without it. Rules that are easy to break
when touching it — an **unpublished** outbox row is the relay's, never the
reconciler's (inferring a lost dispatch there expires runs the relay is about to
deliver); an expired `Result` is never reopened, because a retry must create a
new `Result`; `--pending-dispatch-grace` is *added to* `--nats.max-job-age`
rather than set independently. Retired rows (`retired_at`) leave both the
relay's claim query and `DispatchOutbox.Stats`.

**Postgres compares `uuid` and `text` columns only with a cast.** `results.uid`
is a `uuid` column; every copy of a resource ID stored on a non-manifest table —
`dispatch_outbox.result_uid` — is `text`. Joining them directly fails the whole
query with `operator does not exist: uuid = text`. Bound *parameters* are fine;
it is column-to-column comparison that needs `CAST(results.uid AS TEXT)`.

**JetStream does not report "no message here" consistently.**
`Stream.DeleteMsg` on a sequence past the end of the stream returns a generic
500 (`stream store EOF`), not `ErrMsgNotFound`. `natsq.DropDispatch` confirms
absence with a `GetMsg` rather than matching the error's shape; matching alone
would have the reconciler retry the same entry forever.

**Prob specs in `pkg/urth` tests are decoded strictly.** `execution_test.go`
links every prober into the test binary, as the api-server does, so a fixture
prob is unmarshalled against its registered type rather than falling back to
`map[string]any`. A made-up field (`{"url": …}` for the `http` kind) now fails
with `unknown field` where it used to round-trip unnoticed.

**`file::memory:` gives every pooled connection its own SQLite database.** A row
written on one connection and read on another silently is not there, which reads
exactly like the store failing to persist. Use
`file:<unique-name>?mode=memory&cache=shared`. Relatedly, the outbox table
deliberately carries no `type:TIMESTAMPTZ` tags: gorm's Postgres driver already
maps `time.Time` to `timestamptz`, and naming the type forces it on SQLite too,
where the driver cannot scan it. Verified against a live Postgres — do not
"fix" it by adding the tags back.

**Some tests need a real Postgres.** Result/dispatch atomicity, relay row leasing
(`FOR UPDATE SKIP LOCKED`) and every reconciler test are Postgres properties, so
those tests skip themselves unless `URTH_TEST_POSTGRES_URL` is set. `make test/postgres` and
`make audit/postgres` set it from `store-url`; CI provisions the database as a
service and runs `audit/postgres`. A green `make audit` has not run them. A URL
that is set but unreachable fails rather than skips, deliberately.

**Go naming is enforced.** staticcheck runs with `-checks=all` and the codebase
was renamed to Go initialism convention (`API`, `ID`, `URL`, `HTTP`). Match it.

## Domain notes worth keeping

**Artifact data classification.** Every artifact declares what it may expose:
`clean` (metrics), `redacted` (run logs), `secret-bearing` (HAR), `unknown` (no
declaration — counts as unsafe). Surfaced as `urth/artifact.data-class` and
`urth/artifact.may-contain-secrets` so retention and audits are label queries.

The reasoning matters if this is revisited: a HAR exists to be replayed, which
requires a faithful copy of the exchange — fidelity and redaction are the same
bytes. So HARs are labelled, not redacted. Run logs take the opposite side:
header *values* are written only for an allowlist of safe headers, because
"which headers carry credentials" is not knowable for services you didn't write.

CodeQL alert #1 (`go/clear-text-logging`) is dismissed as a false positive: the
allowlist means no credential reaches the sink, but CodeQL can't model a map
lookup as a sanitiser. The tests are the guard now.

**Executor identity.** A run records which runner and worker executed it,
captured by `executorRef` in `ClaimRun` at the moment of claim — the only point
the association is certain. Also exposed as `urth/runner.*` / `urth/worker.*`
labels.

**Label queries are the search surface** everywhere: scenarios, runners, results,
artifacts, workers. `?labels=key = value` or `key in (a,b)`. `?from=` / `?till=`
filter on creation time and work on all list endpoints (I once wrongly reported
they didn't — that was a shell-quoting bug in my test).

## Conventions

- Commit messages explain *why*, and name the bug a change fixes. Long-form is
  normal here.
- Comments earn their place by explaining reasoning or non-obvious constraints,
  not by restating the code.
- Branch per change, PR to `main`, CI must be green. CI only triggers on PRs
  targeting `main` — a stacked PR gets **zero checks** and still reports
  "mergeable", which reads misleadingly like passing.
- `TODO.md` is the backlog. Keep it honest, including corrections.
