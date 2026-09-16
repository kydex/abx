# Security model

**English** | [Русский](ru/security.md)

This document describes the boundaries enforced by the current implementation. For normal commands and examples, see [Usage](usage.md).

## What ABX isolates

`run`, `shell`, and `work` use the same sandbox policy. `shell` simply omits the project mount and starts the account's login shell. `work` mounts the same project as `run` but starts the login shell at `/workspace`.

| Resource | `run` / `work` / `verify` | `shell` |
|---|---|---|
| Profile home | `/home/agent`, writable | Same |
| Current project | `/workspace`, writable, cwd | Not mounted |
| Shared skills, if configured | `/home/agent/.agents/skills`, read-only | Same |
| Selected system files | Read-only | Same |
| `/tmp`, `/var/tmp`, `/run` | Private tmpfs | Same |
| `/proc`, `/dev` | Private/separate views | Same |
| Network | Shared with host | Shared with host |

Bubblewrap creates separate user, mount, PID, IPC, and UTS namespaces and sets the hostname to `abx`. ABX also passes `--disable-userns`, so code inside the sandbox cannot create another user namespace through Bubblewrap's supported mechanism.

The environment starts empty. ABX sets HOME, USER/LOGNAME, SHELL, PWD, TMPDIR, XDG paths, PATH, and npm/Bun locations. Only terminal/locale/color settings are forwarded from the host: `TERM`, `COLORTERM`, `LANG`, `LANGUAGE`, `TZ`, `NO_COLOR`, `FORCE_COLOR`, and valid `LC_*` variables.

Arbitrary host environment variables, including proxy, credential, and loader/runtime injection variables, are not inherited automatically. The host SSH-agent socket is not exposed.

## Files visible from the host

ABX exposes `/usr` read-only and, when present, the compatibility paths `/bin`, `/sbin`, `/lib`, and `/lib64`. It also exposes a limited set of `/etc` resources needed by typical command-line programs, such as account/group data, DNS configuration, certificates, timezone data, dynamic-loader configuration, OS metadata, shells, and alternatives.

The entire host `/etc`, host home, and host `/run` are not mounted into the sandbox.

ABX does not mount the account home as a whole. The selected profile is mounted writable; `run` and `work` additionally mount only the selected current project writable.

## Filesystem checks

ABX treats its storage and profiles as private data. Required ABX-owned directories must be real directories, belong to the invoking user, and have exact mode `0700` where the policy requires it. Unsafe existing objects are rejected rather than automatically repaired.

The current project is canonicalized before use. ABX rejects projects that overlap the filesystem root, the account home itself, ABX storage, protected system trees, or sensitive home trees such as `.config`, `.ssh`, `.gnupg`, and `dotfiles`.

Selected host sources are opened and retained by file descriptor. Before execution ABX revalidates them, and Bubblewrap receives bind sources through retained FDs rather than reopening the same path. This narrows pathname-replacement races between validation and mounting; it does not make the contents of writable files or directories immutable.

The in-memory sandbox `Plan` is structurally validated when it is built and again before Bubblewrap translation. The validator checks the expected system sources, private mounts, profile/project roles, optional skills mount, working directory, command, and verification probe layout.

## Shared skills

If `<ABX storage>/shared/agents/skills` exists and passes validation, it is mounted read-only at `/home/agent/.agents/skills`.

The shared path is optional. ABX checks the relevant directories and rejects unsafe symlink redirection at the profile mount target. Preparation may leave empty `.agents` / `.agents/skills` directories in the writable profile; the shared content itself remains mounted read-only.

## Verification

`abx verify <profile>` launches the same sandbox backend used for normal execution, with one additional fixed read-only probe executable. It checks observed namespaces, cwd, hostname, environment, private mounts/views, read-only resources, writable profile/project access, and selected access boundaries.

For read-only resources, the probe checks the mount flags at each selected mountpoint in `/proc/self/mountinfo`. It does not independently inspect every nested mount or attempt writes to those resources. Read-only enforcement is delegated to Bubblewrap; a `PASS` for a parent mountpoint is not a separate verification of all its nested mounts.

A successful `verify` is evidence about that invocation and host state. It is not a permanent guarantee about later runs.

## Remaining risks

- The agent can modify or delete anything in the writable profile and selected project.
- Networking is shared, so sandboxed code can access network services reachable from the host and can transmit data it can read.
- Project contents are not recursively sanitized. Projects may contain sockets, hard links, nested mounts, or intentionally dangerous source/build files.
- The sandbox uses the caller's terminal directly. Terminal escape sequences and host-specific terminal ioctls remain part of the risk surface; ABX does not provide terminal-session isolation.
- ABX does not define a seccomp policy or resource quotas. Security still depends on the Linux kernel, Bubblewrap, the host configuration, and the software executed inside the sandbox.
- A hostile process running concurrently as the same host UID may still race changes in data that the user can modify. Retained descriptors and revalidation reduce important pathname-replacement windows but are not a complete same-UID adversary model.
- Some failed operations may leave directories or verification probe files behind. ABX avoids recursive automatic rollback that could delete unrelated user data.

Back up important project and profile data according to your normal workflow; writable access is intentional.
