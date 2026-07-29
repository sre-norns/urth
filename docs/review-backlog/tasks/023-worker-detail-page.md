# 023: Give a Worker Its Own Page

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `done` |
| Priority | `P2` |
| Workstream | Operability |
| Depends on | — |
| Likely conflicts | — |
| Owner | Codex (`feat/capacity-aware-placement`) |

## Why This Matters

A WorkerInstance is the only resource in the system with no page of its own. It
is listed inside its runner and nowhere else, so everything known about one
worker has to fit in a table row shared with every sibling.

Worker liveness made that constraint bite. The server now records two
independent signals — API contact and NATS presence — and derives a condition
from them, precisely so that a half-connected worker is diagnosable. The row can
show the verdict (`no API contact`) and one line of explanation; it has no room
for the evidence behind it: which signal was last heard and when, which contact
proved it, whether the worker announced a shutdown, what it advertised at
registration, and which runs it has actually executed.

That evidence is what an operator needs when a worker is *not* simply healthy,
which is exactly when they open the page.

## Evidence

- `pkg/urth/presence.go`: `WorkerPresenceReport` carries both signals and the
  combined condition; `WorkerInstanceStatus` carries `LastSeenTime`,
  `LastSeenVia`, `NATSLastSeenTime` and `LeftAt`. Only `condition` and a single
  "last seen" line reach the UI today.
- `website/src/components/WorkerList.jsx`: one row per worker, already carrying
  identity, platform, presence, an explanation and two actions.
- `website/src/utils/presence.js`: the rendering vocabulary a detail page would
  reuse unchanged.
- `cmd/api-server/main.go`: `GET /api/v1/workers/:id` already serves a single
  worker with its presence computed. No new endpoint is required.
- A worker's runs are already queryable: `?labels=urth/worker.name = <name>` on
  the results API, per `pkg/urth/labels.go`.

## Required Outcome

A route at `/workers/:name` showing, for one worker:

1. Identity and membership — name, hostname, UID, its runner (linked),
   registration time, and the labels it advertised, including its
   `urth/capability.prob.*` set.
2. **The two signals broken out**, each with its own timestamp and freshness:
   API contact (and whether a heartbeat or a claim proved it), NATS presence,
   and any recorded departure. The combined condition is shown as a verdict
   *derived from* those, not instead of them.
3. Its recent runs, by label query, so "is this worker actually doing anything"
   is answerable without leaving the page.
4. Pause and resume. Drop remains on the Runner's worker list, where the
   registration is seen in the context of its sibling processes.

Reached from a worker's name in `WorkerList`.

## Implementation Constraints

- Read-only with respect to liveness: the page must not compute its own view of
  online/offline from the timestamps. The server owns the timeouts and publishes
  `status.presence`; a second definition in the UI would drift from it. Reuse
  `website/src/utils/presence.js`.
- Poll while mounted, as `RunnerDetail` does, so the page reflects a worker that
  stops while it is open. Reuse the same interval constant rather than choosing
  a second one.
- No new API surface. If something needed is genuinely missing from
  `GET /workers/:id`, add it to that response rather than adding an endpoint.
- JSX belongs in `.jsx` files — vite's oxc transform will not parse it from
  `.js`.

## Acceptance

- [x] `/workers/:name` renders identity, both signals with timestamps, and runs.
- [x] A worker with one broken path shows *which* path, with the timestamp that
      justifies it.
- [x] A worker that never reported reads as unknown, not offline.
- [x] The page updates without a reload when a worker stops.
- [x] Worker names in `WorkerList` link to it.
- [x] `npm test` covers the three presence shapes: online, offline, split.

## Completion Record

- **Implemented:** `/workers/:name`, dedicated WorkerInstance and recent-run
  fetch state, shared Runner/Worker polling cadence, linked worker names and
  Runner backlink, worker links from run details with a polled current-presence
  indicator guarded by the historical executor UID, server-derived presence
  diagnostics, registration metadata, all-time run count plus the latest ten
  runs with the established alternating-row treatment, and Pause/Resume.
  Workers now advertise `urth/worker.hostname`; older registrations render it
  as unknown.
- **Tests added/updated:** `pkg/runner/config_test.go`,
  `website/src/actions/fetchWorkerResults.test.js`,
  `website/src/pages/WorkerDetail.test.jsx`,
  `website/src/pages/RunnerDetail.test.jsx`, and
  `website/src/pages/RunDetail.test.jsx`.
- **Documentation updated:** this task records the agreed page-only action
  boundary: Drop stays on the Runner list and is not exposed on the detail page.
- **Validation evidence:** `go test -race -count=1 ./...` passed; `go vet ./...`
  passed; `npm test` passed 16 files / 204 tests; Prettier passed for every
  changed website file; `git diff --check` passed.
- **Follow-ups:** Worker registrations created before the hostname label was
  introduced show `unknown` until the process registers again.
