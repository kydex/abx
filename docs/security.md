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
| `/tmp`, `/var/tmp`, `/run` | Private tmpfs, 4 GiB each | Same |
| `/proc`, `/dev` | Private/separate views | Same |
| Network | Shared with host | Shared with host |

Bubblewrap creates separate user, mount, PID, IPC, and UTS namespaces and sets the hostname to `abx`. ABX also passes `--disable-userns`, so code inside the sandbox cannot create another user namespace through Bubblewrap's supported mechanism. ABX passes `--die-with-parent`, so the sandbox processes are killed with SIGKILL when the launcher exits. This covers a running sandbox; a signal arriving while Bubblewrap is still setting up the sandbox, before its parent-death signal is armed, may leave an orphaned sandbox behind. Two limits are worth stating plainly. Bubblewrap installs a handler only for `SIGCHLD`, so any signal ABX forwards terminates the monitor by the default action, which arms the parent-death signal and kills the payload with SIGKILL even while a shutdown handler is running — a terminal `Ctrl+C` in `shell` or `work` behaves the same way, because ABX passes neither `--new-session` nor a separate process group and the signal reaches the monitor and the payload together. A background job started inside the sandbox can outlive the session and keep its tmpfs allocations and network access. Measured on a live host: `--die-with-parent` kills the command Bubblewrap started, while a process put in the background beforehand keeps running after that command is gone. The parent-death signal is not inherited across `fork(2)`, so it is not what stops the background process.

The environment starts empty. ABX sets HOME, USER/LOGNAME, SHELL, PWD, TMPDIR, XDG paths, PATH, and npm/Bun locations. Only terminal/locale/color settings are forwarded from the host: `TERM`, `COLORTERM`, `LANG`, `LANGUAGE`, `TZ`, `NO_COLOR`, `FORCE_COLOR`, and valid `LC_*` variables.

Arbitrary host environment variables, including proxy, credential, and loader/runtime injection variables, are not inherited automatically. The host SSH-agent socket is not exposed.

## Files visible from the host

ABX exposes `/usr` read-only and, when present, the compatibility paths `/bin`, `/sbin`, `/lib`, and `/lib64`. It also exposes a limited set of `/etc` resources, when present on the host, needed by typical command-line programs, such as account/group data, DNS configuration, certificates, timezone data, dynamic-loader configuration, OS metadata, shells, and alternatives. Missing files are skipped silently; `abx inspect` prints the actually discovered list.

The entire host `/etc`, host home, and host `/run` are not mounted into the sandbox. Base directories that no later mount covers stay writable inside the sandbox as ephemeral scratch; the agent can only plant files there against itself.

ABX does not mount the account home as a whole. The selected profile is mounted writable; `run` and `work` additionally mount only the selected current project writable.

## Filesystem checks

ABX treats its storage and profiles as private data. Required ABX-owned directories must be real directories, belong to the invoking user, and have exact mode `0700` where the policy requires it. Unsafe existing objects are rejected rather than automatically repaired.

The current project is canonicalized before use. ABX rejects projects that overlap the filesystem root, the account home itself, ABX storage, protected system trees, or sensitive home trees (`.config`, `.ssh`, `.gnupg`, `dotfiles`, `.aws`, `.docker`, `.kube`, `.pki`, `.cache`, `.netrc`, `.git-credentials`, `.password-store`, `.npmrc`, `.gem/credentials`, `.local/share/keyrings`, `.mozilla`, and `.thunderbird`).

Selected host sources are opened and retained by file descriptor. Before execution ABX revalidates them, and Bubblewrap receives bind sources through retained FDs rather than reopening the same path. This narrows pathname-replacement races between validation and mounting; it does not make the contents of writable files or directories immutable. Before execution ABX also marks every inherited file descriptor close-on-exec, except the standard streams and the descriptors it hands to the child: the retained mount sources, the retained Bubblewrap binary, and, for `verify`, the descriptor for `/proc/self/exe`. Every other descriptor, including ABX's own retained storage descriptors, which are never mount sources, is marked, so unrelated host descriptors never reach the sandbox. The original flags are restored on the launcher side afterwards.

The in-memory sandbox `Plan` is structurally validated when it is built and again before Bubblewrap translation. The validator checks the expected system sources, private mounts, profile/project roles, optional skills mount, working directory, command, and verification probe layout.

ABX additionally validates the host objects it depends on: the two it executes, the account database it reads, the account home every session runs against, its own storage, and the place the agent executable really lives.

