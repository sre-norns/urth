# M9 fresh-stack validation

This record covers an isolated local development stack on 2026-10-03. It does
not authorize a release. Initial API, Worker and CLI binaries use Urth commit
`6039e961b7b66477bb2f27762767f2e01aa4a72f`. This validation branch changes
browser verification and its development dependency lock. It changes no Go
runtime code. A merged candidate needs its own exact-head checks.

The final integrated API, Worker and CLI use
`37cb719c0eb9e3a3efa83075b408d62b947c32c0`. All three binaries report this
revision and `vcs.modified=false`. The final pass uses a new empty
`urth_final` database and recreated task-owned NATS container. The website uses
the validated frontend source and lock from this branch. Root's combined Go
checks cover this integrated backend separately.

## Environment

| Item | Value |
| --- | --- |
| Database | Fresh PostgreSQL 18 container `m9-urth-fresh-postgres`, port 18532; final empty database `urth_final` |
| Broker | Fresh NATS 2.10 container `m9-urth-fresh-nats`, JetStream, ports 18522/18523 |
| API / website / fake IdP | Ports 18580 / 18501 / 18590 |
| Held probe / fixture website | Ports 18581 / 18517 |
| Go | `/snap/go/11295/bin/go`, `GOWORK=off`, no module replacement |
| Shared pins | wyrd `v0.7.0`, identity `v0.7.2`, components `0.5.0` |
| Node | Node 24.21.0 from the existing task npm cache |
| Browser | Chromium 1243; `PLAYWRIGHT_CHROMIUM_PATH=/home/soultaker/.cache/ms-playwright/chromium-1243/chrome-linux64/chrome` |
| Identity | Development file mailer; fake OIDC provider; private task CLI profiles |
| Transport | Explicit loopback HTTP and insecure NATS development flags |

Listeners are checked before port use. User services on 5432, 4222 and 8222
remain running. Database fixtures do not use this active-stack database. The
release validator owns the separate complete Go PostgreSQL/race/static checks.

## Reproduced verification defect

The original checked-in live test fails after 60 seconds with no log lines.
Its Worker command omits `--allow-insecure-api` and `--nats.allow-insecure`.
A direct Worker startup proves the first refusal: HTTP loopback enrollment
requires `--allow-insecure-api`. The original test discards process diagnostics.

The corrected test sets both explicit local development flags. It stores the
installation key beside its private token file. It records bounded process
diagnostics and removes the token from failure text. It handles process exits
by signal during cleanup. Its initial real execution check passes in 40.2
seconds. Expanded token checks pass in 51.6 seconds.

## Actual-stack evidence

The tests use the running product API, website, CLI, Worker, database, broker,
file mailer and fake provider. These checks do not use mocked product routes.

| Area | Assertion |
| --- | --- |
| Registration | New email and account; delivered verification link; password setup; consumed link refusal |
| Password | Original password login; delivered reset link; reset completion; prior bearer returns 401 after a 200 positive control; original password refusal; new password login |
| Provider | New fake-IdP identity; account name; delivered confirmation link; account confirmation; subsequent OIDC login |
| CLI | Real device code and browser approval; saved profile; owner status; project create/use/list |
| Execution | Browser project, Runner, grant and Scenario creation; real Worker enrollment and dispatch; successful HTTP execution |
| Logs and artifacts | Authenticated run log and artifact pages; anonymous log request returns 401; connected stream while a held probe is running; stored completion log |
| Token secrecy | Issued secret exists once; ordinary token read and idempotent issue replay omit that secret |
| Token revocation | Anonymous issue returns 401; versioned revoke succeeds; separate Worker enrollment with revoked token fails |
| Worker block | Verified fingerprint block succeeds; stale Runner write rejects; real Worker receives next-claim 403; unblock permits another successful execution |
| Project grant | Last grant removal succeeds; subsequent Result is errored with `no-eligible-runner` placement label |
| UI | Populated identity, infrastructure and monitoring routes; axe assertions; desktop and narrow run screenshots |

The full baseline backend workflow passes with the patched frontend: two tests
pass in 2.0 minutes. The identity test takes 18.8 seconds and the execution
test takes 1.7 minutes. The integrated `37cb719` candidate also passes both
tests in 2.6 minutes. Its identity test takes 22.7 seconds and execution test
takes 2.2 minutes. All assertions in the table pass in that combined run.

The final wide and narrow screenshots are reviewed. The narrow layout wraps
resource IDs and log text without horizontal overflow. The held-run screenshot
shows a running Result and connected live stream before the target responds:

- [Running execution and live connection](validation/m9-fresh-stack/live-running.png)
- [Completed wide layout](validation/m9-fresh-stack/live-run.png)
- [Completed narrow layout](validation/m9-fresh-stack/live-run-narrow.png)

The reusable commands are in [website/README.md](../website/README.md).
The combined live command sets both live flags and runs the two live files with
`--project=desktop --workers=1`. It sets the API, website, broker, mail, fake IdP
and binary paths shown above, plus `URTH_E2E_SLOW_PROBE_PORT=18581`.

## Development dependency correction

The initial install reports two moderate notices for the same Vitest/mock
redirect advisory, [GHSA-82fw-gwwq-j7x9](https://github.com/vitest-dev/vitest/security/advisories/GHSA-82fw-gwwq-j7x9).
The publisher identifies versions below 4.1.11 as affected and 4.1.11 as patched.
The registry confirms Node 24 and Vite 8 support. This branch pins Vitest 4.1.11
and updates its eight-package family. It preserves other dependency versions.
The minimal lock clean-installs with `npm ci`. Its install audit reports zero
vulnerabilities. All 33 unit tests pass with `npm test -- --maxWorkers=1` in
29.58 seconds. The website build passes with the patched dependency set.

## Other checks and limits

`go mod verify` passes. Before the dependency patch, all 33 website unit tests
and the website build pass. The eight mocked desktop/mobile browser and axe
checks pass in 13.8 seconds. Four live cases skip in that fixture-only run;
those skips do not prove the real-stack workflow. The final patched mocked
desktop/mobile browser and axe suite passes all eight cases in 37.2 seconds
with one worker. Its four live skips remain explicit.

One patched unit run times out in a five-second test under concurrent host load
47.78. It reports 32 passes and one timeout. The validator reruns checks with
less concurrency instead of treating this run as a pass. The one-worker unit
rerun passes. One combined live run under the same load exceeds its 150-second
execution-test limit during the route/axe checks. Its held probe succeeds in
17.53 seconds and its live connection assertions pass. The validator preserves
this failure as an incomplete combined run.

This stack does not validate production HTTPS, broker authentication/TLS,
multi-replica signing consistency or key rotation, broker expiry/reconnect,
mail-provider delivery, deployment images, metrics failure alerts, classified
artifact retention, or reporting by an already-claimed execution after grant
revocation. The grant check proves subsequent placement refusal. It does not
replace composed next-claim or bounded reporting tests. The security owners
retain those release requirements and record their separate evidence.

Exact merged-head CI and maintainer release disposition remain open. The
task-owned stack, private mail, profiles, identity keys, tokens and failure
traces require cleanup before handoff. Cleanup completes: the two task containers
are removed, the three verified task service PIDs stop, and private mail and
failure traces are removed. Test cleanup removes Worker keys, tokens and CLI
profiles. A final listener/container check confirms the user database on 5432
and broker on 4222/8222 remain running. Final non-secret logs and screenshots
are preserved in the maintainer's review evidence directory.

The shared identity `v0.7.2` provider confirmation message uses Exp-Bench in
Urth's intermediate registration page. The provider workflow completes. Correct
this shared product copy in a separate change.
