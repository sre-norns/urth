# Urth release packaging

This procedure packages one source set for the API, Worker, CLI and website.
It does not deploy a stack. Complete the [release validation](m9-release-validation.md)
and select a product version before publication. Use a fresh PostgreSQL database
and NATS store; existing-resource migration is unsupported.

## Artifact names

Urth and Exp-Bench name every published artifact `<project>-<component>`, with
the project prefixes `urth` and `expbench`. The CLI keeps the name of its
command, because users install it by the command they type, and a snap named
after its only app puts that app on `PATH` without an alias.

| Component | Archive | Debian package | Snap | Image (`ghcr.io/sre-norns/…`) | Nix package | Command | systemd unit |
| --- | --- | --- | --- | --- | --- | --- | --- |
| API server | `urth-api-srv_…` | `urth-api-srv` | `urth-api-srv` | `urth-api-srv` | `urth-api-srv` | `urth-api-srv` | `urth-api-srv.service` |
| Worker | `urth-worker_…` | `urth-worker` | `urth-worker` | `urth-worker` | `urth-worker` | `urth-worker` | `urth-worker.service`; snap service `urth-worker` |
| Web UI | `urth-web_…` | — | — | `urth-web` | — | — | — |
| CLI | `urthctl_…` | `urthctl` | `urthctl` | `urthctl` | `urthctl` | `urthctl` | — |
| Exp-Bench API server | `expbench-api-srv_…` | `expbench-api-srv` | `expbench-api-srv` | `expbench-api-srv` | `expbench-api-srv` | `expbench-api-srv` | `expbench-api-srv.service` |
| Exp-Bench Web UI | `expbench-web_…` | — | — | `expbench-web` | — | — | — |
| Exp-Bench CLI | `expbctl_…` | `expbctl` | `expbctl` | `expbctl` | `expbctl` | `expbctl` | — |

The Homebrew cask is `urthctl`. The source directories (`cmd/api-server`,
`cmd/nats-worker`) keep their names; only released artifacts carry the
product prefix. Releases up to `v0.1.0-rc.2` used `api-server`, `nats-worker`,
`urth-api-server` and `urth-website`; those published images remain under their
old names and do not receive new versions.

## Deliverables

| Archive | Platforms | Contents |
| --- | --- | --- |
| `urth-api-srv_<version>_<os>_<arch>.tar.gz` | Linux amd64, arm64 | API binary, command guide, license, source record |
| `urth-worker_<version>_<os>_<arch>.tar.gz` | Linux amd64, arm64 | Worker binary, command guide, license, source record |
| `urthctl_<version>_<os>_<arch>.tar.gz` | Linux and macOS amd64, arm64 | CLI binary, command guide, license, source record |
| `urth-web_<version>.tar.gz` | Static assets | Compiled `website/` assets, guide, license, source record |

Each binary archive has one executable at its root, named as the archive.
All archives include
`release.json` with the version, full Git commit, commit timestamp, toolchain
and source state. Binary builds use `CGO_ENABLED=0`, trimmed paths and Go VCS
metadata. No new command-line version flags are added.

| Package | Platforms | Installs |
| --- | --- | --- |
| `urth-api-srv_<deb-version>_<arch>.deb` | Debian/Ubuntu amd64, arm64 | `/usr/bin/urth-api-srv`, disabled `urth-api-srv.service`, `/etc/default/urth-api-srv` |
| `urth-worker_<deb-version>_<arch>.deb` | Debian/Ubuntu amd64, arm64 | `/usr/bin/urth-worker`, disabled `urth-worker.service`, `/etc/default/urth-worker` |
| `urthctl_<deb-version>_<arch>.deb` | Debian/Ubuntu amd64, arm64 | `/usr/bin/urthctl` |
| `urth-api-srv_<version>_<arch>.snap` | Linux amd64, arm64 | Strict snap; command `urth-api-srv` |
| `urth-worker_<version>_<arch>.snap` | Linux amd64, arm64 | Strict snap; disabled service `urth-worker`, native probes |
| `urthctl_<version>_<arch>.snap` | Linux amd64, arm64 | Strict snap; command `urthctl` |

The Debian version is the tag without its `v`; a prerelease such as
`v0.1.0-rc.2` becomes `0.1.0~rc.2`, which sorts before `0.1.0`. The units run
as a systemd dynamic user with state in `/var/lib/<unit>`. The
package does not enable or start them. Edit the matching `/etc/default` file,
then `systemctl enable --now <unit>`. The Worker unit reads the Runner token from
`/etc/urth/runner-token` (root, mode 0600) as a systemd credential. Its
installation key persists in `/var/lib/urth-worker/.config/urth/worker.key`.

A snap has its own home directory, so `urthctl` profiles in the snap are
separate from a host installation's. The API server snap is a command, not a
daemon: it needs PostgreSQL, NATS and identity configuration before it starts.

### Worker snap

