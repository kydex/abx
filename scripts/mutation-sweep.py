#!/usr/bin/env python3
"""Mutation sweep over ABX's protective checks.

Every `if <cond> {` guard in the listed files gets its condition neutralised and
that package's tests are run -- `else if` branches and conditions spanning
lines until the brace included. Four outcomes:

    killed         a test fails, so something notices the guard disappearing
    survived       the tests still pass, so the guard can be deleted unnoticed
    not applicable the guard cannot be neutralised this way -- the package stops
                   compiling, which means the condition declared something used
                   nowhere else
    error          go test exited non-zero with no FAIL line, so the tooling
                   broke, not the code -- retried once, recorded loudly, and
                   the run exits 1 instead of reporting counts

Scope note: earlier runs only saw single-line `if ... {`, the version before
`internal/app` joined the file list skipped guards matching a textual filter,
and the version before the per-package trees measured every package in one
tree, so none of their counts are comparable with runs from the current
version on.

Each package is measured in its own copy of the tree. Package tests link other
packages -- `internal/app` links five of the six others, and `internal/bwrap`
and `internal/probe` link `internal/sandbox` -- so in one shared tree a test
run compiles whatever mutant another worker happens to hold in a linked
package, and that failure is recorded against the guard being measured. A full
run with `internal/app` in the file list read 22 of its 76 guards surviving;
its own tree reads 38, the same as a run of the package alone. One
`internal/host` guard was read killed the same way.

The condition is neutralised as `false && (<cond>)` rather than replaced by a
plain `false`, so the compiler still sees every variable the condition uses.
Replacing the line outright produced "declared and not used" on guards that were
perfectly measurable, and those were filed as not applicable when they were not.

Whether the package still compiles is decided by compiling it on its own, not by
searching the test output for error words. A test message containing a word that
also appears in a compiler diagnostic -- `unreachable` is the one that has bitten
-- turned a kill into a silent not-applicable, and a diagnostic the word list
missed turned it into a kill. Both are errors in the measurement wearing the
costume of a result.

Read the skip report before trusting the counts. A skipped test does not fail, so
a guard whose only test skips is indistinguishable from an uncovered one. The
report names the skips so a survival list is never mistaken for a property of the
code rather than of the machine.

Nothing is written to the repository. HEAD is exported to a scratch directory
with no .git in it and the rewriting happens there.

    scripts/mutation-sweep.py                            # every file below
    scripts/mutation-sweep.py internal/host/account.go   # just one

The file list is manual. A guard in a file nobody added here is invisible to the
sweep, and nothing will say so.
"""

from __future__ import annotations

import concurrent.futures
import io
import os
import pathlib
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import threading

ROOT = pathlib.Path(__file__).resolve().parent.parent

FILES = [
    "internal/host/project.go",
    "internal/host/inputs.go",
    "internal/host/profiles.go",
    "internal/host/shell.go",
    "internal/host/account.go",
    "internal/host/system.go",
    "internal/host/skills.go",
    "internal/sandbox/validate.go",
    "internal/sandbox/plan.go",
    "internal/sandbox/environment.go",
    "internal/bwrap/bwrap.go",
    "internal/probe/probe.go",
    "internal/profile/name.go",
    "internal/cli/cli.go",
    "internal/app/app.go",
    "internal/app/diagnostic.go",
    "internal/app/profiles.go",
    "internal/app/report.go",
    "internal/app/verify.go",
]

# `if <cond> {` or `} else if <cond> {` at the start of a line, possibly
# indented. The condition may span lines until the opening brace; the span
# parser below follows it there.
GUARD_START = re.compile(r"^(?P<indent>[ \t]*)(?P<else>\}\s*else\s+)?if (?P<head>.*)$")

# A failing test announces itself; a passing one does not.
TEST_FAILED = re.compile(r"(?m)^(?:--- FAIL:|FAIL\s)")

KILLED, SURVIVED, NA, ERROR = "killed", "survived", "not applicable", "error"

_lock = threading.Lock()
_tally = {KILLED: 0, SURVIVED: 0, NA: 0}
_survivors: list[tuple[str, int, str]] = []
_errors: list[tuple[str, int, str, int]] = []
_done = 0
_total = 0


def run(args: list[str], cwd: pathlib.Path) -> tuple[int, str]:
    proc = subprocess.run(args, cwd=cwd, capture_output=True, text=True)
    return proc.returncode, proc.stdout + proc.stderr


