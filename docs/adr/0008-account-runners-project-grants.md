# ADR 0008: Runners belong to accounts and are granted to projects

- **Status:** Accepted (2026-09-28)
- **Date:** 2026-09-28
- **Depends on:** wyrd ADR 0001 (portfolio API conventions), wyrd ADR 0002 (shared identity
  module)
- **Amends:** [ADR 0007](./0007-runner-queue-addressing.md) §6, before it is implemented
- **Fulfils:** [ADR 0002](./0002-worker-authentication.md) §1, the enrolment credential
  it prefers

## Context

Urth is adopting the portfolio tenancy model. Users belong to accounts, accounts own
projects, and projects own scenarios and their runs. Two things in Urth do not fit that
hierarchy on their own:

- **A Runner is infrastructure, not a project artefact.** It stands for a network segment —
  a VPC, a factory floor, a branch office — and its operator is whoever controls that
  segment. Several teams' projects may need to probe from the same segment. Putting a Runner
  inside one project either duplicates it per project, meaning N queues and N worker fleets
  in one segment, or forces unrelated teams to share a project.
- **Today anything may run anywhere.** Placement matches a Scenario's label requirements
  against every active Runner. In a multi-tenant installation, a scenario author could then
  aim a probe into another team's network just by writing the right label selector.
  Selectors are a scheduling hint, not an authority.

Exp-Bench has the same shape for its agents and solves it the way this ADR does: an
account-owned machine identity, plus an explicit per-project grant.

Two existing decisions constrain the answer:

- **ADR 0002** keeps entitlement in Postgres, checked at claim, never inferred from queue
  possession. It prefers an opaque enrolment credential stored as a verifier, so that it can
  be revoked. The current 23-hour signed enrolment JWT falls short of that. It is also issued
  by an unauthenticated endpoint (`GET /api/v1/auth/runners/:id`).
- **ADR 0007** moves queues from `urth.v1.jobs.<runner-uid>` to name-addressed
  `urth.v2.jobs.<encoded-name>`, relying on Runner names being unique. Once names are unique
  only per account, two accounts may each have `edge-eu`, so a name-only subject would merge
  their queues. ADR 0007 is accepted but not yet implemented, so it can be amended at no
  cost.

## Decision

### 1. A Runner is account-scoped

`Runner` and `WorkerInstance` are account-scoped kinds. Their names are unique within an
account. Only account `owner`s and `admin`s may create, edit, disable or delete a Runner, or
issue its credentials.

### 2. `RunnerAuthorization` grants a Runner to a project

`RunnerAuthorization` is a new project-scoped kind, stored as the shared module's
`MachineGrant` with the Urth role `runner`:

```yaml
apiVersion: urth.sre-norns.com/v1
kind: runner-authorizations
metadata: {name: edge-eu, project: 91be…}
spec:
  runnerRef: 5d2e…   # Runner UID
  roles: [runner]
```

- **Who may manage grants.** A grant is created or revoked by someone who is both an account
  admin (they own the Runner) and a member of the project (they own the work).
- **Same account only.** A Runner and a project must belong to the same account. A grant
  across accounts is refused.

### 3. Placement considers only granted Runners

The eligible set for a run is computed in this order:

1. Runners granted to the scenario's project;
2. of those, Runners that are active;
3. of those, Runners whose labels satisfy the scenario's requirements.

Capacity then chooses among them, unchanged: capacity decides which queue, never whether.

If the set is empty the run is terminal and unschedulable, as today:
`urth/result.unschedulable=no-eligible-runner`, with no outbox row. A project with no
granted Runners is the ordinary case for a new project, not an error. The placement
preflight (`GET …/scenarios/:id/placement`) reports *no Runner granted* separately from
*no Runner matches*, because the operator fixes them in different places.

### 4. The grant is checked again at claim

The run's `ExecutionSnapshot` records its account and project, and `ClaimRun` adds one check
to the list in ADR 0002 §2: *the Result's placed Runner is still granted to the Result's
project*.

A grant revoked between dispatch and claim causes the claim to be refused. The run is marked
`errored` with `urth/result.unschedulable=runner-not-authorized`. It is not re-placed:
per ADR 0007 §3, a run is never silently re-executed somewhere its placement did not choose.

