# Changelog

## Unreleased

- Fix a false refusal found in pre-release review: system mounts no longer require the source basename to match the target, which broke `abx` on hosts where `/etc/resolv.conf` is a systemd-resolved stub or `/etc/localtime` points into zoneinfo.
- Accept `-v`/`--version` alongside `-h`/`--help` in any argument position, and restore inherited descriptor flags only after confirming the descriptor number still refers to the same object.
- Packaging: accept lightweight tags and repositories carrying several tags on `HEAD`, treat a source archive unpacked inside an unrelated repository as a plain directory, remove partial archives when a build fails partway, and document every gate in the release procedure (English and Russian).
- Document the actual login-shell, passwd-database, data-base, and installation requirements, and correct the signal description: terminal signals reach the payload through the foreground process group, they are not delivered as SIGKILL.

- Fix documentation against code from two audit rounds (English and Russian): shared-skills recipe now sets `0700` on the storage root, sandbox environment lists all variables, `verify` documents its 30-second deadline, exit-code and signal sections describe SIGKILL delivery and per-command argument rules, Bubblewrap trust requirements are stated, and resource/namespace tables match the implementation.
- Sharpen CLI and diagnostics without behavior change for legitimate inputs: `inspect` shows the mount namespace, Bubblewrap refusals name the violated condition, bare `abx` and `-h`/`--help` in any position show help, `-v`/`--version` works, `inspect` write errors return status 1, and dangling-symlink, timeout, and already-exists errors explain the cause.
- Pin policies with unit tests and harden packaging: independent literals pin system-target, project-tree, namespace, and mount-point lists; `Validate` refusals, skills-symlink, oversize-probe, uid-validation, exit-2, argv-literalness, fd-index, fd-accounting, and fuzz-allowlist cases added; inherited FD flags are restored after execution; `package` refuses untracked clutter and verifies the source archive against tracked top-level paths, `dist/` is cleaned before builds, `package-smoke` checks the checksum manifest and mandatory file lists, and CI checkouts set `persist-credentials: false`.
- Stop cleanup failures from overwriting a non-zero exit status: a failing cleanup now yields status `1` only when the operation itself succeeded, so the status reported by the deferred close stays the operation's own. Run the `__probe` sandbox gate before payload decoding, wire `package-smoke` into the release workflow, and replace the inert CI `diff-check` with a local-only target.
- Gate release packaging on the version tag: `make package` refuses unless HEAD carries the matching tag, while plain builds stay ungated.
- Tighten host trust checks (fail-closed, may refuse previously accepted setups): login shells and the passwd database must not be group/other-writable or carry special bits, the executed shell is the resolved target, passwd homes and projects must belong to the invoking user, projects must not be world-writable, and data-base ancestors must not be group/other-writable without the sticky bit. Shell ownership is deliberately not required (user-managed shells under system paths).

## 0.8.21

- Cap sandbox tmpfs mounts at 4 GiB so unbounded temporary files cannot exhaust host memory.
- Reject NUL usernames and NUL-valued host variables in `Session`, found by the new fuzz tests for name, session, and CLI parsing (seed corpora run in `go test`).
- Document how to contain runaway sessions with a systemd scope (`MemoryMax`, `CPUQuota`). (English and Russian.)
- Tidy comments, names, and error constructors without behavior change: fix stale doc names, stop shadowing `close`, use `errors.New` for constant errors, and check executability through `unix` instead of `syscall`.

## 0.8.20

- Name failing bind sources, mounts, and tool paths in errors instead of returning bare filesystem errors.
- Accept login shells under `/etc/alternatives` when their resolved target stays within the mounted system paths.
- Reject probe payloads with non-canonical read-only targets at decode time.
- Test setuid/setgid/sticky rejection for the Bubblewrap tool, pin the sentinel name prefix, and pin failed exits under `WaitDelay`.
- Stage only tracked files into the binary archive too, refuse packaging on a dirty tree, warn outside git trees, and add `make package-smoke`, `make clean`, and CI timeouts.
- Document `--die-with-parent`. (English and Russian.)

## 0.8.19

- Tidy naming and layout without behavior change: name the profile-list header, split the storage-path check, explain the private path tail, and wrap long syscalls.
- Add a troubleshooting row for mise-installed agents. (English and Russian.)
- Cover the probe gate logic, host payload assembly, identity reporting, success message, and probe CLI dispatch with unit tests instead of live-only runs.
- Keep an independent `go vet` pass in `make check` besides the golangci govet check, and document where the `mise` binary must live to be reachable inside the sandbox. (English and Russian.)

## 0.8.18

- Find `run` profile executables in the mise shims directory (`.local/share/mise/shims`) right after `.local/bin` and include it in the sandbox PATH in the same order, so mise-installed tools work with `run` without manual symlinks. (English and Russian.)

## 0.8.17

- Mention `-h`/`--help` in the README command table, link system-source discovery to the sandbox policy, and check uncommitted changes for whitespace errors (`git diff --check`). (English and Russian.)

## 0.8.16