def parse_guard(lines: list[str], start: int) -> tuple[int, str, str, str] | None:
    """Parse the guard starting at 0-based `start`.

    Returns (end, indent, else, full): `end` is the 0-based last line of the
    span, `else` is `} else ` or empty, and `full` is the whole condition with
    the trailing `{` removed. A `{` ending an inner line (composite literal)
    stops the span early; the rewrite below then fails to compile, which the
    compile check reports as not applicable -- loudly, never as a verdict.
    The repository is gofmt-clean, so a brace on its own line cannot occur;
    if it ever does, the same loud path takes it.
    """
    m = GUARD_START.match(lines[start])
    if not m:
        return None
    indent, else_kw, head = m.group("indent"), m.group("else") or "", m.group("head")
    end = start
    while not lines[end].rstrip("\n").endswith("{"):
        end += 1
        if end >= len(lines):
            return None
    if end == start:
        full = head[:-1]  # drop the `{`; identical slicing to the old regex
    else:
        last = lines[end].rstrip("\n")
        full = "\n".join([head] + [l.rstrip("\n") for l in lines[start + 1 : end]] + [last[:-1]])
    return end, indent, else_kw, full


def find_guards(text_lines: list[str]) -> list[tuple[int, int, str, str, str]]:
    """Every guard span as (start, end, indent, else, full), 0-based."""
    spans = []
    i = 0
    while i < len(text_lines):
        parsed = parse_guard(text_lines, i)
        if parsed is None:
            i += 1
        else:
            end, indent, else_kw, full = parsed
            spans.append((i, end, indent, else_kw, full))
            i = end + 1
    return spans


def rewrite_guard(indent: str, else_kw: str, full: str, ending: str) -> str:
    """Return the replacement text for one guard span.

    `full` is the whole condition with the trailing `{` removed, possibly
    spanning lines. `if x, err := f(); err != nil {` keeps its init statement.
    """
    if ";" in full:
        init, _, rest = full.partition(";")
        opener = f"{indent}{else_kw}if {init}; false && ({rest.strip()}"
    else:
        opener = f"{indent}{else_kw}if false && ({full.strip()}"
    return opener + ") {" + ending


def sweep_package(repo: pathlib.Path, rels: list[str]) -> None:
    """Sweep every file of one package, strictly one guard at a time.

    A package's tests see every file in it, so two files of the same package
    must never be mutated at once: the second task would restore a file while
    the first is mid-measurement, and each would report the other's outcome.
    That is not theoretical -- it is how the first parallel version of this
    script lost four of the ten documented debts without saying so.

    `repo` is this package's own copy of the tree (see `main`); no other
    worker writes to it, so the packages its tests link read as `HEAD`.
    """
    for rel in rels:
        sweep_file(repo, rel)


def sweep_file(repo: pathlib.Path, rel: str) -> None:
    global _done

    path = repo / rel
    # From rel, not from path: path is absolute and cwd is the copy, so joining
    # them would produce .//tmp/abx-sweep-xxx/repo/internal/host.
    pkg = "./" + pathlib.PurePosixPath(rel).parent.as_posix()
    original = path.read_text(encoding="utf-8")
    orig_lines = original.splitlines(keepends=True)
    lines = list(orig_lines)

    found = find_guards(orig_lines)

    for start, end, indent, else_kw, full in found:
        ending = "\n" if orig_lines[end].endswith("\n") else ""
        lines = list(orig_lines)
        lines[start : end + 1] = [rewrite_guard(indent, else_kw, full, ending)]
        path.write_text("".join(lines), encoding="utf-8")
        try:
            # Compile the package's tests without running them. A compile error
            # here is the only reliable signal that the guard cannot be measured
            # this way, and it cannot be confused with anything a test printed.
            code, _ = run(["go", "test", "-c", "-o", os.devnull, pkg], repo)
            if code != 0:
                verdict = NA
            else:
                test_code, out = run(["go", "test", "-count=1", pkg], repo)
                if TEST_FAILED.search(out):
                    verdict = KILLED
                elif test_code == 0:
                    verdict = SURVIVED
                else:
                    # No FAIL line but a non-zero exit: rerun once to shake off
                    # flakes, then record loudly. A broken measurement is never
                    # a verdict in either direction.
                    test_code, out = run(["go", "test", "-count=1", pkg], repo)
                    if TEST_FAILED.search(out):
                        verdict = KILLED
                    elif test_code == 0:
                        verdict = SURVIVED
                    else:
                        verdict = ERROR
        finally:
            path.write_text(original, encoding="utf-8")

        preview = full.replace("\n", "\\n")
        with _lock:
            if verdict == ERROR:
                _errors.append((rel, start + 1, preview, test_code))
            else:
                _tally[verdict] += 1
                if verdict == SURVIVED:
                    _survivors.append((rel, start + 1, preview))
            _done += 1
            print(
                f"\r   {_done:>4}/{_total}  killed {_tally[KILLED]:>3}"
                f"  survived {_tally[SURVIVED]:>3}  not applicable {_tally[NA]:>3}   ",
                end="",
                flush=True,
            )

    print(f"\r   {rel}: {len(found)} guards" + " " * 40, flush=True)


