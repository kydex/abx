# Development

**English** | [Русский](ru/development.md)

This document is for contributors. Users do not need these commands to run ABX.

## Build and test

Use Linux and the Go version specified in `go.mod`.

Quick tests:

```sh
make test
```

Build a local static binary:

```sh
make build
./abx version
```

Build the release archives and check their structure:

```sh
make package
make package-smoke
```

`make package-smoke` unpacks the archives from `dist/`. `make package` requires the release tag on `HEAD`; see [Releases](releases.md) for the release flow. `make package-check` builds the archives into a temporary directory without a tag and runs the same checks; `make check` runs it, so a packaging regression fails before the tag.

Run the full local check suite:

```sh
make check
```

`make check` expects `shellcheck`, `actionlint`, `govulncheck`, `golangci-lint`, and `python3` to be available in PATH. It currently includes:

- `golangci-lint fmt` and `golangci-lint run` for Go packages (gofumpt/goimports formatting, vet, staticcheck, gosec, errcheck, ineffassign, unused);
- ShellCheck for `scripts/*.sh`;
- `actionlint` for GitHub Actions workflows;
- a syntax parse of the Python tools in `scripts/`;
- a check that the file target of every relative Markdown link resolves (`scripts/check-links.py`; heading anchors stay manual);
- `govulncheck`;
- static production build check;
- a check that `THIRD_PARTY_LICENSES` matches the build's module list;
- a structure smoke check of both archives, built without a release tag (`make package-check`);
- race-enabled uncached Go tests with the aggregate coverage floor from `Makefile`;
- an independent `go vet` pass besides the golangci govet check.

Whitespace errors in uncommitted changes are a local convenience, not a gate: `make local-diff-check` runs `git diff --check HEAD` on demand, covering both staged and unstaged changes. It is excluded from `make check` because a fresh CI checkout has nothing staged or modified, so it cannot find anything there.

The race detector requires a supported Go platform and a working C toolchain. `make test` is the faster option while iterating.

### Mutation sweep

`scripts/mutation-sweep.py` is not a gate and nothing in `make check` runs it. It neutralises each `if <cond> {` guard in the protective files and runs that package's tests. The condition becomes `false && (<cond>)` rather than a plain `false`, so the compiler still sees every variable it uses: replacing the line outright produced "declared and not used" on guards that were perfectly measurable, and those were filed as not applicable when they were not. Whether the package still compiles is decided by compiling it on its own, never by searching the test output for error words -- a test message containing a word that also appears in a compiler diagnostic turned a kill into a silent not-applicable. A guard whose deletion no test notices is reported as surviving, which means it can be removed without anything failing.

The script never writes to your repository. It exports `HEAD` into a scratch directory with no `.git` in it and does the rewriting there, so a killed run cannot leave a source file modified and nothing has to be restored. If the working tree has uncommitted changes the script says so and measures `HEAD` rather than them.

Each package is measured in its own copy of that scratch tree. Package tests link other packages -- `internal/app` links five of the six other swept packages, and `internal/bwrap` and `internal/probe` link `internal/sandbox` -- so with one shared tree a test run of one package compiles whatever mutant another worker holds in the packages it links, and that failure is recorded against the guard being measured.

A full run re-executes a package's whole test suite once per guard, so it is slow on one package and quick on the rest; packages run in parallel, because a package's tests see every file in it and two files of the same package must not be rewritten at once:

```sh
scripts/mutation-sweep.py
```

Naming files narrows it to a minute or two, which is enough to confirm the sweep itself still behaves:

```sh
scripts/mutation-sweep.py internal/host/account.go
```

Read the skip report it prints before trusting the numbers. A skipped test does not fail, so a guard whose only test skips looks exactly like an uncovered guard. That is not hypothetical: until the root-ownership requirement on `/etc/passwd` was removed on 12 October, the resolve tests skipped on every container, and the guards below them were reported as uncovered although CI and an ordinary host ran them. The script prints the count and names so a survival list is never mistaken for a property of the code rather than of the machine.

Two distinctions the output depends on. A build or vet failure is *not applicable*, not a kill: the guard cannot be neutered that way. And the classification reads the output text, not the exit code, because `go test` exits non-zero both for a build failure and for a failing test. A word in a test message can change the measurement — `unreachable` matches the build-failure pattern.

Two limits of the number. Each guard is one whole `if` condition: operands of `&&` and `||` are not measured separately, and a `switch` statement or a conditional `for` loop is not a guard by itself — only the `if` statements inside it are measured. And `survived` covers three different situations — a guard no test exercises, a guard whose removal a neighbouring check makes invisible, and a guard whose only test skipped on this machine. Read the survivor list line by line before treating it as debt.

## Live sandbox tests