The `urth-worker` snap is a supported way to run a Worker. It installs a
service that stays stopped until it is configured:

```sh
sudo snap install urth-worker
# The token never goes on a command line, where every local user can read it.
printf '%s' "$RUNNER_TOKEN" |
  sudo install -m 0600 -o root -g root /dev/stdin /var/snap/urth-worker/common/runner-token
sudo snap set urth-worker api-url=https://urth.example nats-url=tls://nats.urth.example:4222
snap services urth-worker
sudo snap logs -f urth-worker
```

The configure hook runs on every `snap set`. It enables and starts the service
when `api-url` is set and the token file exists, and restarts a running service
so it reads the new settings. It refuses a setting it cannot use, so `snap set`
fails and keeps the previous values: a URL without a supported scheme, a token
file that is missing, not owned by root or readable by other users, or a token
in `args`. `sudo snap unset urth-worker api-url` stops and disables the service.

| Setting | Required | Worker flag |
| --- | --- | --- |
| `api-url` | Yes | `--client.api-server-address` |
| `nats-url` | No; the Worker default otherwise | `--nats.url` |
| `args` | No | Other flags, split on spaces, for example `--concurrency=4 --custom-labels=zone=a` |

Settings are not secret: `snap get` shows them to local users. Put files that
the Worker reads, such as a NATS CA (`--nats.tls-ca-file`) or credentials file,
under `/var/snap/urth-worker/common` with private permissions. The working
directory is `/var/snap/urth-worker/common/work`. The installation key is
`/var/snap/urth-worker/common/worker.key`; it survives refreshes and reverts.

The snap uses **strict confinement**. The Worker executes scenarios that the
control plane sends, so the snap limits a probe to network access instead of
the full host access that classic confinement gives. The plugs are `network`,
`network-bind` (only for `--metrics-address`) and `network-observe`.

| Probe | In the snap |
| --- | --- |
| HTTP, REST, gRPC, TCP, DNS, HAR replay | Supported |
| ICMP | Supported. The Worker uses a ping socket first, which needs the host's `net.ipv4.ping_group_range` to include root (the systemd default). Otherwise it needs raw sockets: `sudo snap connect urth-worker:network-observe`. That interface is never connected automatically. |
| Puppeteer (browser) | Not supported. The snap carries the native probe registry, as the Worker image does, and does not advertise Puppeteer. A strict snap cannot use a host Node.js, npm or browser. Use the deb or archive on a host with that runtime. |

The release also contains `release-manifest.json`, `images.json` and
`checksums.txt`. Checksums cover every archive, package and both metadata files. Check
them with `sha256sum -c checksums.txt`. Use the image digests in `images.json`
for immutable deployment references; registry tags are mutable references.

| Image | Platforms | Profile |
| --- | --- | --- |
| `ghcr.io/sre-norns/urth-api-srv` | Linux amd64, arm64 | API service |
| `ghcr.io/sre-norns/urth-worker` | Linux amd64, arm64 | Native probes; excludes Puppeteer |
| `ghcr.io/sre-norns/urthctl` | Linux amd64, arm64 | CLI operations and native local probes |
| `ghcr.io/sre-norns/urth-web` | Linux amd64, arm64 | Nginx and compiled website |

The host Worker and CLI archives retain the existing probe registry. Browser
probes still require their external runtimes. Do not treat the native Worker
image as a browser-probe image. See [container images](container-images.md)
for runtime requirements, mounts and supported profiles.

## Other installation channels

**Homebrew.** The `urthctl_<version>_darwin_<arch>.tar.gz` archives are the
macOS deliverable. The release workflow does not write to a tap. A cask in the
maintainer's tap references them, with the digests from `checksums.txt`:

```ruby
cask "urthctl" do
  arch arm: "arm64", intel: "amd64"

  version "0.1.0"
  sha256 arm:   "<sha256 of urthctl_v0.1.0_darwin_arm64.tar.gz>",
         intel: "<sha256 of urthctl_v0.1.0_darwin_amd64.tar.gz>"

  url "https://github.com/sre-norns/urth/releases/download/v#{version}/urthctl_v#{version}_darwin_#{arch}.tar.gz"
  name "urthctl"
  desc "Command-line client for Urth"
  homepage "https://github.com/sre-norns/urth"

  binary "urthctl"
end
```

The binaries are not signed or notarized. Gatekeeper quarantines a downloaded
binary; the cask user can install with `--no-quarantine`.

**Nix.** `flake.nix` provides `urthctl` (Linux and Apple silicon) and
`urth-api-srv` and `urth-worker` (Linux) as packages, apps and
`overlays.default`. Run `nix run github:sre-norns/urth/<tag>#urthctl`. The
licence is not free in Nixpkgs terms; the flake's own outputs allow exactly
these packages, and an overlay user allows them through
`config.allowUnfreePredicate`. The flake's `vendorHash` must change with
`go.mod`/`go.sum`; the Nix workflow fails a PR that leaves it stale and prints
the expected value.

