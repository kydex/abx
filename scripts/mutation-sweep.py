#!/usr/bin/env python3
"""Mutation sweep over ABX's protective checks.

Every `if <cond> {` guard in the listed files gets its condition neutralised and
that package's tests are run. Three outcomes:

    killed         a test fails, so something notices the guard disappearing
    survived       the tests still pass, so the guard can be deleted unnoticed
    not applicable the guard cannot be neutralised this way -- the package stops
                   compiling, which means the condition declared something used
                   nowhere else

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
]

# `if <cond> {` at the start of a line, possibly indented.
GUARD = re.compile(r"^(?P<indent>[ \t]*)if (?P<cond>.*)\{$")

# Format switches and constant comparisons that are not refusals.
SKIP_LINE = re.compile(r"\b[Kk]ind\b\s*==")

# A failing test announces itself; a passing one does not.
TEST_FAILED = re.compile(r"(?m)^(?:--- FAIL:|FAIL\s)")

KILLED, SURVIVED, NA = "killed", "survived", "not applicable"

_lock = threading.Lock()
_tally = {KILLED: 0, SURVIVED: 0, NA: 0}
_survivors: list[tuple[str, int, str]] = []
_done = 0
_total = 0


def run(args: list[str], cwd: pathlib.Path) -> tuple[int, str]:
    proc = subprocess.run(args, cwd=cwd, capture_output=True, text=True)
    return proc.returncode, proc.stdout + proc.stderr


def neutralise(indent: str, cond: str) -> str:
    """`if x, err := f(); err != nil {` keeps its init statement."""
    if ";" in cond:
        init, _, rest = cond.partition(";")
        return f"{indent}if {init}; false && ({rest.strip()}) {{"
    return f"{indent}if false && ({cond}) {{"


def sweep_package(repo: pathlib.Path, rels: list[str]) -> None:
    """Sweep every file of one package, strictly one guard at a time.

    A package's tests see every file in it, so two files of the same package
    must never be mutated at once: the second task would restore a file while
    the first is mid-measurement, and each would report the other's outcome.
    That is not theoretical -- it is how the first parallel version of this
    script lost four of the ten documented debts without saying so.
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

    found = [
        (n, m.group("indent"), m.group("cond"))
        for n, line in enumerate(orig_lines, 1)
        if (m := GUARD.match(line)) and not SKIP_LINE.search(m.group("cond"))
    ]

    for n, indent, cond in found:
        lines[n - 1] = neutralise(indent, cond) + "\n"
        path.write_text("".join(lines), encoding="utf-8")
        try:
            # Compile the package's tests without running them. A compile error
            # here is the only reliable signal that the guard cannot be measured
            # this way, and it cannot be confused with anything a test printed.
            code, _ = run(["go", "test", "-c", "-o", os.devnull, pkg], repo)
            if code != 0:
                verdict = NA
            else:
                _, out = run(["go", "test", "-count=1", pkg], repo)
                verdict = KILLED if TEST_FAILED.search(out) else SURVIVED
        finally:
            lines[n - 1] = orig_lines[n - 1]
            path.write_text("".join(lines), encoding="utf-8")

        with _lock:
            _tally[verdict] += 1
            if verdict == SURVIVED:
                _survivors.append((rel, n, cond))
            _done += 1
            print(
                f"\r   {_done:>4}/{_total}  убито {_tally[KILLED]:>3}"
                f"  выжило {_tally[SURVIVED]:>3}  неприменимо {_tally[NA]:>3}   ",
                end="",
                flush=True,
            )

    print(f"\r   {rel}: {len(found)} проверок" + " " * 40, flush=True)


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
            tar.extractall(repo)

        if subprocess.run(["git", "-C", str(ROOT), "diff", "--quiet"]).returncode:
            print("note: uncommitted changes present; the sweep measures HEAD, not them.")

        by_pkg: dict[str, list[str]] = {}
        for rel in wanted:
            if not (repo / rel).is_file():
                print(f"error: no such file in HEAD: {rel}", file=sys.stderr)
                return 1
            by_pkg.setdefault(pathlib.PurePosixPath(rel).parent.as_posix(), []).append(rel)
        _total = sum(
            len(
                [
                    1
                    for line in (repo / rel).read_text(encoding="utf-8").splitlines()
                    if GUARD.match(line) and not SKIP_LINE.search(GUARD.match(line).group("cond"))
                ]
            )
            for files in by_pkg.values()
            for rel in files
        )

        print("== база ==")
        code, out = run(["go", "test", "-count=1", "./...", "-v"], repo)
        skips = re.findall(r"(?m)^--- SKIP: (\S+)", out)
        if code != 0:
            print("error: the baseline is red, a survival number would mean nothing", file=sys.stderr)
            return 1
        print(f"   тесты зелёные; пропущено {len(skips)}")
        if skips:
            print()
            print(f"   ВНИМАНИЕ: {len(skips)} тестов пропущено. Пропуск не падает, поэтому")
            print("   охваченная им проверка выглядит непокрытой. Список выживших ниже")
            print("   верен для этого окружения и только для него.")
            for name in skips:
                print(f"     {name}")
            print()

        # One worker per package. A package's tests see every file in it, so two
        # files of the same package must not be mutated at the same time. Across
        # packages nothing is shared, and bwrap alone is slower than the rest put
        # together, so this is the difference between half an hour and minutes.
        with concurrent.futures.ThreadPoolExecutor(max_workers=len(by_pkg)) as pool:
            futures = [pool.submit(sweep_package, repo, files) for files in by_pkg.values()]
            concurrent.futures.as_completed(futures)

        print()
        print("== итог ==")
        print(f"   убито тестами      {_tally[KILLED]}")
        print(f"   ВЫЖИЛО (долг)      {_tally[SURVIVED]}")
        print(f"   неприменимо        {_tally[NA]}")

        if _survivors:
            survivors = sorted(_survivors)
            print()
            print("== выжившие ==")
            for rel, n, cond in survivors:
                print(f"   {rel}:{n}\t{cond[:86]}")
            # Redirect stdout to keep the machine-readable form, which the layout
            # above mangles only by its leading spaces.
            print()
            print("== выжившие, TSV ==")
            for rel, n, cond in survivors:
                print(f"{rel}\t{n}\t{cond}")
        return 0
    finally:
        shutil.rmtree(work, ignore_errors=True)


if __name__ == "__main__":
    sys.exit(main())