On a regular-user Linux host with Bubblewrap 0.12.0+ at `/usr/bin/bwrap` and working unprivileged user namespaces.
CachyOS is the distribution the live tests actually exercise; `ubuntu-latest` is the CI baseline, where the live suite runs automated on a source-built Bubblewrap. A host close to CachyOS is the one worth trusting when isolation or process lifetime
has changed:

```sh
make live
```

Equivalent command:

```sh
env ABX_LIVE=1 go test -count=1 ./...
```

Live tests execute real Bubblewrap sessions and cover runtime behavior that ordinary unit tests cannot prove. A root run does not replace a regular-user live run. Unit tests also skip by UID: root-only and non-root-only cases never run together, so no single machine executes the full set; CI runs non-root.

To focus on one scenario:

```sh
env ABX_LIVE=1 go test -count=1 -v ./internal/app -run '^TestLiveVerify$'
```

The suite has two more live scenarios behind the same gate: `TestLiveShellRunAndWork`
and `TestLiveProfilesCLI`.

Run live tests after changes to mounts, namespaces, retained descriptors, project/profile path policy, Bubblewrap translation/execution, or verification behavior.

For diagnosis with disposable profiles, use the [optional manual checks](manual-checks.md) when useful. They are not required before every release and do not replace the automated or live tests above.

## Development rules

- Start from the intended user-visible behavior and its security boundary.
- Put decisions in the package that owns them: CLI syntax in `cli`, host eligibility in `host`, sandbox policy in `sandbox`, execution in `bwrap`.
- Preserve explicit resource ownership and error handling.
- Do not weaken path/FD checks merely to support a host that lacks required Linux functionality.
- Add regression tests for meaningful failure modes and invariants rather than mirroring every implementation detail.
- Keep `README.md` / `README.ru.md` and each English/Russian guide pair synchronized.
- Keep `CHANGELOG.md` in English.

## Rough edges we accept

These were found, cost something to weigh, and were left as they are. They are
here so nobody starts the same discussion from scratch.

- `Plan.Validate()` stays one long function. It sits between a built plan and the
  Bubblewrap arguments; splitting it risks that barrier to buy readability.
  Nine more production functions run past 60 lines -- `cli.Parse`,
  `Inputs.discoverLayout`, `bwrap.open`, `sandbox.build`, `app.runVerify`,
  `probe.check`, `app.runProfiles`, `host.resolveAgent`, and
  `host.resolveProject` -- but each is a linear sequence (one switch, loops,
  or ordered checks), and splitting would move state between the parts for no
  gain.
- Sixteen identifiers are exported although production code outside their own package does not use them:
  `Escape`, `Version` (app), `MinimumVersion`, `Path`, `Translate` (bwrap),
  `ErrProfileNotFound`, `ErrProfileExists`, `ProfileStatus`, `ResolveProject`,
  `ResolveShell`, `Mode`, `Selection`, `Source` (host), `CleanPath`, `Entry`
  (sandbox), and `Check` (probe). Unexporting them would make the compiler
  stop catching misspelled names across package boundaries, for no gain here.
- The fuzz harnesses in `internal/sandbox`, `internal/profile`, `internal/cli`,
  `internal/probe`, and `internal/host` are built and run with their seed
  corpora on every `go test`, but the fuzzing itself (`-fuzz`) never runs in
  CI. They document the input shape; running them on a schedule is a separate
  decision.
- The orchestrator in `internal/app` is knowingly under-covered. The code is
  correct where it was checked. Part of the gap is reachable through existing
  seams -- the `stdio` parameter and `brokenWriter` already pin a failed
  inspection write -- while the rest needs a real Bubblewrap launch or test
  seams that the project's own rules discourage. The mutation sweep behind this
  list measured the size of the gap, but its numbers are historical. The tenth
  full run measured 304 guards killed and 128 survived of 432, none not
  applicable; at that run `internal/app` tests caught 40 of the package's 77
  guards. The current tree is not that run: at `3172040` the guard finder
  counts 85 guards in `internal/app` and 452 across the swept files, so do not
  read the tenth-run pass rates as current.
- The aggregate coverage floor is 74.0% against an actual 92.4%, and `internal/app` is 311 of its
  351 statements (88.6%). Halving that package's coverage still leaves the aggregate at 83.7%; even
  zeroing it leaves 74.9%, still above the floor, so the gate does not fire on this package. The aggregate catches gross
  regressions and nothing finer. A coverage drop with every test still green means tests were deleted
  or skipped, and the skip path is the one worth watching.
  `host.ResolveAccount` is a one-line wrapper over the tested seam; unit tests
  reach it where the host provides a real account entry and skip loudly
  elsewhere, and the live suite covers it fully. The wrapper is left without a
  seam of its own.
- `profile list` stops at the first profile with an unsafe layout instead of
  listing the rest. Documented in [Usage](usage.md) rather than changed.

For the maintainer release workflow and CI details, see [Maintainer releases and CI](releases.md).
