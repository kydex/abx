# Optional manual checks

**English** | [Русский](ru/manual-checks.md)

This walkthrough is optional: use it to investigate a problem or check behaviour after a relevant change to ABX, Bubblewrap, or the host. It is **not required before every release** and adds no release or CI gate. Existing automated and live-test requirements remain unchanged.

Use a supported non-root Linux host. Sections 1–2 prepare the disposable profiles; then choose the needed sections 3–6 and finish with cleanup in section 7. Run each selected scenario once per fresh setup. No agent download, authentication, sudo, or host mount changes are needed. The fixture checks a small executable, not a particular package manager or agent.

## How to enter commands

- **Terminal A** is the first terminal; keep it open until cleanup. It switches between host and sandbox.
- **Terminal B** is a second host terminal, used only for TERM/HUP.
- **ONE COMMAND**: copy that command only, press Enter, and wait. Never combine it with the next block.
- **BLOCK**: copy the entire block. In particular, keep the complete `cat ... <<'SCRIPT'` block, including its closing `SCRIPT` line.
- Do not copy prompts or expected output. Do not paste the whole document at once.
- `abx shell`, `abx work`, `exec /bin/sh`, and `exit` change the shell or session. They always appear separately below.
- Fish is fine as the starting shell. Once a sandbox opens, it may start fish again; switch with the separate `exec /bin/sh` command before continuing.

## 1. Prepare on the host

**Terminal A · host · ONE COMMAND — run separately**

```sh
sh
```

Wait for the new prompt before copying the next block.

**Terminal A · host · BLOCK — paste in full**

```sh
PS1='[HOST A] $ '
export PS1
```

The prompt is now `[HOST A] $`. Ensure `abx` in PATH is the binary you intend to check. Do not enable `set -e`: some cases intentionally return nonzero. Stop on unexpected errors.

**Terminal A · host · BLOCK — paste in full**

```sh
command -v abx
abx version
/usr/bin/bwrap --version
uname -sr
id -u
umask 077
abx_manual_root=$(mktemp -d /tmp/abx-manual.XXXXXXXX) || exit
export XDG_DATA_HOME="$abx_manual_root/data"
mkdir "$abx_manual_root/project"
cd "$abx_manual_root/project" || exit
printf 'Temporary directory: %s\n' "$abx_manual_root"
abx profile create smoke
abx profile create empty
abx profile show smoke
abx profile list
```

Expected: nonzero UID; profiles `smoke` and `empty` under the printed temporary directory. `smoke` is present but its command is unavailable. Keep the printed path; all host commands in A use this temporary storage.

## 2. Install the fixture in shell

**Terminal A · host · ONE COMMAND — run separately**

```sh
abx shell smoke
```

Wait for the new prompt before copying the next block.

**Terminal A · inside sandbox · ONE COMMAND — run separately**

```sh
pwd
```

Expected: `/home/agent`. Now replace the sandbox login shell with sh:

**Terminal A · inside sandbox · ONE COMMAND — run separately**

```sh
exec /bin/sh
```

Wait for the new prompt before copying the next block.

**Terminal A · inside sandbox · BLOCK — paste in full**

```sh
PS1='[SANDBOX] $ '
export PS1
```

**Terminal A · inside sandbox · BLOCK — paste in full**

```sh
test ! -e /workspace && printf 'Project hidden\n'
mkdir -p "$HOME/.local/bin"
cat > "$HOME/.local/bin/smoke" <<'SCRIPT'
#!/bin/sh
case "${1-}" in
  wait) exec sleep 600 ;;
  status17) exit 17 ;;
  skills)
    cat /home/agent/.agents/skills/marker || exit 1
    if printf 'unexpected-write\n' >> /home/agent/.agents/skills/marker; then
      printf 'FAIL: skills writable\n'
      exit 1
    fi
    exit 0
    ;;
esac
printf 'cwd=%s\nhome=%s\nsecret=%s\n' "$(pwd)" "$HOME" "${ABX_MANUAL_SECRET-unset}"
printf 'argc=%s\n' "$#"
for arg do printf 'arg=<%s>\n' "$arg"; done
SCRIPT
chmod 700 "$HOME/.local/bin/smoke"
```

Expected: `Project hidden`. The small script has been installed in the disposable profile. Return to the host:

**Terminal A · inside sandbox · ONE COMMAND — run separately**

```sh
exit
```

Wait for the new prompt before copying the next block.

**Terminal A · host · BLOCK — paste in full**

```sh
pwd
printf 'storage=%s\n' "$XDG_DATA_HOME"
command -v abx
```

Expected: the temporary `project` directory, storage at its sibling `data`, and the host path to ABX. This confirms that you are back on the host.

## 3. Arguments, environment, and exit codes

