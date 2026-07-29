# A list of ideas to implement
(Temporary here until proper task management system is provisioned)

---

## Where things stand (end of UI/admin session)

See `CLAUDE.md` first -- it holds the traps that cost the most time.

**Open PR:** #12 `feat/results-pages` -> main (Results list + standalone run
detail). Was stacked on #11; #11 is merged and #12 has been retargeted to main,
so it should now get real CI. Confirm the checks are green before merging --
while it was stacked GitHub reported "mergeable" with **zero checks run**, which
looks deceptively like passing.

**Also open:** dependabot #2 (Go module bumps, 12 updates). Untouched.

### Next, in the order I would take them

1. **Verify #12's CI and merge.** Nothing depends on it, but it is finished work
   sitting unmerged.
2. **A scheduler that actually schedules.** Still the largest gap between the
   README and reality: scenario `schedule` fields are stored and validated, and
   `nextScheduledRunTime` is computed and displayed, but nothing triggers a run.
   Every run to date is manual. Required for v1.0.

   **Deliberately deferred, not forgotten.** A distributed scheduler is important
   and fiddly enough to deserve a design pass of its own rather than being
   grown incrementally.

   **Correction:** earlier notes here proposed driving it from Postgres
   LISTEN/NOTIFY. [ADR 0004](docs/adr/0004-nats-communication-backbone.md)
   considered that and chose NATS JetStream instead -- a `URTH_EVENTS` stream
   with one durable consumer per projection. Postgres-only remains a viable
   simpler design, but it was weighed and not selected, so treat the ADR as the
   direction rather than this paragraph. The scheduler still owns cron
   evaluation, missed-run policy, and runner selection; NATS carries the
   wakeups, not the decisions.

   Until that design lands, treat manual triggering as the supported path and
   build everything else against it. The scheduler removes the need to press the
   button; it should not change what the button does.

   **One thing it inherits from
   [task 018](docs/review-backlog/tasks/018-fail-unplaceable-runs.md):** a run
   that no active runner matches is now recorded terminal
   (`urth/result.unschedulable=no-eligible-runner`) rather than left pending, and
   deliberately files no dead letter -- a selector matching nothing is an
   ordinary state of a fleet being changed. That is the right answer for a run
   somebody asked for. A scheduler firing every minute against an unplaceable
   scenario would produce a terminal run per tick, so it needs a policy for
   backing off, or for marking the scenario itself as unschedulable, rather than
   filling the run history. The placement preview
   (`GET /scenarios/:id/placement`) is the check it would use.

   **Two things that design pass now inherits**, from
   [ADR 0006](docs/adr/0006-control-loop-placement.md): it is the named trigger
   for extracting the control loops into their own process -- the scheduler needs
   a host, and that host is the controller-manager the relay and reconciler would
   move into (they are already composed in `pkg/controllers` for this). And it
   owns retry policy, including the decision task 003 had to make in passing: a
   pending run whose dispatch outlived job expiry is expired rather than
   republished. §7 draws the boundary -- the reconciler terminates attempts, the
   scheduler decides whether another one happens.
3. **Fix SQLite, or stop offering it.** `--store.url` defaults to a backend that
   cannot start. Either fix `idx_name` upstream in wyrd (`index:idx_name` ->
   `index`, letting gorm name it per table) or change the default to make the
   supported path the obvious one. Currently a new contributor's first run fails.
4. **Retention acting on data classification.** The labels exist and are queried;
   nothing expires. `secret-bearing` artifacts should have a shorter default
   expiry and restricted download. This is the other half of the artifact
   classification work.
5. **Dashboards.** The nav item is disabled and has never led anywhere -- the
   same state Results was in before this session.

### Carrying known debt

