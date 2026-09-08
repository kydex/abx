# Architecture

**English** | [Русский](ru/architecture.md)

This document is for contributors. User-visible behavior is described by [Usage](usage.md), [Installation](installation.md), and the [Security model](security.md).

ABX deliberately keeps the runtime small: host resources are selected and retained, a sandbox plan is built and validated, and one Bubblewrap backend executes that plan.

## Packages

| Package | Responsibility |
|---|---|
| `cmd/abx` | Process entry point and standard streams |
| `internal/cli` | Parse public CLI arguments without reading host state |
| `internal/profile` | Profile-name domain validation shared by CLI and host code |
| `internal/app` | Command orchestration, resource lifetime, diagnostics, inspect, and verify |
| `internal/host` | Account/path resolution, profiles, project policy, host source selection, retained descriptors, and revalidation |
| `internal/sandbox` | Clean environment, mounts, commands, plan construction, and structural plan validation |
| `internal/bwrap` | Validate the fixed Bubblewrap executable, translate a plan to argv/FDs, execute, forward signals, and wait |
| `internal/probe` | Observations made inside the sandbox by `verify` |

The important boundary is:

```text
host state -> host.Inputs -> sandbox.Plan -> bwrap translation/execution
```

`host` decides which host objects are eligible. `sandbox` decides how eligible objects are exposed. `bwrap` implements that plan and should not invent policy of its own.

## Normal `run` / `shell` / `work` flow

1. `app` rejects root execution and `cli` parses the command.
2. `host` resolves the passwd account, ABX storage, profile, system sources, optional shared skills, and shell. `run` and `work` also resolve the current project; only `run` requires the matching agent executable.
3. Sensitive host sources are opened and retained in `host.Inputs`.
4. `sandbox.Session` builds the clean environment; `sandbox.Build` creates a `Plan` and validates its structural invariants.
5. `app` opens and validates `/usr/bin/bwrap` and retains that executable too.
6. Optional mountpoint preparation runs, then selected host sources are revalidated.
7. `bwrap.Translate` validates the `Plan` again, maps retained sources to `--ro-bind-fd` / `--bind-fd`, and builds the remaining Bubblewrap arguments.
8. Bubblewrap is executed through its retained descriptor. Standard streams are connected directly; INT/TERM/HUP are forwarded.
9. `app` closes owned resources. Cleanup failures are not hidden by an otherwise successful child exit.

`run` mounts the project and starts the agent. `shell` omits the project and starts the passwd login shell. `work` mounts the project and starts the same login shell at `/workspace`. Their isolation policy otherwise comes from the same plan construction. An explicit mode is retained in `host.Inputs`; `app` maps it to `sandbox.Input` mode and selects a consistent `PWD`. `Plan` retains a distinct `planWork` that checks expected sources, mounts, and cwd; host/app tests check shell argv selection.

Shared-skills discovery and mountpoint preparation live in `internal/host/skills.go`; descriptor ownership remains in `host.Inputs`. Structural plan validation lives in `internal/sandbox/validate.go` and still runs after construction and before Bubblewrap translation.

## `inspect`

`inspect` resolves the normal run inputs and builds the same `Plan`, then formats it without opening Bubblewrap or preparing mountpoints. The filesystem summary is derived from the plan. Namespace/network text is covered by a regression test against the Bubblewrap translation flags.

`inspect` still describes only `run` and requires the profile executable. It describes intended execution; it does not prove that the host can start the sandbox.

## `verify`

`verify` resolves a profile and project without requiring the agent executable. It uses the same plan builder and Bubblewrap backend, adding only a fixed read-only mount of ABX itself as `/abx-probe`.

The probe reports observations from inside the sandbox. `app` also owns temporary host-side sentinel files and cleanup. Final success is printed only after those resources are cleaned up.

## Design rules

- Keep public syntax in `cli`, host eligibility in `host`, sandbox visibility/environment in `sandbox`, and process execution in `bwrap`.
- Keep ownership of files/processes explicit and close them in the layer that owns them.
- Do not replace retained-FD mounts with a second pathname lookup after validation.
- Treat `Plan.Validate()` as a final structural boundary before backend translation.
- Add abstractions only when they solve a concrete problem; avoid package/interface layers that only rename existing operations.
- Update English and Russian documentation together when behavior changes.
