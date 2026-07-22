# ADR 0001: Model Urth as a resource-oriented monitoring platform

- **Status:** Accepted
- **Date:** 2026-07-21

## Context

Urth performs synthetic, black-box monitoring of services with HTTP, TCP, DNS, ICMP,
gRPC, browser, and other kinds of probes. A probe may need to run from inside a VPC,
behind a VPN, in a DMZ, on a factory network, or from several of those locations. The
place from which a probe runs is part of the observation: the same service can be healthy
from one network and unreachable from another.

The system has several interfaces and participants:

- operators use the Web UI and `urthctl`;
- automation talks directly to the API;
- worker processes claim and execute jobs; and
- the API server coordinates desired configuration, execution, and history.

If each interface owns a separate model or privileged workflow, their capabilities and
semantics will drift. An action-oriented API would also turn important domain objects
such as executions and artifacts into transient responses instead of addressable,
queryable history.

Kubernetes provides a familiar vocabulary for declarative resources, metadata, labels,
selectors, desired configuration, and observed status. Those conventions are valuable
to Urth, but requiring a Kubernetes cluster would unnecessarily constrain where the
control plane and workers can run. Urth is its own control plane, not a Kubernetes
operator.

## Decision

### 1. The resource model is the system's contract

Urth models its durable domain concepts as versioned resources. The initial resource
kinds are:

- **Scenario** — what to probe, when it should run, and the placement requirements for
  its execution;
- **Runner** — an operator-managed registration and scheduling pool for workers;
- **WorkerInstance** — a live worker process registered against a Runner;
- **Result** — one requested execution and its eventual outcome; and
- **Artifact** — output from a Result, such as logs, metrics, HAR files, or screenshots.

Resources have stable identity and metadata. Their external representation follows a
common envelope with `apiVersion`, `kind`, `metadata`, `spec`, and, when the resource has
observed state, `status`. `spec` contains declared configuration or input; `status`
contains state reported or computed by the server and authorised system participants.
Ownership of individual fields must be explicit so that one participant cannot silently
overwrite another participant's state.

The REST API is organised around collections and individual resources. Standard resource
operations use HTTP methods and resource URLs rather than interface-specific commands.
Operations that start work should be expressed as resource creation or a resource state
transition when there is a meaningful thing to track. For example, manually running a
Scenario creates a Result in a pending state; workers and the server then advance its
status. The execution is therefore visible and queryable before, during, and after it
runs.

Labels and selectors are common resource mechanisms, not feature-specific query syntax.
They support discovery, operational grouping, retention and audit queries, worker
capability matching, and probe placement.

### 2. Resource representations are CRD-like, not Kubernetes CRDs

Urth deliberately borrows the shape and semantics of Kubernetes resources:

- versioned manifests;
- kind and metadata;
- spec and status separation;
- labels and label selectors; and
- optimistic resource versions where concurrent updates need protection.

Urth implements these conventions directly in its API and persistence layer. A
Kubernetes cluster, API server, controller runtime, or CRD installation is not required.
"CRD-like" means compatible in spirit and familiar to Kubernetes users; it does not mean
wire compatibility with the Kubernetes API or that Urth manifests can be submitted to
`kubectl`.

This distinction lets Urth retain its own domain language and deployment model while
using proven resource-oriented conventions.

### 3. The API, Web UI, and CLI expose the same control plane

The REST API is the source of truth. The Web UI and `urthctl` are peer clients of that
API:

- core server-side capabilities must not exist only in the UI or only in the CLI;
- a resource created or changed through one client is immediately manageable through
  the other;
- validation, defaulting, authorisation, and state transitions belong behind the API so
  every client observes the same rules; and
- new core workflows should be designed at the resource and API level before being
  added to either client.

Equivalence does not require identical interaction design. The UI may provide visual
editing and navigation, while `urthctl` provides composable output, manifest-based
workflows, and `kubectl`-inspired verbs such as `get`, `apply`, and `delete`. Client-local
tools, such as running a probe without uploading a Result or converting a HAR file, do
not change server state and are outside this equivalence requirement.

