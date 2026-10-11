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

Run the full local check suite:

```sh
make check
```

`make check` expects `shellcheck`, `actionlint`, `govulncheck`, and `golangci-lint` to be available in PATH. It currently includes:

- `golangci-lint fmt` and `golangci-lint run` for Go packages (gofumpt/goimports formatting, vet, staticcheck, gosec, errcheck, ineffassign, unused);
- ShellCheck for `scripts/*.sh`;
- `actionlint` for GitHub Actions workflows;
- `govulncheck`;
- static production build check;
- race-enabled uncached Go tests with the aggregate coverage floor from `Makefile`;
- an independent `go vet` pass besides the golangci govet check.

Whitespace errors in uncommitted changes are a local convenience, not a gate: `make local-diff-check` runs `git diff --check` on demand. It is excluded from `make check` because a fresh CI checkout has nothing staged or modified, so it cannot find anything there.

The race detector requires a supported Go platform and a working C toolchain. `make test` is the faster option while iterating.

### Mutation sweep

`scripts/mutation-sweep.py` is not a gate and nothing in `make check` runs it. It neutralises each `if <cond> {` guard in the protective files and runs that package's tests. The condition becomes `false && (<cond>)` rather than a plain `false`, so the compiler still sees every variable it uses: replacing the line outright produced "declared and not used" on guards that were perfectly measurable, and those were filed as not applicable when they were not. Whether the package still compiles is decided by compiling it on its own, never by searching the test output for error words -- a test message containing a word that also appears in a compiler diagnostic turned a kill into a silent not-applicable. A guard whose deletion no test notices is reported as surviving, which means it can be removed without anything failing.

The script never writes to your repository. It exports `HEAD` into a scratch directory with no `.git` in it and does the rewriting there, so a killed run cannot leave a source file modified and nothing has to be restored. If the working tree has uncommitted changes the script says so and measures `HEAD` rather than them.

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

## Live sandbox tests

On a regular-user Linux host with Bubblewrap 0.12.0+ at `/usr/bin/bwrap` and working unprivileged user namespaces.
CachyOS is the distribution the live tests actually exercise; `ubuntu-latest` is the CI baseline, which
cannot run them. A host close to CachyOS is the one worth trusting when isolation or process lifetime
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
- Nine identifiers are exported although only their own package uses them.
  Unexporting them would make the compiler stop catching misspelled names across
  package boundaries, for no gain here.
- The fuzz harnesses in `internal/probe` are built and vetted but never fuzzed in
  CI. They document the input shape; running them on a schedule is a separate
  decision.
- The orchestrator in `internal/app` is knowingly under-covered. The code is
  correct where it was checked; closing the gap needs test seams that the
  project's own rules discourage. The mutation sweep behind this list measured
  the size of the gap: one of eleven orchestrator mutations is caught.
- The aggregate coverage floor is 74.0% against an actual 90.8%, and `internal/app` is 264 of the
  311 statements its own profile covers (84.9%). Halving that package's own coverage still leaves the
  aggregate at 82.5%, so the gate stays silent; only dropping it to zero trips the gate, at 74.2%, just
  over the floor. The aggregate catches gross
  regressions and nothing finer. A coverage drop with every test still green means tests were deleted
  or skipped, and the skip path is the one worth watching.
  `host.ResolveAccount` is the one function in that package no test reaches: it is a single
  line over `resolveAccount(os.Getuid(), user.LookupId)`, and reaching it needs a real root-owned
  `/etc/passwd`. The seam exists below it and is tested; the wrapper is left uncovered rather than
  given a seam of its own.
- `profile list` stops at the first profile with an unsafe layout instead of
  listing the rest. Documented in [Usage](usage.md) rather than changed.

For the maintainer release workflow and CI details, see [Maintainer releases and CI](releases.md).
