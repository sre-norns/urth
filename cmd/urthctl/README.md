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

## Runner tokens

```shell
> export RUNNER_TOKEN=$(urthctl runners token my-runner)
```

issues the enrolment token a runner's workers register with. It replaced `auth-worker`
and, like every command talking to the API, needs a signed-in profile or `--token`.

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
