#!/usr/bin/env bash
# Verifies the checksum manifest, unpacks both archives and checks structure
# against the mandatory file list plus the embedded version.
# A non-root caller also runs `abx version` from the unpacked binary archive;
# a root caller skips that, since abx refuses to run as root.
# usage: bash scripts/package-smoke.sh [dir]   (default: dist)
set -euo pipefail
script=$(readlink -f -- "${BASH_SOURCE[0]}")
root=$(cd -- "$(dirname -- "$script")/.." && pwd -P)
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
# A manifest that lists a duplicate or a substituted name still verifies; the
# name set catches a hand-edited or truncated file that would leave an archive
# unchecked, and reads a manifest without a trailing newline correctly.
manifest="$dir/abx-$version-SHA256SUMS"
want=$(printf '%s\n' "abx-$version-linux-amd64.tar.gz" "abx-$version-source.tar.gz" | LC_ALL=C sort)
got=$(awk '{print $2}' "$manifest" | LC_ALL=C sort)
if [ "$got" != "$want" ]; then
    printf 'error: %s lists unexpected entries:\n%s\nexpected:\n%s\n' "$manifest" "$got" "$want" >&2
    exit 1
fi
tar -xzf "$dir/abx-$version-linux-amd64.tar.gz" -C "$work"
test -x "$work/abx-$version-linux-amd64/abx"
if [ "$(id -u)" -eq 0 ]; then
    echo "note: built as root, so the archive was not executed: abx refuses to run as root, which is the correct behaviour, not a broken archive"
elif [ "$(uname -m)" = "x86_64" ]; then
    "$work/abx-$version-linux-amd64/abx" version > "$work/version.out"
    printf 'abx %s\n' "$version" | cmp -s - "$work/version.out"
else
    echo "note: archive is linux-amd64 and host is $(uname -m); execution skipped, structure checks still ran"
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
# The source-tree links-check cannot see the different root set of the binary
# archive: a docs link to AGENTS.md resolves in the checkout but not after
# packaging. Check both unpacked trees with the set of files each one ships.
python3 scripts/check-links.py "$work/abx-$version-linux-amd64"
python3 scripts/check-links.py "$work/abx-$version-source"
# Each archive's member list must match its unpacked tree member for member,
# and every member must stay inside the archive's own prefix: a duplicate
# entry, a path escaping the prefix, or an added top-level path fails these
# checks.
for name in "abx-$version-linux-amd64" "abx-$version-source"; do
    listing=$(tar -tzf "$dir/$name.tar.gz")
    members=$(printf '%s\n' "$listing" | wc -l)
    unpacked=$(find "$work/$name" | wc -l)
    if [ "$members" -ne "$unpacked" ]; then
        printf 'error: %s lists %s members, %s entries extracted\n' "$name.tar.gz" "$members" "$unpacked" >&2
        exit 1
    fi
    if printf '%s\n' "$listing" | grep -qvE "^$name(/|$)"; then
        printf 'error: %s contains a member outside %s/\n' "$name.tar.gz" "$name" >&2
        exit 1
    fi
    top=$(printf '%s\n' "$listing" | awk -F/ '{print $2}' | grep -v '^$' | LC_ALL=C sort -u | tr '\n' ' ')
    top=${top% }
    case "$name" in
    "abx-$version-source") want=".github .gitignore .golangci.yml AGENTS.md AGENTS.ru.md CHANGELOG.md LICENSE Makefile README.md README.ru.md THIRD_PARTY_LICENSES cmd docs go.mod go.sum internal scripts" ;;
    *) want="CHANGELOG.md LICENSE README.md README.ru.md THIRD_PARTY_LICENSES abx docs" ;;
    esac
    if [ "$top" != "$want" ]; then
        printf 'error: %s top-level entries are "%s", expected "%s"\n' "$name.tar.gz" "$top" "$want" >&2
        exit 1
    fi
done
# Packaging must keep every shell and Python tool executable (0755) in the
# source archive.
for tool in "$work/abx-$version-source"/scripts/*.sh "$work/abx-$version-source"/scripts/*.py; do
    [ -e "$tool" ] || continue
    test -x "$tool" || {
        printf 'error: %s must stay executable\n' "${tool#"$work/abx-$version-source/"}" >&2
        exit 1
    }
done
(cd "$work/abx-$version-source" && CGO_ENABLED=0 go build -trimpath -buildvcs=false -o /dev/null ./cmd/abx)
printf 'package smoke ok: %s\n' "$version"
