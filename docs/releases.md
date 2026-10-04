# Urth release packaging

This procedure packages one source set for the API, Worker, CLI and website.
It does not deploy a stack. Complete the [release validation](m9-release-validation.md)
and select a product version before publication. Use a fresh PostgreSQL database
and NATS store; existing-resource migration is unsupported.

## Deliverables

| Archive | Platforms | Contents |
| --- | --- | --- |
| `api-server_<version>_<os>_<arch>.tar.gz` | Linux amd64, arm64 | API binary, command guide, license, source record |
| `nats-worker_<version>_<os>_<arch>.tar.gz` | Linux amd64, arm64 | Worker binary, command guide, license, source record |
| `urthctl_<version>_<os>_<arch>.tar.gz` | Linux and macOS amd64, arm64 | CLI binary, command guide, license, source record |
| `urth-website_<version>.tar.gz` | Static assets | Compiled `website/` assets, guide, license, source record |

Each binary archive has one executable at its root. All archives include
`release.json` with the version, full Git commit, commit timestamp, toolchain
and source state. Binary builds use `CGO_ENABLED=0`, trimmed paths and Go VCS
metadata. No new command-line version flags are added.

| Package | Platforms | Installs |
| --- | --- | --- |
| `urth-api-server_<deb-version>_<arch>.deb` | Debian/Ubuntu amd64, arm64 | `/usr/bin/urth-api-server`, disabled `urth-api-server.service`, `/etc/default/urth-api-server` |
| `urth-worker_<deb-version>_<arch>.deb` | Debian/Ubuntu amd64, arm64 | `/usr/bin/urth-worker`, disabled `urth-worker.service`, `/etc/default/urth-worker` |
| `urthctl_<deb-version>_<arch>.deb` | Debian/Ubuntu amd64, arm64 | `/usr/bin/urthctl` |
| `urth-api-server_<version>_<arch>.snap` | Linux amd64, arm64 | Strict snap; command `urth-api-server` |
| `urthctl_<version>_<arch>.snap` | Linux amd64, arm64 | Strict snap; command `urthctl` |

The Debian version is the tag without its `v`; a prerelease such as
`v0.1.0-rc.2` becomes `0.1.0~rc.2`, which sorts before `0.1.0`. Server
executables install under `/usr/libexec/urth` and reach `PATH` through the
prefixed links, so they cannot collide with another product's `api-server`.
The units run as a systemd dynamic user with state in `/var/lib/<unit>`. The
package does not enable or start them. Edit the matching `/etc/default` file,
then `systemctl enable --now <unit>`. The Worker unit reads the Runner token from
`/etc/urth/runner-token` (root, mode 0600) as a systemd credential. Its
installation key persists in `/var/lib/urth-worker/.config/urth/worker.key`.

A snap has its own home directory, so `urthctl` profiles in the snap are
separate from a host installation's. The API server snap is a command, not a
daemon: it needs PostgreSQL, NATS and identity configuration before it starts.

The release also contains `release-manifest.json`, `images.json` and
`checksums.txt`. Checksums cover every archive, package and both metadata files. Check
them with `sha256sum -c checksums.txt`. Use the image digests in `images.json`
for immutable deployment references; registry tags are mutable references.

| Image | Platforms | Profile |
| --- | --- | --- |
| `ghcr.io/sre-norns/urth-api-server` | Linux amd64, arm64 | API service |
| `ghcr.io/sre-norns/urth-worker` | Linux amd64, arm64 | Native probes; excludes Puppeteer |
| `ghcr.io/sre-norns/urthctl` | Linux amd64, arm64 | CLI operations and native local probes |
| `ghcr.io/sre-norns/urth-website` | Linux amd64, arm64 | Nginx and compiled website |

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
`urth-api-server` and `urth-worker` (Linux) as packages, apps and
`overlays.default`. Run `nix run github:sre-norns/urth/<tag>#urthctl`. The
licence is not free in Nixpkgs terms; the flake's own outputs allow exactly
these packages, and an overlay user allows them through
`config.allowUnfreePredicate`. The flake's `vendorHash` must change with
`go.mod`/`go.sum`; the Nix workflow fails a PR that leaves it stale and prints
the expected value.

**Snap Store.** Snaps are release assets. The `snap-store` job also uploads them
when the repository secret `SNAPCRAFT_STORE_CREDENTIALS` exists (`snapcraft
export-login`): stable versions to `stable`, prereleases to `candidate`. The
snap names `urthctl` and `urth-api-server` must be registered to that account
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
eight binaries and build the archives, Debian packages and snaps, then packages
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
record, command link, permissions, and the embedded binary's commit and platform. They also build all four images for both Linux platforms
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