- **Prober defaults are lost on every authoring path.** Now
  [task 017](docs/review-backlog/tasks/017-apply-prober-config-defaults.md),
  which carries the evidence and the fix; this is a pointer, not a second copy.

  **Correction to the earlier note here**, which said `urthctl`'s YAML path
  applied blackbox's defaults and only the UI's JSON path lost them. It does not.
  Defaults come only from a YAML decode *and only when the prober's config
  sub-block is present and non-empty* -- `yaml.v3` skips a custom unmarshaler for
  a null node, and every prober example in this repo has that block commented
  out. So the UI's `IPProtocolFallback` seeding in
  `website/src/utils/probSpec.js` was masking one symptom of a wider problem.

  A probe stored this way runs ip6-only and fails to resolve `localhost` or even
  `127.0.0.1`, reporting `failed` -- indistinguishable from the target being
  down. Sidestepped in practice by the `rest` prober
  (`examples/scenario.rest.httpbin.yml`), which uses no blackbox config types.

- `Active / Disabled / All` in the scenarios header are dead links
  (`href="#"`). They look like filters and are not.
- No authentication on non-GET requests. Anyone who can reach the API can
  disable a runner or drop a worker. Fine for local development, not for the
  "enterprise friendly" claim.
- The UI polls; there is no live update. A run triggered from the UI only
  appears after a refetch. Per ADR 0004, resource changes belong on the durable
  `URTH_EVENTS` JetStream stream used by the scheduler and projections -- do not
  build a separate notification transport for the UI in the meantime.

---


## Code quality
[X] Switch to make
[X] Add `go vet ` to go-lang build pipeline
[X] Add static `testtool` to go-lang build pipeline (staticcheck + govulncheck in `make audit`, run by CI)

# Feature:
[] Enable *scheduler* to actually USE scenario schedules field
[] (MAYBE) Script should be stored compressed / (zlib?)
[X] Ensure that only Scenarios with non-empty script are schedulable / ready
[x] Use kong for CLI flags and config handling
[?] Expose option for headless chrome remote debug?
[] Search for config files using xdg! lib and standard
[] Architecture: Implement web-hooks for events! (UI design to add option to add hooks + UI to manage existing hooks on account level?)
[] Implement server event streaming: new scenarios / new runs / scenario update, to sync
   multiple running instances of web-api. Per ADR 0004 this is the `URTH_EVENTS` JetStream
   stream, not a Redis event queue as previously noted here.
   Live *run logs* already stream (worker -> Core NATS -> SSE); this item is about
   resource change events.
[] Support pluggable scenario runners via go plugins
[] All scripts must contain "EXPECT" section to test for:
- Deadline for a TCP request
- Response Body for TCP request
- Regexp to match response body for TCP request
- Response code for HTTP request
[x] Validate labels names!
[X] Add API to find workers give a set of labels and requirements - to enable better UX where user can see how many probers will qualify for a given set of labels. (NOTE: This is a statdards label-besed search API)
[X] A run results object with an update time-limited JWT token must be created when a job is scheduled. Worker can only update, within a time alloted, an already `pending` run.
[x] Restore labels API: Extract labels from JSON field
[x] Create API must return metadata for a newly created object as `names` may be generated.
[X] For `Create` API set `Location` header to point to a newly created resource as per rest best practice
[] All non-GET request should require authentication!
[] Artifacts should expire and be removed in accordance with retention policy, unless `pinned`


## CLI tooling
[X] `urthctl` - support reading scenario / script from stdin
[X] `urthctl` convert HAR into .http files
[X] `urthctl` `apply` command to create/update scenarios
[ ] `urthctl` `get run artifact` command to fetch artifacts produces during script run

## Web UI
[] UI: Integrate with HAR viewer for artifacts of HAR-kind
[] HAR viewer should offer an option to diff with previous runs!!
[] Web Request runner: if response contains headers about the TRACE-ID, produce an artifact with a link to a trace viewer (configurable for installation)
[] If a request return spanID - add a link to View Trace in <Jager> or <Tempo>
[X] UX - 'run now' button must be locked until post request returns with an ID of message posted into the run Queue.
[X] A RUN must be in `pending` state when a message been posted into the queue but before being picked up by a worker.
[] For manual runs - trigger identity of the who triggered the run as job labels, such that all jobs triggered by a given user can be found!
[] Add visual indicator around STATUS circle for time for time before the next run: `(next_run - previous_run) / (delta_between_runs)`
[] UX: Allow _authenticated_ users to "bookmark" resources in their profiles!
[] UX: Add option for _authenticated_ users to save 'favourite' scenarios and filters based on tags. Personalized "folders" based on multiple tags should help navigation.
"As a user, I want to be save a set of tags that can be quickly acceessed when I use the app UI"

