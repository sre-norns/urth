# ADR 0009: Typed Runner channel policy

- Status: Proposed (implementation in review)
- Date: 2026-10-04
- Extends: ADR 0003 sections 3–5

## Contract

`Scenario.spec.requirements` selects operator-owned Runner metadata labels. It
retains the existing equality and set selector semantics. Runner
`spec.requirements` is obsolete. The API rejects that field. Fresh installations
use the fields below. There is no compatibility reader or migration window.

`Runner.spec.jobRequirements` contains `probeKinds`, optional `labels`, and
optional `minDuration` and `maxDuration`. Probe kinds form an explicit finite
allowlist. An omitted or empty allowlist accepts no jobs. Duration defaults are
`1ns` and `1m`. A probe with no timeout requires `1m`. Explicit timeouts must be
positive. Duration bounds are inclusive Go duration strings. Zero is invalid.
The job labels come from the Scenario at Result creation.

`Runner.spec.workerRequirements` contains an optional label selector, Worker
`version` range, `probeVersions` and `runtimeVersions` maps of version ranges,
`operatingSystems`, `architectures`, `privileges`, and duration bounds. Empty
platform lists accept any declared platform. Required privileges are a subset
of declared privileges. Missing version map entries never satisfy requirements.
An empty version range requires presence but imposes no version constraint.

Version ranges contain whitespace-separated comparators (`=`, `>`, `>=`, `<`,
`<=`) against full semantic versions. All comparators must hold. A bare version
means equality. A leading `v` is permitted. Wildcards and OR expressions are
invalid. Semantic ordering includes prereleases. Raw build versions remain
visible; development, unknown and invalid versions cannot satisfy a nonempty
range. Labels never implement version comparisons.

Worker registration supplies `spec.capabilities`: raw `version`, `os`,
`architecture`, `probeVersions`, `runtimeVersions`, `privileges`, `minDuration`
and `maxDuration`. OS, architecture, a nonempty probe map and positive duration
bounds are required. The authenticated API validates this declaration and stores
`status.effectiveCapabilities`. This is a declaration, not remote attestation.
The Worker discovers capabilities independently of custom labels. Registered
probers alone do not prove that external tools exist. The native profile does
not advertise browser execution. Browser discovery requires installed runtime,
packages and an executable browser. ICMP discovery checks local socket access. ICMP channels require `raw-sockets`
because the accepted probe class includes `dontFragment` variants.

Admission requires every Worker to cover every job kind and the whole job
interval. Worker duration requirements can strengthen coverage but cannot
weaken it. For example, Worker minimum duration cannot exceed the job minimum.
An empty Worker policy still enforces channel coverage. Operators use separate
Runners for distinct capability pools. Policy edits do not rewrite declarations.
Registration refreshes effective capabilities and preserves pause and identity.

## Claims and concurrency

New claims use the immutable Result execution snapshot, the current Runner
policy and labels, and stored effective Worker capabilities. They never use the
current Scenario or claim-body capabilities. The server grants the exact required
job duration. A shorter Worker request is retryable; it cannot shorten a job.
A job beyond the API maximum is terminal. Lease and capability expiry retain the
existing server limits and reporting grace.

A channel that no longer matches stored placement is terminal with
`runner-placement-changed`. Invalid stored selectors fail closed. Current job
policy refusal is terminal with `runner-job-policy-changed`. These outcomes
acknowledge obsolete dispatches. An incapable individual Worker receives a
retryable refusal: another Worker can still execute the pending dispatch. Exact
refusal details remain server-side. After a valid proof fails capability admission,
`Runner.status.lastAdmissionRejection` records its fingerprint, reason and time.
This operator telemetry changes no policy version and issues no Worker authority.
The CLI and UI show this last denial; they do not retain credential material.

PostgreSQL claims hold a Runner shared row lock and a Worker update row lock until the versioned
Result claim commits. Runner edits and capability refreshes serialize with those
locks. Placement persistence checks the selected Runner version under a shared
row lock, checks the grant, and commits Result and outbox together. Concurrent
policy edits produce a version conflict and no dispatch. Preview observes a
point-in-time view; a later edit can change actual scheduling eligibility.

Lost-response reclaims precede new-admission policy checks. Existing leases and
bounded reporting authority remain subject to existing identity, grant,
pause, revocation and expiry checks. New policy does not cancel an active run.

## Provenance

`status.executor` records Runner UID, name, version and `propagatedLabels` at
placement. Claims add Worker identity without replacing those fields.
`Runner.spec.propagatedLabels` is an explicit operator-owned map. Reserved
`urth/` keys are invalid. The server copies the map onto Result labels and derives
Artifact inheritance from the stored executor snapshot. Propagated labels win
over ordinary uploaded labels. Reserved identity and classification labels are
server-derived. Editing or deleting the Runner cannot change this history.

## Example

```yaml
spec:
  active: true
  jobRequirements:
    probeKinds: [http, tcp]
    minDuration: 1ns
    maxDuration: 1m
  workerRequirements:
    version: '>=0.1.0 <1.0.0'
    operatingSystems: [linux]
  propagatedLabels:
    network: private
```

The exact probe kind identifiers must match the published prober manifests.
