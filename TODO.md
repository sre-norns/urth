# A list of ideas to implement
(Temporary here until proper task management system is provisioned)


## Code quality
[x] Switch to make
[] Add `go vet ` to go-lang build pipeline
[] Add static `testtool` to go-lang build pipeline

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
[] Validate labels names!
[] Labels returned by a worked for a job-results / artifacts must be immutable
[] Add API to find workers give a set of labels and requirements - to enable better UX where user can see how many probers will qualify for a given set of labels.
[X] A run results object with an update time-limited JWT token must be created when a job is scheduled. Worker can only update, within a time alloted, an already `pending` run. 
[] Restore labels API: Extract labels from JSON field
[] Create API must return metadata for a newly created object as `names` may be generated.
[X] For `Create` API set `Location` header to point to a newly created resource as per rest best practice
[] All non-GET request should require authentication!
[] API to create artifacts must only accept valid auth-tokens from workers that authorized to run a scenario
[] Artifacts should expire and be removed in accordance with retention policy, unless `pinned`


## CLI tooling
[x] `urthctl` - support reading scenario / script from stdin
[X] `urthctl` convert HAR into .http files
[ ] `urthctl` `apply` command to create/update scenarios
[ ] `urthctl` `get run artifact` command to fetch artifacts produces during script run

## Web UI
[] UI: Allow _authenticated_ users to "bookmark" resources in their profiles!
[] UI: Integrate with HAR viewer for artifacts of HAR-kind
[] HAR viewer should offer an option to diff with previous runs!!
[] Web Request runner: if response contains headers about the TRACE-ID, produce an artifact with a link to a trace viewer (configurable for installation)
[] If a request return spanID - add a link to View Trace in <Jager> or <Tempo>
[X] UX - 'run now' button must be locked until post request returns with an ID of message posted into the run Queue.
[X] A RUN must be in `pending` state when a message been posted into the queue but before being picked up by a worker.
[] For manual runs - trigger identity of the who triggered the run as job labels, such that all jobs triggered by a given user can be found!
[] Add visual indicator around STATUS circle for time for time before the next run: `(next_run - previous_run) / (delta_between_runs)`

## Workers / Script Runners
[x] Add .HTTP/.REST file runner into its own package
[] Worker should check puppeteer availability and add labels it available
[] Workers should be annotated with the type of puppeteer available: JS or Python and versions
[x] Web Request runner: integrate WEB listener to produce HTTP log + HAR file as artifacts
[] Web request runner must inject trancing context / Jaeger / OpenTelemetry
[] Puppeteer Worker: export HAR File as run artifacts
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
