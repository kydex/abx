package host

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func fixture(t *testing.T) *Inputs {
	t.Helper()
	base := t.TempDir()
	paths := Paths{Root: filepath.Join(base, "abx"), Profiles: filepath.Join(base, "abx/profiles"), Shared: filepath.Join(base, "abx/shared"), SharedSkills: filepath.Join(base, "abx/shared/agents/skills")}
	in := &Inputs{account: Account{UID: os.Getuid(), Username: "tester", Home: filepath.Join(base, "realhome")}, paths: paths, home: filepath.Join(paths.Profiles, "demo"), objects: map[string]object{}, links: map[string]string{}}
	for _, p := range []string{in.account.Home, in.home, paths.SharedSkills} {
		if e := os.MkdirAll(p, 0o700); e != nil {
			t.Fatal(e)
		}
	}
	for _, p := range []string{paths.Root, paths.Profiles, in.home} {
		if e := in.private(p); e != nil {
			t.Fatal(e)
		}
	}
	t.Cleanup(func() {
		if e := in.Close(); e != nil {
			t.Error(e)
		}
	})
	return in
}

func TestSourcesRemainBoundToOpenedObject(t *testing.T) {
	in := fixture(t)
	f, e := in.SourceFile(in.home)
	if e != nil {
		t.Fatal(e)
	}
	before, e := f.Stat()
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Rename(in.home, in.home+"-original"); e != nil {
		t.Fatal(e)
	}
	if e = os.Mkdir(in.home, 0o700); e != nil {
		t.Fatal(e)
	}
	after, e := f.Stat()
	if e != nil || !os.SameFile(before, after) {
		t.Fatal("retained descriptor changed")
	}
	if e = in.Revalidate(); e == nil {
		t.Fatal("path replacement accepted")
	}
	if e = in.Close(); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Stat(); e == nil {
		t.Fatal("leaked descriptor")
	}
	if e = in.Close(); e != nil {
		t.Fatal(e)
	}
}

func countOpenFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skipf("no /proc/self/fd: %v", err)
	}
	return len(entries)
}

func TestCreateProfileDoesNotLeakFDs(t *testing.T) {
	before := countOpenFDs(t)
	base := t.TempDir()
	home := filepath.Join(base, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	account := Account{UID: os.Getuid(), Username: "tester", Home: home}
	paths, err := ResolvePaths(account, filepath.Join(base, "data"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateProfile(account, paths, "demo"); err != nil {
		t.Fatal(err)
	}
	if got := countOpenFDs(t); got != before {
		t.Fatalf("CreateProfile leaked %d descriptors", got-before)
	}
}

func TestDiscoverSystemRejectsUnsafeCompatLinks(t *testing.T) {
	t.Run("link into home rejected", func(t *testing.T) {
		in := fixture(t)
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, "usr"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(in.account.Home, filepath.Join(root, "bin")); err != nil {
			t.Fatal(err)
		}
		if err := in.discoverSystem(root); err == nil {
			t.Fatal("accepted compat link into home")
		}
	})
	t.Run("link into system accepted", func(t *testing.T) {
		in := fixture(t)
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, "usr"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root, "usr"), filepath.Join(root, "bin")); err != nil {
			t.Fatal(err)
		}
		if err := in.discoverSystem(root); err != nil {
			t.Fatalf("rejected safe compat link: %v", err)
		}
	})
	t.Run("dangling link rejected", func(t *testing.T) {
		in := fixture(t)
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, "usr"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "bin")); err != nil {
			t.Fatal(err)
		}
		if err := in.discoverSystem(root); err == nil {
			t.Fatal("accepted dangling compat link")
		}
	})
}