def main() -> int:
    global _total

    wanted = sys.argv[1:] or FILES
    work = pathlib.Path(tempfile.mkdtemp(prefix="abx-sweep-"))
    repo = work / "repo"
    repo.mkdir()
    try:
        archived = subprocess.run(["git", "-C", str(ROOT), "archive", "HEAD"], capture_output=True)
        if archived.returncode != 0:
            print("error: could not export HEAD", file=sys.stderr)
            return 1
        with tarfile.open(fileobj=io.BytesIO(archived.stdout)) as tar:
            # Explicit where new Pythons already default: the archive is our
            # own `git archive` (files and directories only), and the gate
            # keeps the script working where `filter` does not exist yet.
            kwargs = {"filter": "data"} if hasattr(tarfile, "data_filter") else {}
            tar.extractall(repo, **kwargs)

        if subprocess.run(["git", "-C", str(ROOT), "diff", "--quiet"]).returncode:
            print("note: uncommitted changes present; the sweep measures HEAD, not them.")

        by_pkg: dict[str, list[str]] = {}
        for rel in wanted:
            if not (repo / rel).is_file():
                print(f"error: no such file in HEAD: {rel}", file=sys.stderr)
                return 1
            by_pkg.setdefault(pathlib.PurePosixPath(rel).parent.as_posix(), []).append(rel)
        def count_guards(rel: str) -> int:
            text = (repo / rel).read_text(encoding="utf-8").splitlines(keepends=True)
            return len(find_guards(text))

        _total = sum(count_guards(rel) for files in by_pkg.values() for rel in files)

        print("== baseline ==")
        code, out = run(["go", "test", "-count=1", "./...", "-v"], repo)
        skips = re.findall(r"(?m)^--- SKIP: (\S+)", out)
        if code != 0:
            print("error: the baseline is red, a survival number would mean nothing", file=sys.stderr)
            return 1
        print(f"   tests green; {len(skips)} skipped")
        if skips:
            print()
            print(f"   WARNING: {len(skips)} tests skipped. A skip does not fail, so a")
            print("   guard whose only test skips looks exactly like an uncovered one.")
            print("   The survivor list below is true of this machine and only of it.")
            for name in skips:
                print(f"     {name}")
            print()

        def sweep_in_own_tree(files: list[str]) -> None:
            # One copy per package, so a linked package reads as HEAD even
            # while another worker mutates its own copy. With one shared tree
            # the two workers would measure each other's mutants.
            name = pathlib.PurePosixPath(files[0]).parent.as_posix().replace("/", "-")
            copy = work / f"repo-{name}"
            shutil.copytree(repo, copy)
            try:
                sweep_package(copy, files)
            finally:
                shutil.rmtree(copy, ignore_errors=True)

        # One worker per package. A package's tests see every file in it, so two
        # files of the same package must not be mutated at the same time. Each
        # worker owns its copy of the tree, and bwrap alone is slower than the
        # rest put together, so this is the difference between half an hour and
        # minutes.
        with concurrent.futures.ThreadPoolExecutor(max_workers=len(by_pkg)) as pool:
            futures = [pool.submit(sweep_in_own_tree, files) for files in by_pkg.values()]
            for future in concurrent.futures.as_completed(futures):
                # Surface worker exceptions: a thread that dies quietly leaves
                # its guards uncounted and the totals lying by omission.
                future.result()

        print()
        print("== summary ==")
        print(f"   killed by tests    {_tally[KILLED]}")
        print(f"   SURVIVED (debt)    {_tally[SURVIVED]}")
        print(f"   not applicable     {_tally[NA]}")

        if _survivors:
            survivors = sorted(_survivors)
            print()
            print("== survivors ==")
            for rel, n, cond in survivors:
                print(f"   {rel}:{n}\t{cond[:86]}")
            # Redirect stdout to keep the machine-readable form, which the layout
            # above mangles only by its leading spaces. A `\\n` inside a condition
            # is an escaped newline from a multi-line guard, not a row break.
            print()
            print("== survivors, TSV ==")
            for rel, n, cond in survivors:
                print(f"{rel}\t{n}\t{cond}")

        if _errors:
            print()
            print("== ERRORS: the tooling broke, these are not verdicts ==")
            for rel, n, cond, code in sorted(_errors):
                print(f"   {rel}:{n}\t{cond[:86]}  (exit {code})")
            print("error: reran once and still no FAIL line; the counts above are untrustworthy", file=sys.stderr)
            return 1
        return 0
    finally:
        shutil.rmtree(work, ignore_errors=True)


if __name__ == "__main__":
    sys.exit(main())