### 4. Probe placement is explicit domain state

Urth uses self-hosted workers so users control the network vantage point of a probe.
Workers make outbound connections to register and claim work; the control plane does not
need inbound access to the network being monitored.

A Runner is the long-lived, operator-managed scheduling channel. WorkerInstance
resources represent the live processes attached to that Runner. Scenario requirements
select stable Runner labels; the Runner's Worker requirements ensure that every process
joining the channel can honour its advertised contract.

The selected Runner is recorded when a Result is scheduled, and the WorkerInstance is
recorded when it claims the job. Historical probe data therefore retains both its
intended vantage point and physical executor even when registrations later change or
disappear. The distinction is specified in
[ADR 0003](./0003-runner-worker-model.md).

This is analogous to self-hosted CI runners: Urth centralises coordination without
centralising execution. A single control plane can orchestrate probes in networks that
cannot reach one another and that should not accept inbound control-plane traffic.

## Architectural rules

The decision implies the following rules for future work:

1. A new durable domain concept should normally be introduced as a resource. An action
   endpoint needs a reason why no meaningful resource or state transition exists.
2. Core workflows must be available through the REST API and must remain operable from
   both the Web UI and `urthctl`.
3. Clients must not independently implement authoritative validation, defaulting, or
   lifecycle rules.
4. Probe placement must be represented through resource metadata and selectors, not
   hidden in a particular API server or queue configuration.
5. Resource field ownership must be documented and enforced, especially across
   operators, workers, and server-managed status.
6. Results must preserve the execution facts needed to interpret history, including the
   probe kind and runner/worker identity at execution time.

## Consequences

### Benefits

- The API, UI, CLI, workers, and automation share one domain language.
- Manifests support repeatable configuration, review, and automation.
- Executions and their artifacts remain addressable and queryable instead of becoming
  ephemeral command responses.
- Labels and selectors provide one mechanism for search, policy, capability matching,
  and placement.
- Users can observe private and segmented networks without opening inbound paths to
  them.
- Adding a probing location is an execution-plane change rather than a new control-plane
  deployment.

### Costs and constraints

- Resource APIs require versioning, field ownership, validation, concurrency handling,
  and lifecycle design that a collection of RPC endpoints might avoid.
- Long-running work is asynchronous. Clients must present intermediate resource states
  and cannot assume that a successful create means a successful probe.
- UI features cannot rely on private server shortcuts; API and CLI support are part of
  completing a core workflow.
- Labels and selectors become public API and need stable syntax and semantics.
- Distributed workers introduce authentication, stale-registration, capacity, and
  scheduling-failure concerns.
- Kubernetes familiarity helps explain the model, but Urth must clearly document where
  its manifests and semantics differ to avoid implying Kubernetes API compatibility.

## Alternatives considered

### Action-oriented or RPC-style API

Endpoints such as `POST /run-scenario`, followed by interface-specific status responses,
would be initially simpler. They were rejected because executions, results, and artifacts
need identity, lifecycle, history, links, and queries of their own.

### UI-owned workflows with a secondary API and CLI

This would speed up individual UI features but create multiple sources of domain rules
and leave automation and CLI users behind. It was rejected in favour of an API-first
control plane with peer clients.

### Centrally hosted probers only

Central execution cannot observe private or mutually isolated networks without inbound
connectivity or bespoke tunnels. It also hides the fact that vantage point affects the
meaning of a monitoring result. It was rejected in favour of user-placed workers that
poll outward.

### Kubernetes CRDs and controllers as the control plane

Native CRDs would provide a ready-made resource API and reconciliation machinery, but
would make Kubernetes a deployment requirement and expose Urth's domain through
Kubernetes implementation constraints. It was rejected; Urth adopts the useful resource
conventions while remaining independently deployable.
