# 019: Serve the Live Run Log Stream Instead of Refusing It

Shared context: [`CONTEXT.md`](../CONTEXT.md).

| Field | Value |
|---|---|
| Status | `ready` |
| Priority | `P1` |
| Workstream | Operability |
| Depends on | — |
| Likely conflicts | — |
| Owner | Unclaimed |

## Why This Matters

Live run logs do not work in a browser, and have not since they were added. The
run page opens an `EventSource`, the browser sends `Accept: text/event-stream`,
and the request is refused with **406 Not Acceptable** before the handler runs.
Every run detail page therefore shows "Log stream disconnected" — for a running
probe and for a finished one alike, since the same URL serves the stored log.

Nothing surfaces this. `LiveRunLog` treats a closed stream as the normal end of a
run (a finished run does close it), so the panel degrades quietly to empty and
the failure appears only as a console error. The feature reads as "logs are not
being produced", which sends an operator looking at the worker.

## Evidence

- `cmd/api-server/main.go`: the route is registered on
  `router.Group("/api/v1", bark.ContentTypeAPI())`, so every request under
  `/api/v1` passes the content-type middleware.
- `wyrd/pkg/bark/http.go`, `replyWithAcceptedType`: accepts only `*/*`, JSON,
  YAML and XML, and aborts with 406 for anything else — including
  `text/event-stream`.
- Reproduction:

  ```sh
  curl -s -o /dev/null -w '%{http_code}\n' -H 'Accept: text/event-stream' \
    localhost:8080/api/v1/scenarios/<scenario>/results/<run>/logs   # 406
  ```

- Observed in the browser while verifying
  [task 018](018-fail-unplaceable-runs.md): the run page logs
  `Failed to load resource: the server responded with a status of 406`.

## Required Outcome

`GET /scenarios/{id}/results/{runId}/logs` serves the stream to a client that
asks for `text/event-stream`, both while a run is live and after it has finished
(where it replays the stored log artifact). No other route's content negotiation
changes.

## Implementation Constraints

- `runLogHandler` never calls `bark.MarshalResponse`: it writes its own SSE
  headers (`writeSSEHeaders`) and uses only `bark.AbortWithError`, which does not
  need the response marshaller the middleware installs. That is what makes the
  local fix safe.
- Preferred fix: register this one route on a sibling group without
  `bark.ContentTypeAPI()` — `router.Group("/api/v1")` — keeping the change in
  this repo and the middleware intact for every resource route.
- The alternative, teaching `bark.replyWithAcceptedType` about
  `text/event-stream`, is a change to a shared library for one endpoint's sake,
  and would have to decide what "marshal a response as SSE" means for every
  other handler. Prefer the routing fix unless a second streaming endpoint
  appears.
- Whichever is chosen, the endpoint must keep answering `*/*` clients (the
  `curl` path) as it does today.

## Non-Goals

- Changing the log transport, retention, or the artifact fallback.
- A generic streaming API for other resources.

## Acceptance Criteria / Definition of Done

- [ ] An `Accept: text/event-stream` request to the logs route returns 200 and
      streams, rather than 406.
- [ ] The Web UI shows live log lines for a running probe and the stored log for
      a finished one.
- [ ] Other `/api/v1` routes still refuse unsupported accept types.
- [ ] A regression test covers the header, so the route cannot be moved back
      under the middleware unnoticed.

## Required Tests

- `cmd/api-server`: a request to the logs route with
  `Accept: text/event-stream` is served; the same header on a resource route is
  still refused with 406.
- `website`: `LiveRunLog.test.jsx` already covers the client side; extend it only
  if the fix changes what the component sends.

## Validation

```sh
make audit
curl -s -o /dev/null -w '%{http_code}\n' -H 'Accept: text/event-stream' \
  localhost:8080/api/v1/scenarios/<scenario>/results/<run>/logs
```

Plus a browser: start a run and watch lines arrive, then reload the finished run
and see the stored log.

## Completion Record

- **Implemented:**
- **Tests added/updated:**
- **Documentation updated:**
- **Validation evidence:**
- **Follow-ups:**
