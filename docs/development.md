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

`make check` expects `shellcheck`, `govulncheck`, and `gosec` to be available in PATH. It currently includes:

- `gofmt` check;
- ShellCheck for `scripts/*.sh`;
- `govulncheck`;
- `gosec`;
- static production build check;
- race-enabled Go tests with the aggregate coverage floor from `Makefile`;
- `go vet`.

The race detector requires a supported Go platform and a working C toolchain. `make test` is the faster option while iterating.

## Live sandbox tests

On a regular-user Linux host with Bubblewrap 0.12.0+ at `/usr/bin/bwrap` and working unprivileged user namespaces:

```sh
make live
```

Equivalent command:

```sh
env ABX_LIVE=1 go test -count=1 ./...
```

Live tests execute real Bubblewrap sessions and cover runtime behavior that ordinary unit tests cannot prove. A root run does not replace a regular-user live run.

To focus on one scenario:

```sh
env ABX_LIVE=1 go test -count=1 -v ./internal/app -run '^TestLiveVerify$'
```

Run live tests after changes to mounts, namespaces, retained descriptors, project/profile path policy, Bubblewrap translation/execution, or verification behavior.

## Development rules

- Start from the intended user-visible behavior and its security boundary.
- Put decisions in the package that owns them: CLI syntax in `cli`, host eligibility in `host`, sandbox policy in `sandbox`, execution in `bwrap`.
- Preserve explicit resource ownership and error handling.
- Do not weaken path/FD checks merely to support a host that lacks required Linux functionality.
- Add regression tests for meaningful failure modes and invariants rather than mirroring every implementation detail.
- Keep `README.md` / `README.ru.md` and each English/Russian guide pair synchronized.
- Keep `CHANGELOG.md` in English.

For the maintainer release workflow and CI details, see [Maintainer releases and CI](releases.md).
