# Maintainer instructions

**English** | [Русский](AGENTS.ru.md)

These instructions apply to the whole repository. English is the primary version; the Russian file is a translation. Explicit user instructions take precedence.

## Before changing code

Read README and the relevant guides in docs. For execution, path, mount, or environment changes, read docs/security.md and docs/architecture.md plus the affected implementation and tests. Preserve unrelated work and keep each change scoped to a concrete problem.

Each guide answers one question: [Usage](docs/usage.md) for user-visible behavior, [Security](docs/security.md) for protection boundaries, [Architecture](docs/architecture.md) for structure and package responsibilities. Inspect the implementation and tests for conformance rather than trusting this file. Historical designs are reference material, not additional requirements. Compatibility with earlier profile layouts or CLI behavior is not guaranteed.

## Implementation

- Prefer straightforward Go, concrete types, small functions, and explicit resource ownership. Add abstractions only for a demonstrated need; avoid speculative backends, registries, configuration layers, and blanket defensive machinery.
- Keep responsibilities in their existing packages: cli parses, host selects and validates sources, sandbox builds policy, bwrap executes, app coordinates, probe observes.
- `run`, `shell`, and `work` share one base isolation policy and differ only in what they mount and what they start; `verify` reuses that policy rather than building a sandbox of its own. Read their current behavior in [Architecture](docs/architecture.md) instead of assuming it from this file.
- Preserve `Plan.Validate()` after plan construction and before translation into Bubblewrap arguments. Keep its structural checks on expected sources, mounts, working directory, and command/probe layout.
- Pass subprocess arguments as structured argv. Never interpolate user, project, profile, or agent values into a shell command.
- Preserve retained source descriptors and their ownership through execution. Do not replace FD mounts with pathname reopening. Keep preparation after runtime validation and report preparation, execution, and cleanup failures accurately.
- Do not silently weaken checks, repair existing permissions, or recursively delete user data as rollback.
- Respect the documented residual risks: do not promise complete same-uid race protection and do not add machinery that implies such a guarantee.
- Use the current ubuntu-latest baseline and CachyOS live target. Do not add old-kernel compatibility paths, a kernel-version parser, or extra runtime dependencies without a concrete task requirement.

## Verification

Use focused behavior or regression tests for meaningful changes, particularly path boundaries, permissions, resource lifetime, and failure handling. Do not add tests just to mirror implementation or meet a coverage percentage.

Run relevant tests during implementation and `make check` for production Go changes. Use `make live` on a supported non-root host when isolation, process launch, signal handling, working directory, or session lifetime changes, even if the namespace and mount configuration is unchanged. If that environment is unavailable, report the missing validation and give a focused follow-up; never describe mocks or skipped tests as a successful live run. Documentation-only changes need link and content checks; packaging changes need an archive smoke check.

Use temporary storage for tests. Never create test profiles in the user's real home or use a working profile as a writable test mount.

## Commits

One commit does one thing. Write the subject as `<type>(<scope>): <description>` — lowercase, no trailing period, at most 72 characters including the prefix. Put anything longer in the body. This follows [Conventional Commits](https://www.conventionalcommits.org/), with one addition.

| Type | Use for |
|---|---|
| `feat` | new user-visible capability |
| `fix` | a defect |
| `docs` | documentation only, code comments included |
| `test` | tests only |
| `refactor` | restructuring, behavior unchanged |
| `perf` | performance |
| `build` | build, packaging, dependencies, linters |
| `ci` | pipelines and automation |
| `chore` | anything invisible to the user |
| `revert` | undoing a change |
| `release` | cutting a version; an extension to the specification |

Scopes in use: `host`, `sandbox`, `bwrap`, `app`, `cli`, `probe`, `profile`, `packaging`, `docs`, `ci`, `live`. The scope is optional.

Mark a breaking change with `!` after the type and/or a `BREAKING CHANGE:` footer saying what changed and why. Messages are English.

Every user-visible change gets a `CHANGELOG.md` entry under `Unreleased` **in the same commit as the change**. A test-only commit that changes nothing a user sees does not. A release commit moves the accumulated entries into the version section.

## Documentation and delivery

- Keep README.md and docs/*.md in English, with README.ru.md and docs/ru/*.md synchronized Russian translations. Keep AGENTS.ru.md aligned with this file. Language links must work in packaged artifacts. AGENTS.md and AGENTS.ru.md belong only in source archives; user installation and usage examples use the executable name abx.
- Keep CHANGELOG.md in English only and preserve published history. On release, move the entries into the version section and update `internal/app/VERSION`, the single release-version source.
- [Releases](docs/releases.md) is the release procedure, including why a version tag must never be pushed with `--force`.
- Documentation commands must use POSIX sh syntax and sh code fences. Keep one-off logs, test projects, and generated binaries out of the source archive.
- CI stays simple: checks on branch pushes and pull requests; artifacts only on version-tag pushes. Use floating action version tags, not SHA pins. Do not add GitHub Release publication or repository write permissions as an incidental change.
- Preserve executable mode 0755 for scripts/*.sh and include both documentation languages in packaging.
- Report what changed, what was actually verified, and remaining limitations. When providing a download archive, include its SHA-256.

## Asking the user

Continue routine implementation and reversible checks within the authorized task. Ask for a decision only when an unresolved choice materially changes the requested behavior, security boundary, data handling, or publication scope. Existing user decisions remain in force.