**Terminal A · host · BLOCK — paste in full**

```sh
abx profile show smoke
abx inspect smoke
env ABX_MANUAL_SECRET=hidden abx run smoke -- 'a b' '$(literal)' ''
abx run smoke -- status17
printf 'exit=%s\n' "$?"
abx run smoke unexpected
printf 'exit=%s\n' "$?"
abx profile create smoke
printf 'exit=%s\n' "$?"
(cd / && abx run smoke)
printf 'exit=%s\n' "$?"
```

Expected: show reports the executable; inspect describes `/workspace`, the profile and clean environment. Run prints `cwd=/workspace`, `home=/home/agent`, `secret=unset`, `argc=3`; arguments are `a b`, `$(literal)`, and an empty string. The four exit codes are `17`, `2`, `1`, `1`: child status, CLI syntax, duplicate profile, forbidden project. The deliberate error messages are expected.

## 4. Work, persistence, and background activity

**Terminal A · host · ONE COMMAND — run separately**

```sh
abx work empty
```

Wait for the new prompt before copying the next block.

**Terminal A · inside sandbox · ONE COMMAND — run separately**

```sh
pwd
```

Expected: `/workspace`, even though no executable named `empty` is installed.

**Terminal A · inside sandbox · ONE COMMAND — run separately**

```sh
exec /bin/sh
```

Wait for the new prompt before copying the next block.

**Terminal A · inside sandbox · BLOCK — paste in full**

```sh
PS1='[SANDBOX] $ '
export PS1
```

**Terminal A · inside sandbox · BLOCK — paste in full**

```sh
printf 'persistent\n' > /home/agent/keep
printf 'private\n' > /tmp/abx-manual-private
nohup /bin/sh -c 'while :; do printf x >> /workspace/heartbeat; sleep 1; done' </dev/null >/tmp/abx-heartbeat.log 2>&1 &
sleep 3
wc -c /workspace/heartbeat
```

Expected: a byte count greater than zero.

**Terminal A · inside sandbox · ONE COMMAND — run separately**

```sh
exit
```

Wait for the new prompt before copying the next block.

**Terminal A · host · BLOCK — paste in full**

```sh
sleep 2
wc -c heartbeat
sleep 3
wc -c heartbeat
```

The two host counts should be equal: the background writer has stopped. If they increase, stop and investigate before cleanup. A stable count is evidence about this fixture, not every possible descendant. Now open a new session:

**Terminal A · host · ONE COMMAND — run separately**

```sh
abx work empty
```

Wait for the new prompt before copying the next block.

**Terminal A · inside sandbox · ONE COMMAND — run separately**

```sh
exec /bin/sh
```

Wait for the new prompt before copying the next block.

**Terminal A · inside sandbox · BLOCK — paste in full**

```sh
PS1='[SANDBOX] $ '
export PS1
```

**Terminal A · inside sandbox · BLOCK — paste in full**

```sh
cat /home/agent/keep
test ! -e /tmp/abx-manual-private && printf 'Fresh tmp\n'
```

Expected: `persistent` and `Fresh tmp`. Profile data persists; the temporary filesystem is new.

**Terminal A · inside sandbox · ONE COMMAND — run separately**

```sh
exit
```

Wait for the new prompt before copying the next block.

## 5. Signals

### 5.1 Ctrl+C

**Terminal A · host · ONE COMMAND — run separately**

```sh
abx run smoke -- wait
```

The command waits without output. After a couple of seconds, press **Ctrl+C once**. When the host prompt returns, immediately run:

**Terminal A · host · ONE COMMAND — run separately**

```sh
printf 'exit=%s\n' "$?"
```

Expected: `exit=130`. Do not enter another command between return and reading `$?`.

### 5.2 TERM

**Terminal A · host · ONE COMMAND — run separately**

```sh
sh -c 'printf "ABX PID: %s\n" "$$"; exec abx run smoke -- wait'
```

Copy the printed PID. Leave A waiting. Open terminal B on the host; start a separate sh even if your normal shell is fish:

**Terminal B · host · ONE COMMAND — run separately**

```sh
sh
```

Wait for the new prompt before copying the next block.

**Terminal B · host · BLOCK — paste in full**

```sh
PS1='[HOST B] $ '
export PS1
```

**Terminal B · host · ONE COMMAND — run separately**

```sh
read -r abx_manual_pid
```

This command waits for input. **Paste only the PID number from A, then press Enter.** Wait for `[HOST B] $`. Then check the process:

**Terminal B · host · ONE COMMAND — run separately**

```sh
ps -p "$abx_manual_pid" -o pid=,args=
```

Continue only if this is the waiting `abx run smoke -- wait` from A. If the process has ended, start a fresh invocation and read its new PID.

