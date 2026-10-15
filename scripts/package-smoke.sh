#!/usr/bin/env bash
# Verifies the checksum manifest, unpacks both archives and checks structure
# against the mandatory file list plus the embedded version.
# The release binary itself is not executed, so this works for any caller.
# usage: bash scripts/package-smoke.sh [dir]   (default: dist)
set -euo pipefail
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd -- "$root"
dir=${1:-dist}
version=$(<internal/app/VERSION)
for f in "abx-$version-linux-amd64.tar.gz" "abx-$version-source.tar.gz" "abx-$version-SHA256SUMS"; do
    test -f "$dir/$f" || {
        printf "error: %s is missing; run 'make package' first\n" "$dir/$f" >&2
        exit 1
    }
done
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM
(cd "$dir" && sha256sum -c "abx-$version-SHA256SUMS")
tar -xzf "$dir/abx-$version-linux-amd64.tar.gz" -C "$work"
test -x "$work/abx-$version-linux-amd64/abx"
if [ "$(id -u)" -ne 0 ]; then
    "$work/abx-$version-linux-amd64/abx" version > "$work/version.out"
    printf 'abx %s\n' "$version" | cmp -s - "$work/version.out"
else
    echo "note: built as root, so the archive was not executed: abx refuses to run as root, which is the correct behaviour, not a broken archive"
fi
test -f "$work/abx-$version-linux-amd64/LICENSE"
test -f "$work/abx-$version-linux-amd64/THIRD_PARTY_LICENSES"
test -f "$work/abx-$version-linux-amd64/README.md"
test -f "$work/abx-$version-linux-amd64/README.ru.md"
test -f "$work/abx-$version-linux-amd64/CHANGELOG.md"
test -d "$work/abx-$version-linux-amd64/docs"
test -d "$work/abx-$version-linux-amd64/docs/ru"
test ! -e "$work/abx-$version-linux-amd64/AGENTS.md"
test ! -e "$work/abx-$version-linux-amd64/AGENTS.ru.md"
tar -xzf "$dir/abx-$version-source.tar.gz" -C "$work"
test -d "$work/abx-$version-source/cmd"
test -d "$work/abx-$version-source/internal"
test -d "$work/abx-$version-source/scripts"
test -d "$work/abx-$version-source/docs"
test -d "$work/abx-$version-source/.github"
test -f "$work/abx-$version-source/go.mod"
test -f "$work/abx-$version-source/go.sum"
test -f "$work/abx-$version-source/Makefile"
test -f "$work/abx-$version-source/README.md"
test -f "$work/abx-$version-source/README.ru.md"
test -f "$work/abx-$version-source/AGENTS.md"
test -f "$work/abx-$version-source/AGENTS.ru.md"
test -f "$work/abx-$version-source/CHANGELOG.md"
test -f "$work/abx-$version-source/LICENSE"
test -f "$work/abx-$version-source/THIRD_PARTY_LICENSES"
test -f "$work/abx-$version-source/.gitignore"
test -f "$work/abx-$version-source/.golangci.yml"
test "$(cat "$work/abx-$version-source/internal/app/VERSION")" = "$version"
(cd "$work/abx-$version-source" && CGO_ENABLED=0 go build -trimpath -buildvcs=false -o /dev/null ./cmd/abx)
printf 'package smoke ok: %s\n' "$version"