func TestDiscoverSystemMatchesPinnedPolicy(t *testing.T) {
	// Independent literals: changing discovery or allowedSystemTarget
	// (internal/sandbox/plan.go) requires updating this list too.
	allowed := map[string]bool{
		"/usr": true, "/bin": true, "/sbin": true, "/lib": true, "/lib64": true,
		"/etc/hosts": true, "/etc/resolv.conf": true, "/etc/nsswitch.conf": true,
		"/etc/gai.conf": true, "/etc/passwd": true, "/etc/group": true,
		"/etc/ssl": true, "/etc/ca-certificates": true, "/etc/pki": true,
		"/etc/ld.so.cache": true, "/etc/ld.so.conf": true, "/etc/ld.so.conf.d": true,
		"/etc/localtime": true, "/etc/timezone": true, "/etc/os-release": true,
		"/etc/shells": true, "/etc/alternatives": true,
	}
	in := fixture(t)
	if err := in.discoverSystem("/"); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, s := range in.system {
		if !allowed[s.Target] {
			t.Errorf("discovered target %q outside pinned policy", s.Target)
		}
		seen[s.Target] = true
	}
	if !seen["/usr"] {
		t.Error("/usr not discovered")
	}
	for _, p := range []string{"/bin", "/sbin", "/lib", "/lib64"} {
		if _, err := os.Lstat(p); err != nil {
			continue
		}
		if !seen[p] {
			t.Errorf("present %s not discovered", p)
		}
	}
	for _, name := range []string{"hosts", "resolv.conf", "nsswitch.conf", "gai.conf", "passwd", "group", "ssl", "ca-certificates", "pki", "ld.so.cache", "ld.so.conf", "ld.so.conf.d", "localtime", "timezone", "os-release", "shells", "alternatives"} {
		if _, err := os.Lstat("/etc/" + name); err != nil {
			continue
		}
		if !seen["/etc/"+name] {
			t.Errorf("present /etc/%s not discovered", name)
		}
	}
}

