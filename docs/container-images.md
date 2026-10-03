# Urth container images

Urth has separate API, Worker, CLI and website images. The image repository
names prevent a collision with the Exp-Bench API image. Publication uses the
release workflow; a local build does not publish an image.

| Component | Image repository | Dockerfile | Context | Target | Runtime |
| --- | --- | --- | --- | --- | --- |
| API | `ghcr.io/sre-norns/urth-api-server` | `Dockerfile` | repository root | `api-server` | Static Go, UID/GID 65532, CA roots |
| Worker | `ghcr.io/sre-norns/urth-worker` | `Dockerfile` | repository root | `worker` | Static native Go probes, UID/GID 65532, CA roots |
| CLI | `ghcr.io/sre-norns/urthctl` | `Dockerfile` | repository root | `cli` | Static Go client, UID/GID 65532, CA roots |
| Website | `ghcr.io/sre-norns/urth-website` | `website/Dockerfile` | `website` | final stage | Nginx as user `nginx`, port 8080 |

Use an explicit published version tag or digest in deployments. Do not assume
these recipes select a release version. The build arguments `VERSION`,
`REVISION` and `CREATED` set the OCI version, source revision and creation-time
labels. They do not add new application version flags. Go source builds report
`(devel)` through the existing build-info API; use the OCI labels and image
digest for release attribution.

## Build contract

BuildKit supplies `BUILDPLATFORM`, `TARGETOS` and `TARGETARCH`. The Go builder
runs on the build platform and cross-compiles with `CGO_ENABLED=0`. The runtime
supports Linux amd64 and arm64. Its final stages contain no shell or package
manager. The root context includes only Go source and module inputs, including
the API's embedded theme. It excludes local configuration, Git metadata,
credentials and web dependencies.

For a local native amd64 build with Podman:

```sh
podman build --platform linux/amd64 --target api-server \
  --build-arg BUILDPLATFORM=linux/amd64 \
  --build-arg TARGETARCH=amd64 \
  -t localhost/urth-api-server:dev .
podman build --platform linux/amd64 --target worker \
  --build-arg BUILDPLATFORM=linux/amd64 \
  --build-arg TARGETARCH=amd64 \
  -t localhost/urth-worker:dev .
podman build --platform linux/amd64 --target cli \
  --build-arg BUILDPLATFORM=linux/amd64 \
  --build-arg TARGETARCH=amd64 \
  -t localhost/urthctl:dev .
podman build --build-arg BUILDPLATFORM=linux/amd64 \
  --secret id=npmrc,src="$HOME/.npmrc" \
  -t localhost/urth-website:dev website
```

The website needs a private npm configuration with GitHub Packages read access.
The public scoped registry setting is generated in the builder. Private `.npmrc`
files and environment files are excluded from its context. The `npmrc` secret
is mounted for `npm ci`; it is never a build argument or copied file. Do not
print it in logs. The final image contains Nginx and compiled public assets.

For multiple platforms without publication:

```sh
docker buildx build --platform linux/amd64,linux/arm64 --target worker \
  --output type=oci,dest=urth-worker.oci.tar .
docker buildx build --platform linux/amd64,linux/arm64 \
  --secret id=npmrc,src="$HOME/.npmrc" \
  --output type=oci,dest=urth-website.oci.tar website
```

The website's target-platform Nginx setup needs a native builder for that
platform or configured emulation. Go image cross-compilation does not require
running target-platform binaries during the build.

## Runtime state and trust

The Go images use `HOME=/home/urth`,
`XDG_CONFIG_HOME=/home/urth/.config` and a writable `/tmp`. The home and work
directories belong to UID/GID 65532. The Worker work directory is
`/home/urth/worker`. The images do not declare anonymous volumes.

Persist the Worker installation key at
`/home/urth/.config/urth/worker.key`. Give each installation its own volume and
key. Do not share one key between Workers. Persist CLI user profiles at
`/home/urth/.config/urth/profiles.json` when sign-in must survive container
replacement. A new named volume uses the image directory's ownership. For a
bind mount, provide directory access for container UID/GID 65532 through the
container engine's user mapping. Do not make secret files readable by all users.

An example named-volume layout is:

```sh
podman volume create urth-worker-config
podman volume create urth-worker-work
podman volume create urth-cli-config
podman run --rm \
  -v urth-cli-config:/home/urth/.config \
  ghcr.io/sre-norns/urthctl:VERSION auth login \
  --api-server-address=https://urth.example
```

Mount the Runner token as a private read-only file readable by UID 65532 and
pass `--token-file=/run/secrets/runner-token`. Pass the API URL through
`--client.api-server-address=https://urth.example`. Persist config and work
volumes at the paths above. Worker metrics stay disabled unless an address is
configured. The Worker needs outbound API, broker and probe-target access; it
does not need an exposed inbound port for normal operation.

The API uses port 8080 and `GIN_MODE=release`. Configure a fresh PostgreSQL
store, identity issuer, mail delivery, broker trust and scoped broker
credentials using the [API guide](../cmd/api-server/README.md). Static builds
use PostgreSQL; SQLite is not a supported deployment database. The bundled CA
roots support public TLS trust for API clients, mail and identity providers.
For private roots, mount a complete trust bundle and set `SSL_CERT_FILE` to its
path. Private keys, certificates and tokens remain deployment inputs.

