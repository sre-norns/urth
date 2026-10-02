# M4: Urth backend tenancy

Urth now uses wyrd v0.6.0 and identity v0.2.1. Product resources and the shared
identity API are served under `/v1`. There is no `/api/v1` alias, default tenant,
old-data migration, or NATS v1 job drain: Urth has no deployed installation to
upgrade. PostgreSQL 18 is required.

## Resource access

| Scope | Collections |
|---|---|
| `/v1/accounts/:account` | `runners`, `workers`, unassignable `dispatch-failures` |
| `/v1/projects/:project` | `scenarios`, `results`, `artifacts`, `dispatch-failures`, `runner-authorizations` |

Product requests require an account user session from the shared identity
service. Account resources require account owner/admin authority. Project
resources require current project membership; account administration alone does
not grant access. Product storage enforces these checks on reads, writes and
label catalogues, including UID lookups. The route scope is authoritative and a
conflicting scope in a submitted manifest is rejected.

Edits are optimistic (ADR 0001 §5). A read carries the resource's version as
its `ETag`, and so does the reply to a create or update. A `PUT` of a scenario,
runner or runner authorization must send `If-Match`: the version it was based
on, or `*` for an unconditional write. It gets `428` without one and `400` for
a malformed one. A superseded version gets `412` and changes nothing, and the
write itself is guarded, so of several edits of one version exactly one lands.
A version sent for a resource that does not exist gets `412`; `*` may create it.
Deletes keep their `?version=` guard. Pausing a worker takes none: a worker
rewrites its own spec on each registration, and a pause should not fail
because of that. `urthctl apply` sends the manifest's own `metadata.version`
when it has one, so applying a stale copy from `get` is refused. Without one
it sends `*`.

Resources use the manifest envelope (`apiVersion`, `kind`, `metadata`, `spec`,
`status` where applicable), including Results. Lists return
`{items, limit, next?, total?}`. Pass `cursor=next` to continue. Nonzero offsets
are rejected; `total` is advisory. Names/label catalogues follow the same cursor
contract. `urthctl` collection commands follow all pages. Its API client accepts
`--account`, `--project`, and `--token`; the server address
is the root URL, such as `http://localhost:8080`.

## Runner grants and worker credentials

A Runner and its shared machine identity are created atomically with the same
UID. A project grant is stored as a shared machine grant:

```yaml
apiVersion: urth.sre-norns.com/v1
kind: runner-authorizations
metadata:
  name: edge-eu
  account: <account-uid>
  project: <project-uid>
spec:
  runnerRef: <runner-uid>
  roles: [runner]
```

Both account administration and project membership are required to grant or
revoke. This restriction also covers mutations through shared identity routes.
Placement first selects runners granted to the project, then applies runner
state and label requirements. Capacity chooses among eligible queues; it does
not refuse an otherwise eligible run.

The frozen execution snapshot records account/project ownership. A claim locks
and rechecks the grant, machine identity, account and project lifecycle in the
same transaction as the version-guarded claim. Revocation before claim produces
`runner-not-authorized`; already-claimed runs can recover their authorization,
finish and upload artifacts after grant revocation.

`POST /v1/agent-identities/:runnerUID/tokens` issues a revocable opaque
machine token as `{resource, token}`. Send a canonical `agent-identity-tokens`
create input and an `Idempotency-Key`. The secret is returned once and redacted
on replay; ordinary reads never expose it. Both name-based plain-text token
routes are retired. `urthctl runners token NAME` resolves the paired identity
UID and uses the shared operation. Registration exchanges that token for a worker session and restricted
NATS credentials. Worker sessions and per-run capabilities remain separate
credentials; neither authenticates product user routes.

Malformed or misrouted messages without an authorized parent run are account
administration diagnostics. A worker cannot strand another runner's run or
learn its scenario through such a report. Failures for an authorized run belong
to its project. Account diagnostic routes never list project-owned failures.

## Broker configuration

Jobs use `urth.v2.jobs.<accountUID>.<encodedRunnerName>` and durable consumers
`runner-<accountUID>-<encodedRunnerName>`. Dots become underscores. Names longer
than 128 encoded bytes use a 63-byte prefix, `~`, and a SHA-256 digest. The marker
cannot occur in a short encoded name. Logs/presence retain UID addressing.

Configure an operator/JWT-mode NATS server with a system account and a
JetStream-enabled application account. The API server's `--nats.creds-file`
must authenticate an administrative user of that application account. Supply
its account signing seed using `--nats.worker-account-seed-file`. Keep this seed
private; it signs one-hour worker user JWTs. Workers receive only permission to
pull and acknowledge their named consumer, use their runner inbox, and publish
their runner's logs/presence. They cannot create consumers, publish jobs, or
subscribe to another runner's messages. Registration renews their credential;
reconnection reads the latest credential.

Startup requires the signing seed unless `--nats.allow-insecure-workers` is
explicitly enabled. `make run-api-server` enables that option for its isolated
local broker. Broker metrics' `runner` label is now the account-qualified
encoded runner name, rather than a resource UID.

## Composition and follow-on milestones

The relay, reconciler, presence and advisory handlers are internal control-plane
operations with fleet visibility. They use dedicated stores; external product
requests use identity visibility. Worker capability handlers explicitly verify
their narrower authority before acting through internal storage.

M4 mounts the shared identity routes and supplies Urth OAuth client defaults.
Embedded hosts can override `apiserver.Config.Identity` and provision users via
`Server.Identity.ProvisionUser`. M5 added the operator's side: identity, mail and
bootstrap flags (see the api-server README's identity section), Urth-branded
sign-in pages, and the web app's sign-in and identity screens in
`website-experiment`. CLI device login and profiles remain M6; the monitoring
pages move onto the scoped APIs in M7. Queue reaping and inherited-message UI remain the existing ADR 0007
control-loop/operator-visibility follow-up, separate from M4 queue addressing.

## Validation

`make audit/postgres store-url=<disposable PostgreSQL 18 URL>` runs module
verification, vet, staticcheck and all tests with the race detector. Integration
tests use private PostgreSQL schemas and real embedded NATS brokers. The M4
cases cover project grants, revocation before/after claim, scoped reads and
mutations, credential separation/revocation, cursor traversal, same-name runners
across accounts, and actual JWT-enforced broker denials.
