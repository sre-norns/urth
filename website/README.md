# Urth website

The supported web UI is a Vite 8 / React / TypeScript 6 application using
`@sre-norns/components` **0.5.0** and its Urth theme and identity client. The old
webpack UI and the `website-experiment` directory are retired.

## Development

Use Node 24. Configure `~/.npmrc` for GitHub Packages with a `read:packages` token
(do not put credentials in this directory), then run:

```sh
npm ci
npm run dev
```

The default browser origin is `http://localhost:3001`. Start the API using
`make run-api-server` from the repository root. The Vite server proxies `/v1`,
the authorization server's OAuth endpoints and OAuth discovery to the API;
`/oauth/callback` belongs to the SPA. Override the backend with `URTH_API_URL`.
When changing the browser port, also set the API's `URTH_ISSUER` and
`URTH_WEB_REDIRECT_URI` to the new browser origin and its `/oauth/callback`.
Check `ss -ltnp` before choosing ports on a shared machine.

Account infrastructure lives at `/a/:accountId`; project monitoring lives at
`/a/:accountId/p/:projectId`. Project membership is required for monitoring,
even for account administrators. Runner identity URLs use IDs; product runner
and run detail endpoints use names. Resource edits retain the ETag read when
opening the form. A stale edit preserves the draft and requires explicit review
of the latest document before retrying. Worker pause remains unversioned.

Scenario forms accept probe configuration, labels and placement requirements as
YAML. Probe configuration supports the complete server schema, including script
probes. Run now performs a fresh placement check. Lists follow opaque cursors;
totals are informational. Account dead letters show unassignable diagnostics;
project dead letters can retry the original execution snapshot.

Live logs use authenticated streaming fetch, with cancellation on scope/session
changes. The display retains up to 5,000 lines and retries transient failures
three times. Live NATS messages are not replayed after reconnects; the UI reports
that gap, and replaces the tail with the stored log when the run completes.
Sensitive and unknown artifact classes require an explicit reveal; HTML and SVG
are displayed as text, never executed. Text previews are limited to 1 MB.

## Checks

```sh
npm test
npm run build
npx playwright install chromium
npm run test:e2e
```

The ordinary browser suite uses mocked API responses on desktop and mobile and
includes axe checks. `URTH_E2E_PORT` overrides its dedicated default port 13017.
CI installs Chromium and runs these checks with Node 24. The GitHub package must
grant this repository's Actions token read access; CI uses `GITHUB_TOKEN`.

The live test needs a **disposable** PostgreSQL database, NATS with JetStream,
an API with the development bootstrap user and a running website. It creates
projects, runners, grants, scenarios and executions. Build a worker first:

```sh
# From the repository root:
go build -o /tmp/m7-nats-worker ./cmd/nats-worker
# From website/, against an isolated stack (adjust ports to yours):
URTH_LIVE_E2E=1 \
URTH_E2E_BASE_URL=http://localhost:13007 \
URTH_E2E_API_URL=http://127.0.0.1:18087 \
URTH_E2E_NATS_URL=nats://127.0.0.1:14227 \
URTH_E2E_PROBE_URL=http://127.0.0.1:18087/v1/version \
URTH_E2E_WORKER=/tmp/m7-nats-worker \
npm run test:e2e -- e2e/live.spec.ts --project=desktop
```

Optional `URTH_E2E_EMAIL` and `URTH_E2E_PASSWORD` replace the bootstrap credentials.
The test starts its own worker, keeps its one-time credential and installation
key in a private temporary directory, and stops that exact process in cleanup.
The test enables the two loopback development flags. Use this test only against
an isolated local development stack. It verifies login,
project creation, runner authorization, a successful probe, authenticated logs,
artifacts and axe checks across the main identity/infrastructure/monitoring
routes. It also verifies token read/replay redaction, unauthorized token issuance,
blocked-Worker next-claim refusal, stale block writes, unblock, grant removal
and revoked-token enrollment refusal. Set `URTH_E2E_SLOW_PROBE_PORT` to a checked
free loopback port to hold the first HTTP probe until the page shows a running
execution and connected live-log stream. The test owns and closes that listener.
Without this option, completion logs alone do not prove a live connection.

The separate identity test verifies fresh email registration, development mail,
one-time verification links, password reset and session revocation, fake-provider
registration/sign-in, and real CLI device approval and project operations.
Start the fake identity provider and configure the API as described in the
[API guide](../cmd/api-server/README.md#identity-issuer-sign-in-mail-and-the-first-user).
Build `urthctl`, then run against the same disposable stack:

```sh
URTH_LIVE_E2E=1 URTH_IDENTITY_LIVE_E2E=1 \
URTH_E2E_BASE_URL=http://localhost:13007 \
URTH_E2E_API_URL=http://127.0.0.1:18087 \
URTH_E2E_MAIL_DIR=/path/to/task-owned/mail \
URTH_E2E_IDP_URL=http://127.0.0.1:18090 \
URTH_E2E_CLI=/path/to/built/urthctl \
npm run test:e2e -- e2e/identity-live.spec.ts --project=desktop
```

The identity test stores CLI profiles in a private temporary directory and removes
them in cleanup. Run the two live files with `--workers=1` when they share a stack.
Set `PLAYWRIGHT_CHROMIUM_PATH` only if a preinstalled browser requires an explicit
path, and record this override with validation evidence.
Failure traces can contain credentials: keep them private and remove
them after diagnosis. Use a separate database for `make audit/postgres`, which
runs destructive integration fixtures.

## Production image

From the repository root:

```sh
podman build -t urth-web --secret id=npmrc,src="$HOME/.npmrc" website
podman run --name urth-web -p 3001:8080 \
  -e URTH_API_UPSTREAM=api-server:8080 urth-web
```

Docker BuildKit accepts the same build-secret option. The runtime image contains
only Nginx and compiled assets. Configure the upstream to a reachable API host
and port. Serve the UI and API through one public origin; configure the API
issuer and redirect URI for that origin. Nginx preserves the browser Host,
proxies OAuth server routes, keeps the SPA callback/deep-link fallback, and
disables response buffering for `/v1` so live logs arrive immediately. Package
credentials are build secrets and do not enter image layers.

The identity API requires wyrd identity/v0.7.0 canonical resources. Deploy the
backend and this UI together. Flat resource reads and writes are unsupported.
Runner enrolment uses the shared token operation; its secret appears only in
the create result and is redacted on replay and ordinary reads.