## Workers / Prob Runners
[X] Worker liveness. A stopped worker used to render exactly like a running one: nothing
   ever recorded that a worker was alive, and re-registration at ⅔ of a 1h session was far
   too coarse to be a signal. Two independent signals are now recorded — API contact
   (heartbeat *or* run claim) and NATS presence — and combined into
   `online`/`offline`/`api-unreachable`/`nats-unreachable`/`unknown`, so a half-connected
   worker is diagnosed rather than merely marked absent. Closes ADR 0003 §7's liveness
   clause. See `cmd/api-server/README.md`.
[] Worker detail page — the per-worker breakdown of both signals wants more room than a
   list row. Filed as review-backlog task 023.
[X] Capacity-aware placement (review-backlog task 014). Placement sorted eligible runners
   by UID and took the first, so a runner won on its identifier and kept winning however
   far behind it fell. It now picks by spare capacity — online workers minus queued and
   running runs — and, when nothing has spare, by a draw weighted on worker count. Both
   numbers come from Postgres, so run creation gained no broker round trip: presence says
   who is reachable, and `results` already knows each runner's queue depth because the
   runner is recorded before the run is persisted. Capacity still cannot refuse a run — an
   all-offline fleet queues, per ADR 0004. See `cmd/api-server/README.md`.
[] Surface the placement preview's new capacity fields (`onlineWorkers`, `queuedRuns`,
   `runningRuns`, `spareCapacity`) in the UI. The "Run now" preflight already fetches them.
[] Per-scenario policy requiring online capacity, mentioned in task 014. Needs a
   `ScenarioSpec` field and a decision about whether a scenario may ever refuse to queue.
[~] Workers should talk to API servers over gRPC -- reconsidered. ADR 0004 evaluated direct
   gRPC streams as the job backbone and rejected them: they would require Urth to implement
   durable offline queues, redelivery, and backpressure itself. gRPC may still be worth it
   for request/response API calls; it is not the job transport.
[X] Add .HTTP/.REST file runner into its own package
[X] `.http` parser: request separators, headers, bodies, `@name`, and `@var`/`{{var}}`
   substitution. Response handlers (`> {% ... %}`) are recognised and skipped; see
   `pkg/http-parser/README.md` for what is deliberately rejected rather than ignored.
[] `.http` parser: response assertions. A script's `> {% client.test(...) %}` block is
   currently discarded, so a scenario that relies on one to decide pass/fail silently
   passes on any response the prober itself accepts. Either run them (a JS engine in the
   runner) or express the assertions in the prob spec.
[] `.http` parser: variables supplied from outside the script, so one scenario can be
   pointed at staging and production. Needs a `variables` field on the rest prob spec.
[] `.http` parser: dynamic variables (`{{$uuid}}`, `{{$timestamp}}`) — rejected today.
[X] Worker should check puppeteer availability and add labels it available
[X] Workers should be annotated with the type of puppeteer available: JS or Python and versions
[x] Web Request runner: integrate WEB listener to produce HTTP log + HAR file as artifacts
[] Web request runner must inject trancing context / Jaeger / OpenTelemetry
[] Puppeteer Worker: export HAR File as run artifacts (Per each test?)
[] Puppeteer Worker: Inject tracing context
[] New prober: DNS prober
[] New prober: TCP runner that checks payload
[] New prober: TCP Fuzzer?
[] New prober: Fuzzer to guess HTTP urls or understands swagger?
[] New prober: Dataset checker!
[X] New runner: HAR executor - replay HAR files using WEB Request runner
[X] When worked reports Node version, parse version string to `node.major` and `node.minor` to enable `<>` comparison using label selectors
[] Script should be typed by `kind`: TCP, DNS and similar **infra** probers have well defined fields. 
[] Split artifact registration (produces upload token) and artifact content upload - use different APIs