- Run Go checks through golangci-lint (`golangci-lint fmt` with gofumpt/goimports and `golangci-lint run` with vet, staticcheck, gosec, and errcheck) instead of separate gofmt, staticcheck, gosec, and vet steps; errcheck and gosec skip test files, and the De Morgan quickfix stays disabled by design. Run unit tests uncached (`-count=1`).

## 0.8.15

- Reject host-side `abx __probe ...` invocations: the private probe now requires `/abx-probe` to be a read-only mount of the running binary with matching device and inode, so it only runs inside the sandbox. Document `__probe` as private in English and Russian. No behavior change for legitimate sessions.
- Report the child's own exit status when it orphans output pipes past WaitDelay instead of failing the launch; this only affects API use with non-file streams, the `abx` binary always passes real files.
- Name the failing system candidate in discovery errors instead of returning the bare filesystem error.
- Normalize release archive file ownership to root and stage only tracked files into the source archive so untracked clutter cannot leak in.
- Cover the Bubblewrap root-ownership check, compat-symlink revalidation, exact/sibling/data-base path rules, and WaitDelay handling with regression tests; remove dead code without behavior change.
- Simplify the installation instructions: unpack the archive, change into its directory, and install `./abx`; PATH setup is left to the user's shell config and checksum verification is documented as optional. (English and Russian.)

## 0.8.14

- Document the known residual same-UID pathname-race windows (agent-executable replacement, profile listing versus inspection, data-base canonicalization versus use, pathname-based metadata rechecks) as accepted in English and Russian, and note that mounts always use retained descriptors. No behavior change.

## 0.8.13

- Drop stale `adapted from abx 0.7.9` notes from package and test comments; the documented behavior is defined by the current implementation and guides. No behavior change.

## 0.8.12

- Name the clean sandbox environment `entries` in session assembly instead of reusing the path-check vocabulary; no behavior change.
- Preallocate the system-target duplicate-detection map in plan construction, matching the other duplicate-detection maps.

## 0.8.11

- Require the sandbox shell to be a canonical absolute path (same strength as the plan command check) so `Session` never accepts a value that `Build` would reject; non-canonical shells now fail fast with a precise diagnostic.
- Preallocate the environment duplicate-detection map in plan construction, matching the validator style.

## 0.8.10

- Refactor without behavior changes: move the test-only Bubblewrap version shorthand into the test file, give the sandbox path check a descriptive name, and name the minimum-version triple next to the version pattern.

## 0.8.9

- Harden private probe-input validation: require canonical absolute home/sentinel paths without NUL bytes, and require the verification link to be a canonical path directly inside `/workspace` with the reserved `.abx-verify-` file name prefix.
- Compile the Bubblewrap version pattern once instead of on every version check.
- Name the tmpfs magic constant in the live-test probe fixture instead of using a bare hexadecimal literal.
- Document the full sensitive home-tree list (`.config`, `.ssh`, `.gnupg`, `dotfiles`, `.aws`, `.docker`, `.kube`, `.pki`, `.cache`) in English and Russian.

## 0.8.8

- Reject sensitive home trees `.aws`, `.docker`, `.kube`, `.pki`, and `.cache` as projects, and reject sibling homes outside the real home under `/home`.
- Reject exact `/tmp`, `/var/tmp`, and `/dev/shm` as projects, and add `/srv`, `/mnt`, `/media`, and `/snap` to protected system trees.
- Reject data bases at the filesystem root, inside sensitive home trees, or with an existing ancestor not owned by the invoking user.
- Reject Bubblewrap executables with setuid, setgid, or sticky bits in open and revalidation.
- Reject effective GID mismatches in addition to effective UID mismatches.
- Skip stray profile entries with invalid names in `profile list` instead of failing the whole listing; valid names with unsafe layout still error.
- Use `RESOLVE_NO_MAGICLINKS` consistently for skills parent handling.
- Document the path policy: rejected exact `/tmp`, `/var/tmp`, and `/dev/shm` projects, protected `/srv`, `/mnt`, `/media`, and `/snap` subtrees, `/home`-scoped sibling-home rejection, and the user-owned ancestor requirement for custom `XDG_DATA_HOME` locations.
- Resolve symlinked sensitive home trees when validating custom data base locations.

## 0.8.7

- Add `staticcheck` and `actionlint` to local and CI checks, and document the required tools.
- Resolve staticcheck style diagnostics in error strings and package/exported comments.
- Keep plural run-argument guidance in the CLI separator diagnostic.
- Add a `make package` convenience target that reads `internal/app/VERSION` and delegates to the release script.

## 0.8.6

- Prevent unneeded inherited file descriptors from reaching sandbox subprocesses.
- Reject effective UID mismatches, make profile-name validation tests independent of host passwd state, and fail closed for unknown internal command kinds.
- Reject Bubblewrap versions below 0.12.0 across all version components.
- Document accepted runtime, verification, path, and release trade-offs in the synchronized security model.

## 0.8.5

- Explain verification mismatches with expected and observed state; report differing environment variable names without their values. Preserve verification criteria and status handling.

- Add regression tests for incomplete probe inputs, report and profile-output failures, and aggregation of cleanup failures without deleting replacement files.
- Update the live profile-list expectation for aligned columns.

