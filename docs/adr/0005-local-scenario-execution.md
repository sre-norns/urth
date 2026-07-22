# ADR 0005: Separate managed Scenario runs from local CLI execution

- **Status:** Accepted
- **Date:** 2026-07-22

## Context

Urth supports two superficially similar but operationally different ways to execute a
Scenario.

A user can manually trigger a registered Scenario through the API and Web UI; as a core
control-plane workflow, this is also expected to have a CLI surface. The API creates a
pending Result, selects a Runner, and places the job on that Runner's queue. A Worker
eventually claims it, executes from the Runner's network vantage point, and posts status
and Artifacts using credentials issued for that Result. The execution becomes part of
Urth's durable monitoring history.

`urthctl` can also execute a Scenario directly on the machine where the command runs.
The definition may come from a local manifest or script under development, from standard
input, or from a registered Scenario fetched from the API by name. This short feedback
loop is valuable while authoring a Scenario and while troubleshooting from the user's
current network.

Calling both operations a "run" can hide important differences:

- the local machine may be in a different network segment from every eligible Runner;
- its DNS, proxy, routing, certificate authorities, installed tools, privileges, and
  probe versions may differ from those of Workers;
- it may not have the credentials or secret providers available to Workers; and
- an ordinary API client is not an authenticated Worker and has no Result-scoped run
  capability with which to report managed execution state.

Local execution must remain powerful without becoming an alternate, weakly
authenticated Worker path or allowing local observations to masquerade as managed
monitoring history.

## Decision

### 1. Managed runs and local executions are distinct operations

Urth uses the following terms and semantics:

| Property | Managed run | Local execution |
|---|---|---|
| Definition source | Registered Scenario | Local file, standard input, or registered Scenario fetched by name |
| Initiation | Create a Result through the API | Invoke `urthctl run` |
| Executor | Worker admitted to the selected Runner | The `urthctl` process on the user's machine |
| Vantage point | Selected Runner, recorded on the Result | User's current host and network; not a Runner |
| Server state | Pending, running, and terminal Result resources | No Result resource is created or updated |
| Execution authority | Worker session followed by a Result-scoped run capability | Local process authority only |
| Artifacts | Uploaded through the Result-scoped API and retained by policy | Printed or saved locally |

A managed run is asynchronous even when manually triggered. Successful creation means
that Urth accepted and queued a Result; it does not mean the probe has completed.
Scheduling, claiming, execution, status reporting, and Artifact upload follow
[ADR 0002](./0002-worker-authentication.md),
[ADR 0003](./0003-runner-worker-model.md), and
[ADR 0004](./0004-nats-communication-backbone.md).

A local execution is synchronous from the CLI user's perspective. It invokes the local
probe implementation and reports its outcome directly to that user. It does not enter
the scheduler, a Runner queue, or the managed Result lifecycle.

### 2. Fetching a Scenario by name fetches only its definition

`urthctl` may resolve a Scenario name through the ordinary resource API and execute the
returned probe specification locally. This supports troubleshooting without requiring
the user to first export the resource to a file.

The API applies normal read authentication and authorisation to that request. Reading a
Scenario does not grant any of the following:

- access to Worker or Runner enrollment credentials;
- access to Worker-local configuration or files;
- expansion of secret references using Worker-only secret providers;
- membership in the Scenario's selected Runner;
- permission to consume its queue; or
- permission to update a managed Result or create its Artifacts.

The server returns the same authorised resource representation it would return for an
ordinary `get`. It must not enrich the response with execution secrets merely because a
client intends to run it locally.

### 3. Local execution uses local capabilities and configuration

The local process executes with the network, operating-system identity, environment,
dependencies, and configuration available on the user's machine. It may use credentials
that the user has deliberately configured locally, but it does not inherit credentials
from an eligible Runner or Worker.

Consequently, a local failure does not prove that Workers will fail, and a local success
does not prove that the managed probe will succeed. Both outcomes are useful evidence
about a specific vantage point, but they answer different questions.

The CLI should make locally produced output clearly recognisable as local. Diagnostics
should identify the probe kind, local timeout, and artifact paths where useful, without
collecting or printing secrets. Any environment or capability warnings are explanatory;
they do not try to simulate Runner admission.

When exact managed conditions matter, the user must trigger a managed run on an
appropriate Runner. When the user's machine is itself intended to become a persistent
monitoring vantage point, the operator deploys a Worker there and enrolls it in a Runner
instead of relying on repeated CLI execution.

### 4. Local execution cannot report managed Results

`urthctl run` does not create a pending Result and does not receive a Worker session or
run capability. It therefore must not post its status, logs, or Artifacts into the
managed Result APIs.

A user's ordinary API credential remains a user credential. The server must not accept
it as a substitute for Worker authentication, even when that user is allowed to create
or manually trigger Results. Only the Worker that successfully claims a pending Result
can receive the capability to report that Result.

