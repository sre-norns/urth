# A list of ideas to implement
(Temporary here until proper task management system is provisioned)


## Code quality
[] Switch to make
[] Add `go vet ` to go-lang build pipeline
[] Add static `testtool` to go-lang build pipeline

# Feature:
[] Enable *scheduler* to actually USE scenario schedules field
[] Script should be passed compressed / (zlib?)
[] Ensure that only Scenarios with non-empty script are schedulable / ready
[x] Use gorilla for CLI flags and config handling
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
[] If a request return spanID - add a link to View Trace in <Jager>
[] UX - 'run now' button must be locked until post request returns with an ID of message posted into the run Queue.
[] A RUN must be in 'scheduled' state when a message been posted into the queue but before being picked up by a worker.

## Workers / Script Runners
[x] Add .HTTP/.REST file runner into its own package
[] Worker should check puppeteer availability and add labels it available
[] Workers should be annotated with the type of puppeteer available: JS or Python and versions
[x] Web Request runner: integrate WEB listener to produce HTTP log + HAR file as artifacts
[] Web request runner must inject trancing context / Jaeger / OpenTelemetry
[] Puppeteer Worker: export HAR File as run artifacts
[] Puppeteer Worker: Inject tracing context
[] New runner: TCP runner that checks payload
[] New runner: TCP Fuzzer?
[] New runner: Fuzzer to guess HTTP urls?
[] New runner: Dataset checker!
[X] New runner: HAR executor - replay HAR files using WEB Request runner


[] Allow for script config / encrypted variables
[] Support authentication for HTTP and Puppeteer scenarios



# Notes
- Docker has `--init` flag to run init process in a container that rips zombie processes.
