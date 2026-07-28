# 017: Apply Prober Config Defaults on Every Authoring Path

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P1` |
| Workstream | Prober configuration (not part of the NATS review; found while verifying 007) |
| Depends on | — |
| Likely conflicts | 015 |
| Owner | Unclaimed |

## Why This Matters

The `http`, `tcp`, `dns`, `icmp` and `grpc` probers wrap blackbox_exporter config
types, which carry their defaults inside `UnmarshalYAML` rather than in the zero
value. A Scenario stored without those defaults runs with
`IPProtocolFallback: false` and an empty `IPProtocol`, which blackbox resolves as
**ip6-only**. On a host with no IPv6 address that fails to resolve `localhost` —
and even `127.0.0.1` — and never attempts IPv4:

```text
level=ERROR msg="Resolution with IP protocol failed" target=127.0.0.1 ip_protocol=ip6
level=ERROR msg="Error dialing TCP" err="address 127.0.0.1: no suitable address found"
```

The probe reports `failed`. Nothing distinguishes that from the target genuinely
being down, so the platform's core job — telling you whether a service is
reachable — returns a confident wrong answer. This is the single most misleading
failure mode currently reachable from the shipped examples.

## Evidence

Measured on the current `main`, not inferred.

- `examples/scenario.tcp.yaml` applied with `urthctl apply` stores:

  ```sql
  select (prob::json)->'spec'->'tcp' from scenarios where name='tcp-self-fondle';
  -- {"IPProtocol":"","IPProtocolFallback":false, ...}
  ```

- Decoding `tcp.Spec` with `yaml.v3` directly:

  ```text
  target: x\ntcp:                 -> IPProtocolFallback=false
  target: x\ntcp:\n  tls: false   -> IPProtocolFallback=true
  target: x                       -> IPProtocolFallback=false
  ```

- `blackbox_exporter@v0.28.0/config/config.go:62-65` — `DefaultTCPProbe` sets
  `IPProtocolFallback: true`; the same pattern covers HTTP, DNS, ICMP and GRPC.
- `blackbox_exporter@v0.28.0/config/config.go:520-526` — `TCPProbe.UnmarshalYAML`
  assigns the default and then decodes over it. It uses the **yaml.v2**-style
  signature, `UnmarshalYAML(unmarshal func(interface{}) error) error`. There is no
  `UnmarshalJSON`.
- `pkg/prob/types.go:146` — `Manifest.UnmarshalYAML` decodes the spec through a
  `yaml.v3` node.
- `pkg/prob/types.go:109` — `Manifest.UnmarshalJSON` applies no defaults.
- `pkg/probers/{http,tcp,dns,icmp,grpc}/prob.go:23-26` — each `Spec` embeds the
  blackbox config type under its own key.
- `pkg/urth/types.go:88` — `ScenarioSpec.Prob` is persisted with
  `gorm:"serializer:json"`, so every load after the first is a JSON decode.
- `website/src/utils/probSpec.js:68-90` — the UI seeds `IPProtocolFallback` into
  its templates as an acknowledged stopgap.

## Diagnosis

Defaults are applied **only by a YAML decode, and only when the prober's config
sub-block is present and non-empty.** Three holes, in order of reach:

1. **No JSON path applies them at all.** The blackbox types implement only
   `UnmarshalYAML`, and the Web UI authors scenarios as JSON. This is the case
   the existing `TODO.md` note described, and it is correct as far as it goes.
2. **A null YAML node skips the unmarshaler.** `yaml.v3` *does* honour blackbox's
   obsolete-style unmarshaler — but only when there is a value node to decode
   into. `tcp:` with every option commented out is a null node, and the field is
   left zero without the unmarshaler ever running. **Every prober example in this
   repository is written that way**, so the YAML path fails silently too.
3. **An absent sub-block is never defaulted either**, with the same result.

Two corrections to earlier notes, both of which would misdirect the fix:

- `TODO.md` said the YAML path applies defaults and only the UI's JSON path loses
  them. It does not; see hole 2.
- An earlier revision of that note said a defaults pass running at parse time
  "does not survive the storage round trip". **It does.** A spec carrying
  `IPProtocolFallback: true` survives JSON marshal/unmarshal unchanged — verified
  — so defaulting once, before persistence, is sufficient. Storage is not the
  culprit and should not be redesigned for this.

## Required Outcome

- The API server applies each prober's config defaults when a Scenario is created
  or updated, on every authoring path — `urthctl`, the Web UI, and any direct API
  client — and before the Scenario is persisted.
- Defaults are owned by the prober, not by the caller. The API server links the
  prober packages already; a kind it was not built with must still be storable,
  and is simply left as supplied.
- Absent, null and partially specified config sub-blocks all end up with the same
  defaults as a fully specified one.
- `website/src/utils/probSpec.js` stops seeding `IPProtocolFallback`; the client
  no longer carries a copy of server knowledge.
- A decision is recorded for Scenarios already stored with zero values: defaulted
  on next update, migrated, or left alone. Note that runs already scheduled hold
  the config immutably in their execution snapshot (task 007) and are not
  repaired by any of these.

## Implementation Constraints

- The defaulting hook belongs in the prob registry beside `RunFunc` and
  `ContentType` (`pkg/prob/register.go:22-33`) — a `Defaults` function or an
  optional interface on the registered prototype. Reaching into blackbox types
  from `pkg/urth` would put prober knowledge in the domain package.
- Do not route JSON through a YAML decode to borrow the unmarshaler. It would
  work and it would be a trap: silent, unobvious, and broken again the moment a
  prober's config is not a blackbox type.
- Defaults must be applied to a null or absent sub-block, which is what
  `UnmarshalYAML` alone cannot do.
- Do not vendor or fork blackbox's defaults into Urth. `DefaultTCPProbe` and
  friends are exported; use them.
- The examples are part of the bug's reach: a fix that leaves
  `examples/scenario.tcp.yaml` failing has not finished.

## Suggested Implementation Sequence

1. Add a failing test: apply each example manifest, store it, reload it, assert
   `IPProtocolFallback` is true.
2. Add the defaulting hook to the prob registry and implement it for the five
   blackbox-backed probers.
3. Call it from Scenario create/update in `pkg/urth/service.go`, before validation
   and persistence.
4. Remove the UI template seeding and its explanatory comment.
5. Decide and record the behaviour for existing stored scenarios.
6. Run a real probe against `localhost` and `127.0.0.1` end to end.

## Non-Goals

- Changing blackbox_exporter upstream, or adding `UnmarshalJSON` to its types.
- The execution snapshot (task 007). It copies the stored prob faithfully,
  defaults or not; it neither causes nor fixes this.
- A general server-side defaulting/admission framework for all resources. This is
  about prob specs.

## Acceptance Criteria / Definition of Done

- [ ] A scenario authored in YAML with an empty or absent prober sub-block is
      stored with the prober's defaults.
- [ ] A scenario authored as JSON by the Web UI is stored with the same defaults.
- [ ] Every shipped example under `examples/` stores defaulted config.
- [ ] Defaults survive the storage round trip and a reload.
- [ ] The UI no longer seeds `IPProtocolFallback`.
- [ ] A tcp probe of `127.0.0.1` and of `localhost` succeeds on an IPv4-only host.
- [ ] A prob kind unknown to the server is still storable, unchanged.

## Required Tests

- Table over the five blackbox-backed kinds: absent, null and partial config
  sub-block each end up defaulted identically.
- Create a Scenario through the service from a JSON manifest; reload it from the
  store; assert defaults.
- Apply every manifest under `examples/`; assert none stores
  `IPProtocolFallback: false`.
- An unregistered prob kind round-trips untouched.

## Validation

```sh
go test -race -count=1 ./pkg/prob ./pkg/probers/... ./pkg/urth
make audit/postgres
(cd website && npm test)
git diff --check
```

Then run it: `make run-api-server`, apply `examples/scenario.tcp.yaml`, trigger a
run, and confirm the worker log shows a successful probe rather than
`Resolution with IP protocol failed ... ip_protocol=ip6`.

## Completion Record

- **Implemented:**
- **Tests added/updated:**
- **Documentation updated:**
- **Validation evidence:**
- **Follow-ups:**
