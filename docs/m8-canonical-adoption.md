# M8: Urth adoption of canonical identity

Urth pins wyrd `v0.7.0`, identity `v0.7.0` and components `0.5.0`. Backend, CLI,
worker and website ship together. No legacy resource reader/writer or data
migration is provided. Tests use new PostgreSQL schemas/databases.

## Boundary inventory

| Surface | Owner / representation | Preconditions and credentials |
| --- | --- | --- |
| Tenant identity resources, profile and sign-in methods | `httpapi.Mount`; `identity.sre-norns.com/v1` envelopes with typed spec/status | Shared identity mutation transactions, POST replay keys, read ETags as If-Match, safe canonical problem paths |
| Account archive and impact preview | Shared tenant routes returning typed preview/system-account projections | Existing explicit preview/confirmation commands; target ETag |
| System administration | No bespoke system route mount in Urth | No duplicate system adapters to convert |
| Runner registration | Product account-scoped Runner envelope, paired identity created in the same transaction | Account administrator; identity UID equals runner UID; metadata/spec sent by the UI adapter |
| Runner enrolment token | Shared `POST /v1/agent-identities/:uid/tokens`; typed `agent-identity-tokens` input and `{resource, token}` result | Idempotency key; secret emitted once, replay redacted; ordinary reads never expose it |
| CLI `runners token NAME` | Resolves product runner name, then calls the published identity SDK | Stable key and token name across a caller's retry; reports an unavailable secret instead of creating again |
| Product runner authorizations | Existing `urth.sre-norns.com/v1` projection of shared grants | Account and project authority; version-guarded writes/deletes |
| Monitoring resources | Existing typed Urth runner/worker/scenario/result/artifact/dead-letter manifests | Canonical Urth group required on HTTP writes and validated by UI readers; existing scoped visibility and conditional edits |
| Worker registration, heartbeat, claim and completion | Explicit worker/capability protocol DTOs; nested resources retain their typed manifests | Separate worker and per-run credentials; worker's submitted manifests use the canonical group |
| Principal, OAuth, directory candidates, placement, version and catalogues | Declared protocol/query DTOs | No fabricated resource identity or version |
| Logs and binary artifacts | Authenticated streaming/content endpoints | Streaming fetch sends bearer credentials; SSE framing and artifact bytes stay unwrapped |

The old product `POST /v1/accounts/:account/runners/:name/tokens` plain-text route
is retired. The shared token endpoint supplies transaction-bound replay and
redaction rather than maintaining a second credential-issuing HTTP path.

CLI project/member/invitation/session JSON and YAML use the identity codec;
invitation creation uses the explicit result codec to preserve its one-time
manual token. Table views remain operator-oriented. Lifecycle changes send
`operation` alone, with the version read by the CLI. Shared components map
canonical errors and retain read ETags; Urth's custom runner registration adapter
uses the product group and metadata/spec fields.

Product apply/get round trips retain the existing typed spec, ownership and
version guard and drop server-owned status. `apiVersion: v1`, an absent group
and unrelated groups are refused. Current examples and mock responses use the
same groups as the server; examples are decoded by a CLI regression test.

## Validation

Run `make audit/postgres store-url=...` against a disposable database, then the
website unit/build/browser checks. `website/e2e/live.spec.ts` exercises real
login, project creation, paired runner registration, project authorization,
canonical token issuance, a separate worker, successful execution, authenticated
logs/artifacts and route accessibility. CLI integration covers read-YAML-apply,
stale/concurrent edits and canonical identity/token replay. The dependency
verification and run results are recorded in the PR description.