| Object | Check |
|---|---|
| `/usr/bin/bwrap` | Regular file, executable, no setuid/setgid/sticky bits, not writable by group/other, owned by root; before execution it must still resolve to the same inode |
| Login shell from `/etc/passwd` | Resolved target is a regular file, executable by the invoking user, not writable by group/other, no setuid/setgid/sticky bits; both the passwd path and its resolved target must be visible in the sandbox |
| Account home from `/etc/passwd` | A real directory, not world-writable, owned by the invoking UID |
| ABX storage and profile directories | Real directories, exact mode `0700` with no setuid/setgid/sticky bits, owned by the invoking UID |
| Profile executable | A regular file, executable by the invoking user, not writable by group/other, no setuid/setgid/sticky bits; its real file must be reachable from what ABX mounts, so it exists inside the sandbox instead of failing at exec time with the tool blamed |
| Mounted system sources (`/usr`, `/etc` files) | Trusted host configuration: each source is resolved against symlinks, rejected on overlap with the home or storage, and held by descriptor; the source and the intermediate directories leading to it are not checked for owner or group/other-writability (see Accepted trade-offs) |
| Compatibility symlinks (`/bin`, `/sbin`, `/lib`, `/lib64`) | Link text must be a relative path resolving inside `/usr`, and the resolved target must land under a mounted system source and not overlap the home or storage; the link is not held by descriptor, its mode and owner are not checked, and its text is reverified before execution |

The account database is reached by two different routes, and which files they read depends on how ABX was built. The login shell is always read from the literal `/etc/passwd`. The account record itself (`home`, `username`) comes from `os/user.LookupId`. The shipped static binary sets `CGO_ENABLED=0`, where `os/user` uses its own implementation and reads that same literal `/etc/passwd`, so the row above covers both routes. Built with cgo, `LookupId` goes through NSS and reads `nsswitch.conf` instead; there a host that resolves `passwd` through `sss` or `ldap` makes the shell lookup fail while `id` and `getent` still report the account correctly, and ABX refuses. Build from source with `CGO_ENABLED=0` to keep both routes covered. A local `/etc/passwd` entry for the invoking UID is required either way; that requirement is stated in [Installation](installation.md) as well.

Ownership of the login shell is deliberately not required: user-managed shells also live under system paths (for example `/usr/local/bin`), and the visibility rule already limits them to mounted system trees. That visible list is static: on a host missing one of its locations, a shell there still passes resolution and fails only at exec time.

The mounted `/etc/shells` is provided for programs inside the sandbox; login-shell validation does not consult it. The built-in checks above are stricter on most systems, and `/etc/shells` on some distributions lists non-system paths.

The account database is looked up twice by name: once for its mode, and once more to read the contents. Each lookup resolves the path again, so unlike the Bubblewrap binary -- which ABX opens once and holds the descriptor until execution -- nothing pins the checked file and the read file to the same inode. Replacing the file between the two lookups needs write access to `/etc/passwd`, which the mode check itself forbids for group and other, so it requires the owner. A host whose account database an unprivileged user owns or can rewrite is therefore outside what ABX protects: that user already controls the host account that ABX reads, and the same file is read again through `os/user` without these checks at all. Holding one descriptor across both lookups would close the window and change nothing else.

## Shared skills

If `<ABX storage>/shared/agents/skills` exists and passes validation, it is mounted read-only at `/home/agent/.agents/skills`.

The shared path is optional. ABX checks the relevant directories and rejects unsafe symlink redirection at the profile mount target. Preparation may leave empty `.agents` / `.agents/skills` directories in the writable profile; the shared content itself remains mounted read-only.

## Verification

`abx verify <profile>` launches the same sandbox backend used for normal execution, with one additional fixed read-only probe executable. It checks observed namespaces, cwd, hostname, environment, private mounts/views, read-only resources, writable profile/project access, and selected access boundaries.

The `__probe` entry point is private: it runs only inside the sandbox, where `/abx-probe` is a read-only mount of the ABX binary, and host-side invocations are rejected.

For read-only resources, the probe checks the mount flags at each selected mountpoint in `/proc/self/mountinfo`. It does not independently inspect every nested mount or attempt writes to those resources. Read-only enforcement is delegated to Bubblewrap; a `PASS` for a parent mountpoint is not a separate verification of all its nested mounts.

A successful `verify` is evidence about that invocation and host state. It is not a permanent guarantee about later runs.

## Remaining risks

