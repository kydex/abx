# Maintainer releases and CI

**English** | [Русский](ru/releases.md)

This document is for maintainers and contributors. End users should follow [Installation](installation.md); they do not need to use GitHub Actions.

## Version source

The release version has one source:

```text
internal/app/VERSION
```

The application embeds this file, and `scripts/package.sh` reads it when validating a tag and naming artifacts.

## Local release checks

Before publishing a version:

```sh
make check
make live
```

`make live` should be run on a representative supported host when runtime/isolation behavior has changed.

The [optional manual checks](manual-checks.md) are available for diagnosis or targeted investigation. They are not a requirement before every release and add no release gate.

Then verify packaging locally:

```sh
version=$(cat internal/app/VERSION)
tag="v$version"
bash scripts/package.sh "$tag"
cd dist
sha256sum -c "abx-$version-SHA256SUMS"
```

The packaging script does not run tests and does not create a git tag. It rejects a tag that does not match `internal/app/VERSION`.

It creates:

| File | Contents |
|---|---|
| `abx-X.Y.Z-linux-amd64.tar.gz` | Static Linux amd64 binary plus user documentation and licenses |
| `abx-X.Y.Z-source.tar.gz` | Source, tests, documentation, Makefile, packaging script, and workflows |
| `abx-X.Y.Z-SHA256SUMS` | SHA-256 hashes for the binary and source archives |

## GitHub Actions

CI is development/release infrastructure, not an installation interface.

- `.github/workflows/check.yml` runs `make check` for pushes and pull requests and is reused by the release workflow.
- `.github/workflows/live.yml` runs the real Bubblewrap test suite on the CI runner.
- `.github/workflows/release.yml` runs for `v*` tags, requires both check and live jobs, runs `scripts/package.sh`, and uploads the resulting `dist/*` files as a workflow artifact.

The workflow artifact is for maintainer validation and distribution. The current workflow does **not** create a GitHub Release page by itself. If the project distributes binaries through a release page or another public channel, publish the verified `dist/*` files there so users can follow the normal installation guide without interacting with Actions.

## Publish a version

1. Move the intended changelog entries from `Unreleased` into a new `X.Y.Z` section.
2. Change `internal/app/VERSION` to the same `X.Y.Z`.
3. Run `make check`, the appropriate live tests, and local packaging/checksum verification.
4. Commit and push the release changes.
5. After branch CI succeeds, create and push an annotated tag:

```sh
version=$(cat internal/app/VERSION)
tag="v$version"
git tag -a "$tag" -m "ABX $version"
git push origin "$tag"
```

6. Confirm that the tag workflow succeeds. Download its workflow artifact, verify `abx-X.Y.Z-SHA256SUMS`, and smoke-test the binary.
7. Publish the verified binary archive, source archive, and checksum manifest through the project's user-facing release channel.

Do not move an already published version tag to different contents. Publish later fixes as a new version.

Archive metadata is not normalized for byte-for-byte reproducibility, and the workflow currently produces no build attestation.
