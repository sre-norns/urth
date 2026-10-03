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

The release also contains `release-manifest.json`, `images.json` and
`checksums.txt`. Checksums cover every archive and both metadata files. Check
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

## Nonpublishing validation

Use Python 3, patched Go 1.27.1, Node 24, npm and GNU Make. The workflow pins the
archive compiler to match the image recipes; the ordinary quality gate tracks
patched stable Go. Configure a
private npm user configuration with GitHub Packages `read:packages` access.
The repository must have package access. Never put a package token in source.

From the repository root:

```sh
make release/snapshot
python3 scripts/check-release.py --output dist/release --smoke
```

The snapshot builder cross-compiles all eight binaries and packages the built
website. It does not publish, create tags, contact GHCR or start services. Output
defaults to ignored `dist/release`; select a new empty `--output` directory for
each build. `--website-dist` selects already built website assets. Use it only
with assets built from the source under validation.

Snapshot versions are `snapshot-<12-character-commit>`. Source records mark
uncommitted changes as `dirty: true`; snapshots are not releases. Tar ownership,
permissions, order and timestamps are fixed to the source commit. A repeat build
of unchanged source and assets with the same toolchain produces the same archives.
Files enter archives through an explicit binary/docs/assets list; private npm
configuration and the working source tree are not archived.

Pull requests that change packaging run the archive inspector and native
`--help` smoke checks. They also build all four images for both Linux platforms
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
6. It creates the GitHub release and uploads all archives, metadata and checksums.

The workflow uses `GITHUB_TOKEN`: `contents: read` and `packages: read` for
validation; `packages: write` for image publication; `contents: write` for
release creation. Grant Urth Actions read access to `@sre-norns/components`
and package publication access for the four GHCR repositories. The website
image receives private npm configuration only through a BuildKit secret. The
temporary secret file is removed after the image step.

Version, full source commit and commit timestamp match between archive records
and OCI image labels. Tag publication is serialized across versions. Existing
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

First hosted publication and production deployment remain maintainer gates.
Local snapshots do not prove registry permissions, hosted multi-platform
execution, a production certificate authority, storage durability or deployment
configuration. Follow the production and broker runbooks for those operations.
