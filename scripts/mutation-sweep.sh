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

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd) || exit 1

FILES="
internal/host/project.go internal/host/inputs.go internal/host/profiles.go
internal/host/shell.go internal/host/account.go internal/host/system.go
internal/host/skills.go internal/sandbox/validate.go internal/sandbox/plan.go
internal/sandbox/environment.go internal/bwrap/bwrap.go
internal/profile/name.go internal/cli/cli.go
"

# Naming files narrows the run. The whole sweep re-executes a package's test
# suite once per guard and there are around three hundred guards, so a full run
# is half an hour; a single file is a minute or two and is enough to check that
# the sweep itself still works.
if [ "$#" -gt 0 ]; then
  FILES="$*"
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM
mkdir "$work/repo"

# Every mutation below rewrites a source file. Doing that in the repository means
# a killed run leaves it modified -- a trap cannot help against SIGKILL -- and a
# tool that edits the working tree on every run is a tool nobody should run
# without reading it first. So nothing is written where you keep your work.
# HEAD is exported to a scratch directory, which has no .git at all, and that is
# where the sweep happens.
git -C "$ROOT" archive HEAD | tar -x -C "$work/repo" || {
  echo 'error: could not export HEAD' >&2
  exit 1
}
cd "$work/repo" || exit 1

if ! git -C "$ROOT" diff --quiet || ! git -C "$ROOT" diff --cached --quiet; then
  echo 'note: the working tree has uncommitted changes; the sweep measures HEAD, not them.'
fi


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

tmp="$work/out"
mkdir "$tmp"

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
