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

Packaging runs after the tag exists (see *Publish a version*, step 5): the script requires the release tag on `HEAD`, so it cannot be rehearsed before that. To rehearse against archives you already have, run `make package-smoke`.

`make package` reads `internal/app/VERSION` and delegates to `scripts/package.sh`. The packaging script does not run tests and does not create a git tag. In a git work tree it requires:

- the tag argument to match `internal/app/VERSION`;
- a clean work tree with no untracked files, so the binary built from the working directory cannot differ from the source archive staged from the index;
- `HEAD` to carry that exact tag, so the archive name cannot describe different contents;
- every tracked top-level path to land in the source archive.

Plain builds (`go build`, `make build-check`, `go test`) are not gated: only archive creation requires the tag. Outside a git work tree the script warns and packages the directory as-is.

It creates:

| File | Contents |
|---|---|
| `abx-X.Y.Z-linux-amd64.tar.gz` | Static Linux amd64 binary, the full `docs/` tree in both languages (user guides and contributor documents alike), and `LICENSE`/`THIRD_PARTY_LICENSES` |
| `abx-X.Y.Z-source.tar.gz` | Source, tests, documentation, Makefile, packaging script, and workflows |
| `abx-X.Y.Z-SHA256SUMS` | SHA-256 hashes for the binary and source archives |

## GitHub Actions

CI is development/release infrastructure, not an installation interface.

- `.github/workflows/check.yml` runs `make check` for pushes and pull requests and is reused by the release workflow.
- `.github/workflows/live.yml` runs the real Bubblewrap test suite on the CI runner.
- `.github/workflows/release.yml` runs for `v*` tags, requires both check and live jobs, runs `scripts/package.sh`, and uploads the resulting `dist/*` files as a workflow artifact.

The workflow artifact is for maintainer validation and distribution. The workflow does **not** create a release page, and adding release publication is out of scope for this project — see the rule in `AGENTS.md`. Step 7 therefore says "the project's release channel" without naming a service: whichever channel you publish through, the deliverable is the same three files and their SHA-256, and a user should be able to follow [Installation](installation.md) without interacting with Actions.

## Publish a version

1. Move the intended changelog entries from `Unreleased` into a new `X.Y.Z` section.
2. Change `internal/app/VERSION` to the same `X.Y.Z`.
3. Run `make check` and the appropriate live tests.
4. Commit and push the release changes.
5. After branch CI succeeds, create and push an annotated tag:

```sh
version=$(cat internal/app/VERSION)
tag="v$version"
git tag -a "$tag" -m "ABX $version"
git push origin "$tag"
```

6. Confirm that the tag workflow succeeds. It packages and then runs `make package-smoke`, which verifies the checksum manifest, unpacks both archives, and checks the mandatory file lists. Download its workflow artifact: the archive files inside keep their `abx-X.Y.Z-*` names, so `(cd <downloaded> && sha256sum -c "abx-X.Y.Z-SHA256SUMS")` verifies them as-is. Then smoke-test the binary.
7. Publish the verified binary archive, source archive, and checksum manifest through the project's release channel — see the note above on what that means. Report the SHA-256 values.

Local packaging can be rehearsed at any time with `make package-smoke` against existing `dist/` files; `make package` itself requires the tag, so run it after step 5.

Do not move an already published version tag to different contents. Publish later fixes as a new version.

Archive metadata is not normalized for byte-for-byte reproducibility, and the workflow currently produces no build attestation.
