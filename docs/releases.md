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

Packaging runs after the tag exists (see *Publish a version*, step 5): the release run requires the tag on `HEAD`. The same archives can be built and smoke-checked without a tag with `make package-check`, which builds into a temporary directory and runs the same checks; `make check` runs it, so a packaging regression fails on a branch or pull request instead of only at the tag. To check archives you already have, run `make package-smoke`.

`make package` reads `internal/app/VERSION` and delegates to `scripts/package.sh`. The packaging script does not run tests and does not create a git tag. In a git work tree it requires:

- the tag argument to match `internal/app/VERSION`;
- a clean work tree with no untracked files, so the binary built from the working directory cannot differ from the source archive staged from the index;
- `HEAD` to carry that exact tag, so the archive name cannot describe different contents;
- every tracked top-level path to land in the source archive.

Plain builds (`go build`, `make build-check`, `go test`) are not gated: only archive creation requires the tag. Outside a git work tree the script warns and packages the directory as-is.

It creates:

| File | Contents |
|---|---|
| `abx-X.Y.Z-linux-amd64.tar.gz` | Static Linux amd64 binary, the full `docs/` tree in both languages (user guides and contributor documents alike), both `README` files, `CHANGELOG.md`, and `LICENSE`/`THIRD_PARTY_LICENSES` |
| `abx-X.Y.Z-source.tar.gz` | Source, tests, documentation, Makefile, packaging script, workflows, and the root support files (`go.mod`, `go.sum`, `.golangci.yml`, `.gitignore`, both READMEs, both AGENTS files, `CHANGELOG.md`, licenses) |
| `abx-X.Y.Z-SHA256SUMS` | SHA-256 hashes for the binary and source archives |

## GitHub Actions

CI is development/release infrastructure, not an installation interface.

- `.github/workflows/check.yml` runs `make check` for pushes and pull requests and is reused by the release workflow.
- `.github/workflows/live.yml` runs the real Bubblewrap test suite on the CI runner.
- `.github/workflows/release.yml` runs for `v[0-9]*` tags, requires both check and live jobs, runs `scripts/package.sh`, and uploads the resulting `dist/*` files as a workflow artifact.

The workflow artifact is for maintainer validation and distribution. The workflow does **not** create a release page, and adding release publication is out of scope for this project — see the rule in `AGENTS.md`. Step 7 therefore says "the project's release channel" without naming a service: whichever channel you publish through, the deliverable is the same three files and their SHA-256, and a user should be able to follow [Installation](installation.md) without interacting with Actions.

## Publish a version

1. Move the intended changelog entries from `Unreleased` into a new `X.Y.Z` section.
2. Change `internal/app/VERSION` to the same `X.Y.Z`.
3. Run `make check` and the appropriate live tests.
4. Commit and push the release changes.
5. Once branch CI is green, create and push an annotated tag:

```sh
version=$(cat internal/app/VERSION)
tag="v$version"
git tag -a "$tag" -m "ABX $version"
git push origin "$tag"
```

Waiting for branch CI is a recommendation, not an enforced precondition: nothing
in this repository or in the workflows verifies that the tagged commit was ever
green on the branch. The guarantee arrives in the next step, where the tag
workflow re-runs both check and live against the tagged commit itself and
packages nothing unless both pass. A tag pushed early is therefore not a
publication risk, only a wasted run.

6. Confirm that the tag workflow succeeds. It packages and then runs `make package-smoke`, which verifies the checksum manifest, unpacks both archives, and checks the mandatory file lists. A non-root caller then executes `abx version` from the unpacked binary archive; a root caller skips that step, since abx refuses to work as root. Download its workflow artifact: the archive files inside keep their `abx-X.Y.Z-*` names, so `(cd <downloaded> && sha256sum -c "abx-X.Y.Z-SHA256SUMS")` verifies them as-is. Then smoke-test the binary.
7. Publish the verified binary archive, source archive, and checksum manifest through the project's release channel — see the note above on what that means. Report the SHA-256 values.

Local packaging can be rehearsed without a tag with `make package-check`; `make package-smoke` checks archives already in `dist/`, and `make package` itself requires the tag, so run it after step 5.

Do not move an already published version tag to different contents. Publish later fixes as a new version.

Pushing all tags at once with `git push --force` starts no workflow: the remote tags move and GitHub runs nothing — no check job, no live job, no release artifacts, and no error anywhere to say why. It reads as a broken Actions setup. A single retargeted tag sent with `--force` is untested. Push tags without `--force`; a branch may be forced when its history is being replaced.

Archive metadata is not normalized for byte-for-byte reproducibility, and the workflow currently produces no build attestation.
