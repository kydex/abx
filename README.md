# ABX

**English** | [Русский](README.ru.md)

ABX (Agent Box) runs command-line coding agents inside Bubblewrap sandboxes on Linux. Each profile is a persistent private home for an agent or agent toolchain: executables, settings, caches, and authentication stay there between runs.

When you run an agent, the current project is mounted writable at `/workspace`. The profile is writable at `/home/agent`; selected system files are available read-only; temporary files and process/device views are private to the sandbox. Networking is shared with the host. See the [security model](docs/security.md) for the exact boundaries.

## Requirements

- Linux. The published prebuilt binary is for x86-64 (`amd64`).
- Run ABX as a regular user, without `sudo`.
- Bubblewrap **0.12.0 or newer** installed at `/usr/bin/bwrap`, with unprivileged user namespaces allowed by the host.
- A local `/etc/passwd` entry for your UID, with a valid home directory and a usable login shell.
- The runtime or package manager required by the agent you want to install, such as Bun or npm.

## Install

Download the binary archive and checksum manifest for the version you want:

```text
abx-X.Y.Z-linux-amd64.tar.gz
abx-X.Y.Z-SHA256SUMS
```

Then verify and install it:

```sh
abx_version=X.Y.Z
sha256sum --check --ignore-missing "abx-$abx_version-SHA256SUMS"
tar -xzf "abx-$abx_version-linux-amd64.tar.gz"
mkdir -p "$HOME/.local/bin"
install -m 755 "abx-$abx_version-linux-amd64/abx" "$HOME/.local/bin/abx"
export PATH="$HOME/.local/bin:$PATH"
abx version
```

The reported version should match the version you downloaded. Add `$HOME/.local/bin` to your shell's PATH permanently if it is not already there.

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

A profile can hold [Herdr](https://herdr.dev/) and several agent CLIs. For detach/reattach, use `work` so that leaving the client returns to a shell in the same sandbox.

On the host, create a profile and open a shell for installation:

```sh
abx profile create herdr
abx shell herdr
```

Inside ABX, install the tools, then return to the host:

```sh
curl -fsSL https://herdr.dev/install.sh | sh
bun add -g opencode-ai
# Install Pi or other agent CLIs with their normal installer.
exit
```

On the host, enter the project and open its working shell:

```sh
cd "$HOME/code/my-project"
abx work herdr
```

Inside ABX, start Herdr:

```sh
herdr
```

ABX mounts the host project at `/workspace`. If Herdr opens its first workspace in `/home/agent`, create a project workspace explicitly from inside ABX:

```sh
herdr workspace create --cwd /workspace --label project --focus
```

After detach, run `herdr` again in the remaining shell. `exit` from that shell ends the ABX session; the next `abx work` creates a new sandbox. Herdr and its agents share the profile and project; use separate profiles for separate state or credentials.

For a direct launch without an outer shell, run `abx run herdr` on the host instead. See [working in a project](docs/usage.md#project-shell-work).

## Commands

| Command | Purpose |
|---|---|
| `abx profile create <name>` | Create an empty persistent profile |
| `abx profile show <name>` | Show profile path, entrypoint status, and shared-skills status |
| `abx profile list` | List profiles |
| `abx shell <name>` | Open the profile's login shell without mounting a project |
| `abx work <name>` | Open a login shell with the current project at `/workspace`; no matching executable required |
| `abx run <name> [-- <argument>...]` | Run the matching profile entry point with the current directory as `/workspace` |
| `abx inspect <name>` | Show the `run` plan without starting Bubblewrap; requires the matching profile executable |
| `abx verify <name>` | Start a test sandbox and verify the main isolation properties |
| `abx help` | Show CLI help |
| `abx version` | Show the ABX version |

Profiles are stored under `~/.local/share/abx/profiles` by default. An absolute `XDG_DATA_HOME` changes the storage base. Use `abx profile show <name>` when you need the exact profile path.

## Documentation

- [Installation](docs/installation.md) — host requirements, binary/source installation, updates, and removal.
- [Usage](docs/usage.md) — profiles, agent installation, projects, arguments, shared skills, inspect/verify, and troubleshooting.
- [Security model](docs/security.md) — what the sandbox exposes, what it isolates, and remaining risks.
- For contributors: [Architecture](docs/architecture.md), [Development](docs/development.md), and [Maintainer releases and CI](docs/releases.md).
- [Changelog](CHANGELOG.md).

ABX is licensed under the [MIT License](LICENSE). Dependency licensing is listed in [THIRD_PARTY_LICENSES](THIRD_PARTY_LICENSES).
