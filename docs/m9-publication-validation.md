# M9 prerelease publication validation

M9 source acceptance and corrected Urth prerelease delivery are complete.
This record identifies published artifacts and their verification. It does not
approve stable promotion or a production deployment. The
[source acceptance record](m9-release-validation.md) retains the security,
fresh-stack, browser, CLI and cross-product evidence at the commits that run it.

## Source and release sequence

| Change | Merged source | Result |
| --- | --- | --- |
| [PR 117: packaging](https://github.com/sre-norns/urth/pull/117) | `d8f200bf0f252bde73459c8de5f30cd7adc90f49` | Adds archives and four multi-platform image publications. Merged-main audit exposes an intermittent test dependency race |
| [PR 118: route fixture](https://github.com/sre-norns/urth/pull/118) | `7059e8ac37b85630b468b29ba07356a4b0c036f5` | Retains race detection and rotation assertions; merged-main and release checks pass |
| [PR 119: metadata outputs](https://github.com/sre-norns/urth/pull/119) | `1843f9a8943bf091ddbe0970b90588713cd989bc` | Preserves string casing while emitting JSON booleans correctly; all merged-main checks pass |

The exact PR 119 merged commit passes
[backend audit and builds](https://github.com/sre-norns/urth/actions/runs/37125752580),
[website](https://github.com/sre-norns/urth/actions/runs/37125752553),
[CodeQL](https://github.com/sre-norns/urth/actions/runs/37125752577) and
[workflow lint](https://github.com/sre-norns/urth/actions/runs/37125752657).
Its tree matches reviewed PR head `ea0ffcfb259c6042b2428ba11e1c72e4aa180232`.

## First published candidate

The maintainer authorizes
[`v0.1.0-rc.1`](https://github.com/sre-norns/urth/releases/tag/v0.1.0-rc.1)
at `7059e8ac37b85630b468b29ba07356a4b0c036f5`.
[Release run 37123847493](https://github.com/sre-norns/urth/actions/runs/37123847493)
passes the exact-tag backend and website checks, archive inspection, all four
image publications and GitHub prerelease creation.

Independent checks download the published assets and query GHCR:

- All nine archives pass checksums, source-record, platform and member checks.
- All twelve assets match the GitHub asset sizes and SHA-256 digests.
- Downloaded Linux amd64 API, Worker and CLI binaries pass `--help` checks.
- All four registry index digests match `images.json`. Each index contains
  Linux amd64 and arm64 images. Their configuration blobs match the declared
  platforms, source versions and revisions.
- All image runtime users are non-root. The Worker image declares the native
  profile, which excludes Puppeteer.
- No image `latest` alias exists before or after publication. The GitHub release
  is a prerelease, not a draft or a stable release.

Registry checks use authenticated access. These checks do not establish
anonymous package access. They inspect all image manifests and configurations;
they do not execute arm64 or macOS binaries on the amd64 validation host.

Image creation labels use lowercase `t` and `z`, while the archive records use
uppercase `T` and `Z`. They represent the same instant. PR 119 corrects the
workflow output conversion, which can also alter mixed-case prerelease tags.
Do not move or replace `v0.1.0-rc.1`; it remains the record of the first trial.
Evidence is retained in `~/workspace/m9-review/prerelease-v0.1.0-rc.1/`.

## Corrected candidate verified

The maintainer explicitly authorizes
[`v0.1.0-rc.2`](https://github.com/sre-norns/urth/releases/tag/v0.1.0-rc.2)
at `1843f9a8943bf091ddbe0970b90588713cd989bc`.
[Release run 37126345015](https://github.com/sre-norns/urth/actions/runs/37126345015)
passes every publication gate. The release contains nine archives, the release
manifest, image digest records and checksums. It is a published prerelease.

Independent verification repeats the archive and registry checks above on the
newly downloaded assets: nine archives, twelve asset sizes and SHA-256 digests,
three native help commands, four image indexes and eight platform configurations
pass. Image versions and revisions match the archive records exactly. Every
creation label is now exactly `2026-10-03T13:18:53Z`, matching the archive
record without case normalization. The timestamp formatting defect is closed.

The published image index digests are:

| Image | Multi-platform digest |
| --- | --- |
| `ghcr.io/sre-norns/urth-api-server` | `sha256:511bd032d812c82069126d44c5d94545c3d0fd9b7bfe3958225be030350e94c5` |
| `ghcr.io/sre-norns/urth-website` | `sha256:ff694cead41ab3dc76a79860059365892b9999d9006b198dfe0f33949a243021` |
| `ghcr.io/sre-norns/urth-worker` | `sha256:af60b9223a30b6230ec122e48fc2b76c4565f8d1dd80b186a762917d97109f69` |
| `ghcr.io/sre-norns/urthctl` | `sha256:8947ed5c95c723e4f18d3b3a95e784f3abad6111ff999cd50c89819d37fdcdec` |

All four images retain non-root users, and the Worker retains the native
profile. Registry inspection uses authenticated access. Stable image aliases
remain absent; the first candidate tag and assets remain unchanged. Evidence
is retained in `~/workspace/m9-review/prerelease-v0.1.0-rc.2/`.

This closes the corrected prerelease publication gate. These checks validate
published artifacts and metadata; they do not claim a new deployed full-stack
run or execution of arm64 and macOS binaries. The earlier source and live-stack
records keep their original commit attribution.

## Remaining decisions

Prerelease verification closes the publication-path and package-permission
checks for this configuration. It does not select a stable product version.
Before stable promotion or deployment, the maintainer and operator must record:

- Public HTTPS origins, trusted proxies, identity provider and mail settings.
- PostgreSQL durability and backup policy; authenticated NATS client and route
  TLS, signing-key custody, replicas and deployment-specific rotation checks.
- Image digest selection, runtime storage, metrics access and failure alerts.
- The disposition of separate product risks: Runner channel policy (008),
  prober defaults (017), artifact retention and provider confirmation wording.

Scheduling remains a separate design and implementation requirement for v1.0.
M9 does not implement that feature backlog. Use fresh databases and broker
stores; no existing-resource migration is supported. Follow the
[release procedure](releases.md), [container guide](container-images.md) and
[broker operations guide](m9-broker-operations.md) for the corresponding steps.
