# Current API examples

Use a signed-in account and project. The CLI stores credentials in its profile;
all resource examples use `apiVersion: urth.sre-norns.com/v1`. Shared identity
resources use `identity.sre-norns.com/v1`. Old flat documents and `v1` aliases
are unsupported.

```sh
urthctl auth login --api-server-address=http://localhost:8080
urthctl projects create quickstart --use
urthctl apply examples/runner.yaml
urthctl apply examples/scenario.http.yaml
# Authorize the runner for this project before triggering; see the main README.
urthctl trigger bb-http-self-prob
urthctl get scenarios -o yaml
```

Edit a read document and apply it back to retain its version precondition:

```sh
urthctl get scenario bb-http-self-prob -o yaml > /tmp/scenario.yaml
# Edit /tmp/scenario.yaml, then:
urthctl apply /tmp/scenario.yaml
```

For direct HTTP, set `API`, `PROJECT_ID`, `SCENARIO` and `ACCESS_TOKEN` to the API,
project UID, scenario name and your user bearer credential. The run request
fixture is a create input, not a worker completion report:

```sh
curl --fail-with-body -X POST "$API/v1/projects/$PROJECT_ID/scenarios/$SCENARIO/results" \
  -H "Authorization: Bearer $ACCESS_TOKEN" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: $(uuidgen)" --data-binary @examples/scenario.run.started.json
```

Shared identity creates use typed envelopes. For example, the project API takes:

```json
{
  "apiVersion": "identity.sre-norns.com/v1",
  "kind": "projects",
  "metadata": {"name": "edge-monitoring"},
  "spec": {"description": "Private network checks"}
}
```

Send it to `POST /v1/accounts/:account/projects` with an idempotency key.
Configuration PATCHes send only writable `metadata`/`spec`; lifecycle changes
send a separate command such as `{"operation":"deactivate"}`. Both edits carry
the ETag read as `If-Match`. For enrolment, `urthctl runners token NAME` uses
`POST /v1/agent-identities/:runnerUID/tokens` and prints its one-time secret.

Local probes need no account or server:

```sh
urthctl run -f examples/scenario.rest.httpbin.yml
```

The REST self-check examples call only the public `/v1/version` endpoint and
contain no credentials. Worker registration, run claims, status reports and
artifact upload are performed by `nats-worker`, using its separate capabilities.
