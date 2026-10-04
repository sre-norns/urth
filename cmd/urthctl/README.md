# Urth command line utility

Command line utility to interact with a Prober Platform `Urth`.
It allows user to run test scripts locally in exactly the same way a script running job would execute them.


# Usage

To get the full list of supported subcommand use `--help` option.
```shell
> go run ./cmd/urthctl --help
```

## Sign in, profiles and the project context

```shell
> urthctl auth login --api-server-address=http://localhost:8080
Open http://localhost:3001/oauth/device?user_code=… and approve code ….
Signed in to http://localhost:8080 as profile "default".
> urthctl context use my-project      # by name or ID; checked on the server
> urthctl auth status                 # who the server sees
> urthctl get scenarios
```

`auth login` is the OAuth device grant: open the printed URL in any browser, sign in and
approve. The session is stored in a *profile* (`default` unless `--profile` names
another) under `$XDG_CONFIG_HOME/urth/profiles.json`, or wherever `URTH_PROFILES` points.
The file holds credentials, so it is written with mode `0600`. Access tokens are
refreshed before they expire. The first profile becomes the default; `profile list|show|use|remove`
manage them and never print credentials.

A profile supplies the endpoint, the token, the account it signed in to, and the
project its context names. `--account` and `--project` override the latter two for
one command. `--token` bypasses profiles entirely, for scripts. A profile's token is
never sent to another endpoint: naming a different `--api-server-address` is an error.

`auth logout` revokes the session on the server and removes the credentials. It keeps
the profile, with its endpoint and context, so a later `auth login` signs in to the same
place.

Sign-in, profiles, the context and output come from the kit Urth shares with Exp-Bench's
`expbctl` (`wyrd/identity/cli`).

## Output

`-o table` (the default), `-o wide`, `-o yaml` and `-o json`. `--format` is an alias.
Structured output is the resource's manifest. `urthctl get runner NAME -o json | jq -r .metadata.uid`
reads a runner's UID, for example.

What `get` prints applies back: `urthctl get scenario NAME -o yaml > s.yaml`, edit,
`urthctl apply s.yaml` (or pipe it to `urthctl apply -`). The manifest carries the
version it was read at, so a copy someone else has changed since is refused (412)
rather than written over it; its `status` is the server's and is not sent. Manifests
say `apiVersion: urth.sre-norns.com/v1`. Legacy `v1` and missing API versions are rejected.

`urthctl trigger SCENARIO` starts a run on the server now, as the Web UI's "Run now"
does; `urthctl run` runs a scenario locally.

## Runner tokens

```shell
> export RUNNER_TOKEN=$(urthctl runners token my-runner)
```

issues the enrolment token a runner's workers register with. It replaced `auth-worker`
and, like every command talking to the API, needs a signed-in profile or `--token`.
The existing command continues to print only the newly issued secret.

Use the shared identity token commands to name, inspect and revoke individual
tokens:

```shell
urthctl runners tokens issue my-runner installation --expires-at=2099-01-02T03:04:05Z
urthctl runners tokens list my-runner
urthctl runners tokens get TOKEN_ID -o yaml
urthctl runners tokens revoke TOKEN_ID
```

`--expires-at` accepts an RFC3339 timestamp. Omit it for a token without an
expiration. The API validates its lifetime, as it does for UI issuance.

`issue` prints the secret once. With `-o json` or `-o yaml`, it prints the shared
operation envelope with `resource` and `token`. List, get and revoke print only
canonical token metadata. Structured lists are arrays. Table, wide, JSON and
YAML reads omit the secret. Keep issuance output in a private token file.

For a retried issuance, reuse the same name, expiration and
`--idempotency-key=OPERATION_KEY`.
The server returns the same token metadata without its secret on replay.
Structured replay output omits `token`; ordinary replay reports that the secret
is unavailable. Do not select a new key merely to repeat a lost response.
Each new issuance creates an independent token. Issue a replacement, update
installations, then revoke the old token by ID. Revocation reads its current
version and sends the shared guarded operation. It prevents enrollment or
refresh with that token; existing Worker sessions and claimed run capabilities
retain their separate lifetimes.

To run a test script:
```shell
> go run ./cmd/urthctl run <scrupt file>
```

Note that running a script from the STDIN is also supported, but a `--kind` hint must be provided to tell the tool which type of script is being run.

For example, `urthctl` can replay [HAR](https://en.wikipedia.org/wiki/HAR_(file_format)) files saved from a web-borwser:
```shell
> go run ./cmd/urthctl run ./website.har
```

Or it can convert HAR file into a .HTTP file, as supported by [VSCode](https://marketplace.visualstudio.com/items?itemName=humao.rest-client) and [IntelliJ IDEA](https://www.jetbrains.com/help/idea/http-client-in-product-code-editor.html): 
```shell
> go run ./cmd/urthctl convert ./website.har 
```


## Runner channel policy

Scenario `requirements` selects Runner metadata labels. Runner `jobRequirements`
selects concrete jobs. Set `probeKinds` explicitly; an empty list admits no jobs.
Duration limits use Go strings such as `30s` and `1m`. The default accepted
interval is `1ns` to `1m`.

Runner `workerRequirements` controls enrollment. Version constraints use semantic
version comparators, for example `>=1.9.0 <2.0.0`. Missing or invalid required
capability values fail admission. Every enrolled Worker must cover every accepted
probe kind and the complete accepted duration interval. Worker capabilities are
validated declarations; they are not remote attestations.

Runner `propagatedLabels` copies operator labels into each Result and its Artifacts.
The Result stores the selected Runner UID, name, version, and label snapshot.
Later Runner edits do not change this history. Protected identity and security
label keys cannot be propagated or supplied by Worker uploads.

The UI Runner form accepts all three policy objects. Edit forms retain the ETag
and preserve drafts after a stale write. Worker details show stored effective
capabilities. Run details show the selected Runner version and propagated labels.
Use `urthctl get runner NAME -o yaml`, `urthctl get worker NAME -o json`, and
`urthctl get workers -o wide` to inspect the same information. Apply a Runner
manifest with `urthctl apply FILE`; updates use the version read by the client.
The removed Runner `requirements` field returns a validation error.

Placement preview reports the rejection reason when no channel accepts a job.
Run details retain unschedulable reasons. Dispatch failure details explain policy
changes that invalidate queued jobs. A Worker that cannot execute a queued job
does not receive a run lease.

Runner details also show the last enrollment rejection after a valid installation
proof. The record contains the verified fingerprint, policy reason, and time.
Use `urthctl get runner NAME -o wide` or `-o json` to inspect it. This operational
record grants no Worker or session authority. Workers receive a generic refusal.
