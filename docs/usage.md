# Usage

**English** | [Русский](ru/usage.md)

Run `abx` commands from the host terminal as a regular user. Use `abx shell` when you need to install or configure software inside a profile.

## Choose a command

| Task | Command |
|---|---|
| Install or configure tools without project access | `abx shell <profile>` |
| Start the profile executable directly in the current project | `abx run <profile>` |
| Open a shell with access to the current project | `abx work <profile>` |

## Profiles

A profile is a persistent home directory for an agent or a related toolchain. `abx run <name>` still launches an executable with the same name as the profile; that entry point may start other agent CLIs installed in the same profile.

```sh
abx profile create opencode
abx profile show opencode
abx profile list
```

Profile names are 1–64 ASCII characters. The first character must be a letter or digit; later characters may also contain `.`, `_`, and `-`.

By default, profiles are stored under:

```text
~/.local/share/abx/profiles/<name>
```

ABX uses the home directory from `/etc/passwd`, not the host `HOME` environment variable. If `XDG_DATA_HOME` is an absolute path, the storage root becomes `<XDG_DATA_HOME>/abx`. Relative values are ignored.

`profile create` creates a new empty profile and refuses to overwrite an existing one. `profile show` reports the actual path and whether the matching entry-point executable and optional shared skills are usable. `profile list` lists all profiles.

`profile show` reports a missing profile as `State: absent` and returns status `0`; it is not an existence check by exit code. Command availability is a host-side file check, not proof that the tool or sandbox can start. Mandatory layout errors still fail the command.

ABX does not automatically rename, migrate, or delete profiles.

## Install agents and tools in a profile

Open the profile shell:

```sh
abx shell opencode
```

Inside the sandbox:

- `HOME=/home/agent`;
- the working directory is `/home/agent`;
- the host project is not mounted;
- `/home/agent/.local/bin`, `/home/agent/.bun/bin`, and `/home/agent/bin` are at the front of PATH;
- system directories are read-only.

Use a system-installed package manager that is visible inside the sandbox. For example, install OpenCode with Bun:

```sh
bun add -g opencode-ai
opencode --version
```

Or with npm:

```sh
npm install -g opencode-ai --allow-scripts=opencode-ai
opencode --version
```

Then leave the profile shell:

```sh
exit
```

Programs installed only in your host user's home are not automatically visible inside the sandbox. Install agent-specific tools inside the profile or use tools installed in the system directories exposed read-only by ABX.

### Multi-agent profile with Herdr

A profile can hold Herdr and several agent CLIs. For detach/reattach, use `work` so that leaving the client returns to a shell in the same sandbox.

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

`work` leaves an outer shell inside the sandbox after Herdr detaches. Herdr can start OpenCode, Pi, and other CLIs from the same profile: they share PATH and access to the project at `/workspace`. For a direct launch without an outer shell, use `abx run herdr` on the host.

Herdr may open its first workspace in `/home/agent` even though `/workspace` is mounted. If that happens, create and focus a project workspace explicitly from inside Herdr:

```sh
herdr workspace create --cwd /workspace --label project --focus
```

The Herdr workspace will then use the project mounted by ABX at `/workspace`.

Herdr and the agents it starts share `/home/agent` and the same writable project, so treat the whole profile as one trust boundary. Use separate profiles when you want separate state or credentials.

Herdr detach/reattach works normally while the enclosing ABX sandbox remains running. If the ABX session itself ends, its sandbox processes end as well; a later Herdr launch may restore persisted session state, but it should not be treated as the same live agent processes.

## Run an agent in a project

The profile name used by `run` must match the executable name. ABX searches the profile in this order:

```text
/home/agent/.local/bin/<name>
/home/agent/.bun/bin/<name>
/home/agent/bin/<name>
```

From the project directory on the host:

```sh
cd "$HOME/code/my-project"
abx run opencode
```

The current host directory becomes writable `/workspace` and the agent starts with `/workspace` as its working directory.

ABX rejects projects that would expose sensitive or system locations, including the filesystem root, the account home itself, ABX storage, protected system trees, and selected sensitive directories such as `.ssh`, `.gnupg`, and `.config`. Use a dedicated project directory such as `~/code/project`.

### Executable symlinks

`run` and `inspect` check the profile entry point using host paths before starting a sandbox. The command status in `profile show` and `profile list` uses the same check. ABX does not translate symlink targets between host paths and sandbox paths.

An absolute link such as `.local/bin/demo -> /home/agent/.local/lib/demo` can work inside the sandbox but be reported as unavailable on the host. Conversely, a link to the profile's absolute host path can pass this check and then fail inside the sandbox, where that path is normally hidden. A candidate rejected by the host check is skipped in favour of the next usable entry in the search order.

For links between files within a profile, use relative targets that stay within the profile, such as `.local/bin/demo -> ../lib/demo`. The entire link chain must remain valid. Avoid links to the profile's absolute host path.

If an installed tool works inside the sandbox but fails this host check, open `abx work <profile>` and launch it there; use `abx shell <profile>` when project access is not needed. Neither command requires a matching profile executable.

For direct `run`, another option is a regular executable wrapper named after the profile in one of the searched directories. It can call the actual tool by its sandbox path and forward arguments, for example:

```sh
#!/bin/sh
exec /home/agent/tools/demo/bin/demo "$@"
```

Replace the example path with the tool's actual path inside the sandbox. The wrapper must have execute permission and call the tool itself, not the wrapper again.

### Pass arguments to the agent

Arguments after `--` are passed directly to the agent:

```sh
abx run opencode -- --version
abx run opencode -- "argument with spaces"
```

ABX does not pass them through an additional shell.

## Project shell: work

On the host:

```sh
cd "$HOME/code/my-project"
abx work herdr
```

Inside ABX:

