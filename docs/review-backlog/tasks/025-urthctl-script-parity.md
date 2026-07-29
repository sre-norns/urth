# 025: Make `urthctl get script` Work

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P2` |
| Workstream | Operability |
| Depends on | — |
| Likely conflicts | 017 |
| Owner | Unclaimed |

## Why This Matters

`urthctl get script <name>` is declared, documented in `--help` as "Get a script
data for a given scenario", and cannot run:

```
$ go run ./cmd/urthctl get script tcp-self-fondle
urthctl: error: no Run() method found in hierarchy of get script <name>
```

The kong command struct exists with no `Run` method and no client call behind it.
`CONTEXT.md` makes the Web UI and `urthctl` co-equal operator surfaces — "anything
an operator can see or do in one, they can see or do in the other" — and this is
the CLI advertising a capability it does not have. A command that parses its
arguments and then fails on its own wiring is worse than an absent one: it reads
as a broken server.

The server side works. `GET /scenarios/:id/script` returns the script for a
script-bearing prob kind and 404 for one without — but only since the reflection
fix, and it had been answering 404 for *every* scenario since the api-server began
linking probers in, because it asserted the typed prob spec to `map[string]any`.
Nobody noticed, which is itself the evidence that nothing calls it.

## Evidence

- `cmd/urthctl/get.go:29-31`: the `Script` struct, arg only, no `Run` method.
- `cmd/urthctl/get.go:62`: registered as a subcommand.
- `pkg/urth/client.go`: `scenariosAPIClient` has `UpdateScript` and no getter, so
  there is no client method to call.
- `cmd/api-server/main.go`, `probScript`: the server side, fixed 2026-07-29 and
  now covered by `cmd/api-server/script_test.go`.
- `website/src/`: no reference to the endpoint — the UI does not use it either.

## Required Outcome

- `urthctl get script <scenario>` writes the scenario's script to stdout, and
  nothing else, so it can be redirected to a file or piped to an editor.
- A scenario whose prob kind carries no script exits non-zero with a message
  naming the kind, rather than printing an empty file.
- A client method on `scenariosAPIClient` fetches it, so the CLI is not building
  URLs by hand.
- Decide whether the Web UI should show a script for script-bearing kinds. If not,
  say so here — the parity rule makes silence look like an oversight.

## Implementation Constraints

- The endpoint returns `text/plain`, not a manifest. Do not route it through the
  resource formatters (`formatter` in `cmd/urthctl/get.go`), which would wrap it
  in a table or JSON envelope.
- Scripts may be large and may carry credentials in a prob spec. Stream to stdout;
  do not log the body.

## Non-Goals

- `PUT /scenarios/:id/script`. A commented-out handler for it was deleted on
  2026-07-29; authoring belongs to `urthctl apply` and a manifest file, per the
  CLI's stated exception to operator-surface parity.
- Applying prober config defaults — [task 017](017-apply-prober-config-defaults.md).

## Acceptance Criteria / Definition of Done

- [ ] `urthctl get script` runs and prints the script.
- [ ] A kind with no script exits non-zero and says which kind it was.
- [ ] The fetch goes through a client method, tested against a stub server.
- [ ] The UI decision is recorded here either way.

## Required Tests

- The client method against an `httptest` server returning `text/plain`.
- A 404 from the server becomes a non-zero exit and a useful message.

## Validation

```sh
go test -race -count=1 ./cmd/urthctl/... ./pkg/urth/...
go run ./cmd/urthctl get script puppeteer-self-prober
```

## Completion Record

- **Implemented:**
- **Tests added/updated:**
- **Documentation updated:**
- **Validation evidence:**
- **Follow-ups:**