The website listens on port 8080 without root privileges. Set
`URTH_API_UPSTREAM` to the API host and port reachable from its network. Configure
the API issuer and web redirect URI for the public website origin. Nginx
forwards OAuth and API routes and keeps live-log response buffering disabled.
See the [website guide](../website/README.md) for the route contract.

## Worker probe profile

The Worker image compiles with `-tags=urth_native`. Its registry and advertised
probe capabilities contain DNS, gRPC, HAR replay, HTTP, ICMP, REST and TCP.
Its OCI label `org.sre-norns.urth.runtime-profile` is `native`. Native probes
execute in Go; HAR replay does not launch a browser. ICMP still depends on the
host's ping-socket or raw-socket policy. The image does not enable privileged
mode or add raw-socket capabilities automatically.

Capability advertisement does not replace Runner and Scenario selection
policy. Keep native-only Workers in pools selected for supported probes. Browser
pools can set `Runner.spec.requirements` to require Puppeteer and refuse native
Worker registration. An unconstrained browser Scenario is not automatically
excluded from every native pool merely because this capability label is absent.
The existing task 008/022 policy gaps are outside image packaging.

Puppeteer is absent from this image's registry and capability labels. It is
not installed at runtime. The default host Worker archive retains Puppeteer
registration and requires an operator-supplied Node/npm/browser environment;
its current prober installs npm packages dynamically. PyPuppeteer is
unimplemented and is not registered by the Worker. No Python/browser support
is implied by the native image.

The CLI image supports API client commands and local native probes. It has no
Node/npm/browser runtime, so local Puppeteer execution is unavailable. Use an
appropriate host installation for browser scripts. The CLI is not a Worker
and does not advertise its local probe registry to the scheduler.

## Local validation

Validation uses base Urth `1e37393dc8ea2b3d69b8d98dcdda42f14aaf3c1f`
plus the candidate recipes and native registry seam. Local OCI labels identify
that base snapshot; they do not claim a pristine merged-source build. The
release workflow supplies the actual published source revision. Validation
uses Go 1.27.1 and Podman 5.4.2 on Linux amd64. Test resources have an `m9-`
prefix. No image publication or release occurs.

| Check | Result |
| --- | --- |
| Native registry regression against the original import set | Fails as expected: eight registrations instead of seven |
| `GOWORK=off go test -race -tags=urth_native -count=1 ./pkg/runner ./pkg/worker` | Pass; runner 1.062 s, Worker 8.103 s |
| `GOWORK=off make audit/postgres store-url=postgres://urth:urth@127.0.0.1:18834/urth_images` | Pass; module verification, vet, Staticcheck 2026.2.1 and full race suite, including NATS 90.067 s and integration 297.345 s |
| Final amd64 API, Worker, CLI and website image builds | Pass; all three Go image entrypoints execute `--help` |
| Final arm64 API, Worker and CLI image builds | Pass; each extracted executable is a static aarch64 ELF and each OCI architecture is arm64 |
| Nonroot Worker challenge-manifest smoke | Pass; seven native capability labels, no browser labels, persistent public installation key across two replacement containers |
| Worker key permissions in the named config volume | UID/GID 65532, mode 600 |
| Nonroot CLI profile save and replacement-container read | Pass; actual atomic profile update persists, UID/GID 65532, mode 600 |
| Native CLI local HTTP and public HTTPS probe execution | Pass; both report `success`; public HTTPS validates the bundled CA trust |
| Nonroot website SPA route and `nginx -t` | Pass; sign-in route returns 200, user UID/GID 101, rendered configuration loads |
| Website private npm credential scan | Pass; exact credential values absent from saved builder/runtime layers and captured build logs; scanner emits only the result |

The Worker smoke uses a local fixture that deliberately refuses admission. It
captures the real image's challenge manifest and checks persistent public-key
identity; it does not claim successful enrollment or execution against a real
control-plane stack. The profile smoke uses local metadata without a live
user credential; it proves nonroot storage behavior, not sign-in. HTTP probe
fixtures explicitly select IPv4. They do not close the existing prober-defaults
work in task 017. The API CA bundle is present and its executable is static;
mail and external identity-provider deployments are separate validation.

The first website build fails with an npm public-registry idle timeout. A retry
with `--network=host` passes. This network override is local validation evidence,
not a recipe requirement. The first nonroot Nginx start fails because the base
uses `/run/nginx.pid`; the corrected recipe replaces the `pid` directive with
`/tmp/nginx.pid` and then passes the runtime smoke.

An initial Podman arm64 attempt inherits empty compile-stage arguments and
produces amd64 bytes. The recipe now persists `GOOS`/`GOARCH` in the builder
and uses the build command's target platform for the scratch runtime. All three
final arm64 images pass executable and metadata inspection. No native arm64
execution or local arm64 website execution is claimed; those remain platform
validation limits.
Earlier smoke fixtures also use the wrong token flag, string JSON duration,
IPv6 defaults and YAML-only field names. They are corrected; failed attempts
remain preserved as provenance, not passing evidence.

Sanitized passed and failed-attempt logs, architecture checks, runtime results
and image identifiers are saved under
`/home/soultaker/workspace/m9-review/release-images/`. Task-owned containers,
volumes and temporary runtime files are removed before handoff. Protected user
PostgreSQL 5432 and NATS 4222/8222 remain intact. Hosted combined-packaging checks,
published image digests and exact release-source attribution are separate
gates.