[] Allow for script config / encrypted variables. Consider gocloud/locked box: secrets API. Review github security considerations for custom workers
[] Support authentication for HTTP and Puppeteer scenarios


# Notes
- Docker has `--init` flag to run init process in a container that rips zombie processes.
- Consider Tempo (Grafana tracing solution) for tracing
- KeyDB - better implementation of Redis for distributed compute
- ~~Consider using Postgres as PubSub for API server - to - worker job distribution~~ --
  settled by ADR 0004 in favour of NATS JetStream.

## TODOs:
[X] Worker instances are manageable: list/get, pause (server-owned, enforced at job
   claim), and drop. Disabling a runner now also stops its already-connected workers.
[X] Web UI: cross-scenario Results list, run detail with artifacts, scenario detail with
   run history and stats, runner detail with worker admin. Search on every list page.
[X] A run now records which runner and worker executed it, captured when a worker claims
   the job in `Results.Auth`, and exposed as `urth/runner.*` / `urth/worker.*` labels.
[X] `GET /scenarios/:id/results` does support a server-side time window: `?from=` / `?till=`
   were already bound by bark and applied by dbstore. The earlier claim that it did not
   was a bad test, not a missing feature. The UI now sends the window.
[X] Result timestamps were stored in Postgres `TIMESTAMP` (without time zone), so local
   wall-clock times were read back as UTC and every run time was off by the server's
   offset. Now `TIMESTAMPTZ`.
[] Secrets injection at replay time: a HAR recording currently stores live credentials
   because fidelity is what makes it replayable. Capture placeholders instead
   (`Authorization: {{urth.secret.auth}}`) and have the runner inject from a secret
   store at run/replay time. Would also fix scenarios storing credentials in their
   spec. Depends on the encrypted-variables work below. Until then HAR artifacts are
   labelled `urth/artifact.data-class: secret-bearing`.
[] Retention and access control should act on `urth/artifact.data-class`: secret-bearing
   artifacts want a shorter default expiry and restricted download.
[] `examples/README.md` references `run.scenario.json`, which does not exist.
[X] A run that no active runner matches is terminal on creation rather than pending
   forever, and the relay settles a dispatch it can never publish instead of retrying
   it hourly. `GET /scenarios/:id/placement` is the preflight the UI gates "Run now"
   on. See [task 018](docs/review-backlog/tasks/018-fail-unplaceable-runs.md).
[] **`notin` means two different things depending on where it is evaluated.** With the
   label *absent*, `wyrd`'s Go matcher (`manifest.Requirement.Matches`) matches — the
   Kubernetes rule — while its SQL translation (`NOT IN` over a JSON path) yields NULL
   and excludes. Urth uses both: placement is a store query, worker admission at
   registration is `Matches`. Correction: this was originally written as "the shipped
   `examples/runner.yaml` cannot satisfy `examples/scenario.rest.httpbin.yml`'s
   `envX notin (dev,testing)`", which is how it was found and what made task 018's runs
   unplaceable. That example now uses a plain `matchLabels: {env: dev}` and the shipped
   manifests place correctly — verified live, `matchingRunners: 1, schedulable: true`.
   **The divergence is unchanged** (`wyrd@v0.2.2` `manifest/selector.go:88-89` vs
   `dbstore/gorm_json.go` `KeyNotIn`); only the symptom left the demo path, which is
   worse — the next operator to write `notin` meets it with no example to warn them.
   Pick one rule, document it, make both evaluators obey it.
   See [task 020](docs/review-backlog/tasks/020-settle-notin-selector-semantics.md).
[] **`make test/postgres` destroys your dev database.** It passes `store-url` — the
   same Postgres the api-server uses — as `URTH_TEST_POSTGRES_URL`, and the tests
   `DropTable` the whole model set on setup and on cleanup. Every runner, scenario and
   run applied by hand disappears, silently. CI is fine, provisioning its own database.
   Either default the tests to a separate database name, or refuse to run when the URL
   matches `store-url` without an explicit override.
