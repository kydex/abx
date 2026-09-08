# Maintainer instructions

**English** | [Русский](AGENTS.ru.md)

These instructions apply to the whole repository. English is the primary version; the Russian file is a translation. Explicit user instructions take precedence.

## Before changing code

Read README and the relevant guides in docs. For execution, path, mount, or environment changes, read docs/security.md and docs/architecture.md plus the affected implementation and tests. Preserve unrelated work and keep each change scoped to a concrete problem.

Use [Usage](docs/usage.md) for user-visible behavior, [Security](docs/security.md) for protection boundaries, and [Architecture](docs/architecture.md) for structure and package responsibilities. Inspect the implementation and tests for conformance. Historical designs are reference material, not additional requirements. Compatibility with earlier profile layouts or CLI behavior is not guaranteed.

## Implementation

- Prefer straightforward Go, concrete types, small functions, and explicit resource ownership. Add abstractions only for a demonstrated need; avoid speculative backends, registries, configuration layers, and blanket defensive machinery.
- Keep responsibilities in their existing packages: cli parses, host selects and validates sources, sandbox builds policy, bwrap executes, app coordinates, and probe observes.
- `run`, `shell`, and `work` share the same base isolation policy. `run` mounts the current project and starts the matching profile executable. `shell` omits the project and does not read the host working directory. `work` mounts the current project and starts the login shell without requiring a matching profile executable. Both shell modes start the passwd shell with `-l`; their initial working directories are `/home/agent` for `shell` and `/workspace` for `work`. Tools launched inside either shell inherit its sandbox. `inspect` describes only the `run` plan; `verify` reuses the shared isolation policy rather than building an independent sandbox.
- Preserve `Plan.Validate()` after plan construction and before translation into Bubblewrap arguments. Keep its structural checks on expected sources, mounts, working directory, and command/probe layout.
- Pass subprocess arguments as structured argv. Never interpolate user, project, profile, or agent values into a shell command.
- Preserve retained source descriptors and their ownership through execution. Do not replace FD mounts with pathname reopening. Keep preparation after runtime validation and report preparation, execution, and cleanup failures accurately.
- Do not silently weaken checks, repair existing permissions, or recursively delete user data as rollback. Respect the documented residual risks; do not promise complete same-uid race protection or add machinery to imply it.
- Use the current ubuntu-latest baseline and CachyOS live target. Do not add old-kernel compatibility paths, a kernel-version parser, or extra runtime dependencies without a concrete task requirement.

## Verification

Use focused behavior or regression tests for meaningful changes, particularly path boundaries, permissions, resource lifetime, and failure handling. Do not add tests just to mirror implementation or meet a coverage percentage.

Run relevant tests during implementation and `make check` for production Go changes. Use `make live` on a supported non-root host when isolation, process launch, signal handling, working directory, or session lifetime changes, even if the namespace and mount configuration is unchanged. If that environment is unavailable, report the missing validation and give a focused follow-up; never describe mocks or skipped tests as a successful live run. Documentation-only changes need link and content checks; packaging changes need an archive smoke check.

Use temporary storage for tests. Never create test profiles in the user's real home or use a working profile as a writable test mount.

## Documentation and delivery

- Keep README.md and docs/*.md in English, with README.ru.md and docs/ru/*.md synchronized Russian translations. Keep AGENTS.ru.md aligned with this file. Language links must work in packaged artifacts. AGENTS.md and AGENTS.ru.md belong only in source archives; user installation and usage examples use the executable name abx.
- Keep CHANGELOG.md in English only. Add user-visible changes under Unreleased and preserve published history. For a release, move the intended entries into the versioned section and update `internal/app/VERSION`, the single release-version source. Follow [Releases](docs/releases.md) for validation and packaging.
- Documentation commands must use POSIX sh syntax and sh code fences. Keep one-off logs, test projects, and generated binaries out of the source archive.
- CI stays simple: checks on branch pushes and pull requests; artifacts only on version-tag pushes. Use floating action version tags, not SHA pins. Do not add GitHub Release publication or repository write permissions as an incidental change.
- Preserve executable mode 0755 for scripts/*.sh and include both documentation languages in packaging.
- Report what changed, what was actually verified, and remaining limitations. When providing a download archive, include its SHA-256.

Continue routine implementation and reversible checks within the authorized task. Ask for a decision only when an unresolved choice materially changes the requested behavior, security boundary, data handling, or publication scope; existing user decisions remain in force.