A run already executing when its grant is revoked finishes and reports, just as it does when
its Runner is disabled (ADR 0002 §4). Revocation stops new work; it does not reach into a
probe in flight.

### 5. Enrolment uses a revocable machine token

The enrolment credential becomes a `MachineToken` of the Runner's machine identity, from the
shared identity module:

- It is opaque, stored as a verifier, and shown once.
- A Runner may have several tokens, each revocable on its own.
- Only an account admin, authenticated as a user, may issue one.

`POST /auth/workers` exchanges it for a worker session exactly as today.

The **worker session credential** and the **run capability token** are unchanged. Their
narrow, short-lived, signed design is what ADR 0002 intended, and the shared module has no
equivalent.

The unauthenticated `GET /auth/runners/:id` is removed. There is no transition period:
serving it would keep an open door beside the new one.

### 6. Queue subjects carry the account (amends ADR 0007 §6)

```text
urth.v2.jobs.<account-uid>.<encoded-runner-name>
consumer: runner-<account-uid>-<encoded-runner-name>
```

- The account token is a UID, so it contains no `.`.
- The name keeps ADR 0007 §2's encoding: total, injective, a single token, with a
  truncation-plus-digest suffix when too long.
- The stream's subject filter becomes `urth.v2.jobs.*.*`.
- A worker session's NATS permissions are narrowed to its own account's subject for its
  own Runner.

Everything else in ADR 0007 stands: name addressing within an account, UID entitlement,
queue reaping, and the drain-based migration from `v1`. The account is fixed for a Runner's
lifetime, so the subject cannot move under a live Runner any more than the name can.

Log and presence subjects stay keyed by Runner UID. UIDs are already globally unique.

### 7. Existing installations migrate into a default tenant

> **Acceptance note (2026-09-28):** there are no Urth deployments to migrate, and Postgres 18
> is the only supported version. This section, and ADR 0007 §6's drain from the `v1` subject
> layout, are therefore not implemented: the account-qualified `v2` layout replaces `v1`
> outright, and a fresh installation starts with no tenant. The section is kept to record
> what an upgrade would have required.

On upgrade, a one-shot, idempotent migration does the following:

- creates an account and a project, both named `default`;
- assigns every existing Runner and WorkerInstance to the account, and every Scenario,
  Result, Artifact and dispatch failure to the project;
- grants every existing Runner to the `default` project;
- creates a bootstrap owner from a configured email address.

Behaviour is then identical to today for existing users. Previously issued enrolment tokens
stop working, because the endpoint that minted them is gone, and workers are re-enrolled
with machine tokens. That is the one operator-visible step of the upgrade.

## Architectural rules

- Selectors choose among authorised Runners. They never widen the set.
- Entitlement is checked twice: at placement from the grant, and at claim from the grant as
  it stands then. Neither check substitutes for the other.
- A Runner and every project it is granted to share one account.
- No subject is built by concatenation outside `pkg/natsq`. The account and name both pass
  through its encoder.
- Revoking a grant never re-places a run.

## Consequences

### Benefits

- One Runner per network segment serves every project that needs it, with no duplicated
  fleets.
- A scenario author cannot reach into a network their project has not been granted, however
  they write their selectors.
- Enrolment stops being an open endpoint and becomes revocable per token, as ADR 0002 asked.
- Runner names become tenant-local, which is what an operator expects.

### Costs

- Placement gains a join against grants on the run-creation path. `idx_results_placement`
  already covers the capacity query; grants need an index on `(project, runner)`.
- Workers must be re-enrolled on upgrade.
- ADR 0007's migration and this one both change queue addressing, and they must land as one
  step. Shipping `v2` without the account and then adding it would mean a second drain.
- Granting is a new administrative step. A new project runs nothing until someone grants it a
  Runner. The UI and `urthctl` need to make that step obvious.

## M4 implementation (2026-09-29)

The backend implements this policy under `/v1`, with `identity/v0.2.1` and
account-qualified NATS v2 queues. See [the implementation and operator notes](../m4-backend-tenancy.md).

Dispatch failures with an authorized parent run are project-owned. A malformed
message or a delivery to the wrong runner is instead an account-level diagnostic
visible only to account administrators. It cannot expose or transition another
runner's result. This preserves diagnostics without inventing a project for an
unassignable message.
