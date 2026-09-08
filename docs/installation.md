# Installation and updates

**English** | [Русский](ru/installation.md)

Run all commands in the host terminal as a regular user. Do not install or run ABX with `sudo`.

## Host requirements

ABX requires:

- Linux;
- Bubblewrap **0.12.0 or newer** at `/usr/bin/bwrap`;
- unprivileged user namespaces allowed by the host;
- a local `/etc/passwd` entry for the invoking UID;
- an absolute, existing home directory and a usable login shell from that passwd entry.

The published prebuilt binary targets `linux/amd64` (`x86_64`). Other Linux architectures can be built from source if the Go toolchain and dependencies support them.

Basic checks:

```sh
uname -m
id -u
/usr/bin/bwrap --version
awk -F: -v uid="$(id -u)" '$3 == uid { print; found = 1 } END { exit !found }' /etc/passwd
```

`id -u` must not print `0`. ABX uses the fixed Bubblewrap path `/usr/bin/bwrap`; installing another `bwrap` earlier in PATH does not change that.

The login shell recorded in `/etc/passwd` must exist, be executable, and be visible through the system directories mounted by ABX. Common system shells such as `/bin/bash` and `/usr/bin/fish` work when installed normally by the distribution.

## Install the prebuilt binary

Download these published files for the version you want:

```text
abx-X.Y.Z-linux-amd64.tar.gz
abx-X.Y.Z-SHA256SUMS
```

Keep the archive and manifest from the same version. Verify the download before extracting it:

```sh
abx_version=X.Y.Z
sha256sum --check --ignore-missing "abx-$abx_version-SHA256SUMS"
```

Continue only if the binary archive is reported `OK` and the command succeeds.

Install it into your user-local binary directory:

```sh
tar -xzf "abx-$abx_version-linux-amd64.tar.gz"
mkdir -p "$HOME/.local/bin"
install -m 755 "abx-$abx_version-linux-amd64/abx" "$HOME/.local/bin/abx"
export PATH="$HOME/.local/bin:$PATH"
command -v abx
abx version
```

The version reported by `abx version` should match the downloaded version. The `export` affects only the current shell; add `$HOME/.local/bin` to your shell configuration if necessary.

Installing the launcher does not create profiles. Existing profile data is left untouched when the binary is replaced.

## Build from source

Building requires the Go version specified by `go.mod` and access to the Go module dependencies. Make is convenient but not required.

From a source archive:

```sh
abx_version=X.Y.Z
tar -xzf "abx-$abx_version-source.tar.gz"
cd "abx-$abx_version-source"
make build
```

`make build` creates `./abx` for the current platform with `CGO_ENABLED=0`.

Without Make, use the equivalent command:

```sh
env CGO_ENABLED=0 go build -trimpath -buildvcs=false -o abx ./cmd/abx
```

Install the result:

```sh
mkdir -p "$HOME/.local/bin"
install -m 755 abx "$HOME/.local/bin/abx"
export PATH="$HOME/.local/bin:$PATH"
abx version
```

If you received the source archive with an `abx-X.Y.Z-SHA256SUMS` manifest, verify it before extracting it in the same way as the binary archive.

`make check`, `make live`, and `scripts/package.sh` are development/release commands; they are not required to install or use ABX. See [Development](development.md) if you are working on the project.

## First use

Create a profile and open its shell:

```sh
abx profile create opencode
abx shell opencode
```

Install the agent inside that shell, then leave it with `exit`. See [Usage](usage.md) for examples with Bun and npm.

From a project directory on the host:

```sh
abx verify opencode
abx run opencode
```

## Update ABX

Exit active ABX sessions, install the new binary over the existing one, then check:

```sh
command -v abx
abx version
```

Updating ABX replaces only the launcher. Profiles, installed agents, settings, and authentication stored inside profiles remain where they are. Read [CHANGELOG.md](../CHANGELOG.md) before updating when behavior changes matter to you.

Agent updates are separate from ABX updates and should be performed inside the corresponding profile shell.

## Remove ABX

For the default installation path:

```sh
rm -i "$HOME/.local/bin/abx"
```

This removes only the launcher. Profiles and shared skills remain on disk.

Use `abx profile show <name>` before uninstalling if you need the exact profile location. Removing a profile directory manually removes that profile's installed programs, settings, caches, and saved authentication. ABX has no automatic profile-delete command.

Do not remove the whole `XDG_DATA_HOME`: it may contain data belonging to unrelated applications.