- The agent can modify or delete anything in the writable profile and selected project.
- Networking is shared, so sandboxed code can access network services reachable from the host and can transmit data it can read.
- Project contents are not recursively sanitized. Projects may contain sockets, hard links, nested mounts, or intentionally dangerous source/build files.
- The sandbox uses the caller's terminal directly. Terminal escape sequences and host-specific terminal ioctls remain part of the risk surface; ABX does not provide terminal-session isolation.
- ABX does not define a seccomp policy or cgroup resource quotas. ABX sets the 4 GiB cap only on its own three tmpfs mounts, `/tmp`, `/var/tmp` and `/run`. Everything else the sandbox gets as writable without a cap: the `--dev` mount builds its own tmpfs, and the sandbox root is created by Bubblewrap, for which ABX sets no size at all. Security still depends on the Linux kernel, Bubblewrap, the host configuration, and the software executed inside the sandbox.
- A hostile process running concurrently as the same host UID may still race changes in data that the user can modify. Retained descriptors and revalidation reduce important pathname-replacement windows but are not a complete same-UID adversary model. The known residual windows — agent-executable or login-shell replacement between inspection and execution, profile entries changing between listing and inspection, data-base components appearing between canonicalization and use, and pathname-based metadata rechecks — are accepted as documented and are not tracked as findings. Mounts always use the retained descriptors, so a late swap cannot redirect what is mounted.
- Some failed operations may leave directories or verification probe files behind. ABX avoids recursive automatic rollback that could delete unrelated user data.

## Accepted trade-offs

The following are deliberate current boundaries rather than pending implementation work:

- **Runtime isolation:** networking is shared, the caller terminal is used directly without terminal-session isolation, no seccomp policy or resource quotas are defined, and project contents and nested mounts are not recursively sanitized. The environment is sanitized only against NUL bytes, so a host value carrying a newline reaches the agent unchanged; `TZ`, `LANG`, `LANGUAGE` and `LC_*` stay unrestricted in content but bounded to 64 KiB per value, which libc uses as files under `/usr/share`. The identity of the process inside the sandbox is neither documented nor checked.
- **Path assumptions:** projects overlapping protected system trees are rejected including subdirectories (`/bin`, `/boot`, `/dev`, `/etc`, `/lib`, `/lib64`, `/opt`, `/proc`, `/root`, `/run`, `/sbin`, `/sys`, `/usr`, `/var`, `/srv`, `/mnt`, `/media`, `/snap`); `/tmp` is rejected only as an exact path, while `/var/tmp` and `/dev/shm` are rejected as subdirectories of protected trees; sibling homes outside the real home are rejected only under `/home`; a project inside the invoking user's own home is still rejected when that home itself sits under a protected tree such as `/var` — there is no carve-out, so `run` and `work` cannot be used from such a home; intermediate project-symlink replacement and the early-start signal race are not fully closed; system symlink aliases rely on trusted host configuration; and the sensitive-home denylist is intentionally finite. The account home is checked for ownership and world-writability but not for setuid or setgid, unlike the storage ancestor beside it, and the intermediate shared-skills directory is not checked for exact mode. Missing storage components are created with mode `0700`; the nearest existing ancestor of a custom `XDG_DATA_HOME` must already be a user-owned directory that is not world-writable unless it carries the sticky bit, and must carry no setuid or setgid bit: a setgid directory makes everything created under the storage root inherit a group the check never inspects. The account home must be owned by the invoking UID. `/etc/passwd` is not required to be owned by root: it must be a regular file without setuid/setgid/sticky bits that group and other cannot write, because the entry it yields names the home every session runs against. Replacing it between the two lookups needs the owner, since the mode check forbids group and other from writing it — see the double-lookup paragraph above. Group-writable ancestors are accepted: that is a team policy, not an attack, and refusing it breaks machines running umask 002. That acceptance extends to the intermediate directories of the login shell, the agent executable, and the mounted system sources: ABX checks the final file, not the directories leading to it, so a group member able to write a parent directory could substitute the binary a session runs. Closing it would break the same team machines, so the risk is accepted as documented. The profile executable may be a symlink into a mounted system path such as `/usr`, or into the profile itself, which is how a package manager installs it; a link resolving outside both is refused while the profile is resolved, because nothing is mounted there and the session would otherwise fail at exec time. The current project is not constrained by ownership or mode at all — team checkouts, shared volumes, and filesystems without POSIX modes (which report `0777` for everything) are legitimate.
- **Verification and diagnostics:** `verify` is a smoke test rather than a complete security attestation; probe metadata travels through argv; cleanup races and residue remain best-effort; Bubblewrap setup errors and payload exit code `1` are not distinguished; and the retained Bubblewrap FD is accepted.
- **Release and CI:** source packaging assumes a clean checkout, CI action and tool versions float, live tests gate release tags rather than pull requests, and archives are not byte-for-byte reproducible or signed/attested.

Back up important project and profile data according to your normal workflow; writable access is intentional.