**Snap Store.** Snaps are release assets. The `snap-store` job also uploads them
when the repository secret `SNAPCRAFT_STORE_CREDENTIALS` exists (`snapcraft
export-login`): stable versions to `stable`, prereleases to `candidate`. The
snap names `urthctl`, `urth-api-srv` and `urth-worker` must be registered to that account
first. Without the secret the job reports a notice and succeeds.

## Nonpublishing validation

Use Python 3, patched Go 1.27.1, Node 24, npm and GNU Make. GoReleaser builds
the binaries and packages; `make release/snapshot` runs the pinned version with
`go run`. Snaps also need `snapcraft`; without it the snapshot omits them and
records `"snap": false`. Release CI always builds snaps. The workflow pins the
archive compiler to match the image recipes; the ordinary quality gate tracks
patched stable Go. Configure a
private npm user configuration with GitHub Packages `read:packages` access.
The repository must have package access. Never put a package token in source.

From the repository root:

```sh
make release/snapshot
python3 scripts/check-release.py --output dist/release --smoke
```

The snapshot builder runs GoReleaser (`.goreleaser.yaml`) to cross-compile all
ten binaries (including the native Worker build for its snap) and build the archives, Debian packages and snaps, then packages
the built website. It does not publish, create tags, contact GHCR or start services. Output
defaults to ignored `dist/release`; select a new empty `--output` directory for
each build. `--website-dist` selects already built website assets. Use it only
with assets built from the source under validation.

Snapshot versions are `snapshot-<12-character-commit>`. Source records mark
uncommitted changes as `dirty: true`; snapshots are not releases. Tar ownership,
permissions, order and timestamps are fixed to the source commit. A repeat build
of unchanged source and assets with the same toolchain produces the same archives.
Files enter archives through an explicit binary/docs/assets list; private npm
configuration and the working source tree are not archived.

Pull requests that change packaging run the archive and package inspector and
native `--help` smoke checks. The inspector checks each package's control
record, installed command, permissions, and the embedded binary's commit and platform. They also build all four images for both Linux platforms
with registry push disabled. They do not create releases or update `latest`.
The ordinary backend and website workflows remain the PR quality gates.

## Publication

The maintainer supplies and pushes the version tag. The workflow never invents
a version, creates a tag or deploys the result. Accept tags of the form
`vMAJOR.MINOR.PATCH` or `vMAJOR.MINOR.PATCH-PRERELEASE`. SemVer numeric identifiers
cannot have leading zeroes. Build metadata (`+...`) is unsupported because the
same version is used as an image tag. Do not move or reuse release tags.

1. Merge the packaging change and verify the source set and release gates.
2. Select the version. Create and push that tag on the intended clean commit.
3. The tag workflow validates the version and rejects an existing GitHub release.
   It reuses the full backend PostgreSQL audit and website checks at that exact
   tag. Each image publication job requires both checks and archive inspection.
4. It builds the archives from a clean checkout and newly built website assets.
   It publishes all four versioned images and records their multi-platform digests.
5. After all images succeed, it verifies their version and commit against the
   archive manifest. Stable versions then update image `latest` aliases. A
   prerelease updates neither image `latest` nor GitHub's latest stable release.
6. It creates the GitHub release and uploads all archives, packages, metadata and checksums.
7. With the optional Snap Store credential, it uploads the snaps.

The workflow uses `GITHUB_TOKEN`: `contents: read` and `packages: read` for
validation; `packages: write` for image publication; `contents: write` for
release creation. Grant Urth Actions read access to `@sre-norns/components`
and package publication access for the four GHCR repositories. The website
image receives private npm configuration only through a BuildKit secret. The
temporary secret file is removed after the image step.

Version, full source commit and commit timestamp match between archive records,
package binaries and OCI image labels. Tag publication is serialized across versions. Existing
backend and website concurrency groups include the calling workflow name so
ordinary PR checks do not cancel release gates.

## Failure and recovery

Publication is not a transaction across GitHub and GHCR. Some versioned images
can exist if another image fails. Stable aliases update only after all four
images succeed, but an alias or final release operation can still fail afterward.
The workflow does not roll back published images or aliases.

Inspect the failed run and registry digests before retrying. Keep the source tag
fixed. If no GitHub release exists, a retry rebuilds the same source and can
replace a partially published version tag. Base images and external tools can
change its digest; use the final recorded digest for deployment. If a GitHub
release already exists, automatic replacement is refused. Correct the issue and
select a new product version instead of moving its tag.

The [M9 publication record](m9-publication-validation.md) records the first
hosted prereleases and independent asset verification. It establishes the
publication path and registry permissions for that source and configuration.
Production deployment remains a maintainer gate: certificate authorities,
storage durability, external providers and deployment configuration require
operator validation. Follow the production and broker runbooks for those
operations.
