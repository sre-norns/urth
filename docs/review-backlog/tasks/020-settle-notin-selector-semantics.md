# 020: Settle `notin` Selector Semantics, and Make Both Evaluators Agree

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P1` |
| Workstream | Runner contract |
| Depends on | — |
| Likely conflicts | 014, 018 |
| Owner | Unclaimed |

## Why This Matters

`wyrd` evaluates the same label selector two ways, and the two disagree about
`notin` when the label is **absent**:

- **In Go** (`manifest.Requirement.Matches`): an absent key **matches**. This is
  Kubernetes' rule.
- **In SQL** (`dbstore` → `KeyNotIn` → `json_extract_path_text(...) NOT IN (...)`):
  an absent key yields `NULL`, so the row is **excluded**.

Urth uses both. Scenario → Runner placement is a store query (SQL); Runner → Worker
admission at registration is `reqSelector.Matches(worker.Labels)` (Go). The same
requirement written by the same operator therefore means different things at two
points of the same dispatch path.

It was not academic. `examples/scenario.rest.httpbin.yml` carried
`envX notin (dev,testing)`, which `examples/runner.yaml` could not satisfy under
the SQL rule because the runner has no `envX` label at all — the api-server logged
`no active runner matches requirements "os=linux,envX notin (dev,testing)"
(0 considered)` with an active, otherwise-suitable runner registered. That is what
made [task 018](018-fail-unplaceable-runs.md)'s stuck runs stuck.

> **Evidence refreshed, 2026-07-29.** That example has since been changed to a
> plain `matchLabels: {env: dev}` and the shipped manifests now place correctly —
> verified live: `GET /scenarios/rest-httpbin-probe/placement` returns
> `matchingRunners: 1, schedulable: true`. **The divergence itself is unchanged**,
> confirmed in `wyrd@v0.2.2`: `manifest/selector.go:88-89` returns
> `!labels.Has(key) || ...` for `NotIn`/`NotEquals`, while
> `dbstore/gorm_json.go`'s `KeyNotIn` builds `json_extract_path_text(...) NOT IN
> (...)`, which is NULL — and therefore false — for an absent key. Editing the
> example removed the symptom from the demo path and left the bug in place, which
> is the more dangerous state: the next operator to write `notin` meets it with no
> example to warn them.

The project also claims Kubernetes semantics: `README.md` and `CLAUDE.md` both say
"if you've written a Kubernetes `nodeSelector`, it's that". Under the SQL rule,
it is not.

## The two readings

Kubernetes documents and implements exclusion as the complement of `in`:

> `tier notin (frontend, backend)` selects all resources with key equal to `tier`
> and values other than `frontend` and `backend`, **and all resources with no
> label with the `tier` key**.

`k8s.io/apimachinery/pkg/labels` implements exactly that — `NotIn`/`NotEquals`
return true when the key is absent — so `in` and `notin` partition the set. To
require presence *and* exclusion, k8s makes you say both:
`envX, envX notin (dev,testing)`.

The other reading — "the key exists and its value is not one of these" — is what
the SQL path currently does, and is arguably the more useful default for
placement: a runner that has never heard of `envX` is not obviously a runner that
should take `envX`-sensitive work. It is a defensible choice; it is just not
Kubernetes', and it must not be one of two answers the system gives.

## Required Outcome

1. One documented rule for `notin`/`!=` against an absent key, stated in `wyrd`'s
   selector documentation and reflected in Urth's `README.md` claim about
   Kubernetes semantics.
2. `manifest.Requirement.Matches` and the `dbstore` SQL translation implement
   that rule identically. If the Kubernetes rule is chosen, the SQL becomes
   `NOT IN (...) OR <key path> IS NULL`; if the presence-requiring rule is
   chosen, `Matches` drops its `!labels.Has(r.key) ||` clause.
3. `examples/` are consistent with the rule: under the Kubernetes rule the
   current files work as written; under the other, every example scenario's
   `notin` clause needs the runner to carry the key, and the examples say so.

## Implementation Constraints

- Both evaluators live in `wyrd`, which is a sibling repository developed locally
  at `~/workspace/wyrd`. The Urth-side work is the documentation claim, the
  examples, and a test that pins the behaviour Urth depends on.
- Whichever rule wins, placement (SQL) and worker admission (Go) must agree —
  that is the defect, independently of which reading is preferred.
- Do not "fix" this by adding placeholder labels to the examples and leaving the
  divergence in place.

## Non-Goals

- Any other operator's semantics (`in`, `exists`, `gt`/`lt`).
- Reworking how selectors are pushed into SQL.

## Acceptance Criteria / Definition of Done

- [ ] The rule is documented where an operator writing a selector will find it.
- [ ] Go and SQL evaluation agree for an absent key, proven by a test that runs
      the same selector through both.
- [ ] `examples/runner.yaml` matches `examples/scenario.rest.httpbin.yml`.
- [ ] Urth's README claim about Kubernetes semantics is true, or corrected.

## Required Tests

- `wyrd`: a table test over `notin`/`!=` with the key present-and-matching,
  present-and-not-matching, and absent — asserted through `Matches` *and* through
  a store query, so the two cannot drift again.
- `pkg/urth`: a placement test with a runner that lacks a key the scenario names
  in a `notin`, asserting the agreed outcome.

## Validation

```sh
make audit/postgres
go run ./cmd/urthctl apply ./examples/runner.yaml
go run ./cmd/urthctl apply ./examples/scenario.rest.httpbin.yml
curl -s localhost:8080/api/v1/scenarios/basic-rest-self-prober-http/placement
```

The preview must report the runner as matching and eligible.

## Completion Record

- **Implemented:**
- **Tests added/updated:**
- **Documentation updated:**
- **Validation evidence:**
- **Follow-ups:**
