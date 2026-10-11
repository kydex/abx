#!/usr/bin/env bash
# Mutation sweep over ABX's protective checks.
#
# Every `if <cond> {` guard in the listed files is replaced with `if false {`,
# that package's tests are run, and the guard is restored. What comes out:
#
#   killed       -- a test fails, so something notices the guard disappearing
#   survived     -- the tests still pass, so the guard can be deleted unnoticed
#   not applicable -- the guard cannot be neutered that way (build or vet error)
#
# The classification reads the output text, not the exit code: `go test` exits
# non-zero for a build failure and for a failing test alike.
#
# Read the skip report before trusting the counts. Skipped tests do not fail, so
# a guard whose only test skips looks exactly like an uncovered guard. That is
# not hypothetical: on a host where /etc/passwd is not owned by root the resolve
# tests skip, and the guards below them are reported as uncovered even though CI
# and any ordinary host run them. A survival list produced with skipped tests is
# conditional, and this script says so rather than letting the numbers speak.
set -uo pipefail

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT" || exit 1

FILES="
internal/host/project.go internal/host/inputs.go internal/host/profiles.go
internal/host/shell.go internal/host/account.go internal/host/system.go
internal/host/skills.go internal/sandbox/validate.go internal/sandbox/plan.go
internal/sandbox/environment.go internal/bwrap/bwrap.go
internal/profile/name.go internal/cli/cli.go
"

# Guards that are not refusals: format switches and constant comparisons that
# cannot be neutered by replacing the condition.
is_skippable() {
  case "$1" in
    *kind*==*|*Kind*==*) return 0 ;;
  esac
  return 1
}

is_build_failure() {
  grep -qE 'build failed|declared and not used|syntax error|missing return|unreachable|cannot use|undefined:|assignment mismatch|too many arguments|not enough arguments|imported and not used' "$1"
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"; git -C "$ROOT" checkout -- $FILES 2>/dev/null' EXIT HUP INT TERM

if ! git diff --quiet || ! git diff --cached --quiet; then
  echo 'error: the tree is dirty; the sweep rewrites files and restores them' >&2
  exit 1
fi

echo '== база =='
skips=$(go test -count=1 ./... -v 2>&1 | grep -c '^--- SKIP' || true)
go test -count=1 ./... >"$tmp/base" 2>&1 || {
  echo 'error: the baseline is red, a survival number would mean nothing' >&2
  tail -20 "$tmp/base" >&2
  exit 1
}
echo "   тесты зелёные; пропущено $skips"

if [ "$skips" -gt 0 ]; then
  echo
  echo "   ВНИМАНИЕ: $skips тестов пропущено. Пропуск не падает, поэтому"
  echo '   охваченная им проверка выглядит непокрытой. Список выживших ниже'
  echo '   верен для этого окружения и только для него.'
  go test -count=1 ./... -v 2>&1 | grep '^--- SKIP' | sed 's/^/     /'
  echo
fi

killed=0 survived=0 skipped=0
: >"$tmp/survived"

for file in $FILES; do
  [ -f "$file" ] || { echo "нет файла: $file" >&2; exit 1; }
  pkg=$(dirname "$file")
  cp "$file" "$tmp/orig"
  mapfile -t lines < <(grep -n '^[[:space:]]*if .*{$' "$file" | cut -d: -f1)
  for n in "${lines[@]}"; do
    cond=$(sed -n "${n}p" "$file")
    indent=${cond%%[!$' \t']*}
    if is_skippable "$cond"; then skipped=$((skipped + 1)); continue; fi
    # Replace only this line; everything else is byte-identical to the original.
    awk -v n="$n" -v repl="$indent"'if false {' 'NR==n {$0=repl} {print}' "$tmp/orig" >"$file"
    go test -count=1 "./$pkg/" >"$tmp/out" 2>&1
    cp "$tmp/orig" "$file"
    if is_build_failure "$tmp/out"; then
      skipped=$((skipped + 1))
    elif [ -s "$tmp/out" ] && grep -qE '^(FAIL|--- FAIL)' "$tmp/out"; then
      killed=$((killed + 1))
    else
      survived=$((survived + 1))
      printf '%s:%s\t%s\n' "$file" "$n" "${cond#"$indent"}" >>"$tmp/survived"
    fi
  done
  echo "   $file: готово"
done

echo
echo '== итог =='
printf '   убито тестами      %d\n' "$killed"
printf '   ВЫЖИЛО (долг)      %d\n' "$survived"
printf '   неприменимо        %d\n' "$skipped"
if [ -s "$tmp/survived" ]; then
  echo
  echo '== выжившие =='
  sed 's/^/   /' "$tmp/survived"
fi