- Clarify directory-type, profile-executable, profile-name, argument-separator, and Bubblewrap-version errors; include full directory paths in profile-creation failures.
- Align profile-list columns, omit unspecified mount modes and empty sources from inspect output, and omit empty verification details.
- Strengthen retained-descriptor, protected-path, non-mutating inspect, environment, and source-error regression tests.

- Document profile executable symlink limitations and workarounds, and clarify the scope of read-only verification in English and Russian. No runtime changes.

- Add optional bilingual manual checks using disposable profiles, with explicit host/sandbox locations and separate shell-transition commands; clarify profile status semantics. The walkthrough is not required before every release. No runtime changes.

## 0.8.4

- Add `abx work <profile>`: open the login shell with the current project writable at `/workspace`, without requiring a matching profile executable. Preserve project-free `shell`, run-only `inspect`, retained-FD execution, and shared isolation policy.
- Add work-mode policy, resource-lifetime, command/environment, and live-shell regression coverage; document same-session Herdr detach/reattach in English and Russian.

- Separate shared-skills preparation and plan validation into focused files without changing runtime policy or resource ownership.
- Use explicit run/shell/work modes in test fixtures and extend shared policy mutations and diagnostic checks across all three modes.

## 0.8.3

- Rewrite and synchronize English/Russian user documentation; separate end-user installation from maintainer CI/Actions workflows.

## 0.8.2

- Use `internal/app/VERSION` as the single release-version source for the binary and packaging; remove release-specific version literals from README and documentation examples and simplify release instructions accordingly.
- Add app-level regression tests for execution ordering, retained-input revalidation, late skills-path replacement, and executor error propagation.
- Extend `make check` with ShellCheck, `govulncheck`, `gosec`, and the static production build; reuse the same check workflow for release tags before artifact packaging.
- Add lifecycle/error regressions for cleanup error aggregation, retained Bubblewrap tool revalidation and execution, descriptor-close ownership, and non-mutating `inspect`; raise aggregate unit coverage from about 66% to about 76%.
- Add a 74% aggregate coverage regression floor to `make check` without adding another CI workflow step.
- Add structural `sandbox.Plan.Validate()` checks after plan construction and before Bubblewrap translation, including explicit plan kind and source-role expectations for home, project, skills, system mounts, and verification-probe policy, with mutation regressions.
- Move profile-name validation into a small shared domain package so CLI parsing and host storage enforce the same invariant without `internal/host -> internal/cli` coupling.
- Extend `inspect` with an additive isolation summary for namespace/network policy, writable mounts, and private runtime filesystems while preserving the existing detailed plan output.

## 0.8.1

- Load a Bubblewrap AppArmor user-namespace profile on the disposable Ubuntu CI runner and report kernel diagnostics on failure.

- Gate tag artifacts on non-root Bubblewrap live tests; branch and pull-request CI runs only make check.
- Document the local passwd account requirement and explicitly accepted TIOCSTI risk.
- Verify user, mount, PID, IPC, and UTS namespace separation directly.
- Disable nested user namespaces for both shell and run, with a live regression check.
- Mount existing /etc/alternatives read-only for system symlink chains.
- Preserve fallback kill errors after failed signal forwarding.

## 0.8.0

- Release the new implementation as 0.8.0, with the same version in source builds and tag artifacts.
- Reject packaging tags that do not match the source version.
- Use POSIX sh examples throughout both documentation languages and clarify installation requirements and verification behavior.
- Preserve the shared shell/run isolation policy and existing profile storage behavior.

## 0.8.0-rc.2

- Reworked README and user guides around installing and running `abx`, with installation, updates, removal, and persistent profile workflows in both languages.
- Excluded AGENTS.md and AGENTS.ru.md from binary archives; both remain in source archives.
- Changed `make build` to produce `./abx`, matching the installed executable name.

## 0.8.0-rc.1

First candidate of the new implementation, targeting 0.8.0. Compatibility with earlier CLIs and profiles is not guaranteed.

- Agent runs and interactive shells share one Bubblewrap policy, persistent profile homes, and a clean environment.
- npm and Bun settings are configured inside profiles. Projects are mounted writable only for run and verify.
- Storage defaults to .local/share/abx in the passwd home; an absolute XDG_DATA_HOME overrides it, while empty and relative values are ignored.
- Explicit profile create, show, and list commands validate ownership, permissions, and paths without automatically repairing existing data.
- Optional shared skills are mounted read-only.
- Inspect shows the execution plan; verify checks a real sandbox without requiring an installed agent.
- Fixed project validation for system trees relocated through symlinks: their resolved targets, descendants, and ancestors are rejected as projects. Added regressions for redirection and unresolved links.
- Sources are retained by descriptor; signals and child exit status are forwarded, and preparation and cleanup failures are reported.
- Unit and live tests, user and developer guides, and documented security boundaries.
- English is the primary README and documentation language, with Russian translations. Added current maintainer instructions in AGENTS.md; the changelog remains English only.
- Commits and pull requests run checks; binary and source archives with checksums are uploaded only on tag pushes.
