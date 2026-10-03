# M9 enrollment client validation

This record covers the Runner token CLI and browser client on the fresh-install
contract. It does not select a release version or close deployment-specific
gates. Backend lifecycle and storage evidence is recorded separately in
[task 005](review-backlog/tasks/005-secure-runner-enrollment.md).

## Source and compatibility

The branch starts at Urth `4990b229e3925b6f9e3a4058c22a3941046efcde`.
The tested candidate adds `runners tokens issue`, `list`, `get` and `revoke` to
`urthctl`. It uses the published `github.com/sre-norns/wyrd/identity v0.7.2`
client and the existing mounted identity API. The website keeps
`@sre-norns/components 0.5.0`; no shared component or backend implementation
changes are included.

The existing `runners token RUNNER` command keeps its secret-only issuance
output. Each issuance creates an independent token. The new issue command
accepts an optional RFC3339 expiration and an idempotency key. Replays return
metadata without the one-time secret. Revocation reads the current revision
and sends the shared guarded operation. List, get and revoke use canonical
secret-free metadata, including JSON and YAML output. Structured lists are
arrays. No migration is introduced.

## Real mounted client evidence

The isolated stack uses PostgreSQL 18 at `127.0.0.1:18833`, NATS 2.10 at
`127.0.0.1:18622`, the API at `127.0.0.1:18680` and Vite at
`http://localhost:18601`. Containers have the `m9-enrollment-clients-` prefix.
The API uses a bootstrap owner, development issuer and the explicit insecure
local broker settings. Those settings provide test isolation, not production
TLS evidence. The API binary uses the base backend source; the CLI binary
contains the candidate client changes. Both builds use `GOWORK=off` and Go
1.27.1. Build metadata records base revision `4990b2` with
`vcs.modified=true`, because the candidate client files are uncommitted at
build time. This does not describe a pristine merged-head build. Node is
24.21.0. The installed Chromium 1243 override is
`/home/soultaker/.cache/ms-playwright/chromium-1243/chrome-linux64/chrome`.

The command uses these task-specific settings from `website/`:

```sh
URTH_LIVE_E2E=1 URTH_ENROLLMENT_LIVE_E2E=1 \
URTH_E2E_BASE_URL=http://localhost:18601 \
URTH_E2E_API_URL=http://127.0.0.1:18680 \
URTH_E2E_CLI=/tmp/m9-enrollment-urthctl \
URTH_E2E_API_LOG=/tmp/m9-enrollment-runtime/api.log \
URTH_E2E_CLIENT_SCREENSHOT=/tmp/m9-enrollment-runtime/tokens-revoked-final.png \
PLAYWRIGHT_CHROMIUM_PATH=/home/soultaker/.cache/ms-playwright/chromium-1243/chrome-linux64/chrome \
npm run test:e2e -- e2e/enrollment-client-live.spec.ts --project=desktop --workers=1
```

The test signs in the CLI through the real OAuth device grant and browser
approval. It signs in the website with the bootstrap password and registers a
Runner through the UI. It then verifies:

- CLI issuance with a future expiration; the mounted metadata read returns the
  same expiration. A retry with the same key omits the secret.
- UI revocation of the CLI-issued token; CLI reads report `revoked`.
- UI issuance shows the secret once. Closing the dialog removes the field.
  CLI reads find its metadata and CLI revocation is visible after UI reload.
- Legacy CLI issuance still returns a usable secret; the new CLI revokes it.
- All three secrets authorize a valid mounted Worker challenge before
  revocation (`200`) and fail after revocation (`401`). This is enrollment
  admission evidence; the test does not start Worker sessions or prove session
  refresh behavior.
- Table, wide, JSON and YAML token reads and Runner reads succeed without the
  issued secrets. A missing token read produces a real HTTP error. Anonymous
  issuance fails.
- Ordinary CLI stdout/stderr, device-login output, browser runtime errors,
  captured API stdout/stderr, private CLI profiles, page content and browser
  local/session storage exclude the issued machine-token values. Authenticated
  one-time issuance output is explicitly allowed to contain its secret.
- Desktop Runner detail and the 420 × 844 token dialog pass axe checks, with no
  browser runtime error. The final screenshot shows revoked metadata only.