```sh
herdr
```

`work` opens the login shell with the profile at `/home/agent` and the current host directory writable at `/workspace`. Initial cwd and `PWD` are `/workspace`; project path checks and isolation restrictions are the same as for `run`. ABX does not automatically find the Git repository root.

The profile must exist, but no executable matching its name is required. You can launch any available tools inside it. `work` accepts only the profile name, with no arguments after `--`. Keep using `abx shell <profile>` for installation and maintenance without project access.

Like `shell`, it starts the passwd shell with `-l`; it does not force interactive mode with `-i`. From a terminal this is a normal interactive session. Profile startup files may change the initial working directory.

For Herdr, launch `herdr` as an ordinary command, without `exec`: detach returns to the outer shell inside ABX. Running `herdr` again in that same live `work` session lets you reconnect to the running server. Do not use `exec herdr` if you want detach to return to the outer ABX shell: `exec` replaces that shell. Ending the ABX session ends sandbox processes; a new `abx work` does not continue those same live processes. Profile and project files persist, while private temporary directories are recreated.

## Inspect a run

```sh
abx inspect opencode
```

`inspect` resolves the same profile, agent, project, system sources, and environment as a normal `run`, then prints the plan without starting Bubblewrap. It requires the profile executable and does not describe a `shell` or `work` plan.

The output contains:

- working directory and command;
- detailed mount operations;
- environment variables;
- an isolation summary;
- writable and private sandbox paths.

`inspect` is useful for understanding what ABX intends to launch. It is not a runtime verification of the sandbox. Mount modes are shown only when explicitly set; omitted modes do not mean `0000` permissions.

## Verify the sandbox

From a project directory:

```sh
abx verify opencode
```

`verify` does **not** require the agent executable to be installed. It starts a real Bubblewrap sandbox and checks the main properties used by normal sessions, including namespaces, writable profile/project mounts, private temporary/device views, read-only system resources, environment, cwd, and access boundaries.

Successful verification ends with:

```text
Verification passed; temporary probe files removed.
```

Individual checks use `PASS`, `FAIL`, `UNAVAILABLE`, and `N/A`. `N/A` is expected when shared skills are not configured. A failed or incomplete mandatory check makes `verify` fail. Failure details describe the expected and observed state where available. Environment mismatches list only differing variable names, without their values.

The read-only checks inspect mount flags at the selected mountpoints; they do not independently check every nested mount. Read-only enforcement is delegated to Bubblewrap. See the [verification scope](security.md#verification).

Verification temporarily creates probe files in the real home and project and removes them before reporting final success. A forced process kill or host crash can leave such temporary files behind.

## Shared skills

Shared skills are optional. ABX looks for them at:

```text
<ABX storage>/shared/agents/skills
```

When configured, the directory is mounted read-only in every profile at:

```text
/home/agent/.agents/skills
```

For default storage:

```sh
abx_root="$HOME/.local/share/abx"
mkdir -p "$abx_root/shared/agents/skills"
chmod 700 "$abx_root/shared"
```

The `shared`, `agents`, and `skills` path components must be real directories where ABX requires them; unsafe symlink redirection is rejected. The `shared` directory must belong to the invoking user and have exact mode `0700`.

When skills are used, ABX may create `.agents` and an empty `.agents/skills` mountpoint inside a profile. These are mount preparation directories, not copies of the shared skills.

## Custom storage

To keep profiles in another base directory, set an absolute `XDG_DATA_HOME` consistently for every related command:

```sh
ABX_DATA="$HOME/abx-data"
env XDG_DATA_HOME="$ABX_DATA" abx profile create opencode
env XDG_DATA_HOME="$ABX_DATA" abx shell opencode
env XDG_DATA_HOME="$ABX_DATA" abx run opencode
```

ABX appends its own `abx` component, so this example stores profiles under `$ABX_DATA/abx/profiles`.

Changing `XDG_DATA_HOME` selects different storage; it does not move existing profiles.

## Environment inside the sandbox

ABX starts from a clean environment. It sets the profile HOME, PATH, XDG locations, `TMPDIR`, `NPM_CONFIG_PREFIX`, and `BUN_INSTALL` itself.

Only these host settings are forwarded when present:

```text
TERM COLORTERM LANG LANGUAGE TZ NO_COLOR FORCE_COLOR LC_*
```

Arbitrary host environment variables, including proxy or credential variables, are not copied automatically, and the host SSH-agent socket is not exposed. Configure anything the agent needs inside its profile.

## Troubleshooting

| Problem | Check |
|---|---|
| `must not run as root` | Run without `sudo` |
| Bubblewrap missing or too old | Check `/usr/bin/bwrap --version`; ABX requires 0.12.0+ |
| Sandbox cannot start | Check that the host permits unprivileged user namespaces |
| Profile not found | Check `abx profile show <name>` and the `XDG_DATA_HOME` used for that command |
| `Command: unavailable` / agent not found | For `run` and `inspect`, install an executable matching the profile name. This status alone does not prevent `shell` or `work` |
| Shell unavailable | Check the login shell in `/etc/passwd` and that it is system-installed and executable |
| Project rejected | Use a dedicated project directory outside protected paths |
| Host tool missing inside `abx shell` | It may exist only in the host home; install it in the profile or system-wide |
| `verify` reports `FAIL` or `UNAVAILABLE` | Read the failing observation; the sandbox did not complete verification successfully |

## Exit codes and signals

ABX uses exit code `0` for successful launcher operations, `1` for launcher/runtime/verification errors, and `2` for CLI syntax errors. `run`, `shell`, and `work` normally return the child process exit status; a signal exit is reported as `128 + signal`.

`SIGINT`, `SIGTERM`, and `SIGHUP` received by ABX are forwarded to the Bubblewrap child.