**Terminal B · host · ONE COMMAND — run separately**

```sh
kill -TERM "$abx_manual_pid"
```

In **A**, wait for the host prompt, then immediately run:

**Terminal A · host · ONE COMMAND — run separately**

```sh
printf 'exit=%s\n' "$?"
```

Expected: `exit=143`.

### 5.3 HUP

**Terminal A · host · ONE COMMAND — run separately**

```sh
sh -c 'printf "ABX PID: %s\n" "$$"; exec abx run smoke -- wait'
```

A prints a **new PID** and waits. In B, read this new number:

**Terminal B · host · ONE COMMAND — run separately**

```sh
read -r abx_manual_pid
```

Paste only the new PID and press Enter. After the B prompt returns:

**Terminal B · host · ONE COMMAND — run separately**

```sh
ps -p "$abx_manual_pid" -o pid=,args=
```

Check that the process is the fresh waiting invocation from A, then:

**Terminal B · host · ONE COMMAND — run separately**

```sh
kill -HUP "$abx_manual_pid"
```

In **A**, after the prompt returns, immediately run:

**Terminal A · host · ONE COMMAND — run separately**

```sh
printf 'exit=%s\n' "$?"
```

Expected: `exit=129`. Each signal should end this fixture promptly. If it does not, record the problem; do not count expiry of its 600-second sleep as success. Do not reuse a PID after the invocation ends. Close the dedicated shell in B:

**Terminal B · host · ONE COMMAND — run separately**

```sh
exit
```

Wait for the new prompt before copying the next block.

No more commands are needed in B. Continue in A on the host.

## 6. Verify and shared skills

**Terminal A · host · ONE COMMAND — run separately**

```sh
abx verify smoke
```

Expected: final verification success and `N/A` for shared skills. Then configure skills:

**Terminal A · host · BLOCK — paste in full**

```sh
abx_manual_skills="$XDG_DATA_HOME/abx/shared/agents/skills"
mkdir -p "$abx_manual_skills"
chmod 700 "$XDG_DATA_HOME/abx/shared"
printf 'shared-marker\n' > "$abx_manual_skills/marker"
env LC_ALL=C abx run smoke -- skills
cat "$abx_manual_skills/marker"
```

Expected: the attempted write reports `Read-only file system`. The final host `cat` prints only `shared-marker`, without `unexpected-write`. Then:

**Terminal A · host · BLOCK — paste in full**

```sh
abx verify smoke
find . "$XDG_DATA_HOME/abx/profiles" -name '.abx-*' -print
```

Expected: successful verify, including `PASS` for the skills mount. The final `find` prints nothing. Verify also creates a temporary sentinel in the passwd home; normal completion removes it, but interruption can leave it behind. This checks the ordinary skills mount, not nested mounts; see [verification scope](security.md#verification).

## 7. Results and cleanup

For the cases you chose, record actual results and any unexpected output, along with ABX/Bubblewrap versions and host details. Do not label skipped cases as passed. Retain evidence outside the temporary directory. Exit all test sandboxes; if a background process survived, stop that specific process before deleting files.

**Terminal A · host · BLOCK — paste in full**

```sh
pwd
printf 'temporary=%s\nstorage=%s\n' "$abx_manual_root" "$XDG_DATA_HOME"
```

Check that the temporary path matches section 1 and storage is its `data` subdirectory. If it does not, do not delete anything. Otherwise paste the cleanup block:

**Terminal A · host · BLOCK — paste in full**

```sh
cd / || exit
case "$abx_manual_root" in
  /tmp/abx-manual.?*) rm -r -- "$abx_manual_root" ;;
  *) printf 'Unexpected temporary path; not removed\n' ;;
esac
```

If removal finishes without errors, close the dedicated host shell with a separate command:

**Terminal A · host · ONE COMMAND — run separately**

```sh
exit
```

Wait for the new prompt before copying the next block.

You are back in your original shell, including fish if that is what you started with. Only the disposable directory was removed.

## If you lose track of the shell

Stop copying further steps. A plain `sh-…$` prompt alone does not tell you whether you are on the host. Use these read-only commands in the current terminal:

**Current terminal · BLOCK — paste in full**

```sh
pwd
printf 'home=%s\nstorage=%s\n' "$HOME" "$XDG_DATA_HOME"
command -v abx
```

Inside this sandbox, HOME is `/home/agent`, storage is `/home/agent/.local/share`, and the host ABX binary is normally unavailable. In the prepared host shell A, storage is `/tmp/abx-manual.…/data`. If still inside the sandbox, enter `exit` **alone**, wait, and check again. Do not create profiles or reset the storage variables until you have identified the host shell. After `exec /bin/sh`, do not assume that commands pasted after it were executed: copy the intended next block again only after checking the state.
