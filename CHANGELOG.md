# Changelog

## Unreleased

- The development guides now name the `THIRD_PARTY_LICENSES` check among the `make check` steps; the Russian guide now carries the 12 October date for the `/etc/passwd` root-ownership removal and a translated releases link text. (English and Russian.)

## 0.8.26

- Fixed: `profile show` and `profile list` no longer reject an agent installed as a symlink under a mounted system path such as `/usr`. Profile inputs carried no discovered system sources, so the placement check refused the layout `run` accepts and the security guide calls supported.
- Fixed: a dangling `/etc` resource no longer aborts every session; the entry is skipped like a missing one, as the security guide states.
- Fixed: `run`, `shell`, and `work` keep the child's exit status when a cleanup failure is reported with it. `Run` turned every session error into status `1`, so the documented keep-the-status rule was not observable through the CLI.
- Fixed: a cleanup failure after a launcher-side failure no longer reports `child exited with status 1`; the phrase names the child's own exit status only.
- Fixed: a forwarded host environment value longer than 64 KiB is refused before the sandbox starts, with an error naming the variable. Such a value previously failed inside `execve` as a raw "argument list too long" whose troubleshooting advice pointed at unprivileged user namespaces. The verify probe keeps its own 64 KiB input bound, now named in the guides. (English and Russian.)
- Fixed: a Bubblewrap version probe that fails for a reason other than its 5s deadline or caller cancellation now reports that reason. The message wrapped the probe deadline context instead of the run error and rendered the nil value as `%!w(<nil>)`.
- Fixed: when selected sources changed between validation and preparation, the error now names the same object every time -- the first in sorted order -- and says what it is (profile home, data root, project, and so on). The name used to depend on map order and carry no role.
- Fixed: an option-like profile name, as in `abx run --version`, is refused as an unknown option with a pointer to `abx --help` instead of the profile name rules; `abx -h extra` names the token as typed.
- Fixed: diagnostic text keeps invalid UTF-8 bytes as `\xNN` instead of replacing each one with U+FFFD, so a path with such bytes is reported exactly.
- Fixed: the compatibility link check (the /bin, /sbin, /lib, /lib64 aliases) resolves the link text from the sandbox root, where the link sits, so "usr/bin/../../etc" is refused as /etc; option-like texts and /usr itself are refused too. Only a path below /usr passes.
- Fixed: a compatibility symlink (/bin, /sbin, /lib, /lib64) whose target resolves outside the mounted system sources -- for example through a further link inside /usr -- is now refused at discovery, naming the link and its target. It previously built a sandbox whose compatibility path led nowhere, and the session failed at exec blaming the tool.
- Fixed: a project swapped for a regular file between path validation and opening is refused with an error that says so, instead of being retained and mounted at `/workspace`.
- Fixed: a failure inspecting the shared skills path says which check failed (`inspect shared skills "<path>": ...`) instead of returning a bare `lstat` error. Late entry: the change shipped in 0.8.25 without a changelog note.
- The `inspect` isolation summary gained two lines: `nested user namespaces: disabled` and `lifetime: killed with the launcher`. Both properties were enforced all along by the shared Bubblewrap arguments; the summary just did not name them.
- The mutation sweep measures every guard in its file list -- the filter that skipped `Kind ==` guards is gone and `internal/app` joined the list -- and gives each package its own copy of the scratch tree: package tests link one another, and in a shared tree one package's mutant was recorded against another package's guard (a full run read 54 of `internal/app`'s 76 guards killed; in its own tree the number is 38). Runs from before these changes are not comparable. Its eighth run measured 288 of 425 guards killed, 137 survived and none unmeasurable.
- `make package-check` rehearses release packaging without a release tag: it builds both archives into a temporary directory and runs the same smoke checks as `make package-smoke`, and `make check` runs it, so a packaging regression fails on a branch instead of only at the tag.
- The security guide records the `__probe` gate's limit: it proves the mode the private entry point runs under (the binary executing as the read-only `/abx-probe` mount), not the origin of the call. (English and Russian.)
- Fixed: the security and usage guides no longer claim a background job can outlive the session -- a claim added in 0.8.25. The sandbox is its own PID namespace; when its init process (pid 1) dies, the kernel kills every process left in it, `nohup` and `setsid` jobs included, and their tmpfs allocations and network access end with the session. A live run of the manual check on Bubblewrap 0.13.1 confirmed that a background writer stops when the session ends. (English and Russian.)
- Fixed: `scripts/package.sh` resolves the script path and the checkout root physically before its git checks, so packaging a checkout reached through a symlinked path no longer skips the release-tag, cleanliness, and untracked-file gates.
- The package smoke check now compares each archive's member list with its unpacked tree, refuses a member outside the archive prefix or an unexpected top-level path, and requires `scripts/*.sh` in the source archive to stay executable.
- Fixed: the release guide and the smoke check's comments now say that a non-root `make package-smoke` executes `abx version` from the unpacked binary archive, instead of claiming the binary is never run. (English and Russian.)
- Fixed: a symlink cycle in a sensitive home tree (`~/.ssh -> .ssh`) no longer stops every command at startup. Like a dangling link, a link that cannot resolve is compared by the literal target its own text names; a cycle never resolves, so no data can sit behind it.
- Fixed: a missing Bubblewrap no longer reports only a bare `lstat` error, and a dangling `/usr/bin/bwrap` link names the path ABX checked (`resolve Bubblewrap executable "/usr/bin/bwrap": ...`) instead of only its target.
- The `abx help` storage line now says that `$XDG_DATA_HOME` is used only when absolute, matching the guides.
- Fixed: the invalid-profile-name message now names the rule the input broke -- `must start with a letter or digit` or the allowed character set -- instead of asserting both for a name like `a b`.
- The custom-storage guide now lists all six data base refusal conditions, including the ancestor checks for a non-directory and for setuid or setgid bits. (English and Russian.)
- Fixed: the usage guide no longer promises the skills failure reason from `profile list`; only `profile show` prints it. (English and Russian.)

## 0.8.25

Closed the fourth audit round: 40 findings, and from this release every one of them
carries a recorded decision rather than a silent gap. Four of them were code. A `/tmp`
that is a symlink into a tree outside the protected roots canonicalized straight past
the exact-path refusal; a cleanup failure after a nonzero child exit reported only the
cleanup, leaving the status to be read out of the return value alone; a session
username carrying a newline reached the agent because only a NUL byte was refused;
and a plan altered after `Build` was translated into Bubblewrap arguments without a
word, because validation compared the command with nothing.

The mutation sweep gained `else if` branches and multi-line conditions. Its sixth run
measured 240 of 336 guards killed, 96 survived and none unmeasurable. It then caught
a defect in this repository's own tests: the new expected-command check had been
masking three older command-shape rows, which asserted one refusal and exercised
another. Each row now isolates its own check, and all three mutants are caught. Four
refusals the sweep reported as unpinned are pinned now, each verified against its own
mutant.

Aggregate coverage moved from 90.8% to 91.5%. Seven documentation claims that outran
the code are corrected, the security model stops promising a root-ownership check on
`/etc/passwd` that the code dropped in 0.8.24, the release guide states exactly what
its force-push evidence covers, and the unchecked parent directories of the login
shell, the agent executable and the mounted system sources are recorded as an accepted
residual risk instead of staying unmentioned.

- Documented that a child exit status of `2` is indistinguishable from a CLI syntax error, and that a cleanup failure after a nonzero child exit keeps that status and joins both errors. (English and Russian.)
- Documented that a bare `abx run <profile> --` with no agent arguments is accepted as a no-op, matching what the parser already did. (English and Russian.)
- The security model now names the checks it had been leaving implicit: mounted system sources are trusted configuration without mode checks, login-shell replacement is a residual window like the agent one, the visible-shell list is static, a home under a protected tree such as `/var` has no carve-out, and uncovered base directories stay writable as ephemeral scratch. (English and Russian.)
- The release table now lists both `README` files and `CHANGELOG.md` in the binary archive, matching what the packaging script copies. (English and Russian.)
- The Russian security guide regained the 4 GiB tmpfs scope its English original states, and the Russian usage guide its code quotes around `SIGKILL`.
- Fixed: the development guide no longer claims `ubuntu-latest` cannot run the live tests. CI runs them automated on a source-built Bubblewrap; the guide now names the CachyOS host the reference live environment instead. (English and Russian.)
- Fixed: the `Ctrl+\` check in `manual-checks.md` no longer breaks bold markup. The backslash escaped the closing asterisks, so the emphasis ran past the paragraph in rendered output. (English and Russian.)
- Fixed: the architecture guide no longer claims the account home is revalidated for exact `0700`. The profile home and storage roots are; the passwd home is checked once at resolution for a non-world-writable directory owned by the invoking UID. (English and Russian.)
- Fixed three stale claims in the usage guide: the verify deadline covers tool opening onward rather than all setup, probe files land in the real home only rather than home and project, and the no-arguments-after-`--` rule for `work` and `shell` now reads in English as it already did in Russian. (English and Russian.)
- The release guide names the narrowed tag trigger and lists the full source-archive contents. (English and Russian.)
- The development guide drops the root-owned passwd claim for `ResolveAccount` and names all three live scenarios. (English and Russian.)
- The Russian installation and security guides now say `other` where the home check allows group writes.
- The release guide states precisely what is proven about force-pushed tags: pushing all tags at once starts no workflow; a single retargeted tag is untested. (English and Russian.)
- Fixed: the security model no longer requires `/etc/passwd` to be owned by root. The code stopped requiring that in 0.8.24 because it refused to start in containers, but the document kept promising a refusal that never comes. The entry now matches the installation guide: a regular file group and other cannot rewrite, no owner requirement. (English and Russian.)
- Documented that the parent directories of the login shell, agent executable, and mounted system sources are not checked for writability: a group member able to write one could substitute the binary a session runs. Accepted as a trade-off rather than fixed, since refusing would break team machines running umask 002. (English and Russian.)
- Fixed: a session username containing a newline is refused the way a NUL byte already
  was. The account lookup cannot produce one, so no legitimate session changes, but the
  barrier no longer depends on the shape of its source.
- Fixed: `/tmp`, `/var/tmp` and `/dev/shm` are refused as project paths through either
  spelling. On a host where one of them is a link into a tree outside the protected roots,
  the literal comparison let the resolved path through; the exact roots now resolve the
  way the protected-tree loop already did, and a lookup error is a refusal.
- Fixed: plan validation compares the built command against the one the plan expected at
  construction, so a plan altered after `Build` is refused instead of translated into
  Bubblewrap arguments. Valid plans are unaffected.

## 0.8.24

Closed the third audit round: 102 findings, and from this release every one of them
carries a recorded decision rather than a silent gap. Five code findings were fixed
and two documented with the reason they are safe. `Ctrl+\` now interrupts a session
instead of dumping the launcher and exiting with the status this file assigns to a
CLI syntax error. ABX no longer refuses to start when `/etc/passwd` is not owned by
root, which is every container, and which had been skipping sixteen tests.

The mutation sweep that produced those numbers is now in the repository rather than
in a scratch directory — and it lied three ways before it did: it read build errors
out of test output by matching words, it discarded 79 measurable guards as
unmeasurable, and it ran files of one package in parallel, which silently dropped
four documented debts from its own report. Fixed, and its claims now cross-checked
by hand. Two guards it had been reporting as uncovered turned out to be reachable
after all, one of them behind a test that had been green for the wrong reason.

Aggregate coverage moved from 83.7% to 90.8%, almost entirely from tests that had
been skipping. Four refusals for file metadata that cannot be unreadable on Linux
are gone, matching the other eighty-eight assertions in the tree that never had
them. Seven documentation claims that outran the code are corrected.

- Compatibility links `/bin`, `/sbin`, `/lib` and `/lib64` are now required to
  point inside `/usr`. A host whose `/bin` pointed elsewhere used to build a
  valid plan and mount an empty `/bin`, so the session started with no binaries
  and no message naming the cause. On a normal host, where the four links point
  into `/usr`, nothing changes.

- The README no longer repeats the full Herdr walkthrough that the usage guide
  already owns; it points there instead, and the Quick start now shows one setup
  path rather than two. The download step now says plainly that the project has
  no download page of its own, and the Bubblewrap requirement is split into one
  condition per bullet. (English and Russian.)

- The installation guide now says what the checksum command does when the archive
  is not in the current directory: it exits `1` with no `OK` line, rather than
  passing quietly as an earlier version of this entry claimed. `--ignore-missing`
  skips only the archives you did not download, so a partial download verifies
  cleanly. (English and Russian.)

- `make package-smoke` now checks the binary archive's documentation contents: the Russian
  README, `THIRD_PARTY_LICENSES`, `CHANGELOG.md` and `docs/ru` must be present, and `AGENTS.md`
  and `AGENTS.ru.md` must be absent. The build already produced the right archives -- the gate
  simply did not look. An archive missing the Russian README or carrying the maintainer
  instructions passed with `package smoke ok`. (English and Russian.)

- The security model lists every host object ABX validates, not the three it named before:
  the account home, the storage and profile directories, and the profile executable. The
  paragraph above the table no longer reads as an exhaustive list it did not deliver.
  (English and Russian.)

- `make package-smoke` now compiles the unpacked source archive. The gate confirmed that
  `internal/` was present but never built it, so a source archive missing a Go file passed
  with `package smoke ok` -- and the installation guide tells users they can build the binary
  from it. A shipped source tree that cannot be built was shipped.

- The mode rule for trusted host objects -- group- or other-writable permissions and
  setuid, setgid or sticky bits -- is stated once in `internal/host` and reused by the
  account database and login shell checks, instead of being written out at each. The
  Bubblewrap copy is deliberately independent, since `bwrap` must not depend on `host` and
  its rule adds the execute bit; both copies are now pinned by the same table of modes in
  each package, so weakening one shows up as a failing row rather than a silent gap.

- `profile show` and `profile list` no longer report an agent that `abx run` refuses. Both went
  through the resolver without the placement check that `run` performs, so a symlink ending outside
  the profile and outside every mounted system source showed up as an available command. (English and
  Russian not needed: no user-visible text changed.)

- Three claims no longer outrun the code. `verify` on timeout may leave the check lines it already
  printed, since the probe writes as it runs; a non-zero probe status is reported as 128 plus the signal
  number, not as an exit code; and the security model lists the descriptors ABX actually exempts from
  the close-on-exec sweep, which is everything handed to the child, not the retained mount sources. The
  account-database paragraph now says that with `CGO_ENABLED=0` -- the shipped build -- both routes read
  the same literal `/etc/passwd`, and that the asymmetry it described only exists in a cgo build.
  `architecture.md` now says `Plan.Validate()` compares a plan against expectations recorded while
  building it, rather than implying an independent check. `development.md` names CachyOS as the live test
  target, which until now appeared only in the maintainer rules. (English and Russian.)

- Removed: four unreachable refusals for file metadata that cannot be unreadable on
  Linux. `info.Sys().(*syscall.Stat_t)` always yields that type and nothing else, so a
  `!ok` branch after it cannot be taken; the remaining 88 type assertions in the tree
  never had one. The branches promised a handled failure that no input could produce,
  and a failed assertion would have panicked rather than returned the named error.
  Behaviour is unchanged: a project and a home are still checked for ownership, the
  Bubblewrap binary still for root ownership, and a signalled child still reports
  `128 + signal`. (Also in `probe`, whose package was absent from the sweep list.)

- Removed: ABX no longer requires `/etc/passwd` to be owned by root. The requirement refused to
  start on every container, where the file belongs to an unmapped uid, and the tests covering the
  resolve path had nothing to exercise there: 16 of them skipped. The file checks that remain --
  regular file, not writable by group or other, no privilege-escalation bits -- still stand. A host
  whose account database an unprivileged user can rewrite was never inside what ABX protects: that
  user already controls the account ABX reads, and the same file is read again through `os/user`
  with no checks at all. Skipped tests in a container: 21 -> 5, and the five are by design (three
  live, one needs real root, one needs a real ownership change).

- Documented what the audit of 11 October left in place instead of changed. `docs/security.md` names the
  limits the code already had: environment values are sanitized against NUL bytes only, so a newline in a
  host value reaches the agent unchanged; `TZ`, `LANG` and `LC_*` are forwarded unrestricted; the process
  identity inside the sandbox is unchecked; the account home is not checked for setuid or setgid unlike the
  storage ancestor beside it. `usage.md` now states that `profile list` stops at the first profile with an
  unsafe layout instead of reporting it as usable, which the code has always done. `development.md` gains a
  short list of rough edges we accept and why, so the next reader does not reopen them. (English and Russian.)

- `make check` now compiles the live-test fixture in `internal/app/testdata`, which Go leaves out of
  `go build ./...`. Only `make live` built it, so a broken fixture passed every branch check.

- `inspect` names every mount kind: the table pins all seven plus what an unknown kind prints. The
  function had no test at all, so a renamed kind would print `unknown` with nothing failing.

- Three claims corrected. The storage guide said the `private directory` message names one path
  component; it names the full path in every command but `profile create`, which walks components.
  The security model listed the close-on-exec exemptions as the retained mount sources when the code
  exempts everything passed to the child, and it left out the sandbox root, for which ABX sets no size:
  only its three own tmpfs mounts get the 4 GiB cap. The parent-death paragraph said `--die-with-parent`
  covers the sandbox root's pid 1; measured on a live host, it kills the command Bubblewrap started while
  a process put in the background beforehand keeps running, which is what the paragraph already
  concluded. The conclusion was right and the explanation was not. (English and Russian.)

- Fixed: a project under another user's home is now refused on hosts where your own
  home is not under `/home`. The rule required that arrangement before it applied at all,
  so `/var/lib/users/alice` or a container root switched it off entirely and
  `/home/bob/proj` mounted writable. Home layout is irrelevant to the rule, and projects
  outside `/home` are unaffected either way.

- Documented why looking the account database up twice is safe: replacing
  `/etc/passwd` between the lookups needs write access to it, which the mode check
  itself forbids for group and other, so it requires the owner.

- Fixed: a profile executable is refused when its group can rewrite it or when it carries
  setuid, setgid or sticky bits. The agent runs in the same launcher position as the login
  shell and now gets the same remaining checks; it previously stopped after the execute
  test, so an agent an equivalent shell would refuse was accepted. An agent installed with a
  group-writable bit (`chmod 775`) now needs `chmod 755`.

- Fixed: `Ctrl+\` no longer crashes the launcher. `SIGQUIT` is forwarded to the sandbox like
  `Ctrl+C`, `SIGTERM` and `SIGHUP` already were. Previously the terminal's `SIGQUIT` took ABX
  by the default action: a dump of every internal goroutine and exit status `2`, the status
  documented for a CLI syntax error, with no signal reaching the agent. Measured on a host
  process: unhandled `SIGQUIT` produces 156 lines of dump and status `2`.

## 0.8.23

Tightened what ABX trusts before it starts: the account home, the storage root, the
account database, and the place the agent binary really lives. Verification no longer
reports observations it could not fail, packaging gained a Bubblewrap provenance pin and
an execution smoke check, and a mutation sweep over 280 guards turned ten refusals that
could be deleted without any test noticing into pinned behaviour. The container-image
layout is no longer supported — see the first entry below for exactly what that refuses.

- The account home must now be owned by the invoking UID, and `/etc/passwd` must
  be owned by root. Both were previously accepted from any owner. The home is
  named by the passwd entry and is where the profile and the writable sandbox
  home live, and the passwd entry is what resolves the login shell, so a file
  owned by somebody else is one somebody else can replace. The storage root
  additionally rejects a setgid ancestor: a setgid directory makes everything
  created under it inherit a group the check never inspects, while the sticky
  bit stays allowed because it is the documented answer to a shared directory.

  **This drops support for container images that hand the process a root-owned
  home or a passwd file of their own** (runAsUser, and any user-namespace
  remapping of `/etc`). ABX now targets real hosts. If you ran it inside such a
  container, `abx` will refuse to start with `passwd home ... is owned by uid 0`
  or `passwd database ... is not owned by root`.
- A profile executable that resolves outside the profile and outside every
  mounted system path is now refused while the profile is resolved, with a
  message naming the resolved location. It previously produced a path that does
  not exist inside the sandbox and failed at exec time blaming the tool. Package
  managers are unaffected: a symlink into `/usr` or into the profile still
  works, which is what `~/.local/bin/<name>` pointing at a separately installed
  binary is.
- `make package-smoke` now runs the binary out of the unpacked release archive
  and compares its reported version. The gate already verified the checksums and
  unpacked both archives; it never executed what it unpacked, so an archive that
  was structurally intact but did not run passed. Built as root, the execution
  step is skipped with a note instead: ABX refuses to run as root before it
  parses any argument, so that outcome says nothing about the archive.

- Correct a 0.8.22 claim: packaging drops the three stale archives of the
  version being built, not everything in `dist/`. Older builds of other versions
  are left alone on purpose, so the wording "cleans `dist/`" overstated what the
  script does.
- Correct a 0.8.22 claim about exit codes. "Cleanup failures no longer overwrite a
  non-zero exit status" holds inside the session: `finishClose` only raises a zero
  status to 1. Through the CLI it is still not observable, because a joined
  cleanup error comes back from `runSession` as a non-nil error and `Run` turns
  any error into status 1. A child that failed with its own status and left
  cleanup residue behind therefore still reports 1. The published entry stays as
  written — it is history — and the correction goes here.
- The profile argument is spelled `<profile>` everywhere now. `cli.Usage`, the
  README command tables and the guides had drifted between `<name>` and
  `<profile>` for the same argument. (English and Russian.)

- `verify` no longer reports an observation it could not fail. It created a
  temporary marker in the passwd home and a symlink in the project pointing at
  that marker, then checked both were unreachable from inside the sandbox. The
  symlink target was an absolute host path, and host paths are never mounted
  inside, so the second check could only fail when the first one already did: it
  verified the project's reachability by a route the project's own mount is not
  part of. Removing it takes the duplicated line out of the output and stops
  `verify` from creating anything in the user's project. The property it stood
  for — the passwd home is unreachable from inside — is still checked, directly,
  against the marker itself.
- The live integration gate now records which Bubblewrap it built. The workflow
  cloned `v0.12.0` by name and installed the result as `/usr/bin/bwrap`, which
  `abx` then trusts on the single ground that it is root-owned — nothing kept the
  commit anywhere. A tag is only a name and can be moved, so the gate now fails
  unless the checkout resolves to the expected commit.
- The host requirements list a mounted `/proc`. ABX inventories inherited file
  descriptors through `/proc/self/fd` and has always failed without it; the
  requirement was simply never written down.
- State plainly in the release procedure that the workflow creates no release page
  and that adding one is out of scope, instead of a hypothetical sentence about
  "another public channel" that left the step ambiguous. (English and Russian.)

- A dangling symlink among the sensitive home trees (`~/.config`, `~/.ssh`,
  `~/.aws`, `~/.netrc`, `~/.mozilla` and the rest) no longer takes down every
  command. ABX checks seventeen paths under the passwd home; the path existed as
  a link, so the missing-path branch did not apply and resolving it failed —
  `abx profile list`, `abx run`, even `abx help` all stopped until someone deleted
  the link. Uninstalling a program leaves exactly that behind. A dangling link
  shelters nothing, so it is now treated as a missing path. A symlink that
  resolves is still followed, and the overlap rule still applies to its target.

The check keeps its teeth where they matter. An unresolvable sensitive link is
compared by its target text rather than by the link itself, so a project or a
storage base that sits where the link is headed is still refused even before the
target exists. Protected system roots keep the strict rule and are still a hard
refusal. (English and Russian.)

- Correct the documented signal behaviour. A terminal `Ctrl+C` in `shell` or `work`
  reaches the payload and the Bubblewrap monitor at the same time: the monitor
  installs a handler only for `SIGCHLD`, so it dies by the default action, and the
  resulting parent-death signal kills the sandbox with `SIGKILL` before a shutdown
  handler can finish. The session exits `130` and unsaved work is lost. The previous
  text promised that an interactive session "reacts to Ctrl+C normally"; it never did.
- State that `--die-with-parent` covers the initial sandbox process and pid 1 of the
  sandbox, but not processes they spawn — a background job started inside the sandbox
  can outlive the session. The previous text implied it terminated every sandbox
  process.
- Document that the account database is read by two routes, NSS for the account
  record and a literal `/etc/passwd` for the login shell, and that only the second is
  covered by the trust checks: a host resolving `passwd` through `sss` or `ldap` is
  refused even though `id` and `getent` report the account correctly.
- State in the architecture guide that the namespace and lifetime flags are a literal
  at the translation site rather than a field of `Plan`, so they sit outside
  `Plan.Validate` and are spelled out in three places derived from nothing.
- `verify` no longer reports a difference in the environment that nothing could show.
  The expected values reach the probe through JSON, and the JSON encoder rewrites
  invalid UTF-8, so a host variable holding such a byte sequence could never match
  its own round trip. Both sides of the comparison now pass through the same
  transform. A real difference in bytes a user could read is still reported.
- `profile`, `run`, `shell`, `work`, `inspect`, `verify` and the `profile`
  subcommands all take the profile name and nothing else; the usage guide said
  `inspect` and `verify` accept "no arguments after `--`", which read as if a lone
  `--` were tolerated. It is an error there, as it is for `shell` and `work`.
- State that a `profile create` refusal names the single path component it stopped
  at, not the whole path, and that it stops at the first component that fails.
  The guide said it "reports only the last component".
- State what the binary archive actually contains: the whole `docs/` tree in both
  languages, contributor documents included.
- Packaging: the staging directory is removed again. A second `trap ... EXIT`
  replaced the first one, so every run left its staging directory behind. Tracked
  symlinks are now copied as symlinks instead of being followed, so a symlink in
  the staged trees cannot pull the contents of a path outside the repository into a
  release archive.

(English and Russian.)

## 0.8.22

Hardening of host trust checks, packaging gates, and a documentation pass driven by
two independent audit rounds plus three pre-release reviews. Fail-closed refusals
may reject setups that previously worked — the exact conditions are listed below and
in `docs/installation.md`.

### Host trust checks

- The login shell must not be group/other-writable and must carry no setuid, setgid,
  or sticky bits. The resolved target is what executes, not the raw passwd path, and
  both must be visible inside the sandbox. Shell ownership is deliberately not
  required, so user-managed shells under `/usr/local` keep working.
- `/etc/passwd` must be a regular file, not group/other-writable, without special bits.
- The passwd home must not be world-writable. Ownership is not required: container
  images with `runAsUser` and NFS with `root_squash` legitimately differ.
- The current project is not constrained by ownership or mode. Team checkouts
  (`root:dev` 2775) and filesystems without POSIX modes (always `0777`) are valid.
- Storage ancestors must belong to the invoking user and must not be world-writable
  unless they carry the sticky bit. Group-writable ancestors stay allowed: that is a
  team policy, and refusing it breaks machines running umask 002.
- Compatibility links `/bin`, `/sbin`, `/lib`, `/lib64` are resolved and rejected if
  they point into the account home or ABX storage.
- The sensitive-home denylist grows from 9 to 17 entries (`.netrc`, `.npmrc`,
  `.git-credentials`, `.password-store`, keyrings, mail profiles).

### Execution and diagnostics

- A wedged Bubblewrap binary is reported as the 5-second probe timeout instead of the
  exec wording `signal: killed`; caller cancellation is reported separately.
- Inherited descriptor flags are restored after execution, only after confirming the
  descriptor number still refers to the same object.
- `open` and `Revalidate` share one mode-bit rule, so the two checks can no longer
  drift apart; the deliberate difference in ownership handling is documented.
- The `__probe` sandbox gate runs before payload decoding.
- Cleanup failures no longer overwrite a non-zero exit status.
- Refusals name the object they failed on: Bubblewrap trust conditions, dangling
  symlinks, already-exists profile objects (type and mode), and skills close errors.

### CLI

- Bare `abx` prints help. `-h`/`--help` and `-v`/`--version` are commands in the first
  argument position only — `abx profile create demo --version` is an argument error,
  not a silent no-op.
- `inspect` reports the mount namespace and returns status `1` when writing the plan fails.

### Plan validation

- `Plan.Validate` enforces the environment allowlist and requires `PWD` to be set
  exactly once and to match the working directory.

### Packaging and CI

- `make package` requires the release tag on `HEAD`, a clean tree, and no untracked
  files; it verifies every tracked top-level path lands in the source archive, cleans
  `dist/` before building, and removes partial archives when a build fails partway.
  Plain builds stay ungated. Lightweight tags and several tags on `HEAD` are accepted,
  and a source archive unpacked inside an unrelated repository is treated as a plain
  directory.
- `make package-smoke` verifies the checksum manifest, unpacks both archives, and
  checks the mandatory file lists. It now runs in the release workflow.
- The inert CI `diff-check` is replaced by a local-only `local-diff-check`.
- CI checkouts set `persist-credentials: false`.

### Tests and documentation

- Independent literals pin the system-target, project-tree, namespace, and mount-point
  policies so a change to one list cannot pass silently. Added coverage for validator
  refusals, inside-home skills symlinks, oversized probe input, UID validation, exit
  code 2, environment argv literalness, the descriptor index, descriptor accounting,
  and the environment allowlist in fuzzing. (English and Russian.)
- Documentation brought in line with the implementation: storage-base restrictions and
  their refusal messages, the 30-second `verify` deadline, per-command argument rules,
  Bubblewrap and login-shell trust requirements, the uncapped `--dev` tmpfs,
  inherited-descriptor handling, and the signal model — terminal signals reach the
  payload through the foreground process group rather than as SIGKILL. (English and Russian.)

## 0.8.21

- Cap sandbox tmpfs mounts at 4 GiB so unbounded temporary files cannot exhaust host memory.
- Reject NUL usernames and NUL-valued host variables in `Session`, found by the new fuzz tests for name, session, and CLI parsing (seed corpora run in `go test`).
- Document how to contain runaway sessions with a systemd scope (`MemoryMax`, `CPUQuota`). (English and Russian.)
- Tidy comments, names, and error constructors without behavior change: fix stale doc names, stop shadowing `close`, use `errors.New` for constant errors, and check executability through `unix` instead of `syscall`.

## 0.8.20

- Name failing bind sources, mounts, and tool paths in errors instead of returning bare filesystem errors.
- Accept login shells under `/etc/alternatives` when their resolved target stays within the mounted system paths.
- Reject probe payloads with non-canonical read-only targets at decode time.
- Test setuid/setgid/sticky rejection for the Bubblewrap tool, pin the sentinel name prefix, and pin failed exits under `WaitDelay`.
- Stage only tracked files into the binary archive too, refuse packaging on a dirty tree, warn outside git trees, and add `make package-smoke`, `make clean`, and CI timeouts.
- Document `--die-with-parent`. (English and Russian.)

## 0.8.19

- Tidy naming and layout without behavior change: name the profile-list header, split the storage-path check, explain the private path tail, and wrap long syscalls.
- Add a troubleshooting row for mise-installed agents. (English and Russian.)
- Cover the probe gate logic, host payload assembly, identity reporting, success message, and probe CLI dispatch with unit tests instead of live-only runs.
- Keep an independent `go vet` pass in `make check` besides the golangci govet check, and document where the `mise` binary must live to be reachable inside the sandbox. (English and Russian.)

## 0.8.18

- Find `run` profile executables in the mise shims directory (`.local/share/mise/shims`) right after `.local/bin` and include it in the sandbox PATH in the same order, so mise-installed tools work with `run` without manual symlinks. (English and Russian.)

## 0.8.17

- Mention `-h`/`--help` in the README command table, link system-source discovery to the sandbox policy, and check uncommitted changes for whitespace errors (`git diff --check`). (English and Russian.)

## 0.8.16

- Run Go checks through golangci-lint (`golangci-lint fmt` with gofumpt/goimports and `golangci-lint run` with vet, staticcheck, gosec, and errcheck) instead of separate gofmt, staticcheck, gosec, and vet steps; errcheck and gosec skip test files, and the De Morgan quickfix stays disabled by design. Run unit tests uncached (`-count=1`).

## 0.8.15

- Reject host-side `abx __probe ...` invocations: the private probe now requires `/abx-probe` to be a read-only mount of the running binary with matching device and inode, so it only runs inside the sandbox. Document `__probe` as private in English and Russian. No behavior change for legitimate sessions.
- Report the child's own exit status when it orphans output pipes past WaitDelay instead of failing the launch; this only affects API use with non-file streams, the `abx` binary always passes real files.
- Name the failing system candidate in discovery errors instead of returning the bare filesystem error.
- Normalize release archive file ownership to root and stage only tracked files into the source archive so untracked clutter cannot leak in.
- Cover the Bubblewrap root-ownership check, compat-symlink revalidation, exact/sibling/data-base path rules, and WaitDelay handling with regression tests; remove dead code without behavior change.
- Simplify the installation instructions: unpack the archive, change into its directory, and install `./abx`; PATH setup is left to the user's shell config and checksum verification is documented as optional. (English and Russian.)

## 0.8.14

- Document the known residual same-UID pathname-race windows (agent-executable replacement, profile listing versus inspection, data-base canonicalization versus use, pathname-based metadata rechecks) as accepted in English and Russian, and note that mounts always use retained descriptors. No behavior change.

## 0.8.13

- Drop stale `adapted from abx 0.7.9` notes from package and test comments; the documented behavior is defined by the current implementation and guides. No behavior change.

## 0.8.12

- Name the clean sandbox environment `entries` in session assembly instead of reusing the path-check vocabulary; no behavior change.
- Preallocate the system-target duplicate-detection map in plan construction, matching the other duplicate-detection maps.

## 0.8.11

- Require the sandbox shell to be a canonical absolute path (same strength as the plan command check) so `Session` never accepts a value that `Build` would reject; non-canonical shells now fail fast with a precise diagnostic.
- Preallocate the environment duplicate-detection map in plan construction, matching the validator style.

## 0.8.10

- Refactor without behavior changes: move the test-only Bubblewrap version shorthand into the test file, give the sandbox path check a descriptive name, and name the minimum-version triple next to the version pattern.

## 0.8.9

- Harden private probe-input validation: require canonical absolute home/sentinel paths without NUL bytes, and require the verification link to be a canonical path directly inside `/workspace` with the reserved `.abx-verify-` file name prefix.
- Compile the Bubblewrap version pattern once instead of on every version check.
- Name the tmpfs magic constant in the live-test probe fixture instead of using a bare hexadecimal literal.
- Document the full sensitive home-tree list (`.config`, `.ssh`, `.gnupg`, `dotfiles`, `.aws`, `.docker`, `.kube`, `.pki`, `.cache`) in English and Russian.

## 0.8.8

- Reject sensitive home trees `.aws`, `.docker`, `.kube`, `.pki`, and `.cache` as projects, and reject sibling homes outside the real home under `/home`.
- Reject exact `/tmp`, `/var/tmp`, and `/dev/shm` as projects, and add `/srv`, `/mnt`, `/media`, and `/snap` to protected system trees.
- Reject data bases at the filesystem root, inside sensitive home trees, or with an existing ancestor not owned by the invoking user.
- Reject Bubblewrap executables with setuid, setgid, or sticky bits in open and revalidation.
- Reject effective GID mismatches in addition to effective UID mismatches.
- Skip stray profile entries with invalid names in `profile list` instead of failing the whole listing; valid names with unsafe layout still error.
- Use `RESOLVE_NO_MAGICLINKS` consistently for skills parent handling.
- Document the path policy: rejected exact `/tmp`, `/var/tmp`, and `/dev/shm` projects, protected `/srv`, `/mnt`, `/media`, and `/snap` subtrees, `/home`-scoped sibling-home rejection, and the user-owned ancestor requirement for custom `XDG_DATA_HOME` locations.
- Resolve symlinked sensitive home trees when validating custom data base locations.

## 0.8.7

- Add `staticcheck` and `actionlint` to local and CI checks, and document the required tools.
- Resolve staticcheck style diagnostics in error strings and package/exported comments.
- Keep plural run-argument guidance in the CLI separator diagnostic.
- Add a `make package` convenience target that reads `internal/app/VERSION` and delegates to the release script.

## 0.8.6

- Prevent unneeded inherited file descriptors from reaching sandbox subprocesses.
- Reject effective UID mismatches, make profile-name validation tests independent of host passwd state, and fail closed for unknown internal command kinds.
- Reject Bubblewrap versions below 0.12.0 across all version components.
- Document accepted runtime, verification, path, and release trade-offs in the synchronized security model.

## 0.8.5

- Explain verification mismatches with expected and observed state; report differing environment variable names without their values. Preserve verification criteria and status handling.

- Add regression tests for incomplete probe inputs, report and profile-output failures, and aggregation of cleanup failures without deleting replacement files.
- Update the live profile-list expectation for aligned columns.

- Clarify directory-type, profile-executable, profile-name, argument-separator, and Bubblewrap-version errors; include full directory paths in profile-creation failures.
- Align profile-list columns, omit unspecified mount modes and empty sources from inspect output, and omit empty verification details.
- Strengthen retained-descriptor, protected-path, non-mutating inspect, environment, and source-error regression tests.

- Document profile executable symlink limitations and workarounds, and clarify the scope of read-only verification in English and Russian. No runtime changes.

- Add optional bilingual manual checks using disposable profiles, with explicit host/sandbox locations and separate shell-transition commands; clarify profile status semantics. The walkthrough is not required before every release. No runtime changes.

## 0.8.4

- Add `abx work <profile>`: open the login shell with the current project writable at `/workspace`, without requiring a matching profile executable. Preserve project-free `shell`, run-only `inspect`, retained-FD execution, and shared isolation policy.
- Add work-mode policy, resource-lifetime, command/environment, and live-shell regression coverage; document same-session Herdr detach/reattach in English and Russian.

- Separate shared-skills preparation and plan validation into focused files without changing runtime policy or resource ownership.
- Use explicit run/shell/work modes in test fixtures and extend shared policy mutations and diagnostic checks across all three modes.

## 0.8.3

- Rewrite and synchronize English/Russian user documentation; separate end-user installation from maintainer CI/Actions workflows.

## 0.8.2

- Use `internal/app/VERSION` as the single release-version source for the binary and packaging; remove release-specific version literals from README and documentation examples and simplify release instructions accordingly.
- Add app-level regression tests for execution ordering, retained-input revalidation, late skills-path replacement, and executor error propagation.
- Extend `make check` with ShellCheck, `govulncheck`, `gosec`, and the static production build; reuse the same check workflow for release tags before artifact packaging.
- Add lifecycle/error regressions for cleanup error aggregation, retained Bubblewrap tool revalidation and execution, descriptor-close ownership, and non-mutating `inspect`; raise aggregate unit coverage from about 66% to about 76%.
- Add a 74% aggregate coverage regression floor to `make check` without adding another CI workflow step.
- Add structural `sandbox.Plan.Validate()` checks after plan construction and before Bubblewrap translation, including explicit plan kind and source-role expectations for home, project, skills, system mounts, and verification-probe policy, with mutation regressions.
- Move profile-name validation into a small shared domain package so CLI parsing and host storage enforce the same invariant without `internal/host -> internal/cli` coupling.
- Extend `inspect` with an additive isolation summary for namespace/network policy, writable mounts, and private runtime filesystems while preserving the existing detailed plan output.

## 0.8.1

- Load a Bubblewrap AppArmor user-namespace profile on the disposable Ubuntu CI runner and report kernel diagnostics on failure.

- Gate tag artifacts on non-root Bubblewrap live tests; branch and pull-request CI runs only make check.
- Document the local passwd account requirement and explicitly accepted TIOCSTI risk.
- Verify user, mount, PID, IPC, and UTS namespace separation directly.
- Disable nested user namespaces for both shell and run, with a live regression check.
- Mount existing /etc/alternatives read-only for system symlink chains.
- Preserve fallback kill errors after failed signal forwarding.

## 0.8.0

- Release the new implementation as 0.8.0, with the same version in source builds and tag artifacts.
- Reject packaging tags that do not match the source version.
- Use POSIX sh examples throughout both documentation languages and clarify installation requirements and verification behavior.
- Preserve the shared shell/run isolation policy and existing profile storage behavior.

## 0.8.0-rc.2

- Reworked README and user guides around installing and running `abx`, with installation, updates, removal, and persistent profile workflows in both languages.
- Excluded AGENTS.md and AGENTS.ru.md from binary archives; both remain in source archives.
- Changed `make build` to produce `./abx`, matching the installed executable name.

## 0.8.0-rc.1

First candidate of the new implementation, targeting 0.8.0. Compatibility with earlier CLIs and profiles is not guaranteed.

- Agent runs and interactive shells share one Bubblewrap policy, persistent profile homes, and a clean environment.
- npm and Bun settings are configured inside profiles. Projects are mounted writable only for run and verify.
- Storage defaults to .local/share/abx in the passwd home; an absolute XDG_DATA_HOME overrides it, while empty and relative values are ignored.
- Explicit profile create, show, and list commands validate ownership, permissions, and paths without automatically repairing existing data.
- Optional shared skills are mounted read-only.
- Inspect shows the execution plan; verify checks a real sandbox without requiring an installed agent.
- Fixed project validation for system trees relocated through symlinks: their resolved targets, descendants, and ancestors are rejected as projects. Added regressions for redirection and unresolved links.
- Sources are retained by descriptor; signals and child exit status are forwarded, and preparation and cleanup failures are reported.
- Unit and live tests, user and developer guides, and documented security boundaries.
- English is the primary README and documentation language, with Russian translations. Added current maintainer instructions in AGENTS.md; the changelog remains English only.
- Commits and pull requests run checks; binary and source archives with checksums are uploaded only on tag pushes.