[] **`urthctl get script` cannot run**: the kong subcommand is declared with no `Run`
   method and no client call behind it, so it fails with `no Run() method found in
   hierarchy`. The server endpoint behind it was also answering 404 for every scenario
   until it was fixed to read typed prob specs — nobody noticed, which is the evidence
   that nothing calls it. See [task 025](docs/review-backlog/tasks/025-urthctl-script-parity.md).
[X] **A claimed job was acknowledged with `Msg.Ack`, which only publishes the ack and
   returns.** A connection lost before the server recorded it redelivers the message
   while the probe is running — and because the API's claim is idempotent for the same
   worker and dispatch, the redelivery is *authorised* rather than refused, so one
   process ran the same external probe twice, concurrently. Now `DoubleAck`, inside a
   reserve carved out of the consumer's own `AckWait` (the claim previously had a
   hardcoded 30s timeout, which is the entire default window), plus an in-process
   ownership set that drops a redelivery for a run already executing here. An
   unconfirmed ack never withholds execution: the claim has committed and the Result is
   leased. This narrows the duplicate window; ADR 0004 §5 still stands — probe execution
   is not exactly once. `--metrics-address` exports the numbers that say how wide the
   remaining window is. See [task 010](docs/review-backlog/tasks/010-synchronous-jetstream-ack.md).
[X] JetStream assets are bounded and observable: global message/byte/size limits
   beside the per-runner one, `MaxAckPending` on consumers, config validated
   before the broker is dialled, existing-stream drift reconciled or reported,
   and Prometheus metrics on `/metrics` for stream, outbox and dead-letter state.
   See [task 013](docs/review-backlog/tasks/013-bound-and-observe-jetstream.md).
[] Live run logs return 406 in a browser: `EventSource` sends
   `Accept: text/event-stream` and `bark.ContentTypeAPI()` on the `/api/v1` group
   refuses it before the handler runs. See
   [task 019](docs/review-backlog/tasks/019-serve-run-log-stream.md).
[] SQLite backend is broken: AutoMigrate fails with `index idx_name already exists`.
   `wyrd`'s `manifest.ResourceMeta.Name` carries a hardcoded `gorm:"index:idx_name"`, and
   every model embeds it; index names are schema-global in SQLite so the second
   CREATE INDEX collides. Postgres is unaffected. Either fix upstream in `wyrd`
   (use `index` and let gorm name it per-table) or drop the `sqlite:test.sqlite`
   default from `dbstore.Config` so the broken path isn't the default.
[X] Rename identifiers to Go initialism convention (`Api`->`API`, `Id`->`ID`, `Url`->`URL`,
    `Http`->`HTTP`) so `staticcheck` passes and `make audit` is green.
[X] Fix API to accept `version` query param
[X] Add `create` command to urthctl
[X] Move dbstore => separate module!
[X] Move runners implementation => separate modules with registration on import!
[] Remove MIME guessing code out of `pkg/urth`
[] Move `script` out of `CreateScenario` => `Scenario`
[] Use proper types for Script marshaling
[] runner/log.go must implement `go/logger` interface!
## NATS migration (ADR 0004)

`cmd/nats-worker`, `pkg/natsq`, per-runner JetStream consumers, worker sessions, the
authenticated claim, live run logs, the transactional dispatch outbox, and the
dispatch/execution reconciler have landed as a development slice.

The detailed NATS review and migration work is tracked in
[`docs/review-backlog/`](docs/review-backlog/README.md). Those task files are the source of
truth for outbox/reconciliation, claim outcomes, NATS credentials, enrollment and run
capabilities, immutable execution snapshots, Runner policy and blocklists, JetStream ACKs,
failure testing, dead letters, capacity/observability, placement, and eventual Asynq
retirement. Do not duplicate those items as flat bullets here.

[] Ensure DB constraints: Each Scenario ->* Result -> * Artifacts
[] Use staw / S3 for artifacts storage!
[] Ensure that `Worker Instance` login session expires.
[] OTel instrument server and worker
[] HAR prob should produce HAR files as output.

[] Adopt [SecretSpec](https://secretspec.dev/) for secret management.