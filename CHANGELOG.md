# Changelog

## Unreleased

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