func TestVisibleInSandboxRootsPinned(t *testing.T) {
	for path, want := range map[string]bool{
		"/usr/bin/sh": true, "/usr": true, "/bin/sh": true,
		"/etc/alternatives": true, "/etc/alternatives/mplayer": true,
		"/etc/passwd": false, "/etc/shadow": false, "/etc/group": false,
		"/opt/tool": false, "/home/u/sh": false, "/sbin/init": true,
	} {
		if got := visibleInSandbox(path); got != want {
			t.Errorf("visibleInSandbox(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestSkillsTargetsRejectSymlinksBeforeMutation(t *testing.T) {
	for _, target := range []string{".agents", ".agents/skills"} {
		t.Run(target, func(t *testing.T) {
			in := fixture(t)
			if target != ".agents" {
				if e := os.Mkdir(filepath.Join(in.home, ".agents"), 0o700); e != nil {
					t.Fatal(e)
				}
			}
			outside := t.TempDir()
			if e := os.Symlink(outside, filepath.Join(in.home, target)); e != nil {
				t.Fatal(e)
			}
			if e := in.observeSkills(); e == nil {
				t.Fatal("accepted symlink target")
			}
			entries, e := os.ReadDir(outside)
			if e != nil || len(entries) != 0 {
				t.Fatal("modified external target")
			}
		})
	}
	// The flag under test is RESOLVE_NO_SYMLINKS: a target inside the home must
	// fail too, not just one outside it. The target has to be RELATIVE for that
	// to hold — RESOLVE_BENEATH alone already rejects an absolute target with
	// EXDEV, so an absolute symlink here would keep the test green with the
	// flag removed. A relative target passes RESOLVE_BENEATH and is rejected
	// only by RESOLVE_NO_SYMLINKS, so this subtest is what actually pins the
	// flag.
	for _, target := range []string{".agents", ".agents/skills"} {
		t.Run("inside-home-"+target, func(t *testing.T) {
			in := fixture(t)
			if target != ".agents" {
				if e := os.Mkdir(filepath.Join(in.home, ".agents"), 0o700); e != nil {
					t.Fatal(e)
				}
			}
			inside := filepath.Join(in.home, "other")
			if e := os.Mkdir(inside, 0o700); e != nil {
				t.Fatal(e)
			}
			link := filepath.Join(in.home, target)
			relative, e := filepath.Rel(filepath.Dir(link), inside)
			if e != nil {
				t.Fatal(e)
			}
			if e := os.Symlink(relative, link); e != nil {
				t.Fatal(e)
			}
			if e := in.observeSkills(); e == nil {
				t.Fatal("accepted inside-home symlink target")
			}
		})
	}
}

func TestPrepareSkillsExactModeAndClosedOwner(t *testing.T) {
	in := fixture(t)
	if e := in.observeSkills(); e != nil {
		t.Fatal(e)
	}
	old := unix.Umask(0o777)
	e := in.Prepare()
	unix.Umask(old)
	if e != nil {
		t.Fatal(e)
	}
	info, e := os.Stat(filepath.Join(in.home, ".agents"))
	if e != nil || info.Mode().Perm() != 0o700 {
		t.Fatal("incorrect preparation mode")
	}
	if e = in.Close(); e != nil {
		t.Fatal(e)
	}
	if e = in.Prepare(); e == nil {
		t.Fatal("closed owner prepared files")
	}
}

func TestSkillsSourceSymlinksRejected(t *testing.T) {
	in := fixture(t)
	if e := os.Remove(in.paths.SharedSkills); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(t.TempDir(), in.paths.SharedSkills); e != nil {
		t.Fatal(e)
	}
	if e := in.observeSkills(); e == nil {
		t.Fatal("accepted linked source")
	}
}

func TestPermissionAndAgentLookup(t *testing.T) {
	in := fixture(t)
	if e := os.Chmod(in.home, 0o755); e != nil {
		t.Fatal(e)
	}
	if e := in.Revalidate(); e == nil {
		t.Fatal("accepted changed profile permissions")
	}
	for _, dir := range []string{".local/bin", ".bun/bin"} {
		p := filepath.Join(in.home, dir, "demo")
		if e := os.MkdirAll(filepath.Dir(p), 0o700); e != nil {
			t.Fatal(e)
		}
		mode := os.FileMode(0o600)
		if dir == ".bun/bin" {
			mode = 0o700
		}
		if e := os.WriteFile(p, []byte("#!/bin/sh\n"), mode); e != nil {
			t.Fatal(e)
		}
	}
	got, e := resolveAgent(in.home, "demo")
	if e != nil || got != "/home/agent/.bun/bin/demo" {
		t.Fatalf("lookup=%q %v", got, e)
	}
}

func TestAgentLookupFallsBackToMiseShims(t *testing.T) {
	in := fixture(t)
	shim := filepath.Join(in.home, ".local/share/mise/shims/demo")
	if e := os.MkdirAll(filepath.Dir(shim), 0o700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(shim, []byte("#!/bin/sh\n"), 0o700); e != nil {
		t.Fatal(e)
	}
	got, e := resolveAgent(in.home, "demo")
	if e != nil || got != "/home/agent/.local/share/mise/shims/demo" {
		t.Fatalf("lookup=%q %v", got, e)
	}
	managed := filepath.Join(in.home, ".bun/bin/demo")
	if e := os.MkdirAll(filepath.Dir(managed), 0o700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(managed, []byte("#!/bin/sh\n"), 0o700); e != nil {
		t.Fatal(e)
	}
	got, e = resolveAgent(in.home, "demo")
	if e != nil || got != "/home/agent/.local/share/mise/shims/demo" {
		t.Fatalf("shims lost to .bun/bin: %q %v", got, e)
	}
	explicit := filepath.Join(in.home, ".local/bin/demo")
	if e := os.MkdirAll(filepath.Dir(explicit), 0o700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(explicit, []byte("#!/bin/sh\n"), 0o700); e != nil {
		t.Fatal(e)
	}
	got, e = resolveAgent(in.home, "demo")
	if e != nil || got != "/home/agent/.local/bin/demo" {
		t.Fatalf("explicit install lost to shims: %q %v", got, e)
	}
}

func TestDataRootLinkDoesNotModifyTarget(t *testing.T) {
	base := t.TempDir()
	target := t.TempDir()
	if e := os.Symlink(target, filepath.Join(base, "abx")); e != nil {
		t.Fatal(e)
	}
	p, e := ResolvePaths(Account{UID: os.Getuid(), Home: base}, base)
	if e != nil {
		t.Fatal(e)
	}
	in := &Inputs{objects: map[string]object{}}
	if e = in.retain(p.Root); e == nil {
		t.Fatal("accepted data-root symlink")
	}
	if e = in.Close(); e != nil {
		t.Fatal(e)
	}
	entries, e := os.ReadDir(target)
	if e != nil || len(entries) != 0 {
		t.Fatal("changed target")
	}
}

func TestResolveFailureDoesNotReturnOwner(t *testing.T) {
	in := fixture(t)
	result, e := Resolve(in.account, in.paths, "missing", "", Shell, nil)
	if e == nil || result != nil {
		t.Fatalf("failed resolve=%v %v", result, e)
	}
	if _, e = os.Stat(filepath.Join(in.paths.Profiles, "missing")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("created missing profile")
	}
}

func TestShellResolutionDoesNotRequireProjectOrAgent(t *testing.T) {
	in := fixture(t)
	got, e := Resolve(in.account, in.paths, "demo", "/definitely/missing/project", Shell, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer got.Close()
	if got.Project() != "" || len(got.Command()) != 2 || got.Command()[1] != "-l" {
		t.Fatal("shell resolved an agent or project")
	}
}

func TestPrivateOwnerAndSpecialModes(t *testing.T) {
	in := fixture(t)
	info, e := os.Stat(in.home)
	if e != nil {
		t.Fatal(e)
	}
	if e = privateInfo(in.home, info, os.Getuid()+1); e == nil {
		t.Fatal("wrong expected owner accepted")
	}
	for _, mode := range []os.FileMode{0o700 | os.ModeSetgid, 0o700 | os.ModeSticky, 0o700 | os.ModeSetuid} {
		if e = os.Chmod(in.home, mode); e != nil {
			t.Fatal(e)
		}
		info, e = os.Stat(in.home)
		if e != nil {
			t.Fatal(e)
		}
		if info.Mode()&(os.ModeSetgid|os.ModeSticky|os.ModeSetuid) == 0 {
			t.Log("filesystem did not preserve special mode")
			continue
		}
		if e = privateInfo(in.home, info, os.Getuid()); e == nil {
			t.Fatal("special mode accepted")
		}
	}
}

func TestAgentXOKAsInvokingUser(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("requires non-root permission semantics")
	}
	in := fixture(t)
	p := filepath.Join(in.home, "bin/demo")
	if e := os.MkdirAll(filepath.Dir(p), 0o700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, nil, 0o001); e != nil {
		t.Fatal(e)
	}
	if _, e := resolveAgent(in.home, "demo"); e == nil {
		t.Fatal("other-only execute accepted")
	}
}

func TestVerificationResolvesRunProjectWithoutAgent(t *testing.T) {
	in := fixture(t)
	project := t.TempDir()
	got, err := ResolveVerification(in.account, in.paths, "demo", project)
	if err != nil {
		t.Fatal(err)
	}
	defer got.Close()
	if got.Project() != project || len(got.Command()) != 0 {
		t.Fatal("verify selected an agent or wrong project")
	}
	if _, err = ResolveVerification(in.account, in.paths, "demo", in.account.Home); err == nil {
		t.Fatal("verify bypassed protected project checks")
	}
}

func TestCloseReportsDescriptorFailureAndReleasesOwnership(t *testing.T) {
	in := fixture(t)
	var retained *os.File
	for _, o := range in.objects {
		retained = o.file
		break
	}
	if retained == nil {
		t.Fatal("fixture retained no source descriptors")
	}
	if err := retained.Close(); err != nil {
		t.Fatal(err)
	}
	if err := in.Close(); err == nil {
		t.Fatal("descriptor close failure was lost")
	}
	if len(in.objects) != 0 {
		t.Fatal("Close retained ownership after failure")
	}
	if err := in.Close(); err != nil {
		t.Fatal("second Close should be harmless", err)
	}
	if _, err := in.SourceFile(in.home); err == nil {
		t.Fatal("closed Inputs still lent a source descriptor")
	}
}

func TestWorkResolvesProjectWithoutAgentAndRetainsSource(t *testing.T) {
	in := fixture(t)
	project := filepath.Join(in.account.Home, "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve(in.account, in.paths, "demo", project, Work, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer got.Close()
	selection := ResolveShell(in.account.UID)
	if got.Mode() != Work || got.Project() != project || got.executable != "" || !slices.Equal(got.Command(), selection.Argv) {
		t.Fatalf("work: mode=%v project=%q command=%q", got.Mode(), got.Project(), got.Command())
	}
	f, err := got.SourceFile(project)
	if err != nil {
		t.Fatal(err)
	}
	before, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(project, project+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("retained source changed: %v", err)
	}
	if err := got.Revalidate(); err == nil {
		t.Fatal("accepted replaced work project")
	}
}

func TestWorkRejectsUnsafeProjectAndArguments(t *testing.T) {
	in := fixture(t)
	for _, project := range []string{"/", in.account.Home, in.paths.Root, filepath.Join(in.account.Home, "missing")} {
		got, err := Resolve(in.account, in.paths, "demo", project, Work, nil)
		if got != nil {
			got.Close()
		}
		if err == nil {
			t.Fatalf("accepted project %q", project)
		}
	}
	for _, mode := range []Mode{0, Shell, Work} {
		got, err := Resolve(in.account, in.paths, "demo", "", mode, []string{"-c", "echo bad"})
		if got != nil {
			got.Close()
		}
		if err == nil {
			t.Fatalf("accepted mode/arguments: %v", mode)
		}
	}
}

func TestDiagnosticReasonsForInvalidProfileObjects(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "bin", "demo")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := privateInfo(path, info, os.Getuid()); err == nil || !strings.Contains(err.Error(), "is not a directory") {
		t.Fatalf("file misdiagnosed as a permissions error: %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveAgent(home, "demo"); err == nil || !strings.Contains(err.Error(), "no execute permission bits") {
		t.Fatalf("missing execute permissions: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveAgent(home, "demo"); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("directory executable: %v", err)
	}
}