The exclusion assertions compare against all three actual issued values. They
prove those observed values do not occur in the inspected outputs; they do not
claim exhaustive redaction of all possible secret types. The private CLI
profile still stores its authenticated user session as designed. Browser traces
are disabled for this test because issuance responses contain secrets.

## Checks and provenance

| Check | Result |
| --- | --- |
| CLI gap regression against the original source | Fails as expected: parser rejects `runners tokens` |
| Expiration regression before the option is added | Fails as expected: parser rejects `--expires-at` |
| `GOWORK=off go test -race -count=1 ./cmd/urthctl` on final CLI source | Pass, 1.214 s; canonical expiry, no-expiry default and invalid RFC3339 cases included |
| `npm ci --cache /tmp/m8-npm-cache` | Pass; 0 reported vulnerabilities |
| `npm test -- --maxWorkers=1` | Pass; 33 tests in 6 files |
| `npm run build` | Pass |
| Fixture `npm run test:e2e -- --workers=1` at checked port 18617 | Pass; 8 desktop/mobile checks, 6 live opt-in skips, 16.9 s |
| Real mounted client test before expiration/profile additions | Pass; 1 test, 11.3 s |
| Real mounted client test on final candidate | Pass twice; 9.4 s and 9.0 s (10.1 s and 9.5 s total) |
| `make audit/postgres store-url=postgres://urth:urth@127.0.0.1:18833/urth_clients_audit` before expiration addition | Pass; module verification, vet, Staticcheck 2026.2.1 and full race suite, including NATS 53.810 s and integration 126.182 s |
| Same full audit on final CLI source | Pass; unchanged packages reuse valid cached results |
| `URTH_TEST_POSTGRES_URL=postgres://urth:urth@127.0.0.1:18833/urth_clients_audit go test -race -buildvcs -count=1 ./...` on final CLI source | Pass; full uncached suite, CLI 1.257 s, NATS 63.941 s, integration 125.791 s |

The audit database is separate from the mounted API database. No PostgreSQL
checks are claimed from a database-free run. Fixture browser checks are separate
from the mounted-stack evidence.

The first live attempts fail because the test expects an HTTP `items` envelope
instead of the shared CLI's canonical array, then because it uses plural
`get runners NAME` instead of singular `get runner NAME`. Those fixtures are
corrected. An expiration attempt fails because timestamp normalization leaves
milliseconds in an exact string comparison. A subsequent attempt exposes the
API's equivalent local-offset timestamp representation. The test now compares
the expiration instant at second precision. Earlier CLI test setup also uses
the wrong Runner field before
correction to `spec.active`. A restricted-cache setup failure is retried with
authorized cache access. A late desktop axe scan reports one violation before
explicit detail-heading
readiness. The corrected test waits for that heading and keeps both axe
assertions, with safe rule/target diagnostics. The corrected test passes twice. No rule
is disabled and no shared UI implementation changes.

These failed runs are preserved as test/setup provenance. They are not
passing release evidence. Secret-bearing
failure traces are removed after diagnosis.

## Limits and cleanup

The narrow token table requires horizontal scrolling to read the full status
labels. This shared component layout is unchanged. Deployment TLS, mail,
external providers, database verifier replay, serialized backend manifests,
Runner disable/delete, identity suspension, Worker refresh and run-capability
lifetimes are outside this client test. See task 005 and the existing
[release record](m9-release-validation.md) for their separate evidence and gates.

Sanitized passed and failed-attempt logs, build metadata, a hash manifest and
the revoked-metadata screenshot are saved under
`/home/soultaker/workspace/m9-review/enrollment-clients/`. The verified task
API/Vite processes and both named task containers are removed. Private runtime
files and failure traces are removed. The PR handoff removes the branch
worktree after publication. User PostgreSQL 5432, NATS 4222/8222, main checkouts
and protected Exp-Bench OpenSpec directories are preserved.

PR [115](https://github.com/sre-norns/urth/pull/115) merges as `1e37393`.
The exact merged backend, website and CodeQL checks pass; links are in the
[release record](m9-release-validation.md). CLI and website files match the
locally tested PR head `ad1bd16`. The other merged follow-ups add backend tests
and documentation, so this attribution does not claim another live run at the
merge commit. Deployment and release-version gates remain open.