The CLI must not attach a local execution to an existing pending or running Result,
invent Runner or Worker identity, or label local output as if it came from a managed
vantage point. A future feature may define an explicitly untrusted imported-observation
resource, but it must not reuse managed Result provenance without a new architectural
decision.

### 5. Local execution is a deliberate CLI-specific capability

ADR 0001 requires the API, Web UI, and CLI to expose equivalent control-plane
capabilities. Local execution is outside that equivalence rule because it changes no
server state and depends on code and host capabilities available to a local executable.
The Web UI is not required to reproduce it inside a browser.

The managed manual-trigger workflow remains a control-plane capability and must be
available equivalently through the API, Web UI, and `urthctl`. CLI command names and help
must distinguish "trigger this registered Scenario through Urth" from "execute this
definition on this machine". `urthctl run` retains the latter meaning; a managed-trigger
surface must not silently change its execution location.

## Execution flows

```text
Managed manual trigger

User/UI/CLI -> API creates pending Result -> Runner queue -> Worker claims Result
                                                           -> executes from Runner vantage
                                                           -> uploads status and Artifacts

Local execution

local file/stdin -------------------------> urthctl -> local probe -> local output
registered Scenario -> API read by name --^           (no Result, queue, or upload)
```

## Architectural rules

1. A managed manual trigger creates a Result and follows the normal Runner and Worker
   lifecycle.
2. Local execution creates and updates no managed Result or Artifact resources.
3. Reading a Scenario grants access only to its authorised resource representation, not
   to Worker secrets, configuration, identity, or queue authority.
4. User credentials, even those allowed to trigger runs, never substitute for Worker
   sessions or Result-scoped run capabilities.
5. Local observations carry local provenance and must not be represented as observations
   from a Runner or Worker.
6. Local execution uses local network access, tools, privileges, configuration, and
   credentials; differences from managed execution are expected rather than hidden.
7. A persistent local-machine vantage point is modelled by deploying a Worker and
   enrolling it in a Runner.
8. CLI help and documentation explicitly distinguish local execution from managed
   triggering.

## Consequences

### Benefits

- Scenario authors get a fast edit-run-debug loop without registering incomplete
  resources or waiting for queue dispatch.
- Users can execute a registered Scenario from their current network during an
  incident, even when they do not have its manifest locally.
- The same probe implementations can be exercised locally without weakening Worker
  authentication or Result provenance.
- Managed history retains a trustworthy account of which Runner and Worker produced an
  observation.
- A local client cannot use broad user permissions to impersonate a Worker or attach
  arbitrary Artifacts to a Result.

### Costs and constraints

- Users must understand that local and managed outcomes may legitimately differ.
- Local execution may fail because required tools, privileges, network access, or
  credentials exist only on Workers.
- Some probe kinds may not be locally executable on every platform.
- CLI output and managed Result views are separate; local executions do not appear in
  server history.
- The CLI needs clearly separated commands or wording for local execution and managed
  triggering.

## Alternatives considered

### Create a managed Result and let `urthctl` execute it

This would make local output visible in normal history, but it would either require the
CLI to impersonate an eligible Worker or weaken Result reporting so a user credential
could claim execution authority. It would also misrepresent the selected Runner's
vantage point. It was rejected.

### Temporarily enroll every CLI process as a Worker

This would preserve Worker authentication, but turns an authoring command into a
short-lived infrastructure deployment with enrollment secrets, capability admission,
queue membership, heartbeats, cleanup, and Runner selection. Users who need their host
as a real vantage point can deliberately deploy a Worker. Automatic enrollment was
rejected.

### Allow local execution only from files

This is sufficient during initial authoring but awkward during troubleshooting: users
would need to find, export, and clean up a manifest before testing a registered Scenario.
Fetching the ordinary resource by name is safe and useful, so file-only execution was
rejected.

### Resolve Worker secrets and return them to the CLI

This would make local execution more closely resemble a managed run, but crosses the
Worker trust boundary, broadens secret exposure, and still cannot reproduce the
Worker's network and host environment. It was rejected. Users may configure suitable
local credentials through an explicit local mechanism.

### Upload local output as a normal Result after execution

Post-hoc upload has no authenticated claim, recorded Runner selection, Worker identity,
or server-enforced execution deadline. Treating it as managed history would make Result
provenance unreliable. It was rejected; any future import facility must use a distinct,
explicitly untrusted model.

## Implementation status at acceptance

`urthctl run` already supports the local half of this decision. It can load a Scenario
manifest or supported script from a file or standard input, or fetch a registered
Scenario by name. It executes through the shared probe runner and handles produced
Artifacts locally without uploading them.

The API and Web UI already create a pending Result for a managed manual trigger. A
distinct `urthctl` surface for that managed trigger is still required for full
control-plane equivalence; it must coexist with, rather than replace, the local meaning
of `urthctl run`.
