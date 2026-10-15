# ABX

**English** | [Русский](README.ru.md)

ABX (Agent Box) runs command-line coding agents inside Bubblewrap sandboxes on Linux. Each profile is a persistent private home for an agent or agent toolchain: executables, settings, caches, and authentication stay there between runs.

When you run an agent, the current project is mounted writable at `/workspace`. The profile is writable at `/home/agent`; selected system files are available read-only; temporary files and process/device views are private to the sandbox. Networking is shared with the host. See the [security model](docs/security.md) for the exact boundaries.

## Requirements

- Linux. The published prebuilt binary is for x86-64 (`amd64`).
- Run ABX as a regular user, without `sudo`.
- Bubblewrap **0.12.0 or newer**, installed at `/usr/bin/bwrap` and owned by root. The
  binary must be a regular file, not group- or world-writable, and without
  setuid, setgid or sticky bits.
- Unprivileged user namespaces allowed by the host.
- A local `/etc/passwd` entry for your UID, with a valid home directory and a usable login shell.
- The runtime or package manager required by the agent you want to install, such as Bun or npm.

## Install

Download the archive for the version you want from the project's
[releases page](https://github.com/kydex/abx/releases):

```text
abx-X.Y.Z-linux-amd64.tar.gz
```

Unpack it and install the binary:

```sh
abx_version=X.Y.Z
tar -xzf "abx-$abx_version-linux-amd64.tar.gz"
cd "abx-$abx_version-linux-amd64"
mkdir -p "$HOME/.local/bin"
install -m 755 ./abx "$HOME/.local/bin/abx"
abx version
```

The reported version should match the version you downloaded. Make sure `$HOME/.local/bin` is on your shell's PATH.

If you also downloaded the `abx-X.Y.Z-SHA256SUMS` manifest, you can verify the archive before extracting it:

```sh
abx_version=X.Y.Z
sha256sum --check --ignore-missing "abx-$abx_version-SHA256SUMS"
```

To build instead of using the prebuilt binary, see [Installation](docs/installation.md#build-from-source).

## Quick start

Create a profile. The profile name must match the executable that `run` will start:

```sh
abx profile create opencode
abx shell opencode
```

You are now inside the profile sandbox with `HOME=/home/agent`. For example, install OpenCode with Bun:

```sh
bun add -g opencode-ai
opencode --version
exit
```

Now go to a project on the host and run the agent:

```sh
cd "$HOME/code/my-project"
abx verify opencode
abx run opencode
```

`verify` is optional but useful after installation or host changes. It starts a real sandbox and checks the main isolation properties without requiring the agent executable itself.

A profile can hold [Herdr](https://herdr.dev/) and several agent CLIs, and `work` keeps the sandbox alive when a client detaches from it. The full walkthrough, including detach and reattach, is in [Multi-agent profile with Herdr](docs/usage.md#multi-agent-profile-with-herdr).

## Commands

| Command | Purpose |
|---|---|
| `abx profile create <profile>` | Create an empty persistent profile |
| `abx profile show <profile>` | Show profile path, entrypoint status, and shared-skills status |
| `abx profile list` | List profiles |
| `abx shell <profile>` | Open the profile's login shell without mounting a project |
| `abx work <profile>` | Open a login shell with the current project at `/workspace`; no matching executable required |
| `abx run <profile> [-- <argument>...]` | Run the matching profile entry point with the current directory as `/workspace` |
| `abx inspect <profile>` | Show the `run` plan without starting Bubblewrap; requires the matching profile executable |
| `abx verify <profile>` | Start a test sandbox and verify the main isolation properties |
| `abx help`, `-h`, `--help` | Show CLI help |
| `abx version`, `-v`, `--version` | Show the ABX version |

Profiles are stored under `.local/share/abx/profiles` in the home directory from `/etc/passwd` (not `$HOME`) by default. An absolute `XDG_DATA_HOME` changes the storage base. Use `abx profile show <profile>` when you need the exact profile path.

## Documentation

- [Installation](docs/installation.md) — host requirements, binary/source installation, updates, and removal.
- [Usage](docs/usage.md) — profiles, agent installation, projects, arguments, shared skills, inspect/verify, and troubleshooting.
- [Security model](docs/security.md) — what the sandbox exposes, what it isolates, and remaining risks.
- For contributors: [Architecture](docs/architecture.md), [Development](docs/development.md), and [Maintainer releases and CI](docs/releases.md).
- [Changelog](CHANGELOG.md).

ABX is licensed under the [MIT License](LICENSE). Dependency licensing is listed in [THIRD_PARTY_LICENSES](THIRD_PARTY_LICENSES).
