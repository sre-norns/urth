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
cmd/api-server/     REST API, owns all resources, hands out jobs
cmd/asynq-runner/   the worker: claims jobs from Redis, executes, uploads results
cmd/urthctl/        CLI (kubectl-shaped)
pkg/urth/           domain model + service impl + REST client. The centre of gravity.
pkg/prob/           prob registry and the interface probers implement
pkg/probers/*/      one package per prob kind (http, tcp, dns, icmp, grpc, rest, har, puppeteer…)
pkg/runner/         job dispatch, run logging, metrics
website/            React UI (webpack, emotion, redux, wouter)
```

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
make run-redis-podman
make run-api-server             # passes a Postgres URL explicitly
go run ./cmd/urthctl apply ./examples/runner.yaml
go run ./cmd/urthctl apply ./examples/scenario.tcp.yaml
export RUNNER_TOKEN=$(go run ./cmd/urthctl auth-worker -f ./examples/runner.yaml)
go run ./cmd/asynq-runner --client.token="$RUNNER_TOKEN"
cd website && npm start         # :3000, proxies /api to :8080
```

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
- `cd website && npm test` — vitest, currently ~119 tests.
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

## Traps, in rough order of how much time they cost

**`dbstore.Update` silently drops zero values.** It passes the struct to gorm's
`Updates`, which skips zero-valued fields — so no bool can ever be set to
`false`. This defeated *disabling a scenario* and *disabling a runner* entirely;
both returned 200 and changed nothing. Resource edits now use `saveResource()`
(→ `CreateOrUpdate` → gorm `Save`). **Do not blanket-replace the remaining
`store.Update` calls**: the job-claim path in `resultsAPIImpl.Auth` relies on the
version-guarded update to lose the race when two workers reach for the same run.

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

**`file::memory:` gives every pooled connection its own SQLite database.** A row
written on one connection and read on another silently is not there, which reads
exactly like the store failing to persist. Use
`file:<unique-name>?mode=memory&cache=shared`. Relatedly, the outbox table
deliberately carries no `type:TIMESTAMPTZ` tags: gorm's Postgres driver already
maps `time.Time` to `timestamptz`, and naming the type forces it on SQLite too,
where the driver cannot scan it. Verified against a live Postgres — do not
"fix" it by adding the tags back.

**Some tests need a real Postgres.** Result/dispatch atomicity and relay row
leasing (`FOR UPDATE SKIP LOCKED`) are Postgres properties, so those tests skip
themselves unless `URTH_TEST_POSTGRES_URL` is set. `make test/postgres` and
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

**Executor identity.** A run records which runner and worker executed it, captured
in `Results.Auth` at the moment of claim — the only point the association is
certain. Also exposed as `urth/runner.*` / `urth/worker.*` labels.

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
