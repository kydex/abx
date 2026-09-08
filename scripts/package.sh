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
umask 022
mkdir -p dist
stage=$(mktemp -d)
trap 'rm -rf -- "$stage"' EXIT
name="abx-$version-linux-amd64"
mkdir "$stage/$name"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false \
    -o "$stage/$name/abx" ./cmd/abx
cp LICENSE THIRD_PARTY_LICENSES README.md README.ru.md CHANGELOG.md "$stage/$name/"
cp -R docs "$stage/$name/docs"
tar -C "$stage" -czf "dist/$name.tar.gz" "$name"
source="abx-$version-source"
mkdir "$stage/$source"
cp -R cmd internal scripts docs .github "$stage/$source/"
cp go.mod go.sum Makefile README.md README.ru.md AGENTS.md AGENTS.ru.md CHANGELOG.md LICENSE THIRD_PARTY_LICENSES .gitignore "$stage/$source/"
tar -C "$stage" -czf "dist/$source.tar.gz" "$source"
(cd dist && sha256sum "$name.tar.gz" "$source.tar.gz" > "abx-$version-SHA256SUMS")
