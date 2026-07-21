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
   Every run to date is manual. This is the feature that makes the product what
   the README says it is.
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

- `Active / Disabled / All` in the scenarios header are dead links
  (`href="#"`). They look like filters and are not.
- No authentication on non-GET requests. Anyone who can reach the API can
  disable a runner or drop a worker. Fine for local development, not for the
  "enterprise friendly" claim.
- `examples/README.md` references `run.scenario.json` and `worker.yml`, neither
  of which exist.
- The UI polls; there is no live update. A run triggered from the UI only
  appears after a refetch.

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
[] Implement server event streaming: new scenarios / new runs / scenario update. To sync multiple running instances of web-api - use redis events queue
[] Support pluggable scenario runners via go plugins
[] All scripts must contain "EXPECT" section to test for:
- Deadline for a TCP request
- Response Body for TCP request
- Regexp to match response body for TCP request
- Response code for HTTP request
[x] Validate labels names!
[~] Labels returned by a worker for a job-results / artifacts must be immutable
   (system labels are merged last, so a worker cannot relabel its own upload as clean;
    worker-supplied labels are still stored as given)
[X] Add API to find workers give a set of labels and requirements - to enable better UX where user can see how many probers will qualify for a given set of labels. (NOTE: This is a statdards label-besed search API)
[X] A run results object with an update time-limited JWT token must be created when a job is scheduled. Worker can only update, within a time alloted, an already `pending` run.
[x] Restore labels API: Extract labels from JSON field
[x] Create API must return metadata for a newly created object as `names` may be generated.
[X] For `Create` API set `Location` header to point to a newly created resource as per rest best practice
[] All non-GET request should require authentication!
[] API to create artifacts must only accept valid auth-tokens from workers that authorized to run a scenario
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
[] Workers should talk to API servers over gRPC
[X] Add .HTTP/.REST file runner into its own package
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
- Consider using Postgres as PubSub for API server - to - worker job distribution and scheduling.

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
[] `examples/README.md` references `run.scenario.json` and `worker.yml`, neither of which
   exist.
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
[] Serialize `job` into msgpack!
[] Ensure DB constraints: Each Scenario ->* Result -> * Artifacts
[] Use staw / S3 for artifacts storage!
[] Separate `Runner` -> `Worker (Slot)` + `Worker Instance` object
[] Ensure that `Worker Instance` login session expires.
[] All tokens must be treated as passwords: stored securely salted and hashed
[] OTel instrument server and worker
[] HAR prob should produce HAR files as output.
