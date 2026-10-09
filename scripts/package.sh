#!/usr/bin/env bash
set -euo pipefail
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd -- "$root"
tag=${1:?usage: bash scripts/package.sh vX.Y.Z}
if [[ ! $tag =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
    printf 'Invalid release tag: %s\n' "$tag" >&2
    exit 2
fi
version=$(<internal/app/VERSION)
if [[ ! $version =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
    printf 'Invalid source version: %s\n' "$version" >&2
    exit 2
fi
if [[ ${tag#v} != "$version" ]]; then
    printf 'Tag %s does not match source version %s\n' "$tag" "$version" >&2
    exit 2
fi
if git -C "$root" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    in_git=true
else
    in_git=false
    printf 'warning: not a git work tree; packaging archives from the working directory as-is\n' >&2
fi
if $in_git; then
    # A-068: the tag must point at this checkout, otherwise the archive
    # name would lie about its contents. Plain builds (go build,
    # build-check, tests) are unaffected — only release packaging is gated.
    head_tag=$(git -C "$root" describe --exact-match HEAD 2>/dev/null || true)
    if [[ $head_tag != "$tag" ]]; then
        printf 'error: HEAD is not tagged %s (found: %s); tag the release commit before packaging\n' "$tag" "${head_tag:-none}" >&2
        exit 2
    fi
    if ! git -C "$root" diff --quiet || ! git -C "$root" diff --cached --quiet; then
        printf 'error: git work tree is not clean; commit or stash changes before packaging\n' >&2
        exit 2
    fi
    # M-019: diff --quiet ignores untracked files, yet the binary is built
    # from the working directory while the source archive is staged from
    # tracked files only. Refuse untracked clutter so a single build cannot
    # produce a binary and a source archive with different contents.
    if [ -n "$(git -C "$root" ls-files --others --exclude-standard)" ]; then
        printf 'error: untracked files present; commit, stash, or remove them before packaging\n' >&2
        exit 2
    fi
fi
umask 022
mkdir -p dist
# A-067: drop stale archives first so a failed build cannot leave old files
# behind for a later smoke check to bless.
rm -f dist/abx-"$version"-linux-amd64.tar.gz dist/abx-"$version"-source.tar.gz dist/abx-"$version"-SHA256SUMS
stage=$(mktemp -d)
trap 'rm -rf -- "$stage"' EXIT
name="abx-$version-linux-amd64"
mkdir "$stage/$name"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false \
    -o "$stage/$name/abx" ./cmd/abx
cp LICENSE THIRD_PARTY_LICENSES README.md README.ru.md CHANGELOG.md "$stage/$name/"
if $in_git; then
    # Stage exactly the tracked files so untracked clutter cannot leak
    # into the user-facing archive either.
    git -C "$root" ls-files -z -- docs | (cd -- "$root" && xargs -0 -r cp --parents -t "$stage/$name")
else
    cp -R docs "$stage/$name/docs"
fi
tar --owner=0 --group=0 --numeric-owner -C "$stage" -czf "dist/$name.tar.gz" "$name"
source="abx-$version-source"
mkdir "$stage/$source"
if $in_git; then
    # Stage exactly the tracked files so untracked clutter under the staged
    # trees cannot leak into the source archive.
    git -C "$root" ls-files -z -- cmd internal scripts docs .github | (cd -- "$root" && xargs -0 -r cp --parents -t "$stage/$source")
else
    cp -R cmd internal scripts docs .github "$stage/$source/"
fi
cp go.mod go.sum Makefile README.md README.ru.md AGENTS.md AGENTS.ru.md CHANGELOG.md LICENSE THIRD_PARTY_LICENSES .gitignore .golangci.yml "$stage/$source/"
if $in_git; then
    # M-020: every tracked top-level path must land in the source archive.
    # A new directory or root file that is not staged fails here instead of
    # silently dropping out of the release.
    missing=$(git -C "$root" ls-files | sed 's|/.*||' | sort -u | while IFS= read -r top; do
        [ -e "$stage/$source/$top" ] || printf '%s\n' "$top"
    done)
    if [ -n "$missing" ]; then
        printf 'error: tracked top-level paths missing from source archive:\n%s\n' "$missing" >&2
        exit 2
    fi
fi
tar --owner=0 --group=0 --numeric-owner -C "$stage" -czf "dist/$source.tar.gz" "$source"
(cd dist && sha256sum "$name.tar.gz" "$source.tar.gz" > "abx-$version-SHA256SUMS")
