package host

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// A mutation sweep found these guards removable with the suite still green:
// replacing the condition with `false` did not fail a single test. Each case
// below states the refusal it pins.

func TestRevalidateRejectsClosedInputs(t *testing.T) {
	in := fixture(t)
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if err := in.Revalidate(); err == nil || !strings.Contains(err.Error(), "inputs are closed") {
		t.Fatalf("revalidate accepted closed inputs: %v", err)
	}
}

func TestResolveRejectsUnknownSessionMode(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	if _, err := resolve(Account{UID: os.Getuid(), Username: "tester", Home: home}, Paths{}, "demo", project, Mode(99), false, nil); err == nil || !strings.Contains(err.Error(), "invalid session mode") {
		t.Fatalf("unknown session mode accepted: %v", err)
	}
}

func TestResolveRequiresAgentForRun(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	root := filepath.Join(home, "data")
	paths := Paths{Root: root, Profiles: filepath.Join(root, "profiles")}
	// The profile exists but carries no entry point, so the refusal has to come
	// from resolving the agent rather than from the profile being absent.
	profileHome := filepath.Join(paths.Profiles, "demo")
	if err := os.MkdirAll(filepath.Join(profileHome, ".local", "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	account := Account{UID: os.Getuid(), Username: "tester", Home: home}
	in, err := resolve(account, paths, "demo", project, Run, true, nil)
	if err == nil {
		t.Fatalf("run mode accepted a profile with no executable: %+v", in.executable)
	}
	if !strings.Contains(err.Error(), "no executable") {
		t.Fatalf("unexpected error for a profile without an entry point: %v", err)
	}
}

func TestProjectRejectsFilesystemRootAndHome(t *testing.T) {
	home := t.TempDir()
	data := filepath.Join(home, "data", "abx")
	if _, err := resolveProject("/", home, data, nil); err == nil || !strings.Contains(err.Error(), "filesystem root") {
		t.Fatalf("filesystem root accepted as a project: %v", err)
	}
	if _, err := resolveProject(home, home, data, nil); err == nil || !strings.Contains(err.Error(), "real home or its ancestor") {
		t.Fatalf("the real home accepted as a project: %v", err)
	}
	if _, err := resolveProject(filepath.Dir(home), home, data, nil); err == nil || !strings.Contains(err.Error(), "real home or its ancestor") {
		t.Fatalf("an ancestor of the real home accepted as a project: %v", err)
	}
}

func TestCheckPasswdFileRejectsNonRegularAndSpecialBits(t *testing.T) {
	dir := t.TempDir()
	t.Run("not a regular file", func(t *testing.T) {
		if err := checkPasswdFile(dir); err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("directory accepted as the passwd database: %v", err)
		}
	})
	t.Run("special bits", func(t *testing.T) {
		path := filepath.Join(dir, "passwd")
		if err := os.WriteFile(path, []byte("x:1\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o600|os.ModeSetuid); err != nil {
			t.Fatal(err)
		}
		if err := checkPasswdFile(path); err == nil || !strings.Contains(err.Error(), "setuid, setgid, or sticky bit") {
			t.Fatalf("passwd database with setuid accepted: %v", err)
		}
	})
}

func TestObserveSkillsRejectsNonDirectory(t *testing.T) {
	in := fixture(t)
	// .agents must be a directory; a regular file there is refused.
	if err := os.WriteFile(filepath.Join(in.home, ".agents"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := in.observeSkills(); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("skills source that is not a directory accepted: %v", err)
	}
}

// The same refusal for the shared side: the skills tree itself, or its parent,
// has to be a directory. The shared paths are built here rather than through
// fixture, because fixture insists they are private directories and this case
// is precisely what happens when they are not.
func TestObserveSkillsRejectsNonDirectorySharedSource(t *testing.T) {
	base := t.TempDir()
	shared := filepath.Join(base, "shared")
	agents := filepath.Join(shared, "agents")
	if err := os.MkdirAll(agents, 0o700); err != nil {
		t.Fatal(err)
	}
	skills := filepath.Join(agents, "skills")
	if err := os.WriteFile(skills, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	in := &Inputs{
		account: Account{UID: os.Getuid(), Username: "tester", Home: filepath.Join(base, "realhome")},
		paths: Paths{
			Root:         filepath.Join(base, "abx"),
			Profiles:     filepath.Join(base, "abx", "profiles"),
			Shared:       shared,
			SharedSkills: skills,
		},
		home:    filepath.Join(base, "abx", "profiles", "demo"),
		objects: map[string]object{},
		links:   map[string]string{},
	}
	t.Cleanup(func() { _ = in.Close() })
	for _, p := range []string{in.account.Home, in.home} {
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := in.private(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := in.observeSkills(); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("skills source that is not a directory accepted: %v", err)
	}
}

// createDirectoryAt is called with exclusive=false for the shared ancestors
// that may already exist. Without that branch a second call would fail on EEXIST
// instead of opening what is there.
func TestCreateDirectoryAtReopensNonExclusiveDirectory(t *testing.T) {
	root := t.TempDir()
	parent, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	first, created, err := createDirectoryAt(parent, "shared", os.Getuid(), false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("the first call did not report creating the directory")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, created, err := createDirectoryAt(parent, "shared", os.Getuid(), false, false)
	if err != nil {
		t.Fatalf("non-exclusive reopen failed: %v", err)
	}
	if created {
		t.Fatal("reopening an existing directory reported a creation")
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}

// discoverLayout takes the host layout as data, so the refusal for a /usr that
// is not a directory can be exercised against an object the test owns. The real
// /usr is always a directory, which is why removing the check left the suite
// green.
func TestDiscoverSystemRejectsNonDirectoryUsr(t *testing.T) {
	in := fixture(t)
	root := t.TempDir()
	usr := filepath.Join(root, "usr")
	if err := os.WriteFile(usr, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := in.discoverLayout(root, systemLayout{usr: "/usr"})
	if err == nil || !strings.Contains(err.Error(), "/usr is not a directory") {
		t.Fatalf("a /usr that is not a directory was accepted: %v", err)
	}
}

// A missing compatibility directory or /etc entry is skipped rather than
// refused, and only /usr is mandatory. The layout is what decides which is
// which, so this pins that distinction against a layout the test owns.
func TestDiscoverLayoutAcceptsAbsentOptionalPaths(t *testing.T) {
	in := fixture(t)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "usr"), 0o755); err != nil {
		t.Fatal(err)
	}
	layout := systemLayout{
		usr:    "/usr",
		compat: []string{"/bin", "/absent"},
		etc:    []string{"hosts", "absent"},
	}
	if err := in.discoverLayout(root, layout); err != nil {
		t.Fatalf("absent optional system paths refused: %v", err)
	}
	if len(in.system) != 1 || in.system[0].Target != "/usr" {
		t.Fatalf("only /usr should survive: %+v", in.system)
	}

	// The same layout with the optional paths present must pick them up.
	in2 := fixture(t)
	if err := os.Mkdir(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc", "hosts"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := in2.discoverLayout(root, layout); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"/usr", "/bin", "/etc/hosts"} {
		found := false
		for _, s := range in2.system {
			if s.Target == target {
				found = true
			}
		}
		if !found {
			t.Errorf("layout did not produce %q: %+v", target, in2.system)
		}
	}
	if len(in2.system) != 3 {
		t.Errorf("layout produced %d sources, want 3: %+v", len(in2.system), in2.system)
	}
}

// A second sweep round. Same rule as above: each case pins one refusal, and
// every one of them was confirmed by deleting the condition and watching the
// suite stay green.

func TestResolveProjectRejectsRelativeWorkingDirectory(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	if _, err := ResolveProject(project, home, filepath.Join(home, "data")); err != nil {
		t.Fatalf("absolute project rejected: %v", err)
	}
	// A relative path resolves against whatever directory abx happens to run
	// in, so the same project would be a different directory next time.
	for _, cwd := range []string{"proj", "./proj", "../proj", ""} {
		if _, err := ResolveProject(cwd, home, filepath.Join(home, "data")); err == nil ||
			!strings.Contains(err.Error(), "not absolute") {
			t.Fatalf("relative working directory %q accepted: %v", cwd, err)
		}
	}
}

func TestCanonicalSensitiveTreeResolvesRelativeLinkTarget(t *testing.T) {
	home := t.TempDir()
	// ~/.netrc -> credentials/ : the link resolves nowhere yet, but it says
	// where it is headed, and that is the place a project must not overlap.
	if err := os.Symlink("credentials", filepath.Join(home, ".netrc")); err != nil {
		t.Fatal(err)
	}
	got, err := canonicalSensitiveTree(filepath.Join(home, ".netrc"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "credentials"); got != want {
		t.Fatalf("relative link target = %q, want %q", got, want)
	}
	// An absolute target is used as written.
	other := t.TempDir()
	if err := os.Symlink(other, filepath.Join(home, ".ssh")); err != nil {
		t.Fatal(err)
	}
	if got, err = canonicalSensitiveTree(filepath.Join(home, ".ssh")); err != nil || got != other {
		t.Fatalf("absolute link target = %q, %v", got, err)
	}
}

func TestCreateDirectoryAtExclusiveRejectsExistingObject(t *testing.T) {
	base := t.TempDir()
	parent, err := openDirectoryAt(unix.AT_FDCWD, base)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	// Open mode reuses an existing directory; exclusive mode refuses it. The
	// difference is the whole point of the flag, so a caller that asks for
	// exclusive must never silently get the other behavior.
	dir, created, err := createDirectoryAt(parent, "shared", os.Getuid(), true, true)
	if err != nil || !created || dir == nil {
		t.Fatalf("exclusive create: %v", err)
	}
	if err = dir.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err = createDirectoryAt(parent, "shared", os.Getuid(), true, true); !errors.Is(err, ErrProfileExists) {
		t.Fatalf("exclusive create reused an existing directory: %v", err)
	}
	reused, created, err := createDirectoryAt(parent, "shared", os.Getuid(), true, false)
	if err != nil || created || reused == nil {
		t.Fatalf("non-exclusive create did not reuse: %v", err)
	}
	if err = reused.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestResolveAgentReportsFirstBrokenCandidate(t *testing.T) {
	home := t.TempDir()
	// Two candidates, both unusable, in the order resolveAgent tries them.
	// The report must name the first: it is the one an operator sees at the
	// documented path, and the later fallback is only a fallback.
	if err := os.MkdirAll(filepath.Join(home, ".local/bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(home, ".local/bin/demo"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "bin/demo"), []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := resolveAgent(home, "demo")
	if err == nil || !strings.Contains(err.Error(), filepath.Join(".local", "bin", "demo")) {
		t.Fatalf("agent error does not name the first candidate: %v", err)
	}
	if err != nil && strings.Contains(err.Error(), "no execute permission") {
		t.Fatalf("reported the fallback instead of the first candidate: %v", err)
	}
}

// The same first-error rule, reached through the other branch: a candidate that
// cannot even be inspected because a parent is not a directory. Here the first
// candidate fails to stat, so only the stat-side guard can keep it.
func TestResolveAgentKeepsFirstStatFailureOverLaterFallback(t *testing.T) {
	home := t.TempDir()
	// .local as a file makes .local/bin/demo unreachable: stat returns ENOTDIR.
	if err := os.WriteFile(filepath.Join(home, ".local"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "bin/demo"), []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := resolveAgent(home, "demo")
	if err == nil {
		t.Fatal("no usable agent reported for unreadable candidate paths")
	}
	if !strings.Contains(err.Error(), filepath.Join(".local", "bin", "demo")) {
		t.Fatalf("reported a later candidate instead of the first: %v", err)
	}
	if strings.Contains(err.Error(), "no execute permission") {
		t.Fatalf("the fallback overwrote the first failure: %v", err)
	}
}

func TestShellFromPasswdSkipsCommentsAndBlankLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "passwd")
	// A commented-out entry for the same uid must not win over the real one.
	data := "# tester:x:1000:1000::/home/tester:/bin/dash\n" +
		"\n" +
		"malformed line without all fields\n" +
		"tester:x:1000:1000::/home/tester:/usr/bin/fish\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := shellFromPasswd(1000, path)
	if err != nil || got != "/usr/bin/fish" {
		t.Fatalf("shellFromPasswd = %q, %v", got, err)
	}
	// Without the skip the comment parses as an entry, so uid 1000 would
	// resolve to /bin/dash and the comment would win.
}

func TestValidateExecutableReportsMissingShellDistinctly(t *testing.T) {
	// The missing case gets its own message because callers surface it to the
	// user as "the shell you configured is gone", not as a generic resolve error.
	_, err := validateExecutable(filepath.Join(t.TempDir(), "absent"))
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing shell reported as %v", err)
	}
	// A dangling symlink is the same situation seen through a link.
	dir := t.TempDir()
	link := filepath.Join(dir, "sh")
	if err := os.Symlink(filepath.Join(dir, "gone"), link); err != nil {
		t.Fatal(err)
	}
	if _, err = validateExecutable(link); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("dangling shell link reported as %v", err)
	}
}

func TestPrepareWithoutSkillsLeavesProfileHomeUntouched(t *testing.T) {
	in := fixture(t)
	if err := in.Prepare(); err != nil {
		t.Fatal(err)
	}
	// No skills configured means nothing to mount, so Prepare must not create
	// the mountpoint: it would leave a directory the user never asked for and
	// that the next run would then find "already there".
	if _, err := os.Lstat(filepath.Join(in.home, ".agents")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Prepare created .agents without skills configured: %v", err)
	}
	entries, err := os.ReadDir(in.home)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("Prepare added entries: %v", entries)
	}
}

// The four refusals added after the 0.8.21 round. Each one narrows what is
// still trusted: the home named by the account database, the storage root, the
// account database itself, and the place the agent binary really lives.

func TestResolvePathsRejectsSetgidDataBaseAncestor(t *testing.T) {
	base := t.TempDir()
	account := Account{UID: os.Getuid(), Username: "tester", Home: base}
	if _, err := ResolvePaths(account, filepath.Join(base, "missing/data")); err != nil {
		t.Fatalf("plain data base rejected: %v", err)
	}
	// The sticky bit is the documented answer to a shared directory and stays
	// allowed; setgid is not, because everything created under the storage
	// root would inherit a group this check never inspects.
	if err := os.Chmod(base, os.ModeSetgid|0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolvePaths(account, filepath.Join(base, "missing/data")); err == nil ||
		!strings.Contains(err.Error(), "setuid or setgid") {
		t.Fatalf("setgid data base ancestor accepted: %v", err)
	}
	if err := os.Chmod(base, os.ModeSticky|0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolvePaths(account, filepath.Join(base, "missing/data")); err != nil {
		t.Fatalf("sticky data base ancestor rejected: %v", err)
	}
}

// The static tail of the agent-placement refusal, pinned as one piece: the
// hint was already reworded once (an absolute host target passes the check
// but points nowhere inside the sandbox), and a silent reword is exactly what
// this assertion exists to prevent.
const agentOutsideHint = "put the binary inside the profile -- link it with a relative target " +
	"such as ../lib/demo -- or under a mounted system path such as /usr"

func TestCheckAgentPlacementRefusesUnmountedTarget(t *testing.T) {
	in := fixture(t)
	// A path that merely shares the mount-point text is outside the mount,
	// even when the matching host object exists: "inside" is a component.
	if err := os.WriteFile(filepath.Join(in.home, "X"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	in.executable = agentMountPoint + "X"
	if err := in.checkAgentPlacement(); err == nil ||
		!strings.Contains(err.Error(), "not inside the agent mount") {
		t.Fatalf("shared prefix accepted: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "agent-bin")
	if err := os.WriteFile(outside, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(in.home, ".local", "bin", "demo")
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	in.executable = filepath.Join(agentMountPoint, ".local", "bin", "demo")
	// A link that leaves the profile and every mounted source resolves to a
	// path the sandbox has no mount for: the session would die at exec time
	// blaming the tool. It has to be refused while the profile is resolved.
	if err := in.checkAgentPlacement(); err == nil ||
		!strings.Contains(err.Error(), "does not mount") ||
		!strings.Contains(err.Error(), agentOutsideHint) {
		t.Fatalf("agent outside the sandbox accepted: %v", err)
	}
	// A link that stays inside the profile is the package-manager layout and
	// must keep working.
	inside := filepath.Join(in.home, ".local", "libexec")
	if err := os.MkdirAll(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inside, "demo"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(inside, "demo"), link); err != nil {
		t.Fatal(err)
	}
	if err := in.checkAgentPlacement(); err != nil {
		t.Fatalf("agent inside the profile refused: %v", err)
	}
	// So must a link into a mounted system source, which is how a system-wide
	// package manager installs an agent.
	if in.system == nil {
		in.system = []Source{{Path: "/usr", Target: "/usr"}}
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/usr/bin/sh", link); err != nil {
		t.Fatal(err)
	}
	if err := in.checkAgentPlacement(); err != nil {
		t.Fatalf("agent in a mounted system path refused: %v", err)
	}
}

// checkAgentPlacement is only worth anything if resolve() actually calls it.
// This walks the real path through resolve(), which reads the login shell from
// the host /etc/passwd first. The database needs no particular owner, only a
// regular file that group and other cannot rewrite, so on an ordinary host the
// walk reaches the agent-placement refusal below; a host without a passwd entry
// for the invoking UID fails loudly here instead of passing silently.
func TestResolveRefusesAgentThatLeavesTheSandbox(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	root := filepath.Join(home, "data")
	paths := Paths{Root: root, Profiles: filepath.Join(root, "profiles")}
	profileHome := filepath.Join(paths.Profiles, "demo")
	binDir := filepath.Join(profileHome, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.WriteFile(outside, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(binDir, "demo")); err != nil {
		t.Fatal(err)
	}
	account := Account{UID: os.Getuid(), Username: "tester", Home: home}
	if _, err := resolve(account, paths, "demo", project, Run, true, nil); err == nil ||
		!strings.Contains(err.Error(), "does not mount") ||
		!strings.Contains(err.Error(), agentOutsideHint) {
		t.Fatalf("resolve accepted an agent the sandbox cannot mount: %v", err)
	}
	// The same layout with the binary inside the profile must still resolve:
	// this is the package-manager case, and refusing it would be the bug.
	if err := os.Remove(filepath.Join(binDir, "demo")); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(profileHome, ".local", "libexec")
	if err := os.MkdirAll(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inside, "demo"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(inside, "demo"), filepath.Join(binDir, "demo")); err != nil {
		t.Fatal(err)
	}
	got, err := resolve(account, paths, "demo", project, Run, true, nil)
	if err != nil {
		t.Fatalf("resolve refused a package-manager layout: %v", err)
	}
	if err = got.Close(); err != nil {
		t.Fatal(err)
	}
}

// checkDataBaseOwner's owner rule lost its only coverage when the session
// tests started skipping on hosts whose passwd database is not root-owned, so
// it is pinned directly here. The uid is a parameter, so no chown is needed.
func TestCheckDataBaseOwnerRequiresInvokingUser(t *testing.T) {
	base := t.TempDir()
	uid := os.Getuid()
	if err := checkDataBaseOwner(base, uid); err != nil {
		t.Fatalf("data base owned by the invoking user refused: %v", err)
	}
	// A data base under an ancestor owned by somebody else would let that
	// somebody replace the storage root, so the walk must refuse it.
	if err := checkDataBaseOwner(base, uid+1); err == nil ||
		!strings.Contains(err.Error(), "not owned by the invoking user") {
		t.Fatalf("foreign-owned data base ancestor accepted: %v", err)
	}
	if err := checkDataBaseOwner(base, uid+1); err == nil &&
		strings.Contains(err.Error(), "world-writable") {
		t.Fatal("owner rule reported a mode problem")
	}
	// A regular file is not a valid ancestor either.
	file := filepath.Join(base, "plain")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkDataBaseOwner(file, uid); err == nil ||
		!strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("regular file accepted as a data base ancestor: %v", err)
	}
}

// Retaining the same path twice must keep one descriptor, not two. Without the
// dedup the second open overwrites the map entry and the first descriptor is
// lost: it stays open for the life of the process and never reaches Close.
// This is observable, not internal bookkeeping — it is a descriptor leak.
func TestRetainTwiceKeepsOneDescriptor(t *testing.T) {
	before := countOpenFDs(t)
	in := fixture(t)
	path := filepath.Join(in.home, "retained")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	objectsBefore := len(in.objects)
	if err := in.retain(path); err != nil {
		t.Fatal(err)
	}
	if got := len(in.objects) - objectsBefore; got != 1 {
		t.Fatalf("one path added %d objects", got)
	}
	afterFirst := countOpenFDs(t)
	if err := in.retain(path); err != nil {
		t.Fatalf("second retain refused an already-retained path: %v", err)
	}
	if got := countOpenFDs(t); got != afterFirst {
		t.Fatalf("second retain opened another descriptor: %d -> %d", afterFirst, got)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if after := countOpenFDs(t); after != before {
		t.Fatalf("descriptor leaked past Close: %d -> %d", before, after)
	}
}

// The rule against mounting another user's home used to require that the
// caller's own home sit under /home. Everywhere else -- /var/lib/users/alice, a
// container root -- the rule was silently off and /home/bob/proj mounted
// writable. The home arrangement is irrelevant to the rule, so the cases below
// vary it and expect the same answer.
func TestSiblingHomeRuleDoesNotDependOnHomeLayout(t *testing.T) {
	for _, homeLayout := range []string{
		"/home/alice",          // the ordinary arrangement
		"/var/lib/users/alice", // a system that stores homes elsewhere
		"/root",                // a container running as root's neighbour
	} {
		t.Run(homeLayout, func(t *testing.T) {
			// Whoever I am, a directory under /home that is not my own home is
			// another user's and must be refused.
			for _, project := range []string{"/home/bob/project", "/home/bob"} {
				if !siblingOutsideHome(homeLayout, project, "/home") {
					t.Errorf("accepted another user's home %s with own home %s", project, homeLayout)
				}
			}
			// Outside the shared home root the rule has nothing to say, whatever
			// my own home looks like.
			for _, project := range []string{"/tmp/build/project", "/homework/project", homeLayout + "/code/mine"} {
				if siblingOutsideHome(homeLayout, project, "/home") {
					t.Errorf("refused %s with own home %s", project, homeLayout)
				}
			}
		})
	}
}

// These three refusals are plain input checks, reachable by calling the
// functions with what a broken account database would hand over. The mutation
// sweep reported all three as uncovered because nothing called them that way:
// every existing test builds its fixture from os.Getuid() and a real home, so
// the guards were never on the path being exercised.
func TestAccountEntryMustNameARealNonRootUser(t *testing.T) {
	uid := os.Getuid()
	for _, tc := range []struct {
		name     string
		uid      int
		username string
		home     string
		want     string
	}{
		{"zero uid", 0, "tester", "/home/tester", "invalid non-root uid"},
		{"negative uid", -1, "tester", "/home/tester", "invalid non-root uid"},
		{"empty username", uid, "", "/home/tester", "empty username"},
		{"relative home", uid, "tester", "home/tester", "not absolute"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := accountFromPasswd(tc.uid, tc.username, tc.home); err == nil ||
				!strings.Contains(err.Error(), tc.want) {
				t.Fatalf("accepted %s: %v", tc.name, err)
			}
		})
	}
	if _, err := accountFromPasswd(uid, "tester", t.TempDir()); err != nil {
		t.Fatalf("ordinary entry refused: %v", err)
	}
}

func TestResolvePathsRequiresAbsoluteHome(t *testing.T) {
	for _, home := range []string{"relative/home", "", ".", "home/../.."} {
		if _, err := ResolvePaths(Account{UID: os.Getuid(), Username: "tester", Home: home}, ""); err == nil {
			t.Fatalf("accepted home %q", home)
		}
	}
}
