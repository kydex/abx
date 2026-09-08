package host

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/sys/unix"
)

func fixture(t *testing.T) *Inputs {
	t.Helper()
	base := t.TempDir()
	paths := Paths{Root: filepath.Join(base, "abx"), Profiles: filepath.Join(base, "abx/profiles"), Shared: filepath.Join(base, "abx/shared"), SharedSkills: filepath.Join(base, "abx/shared/agents/skills")}
	in := &Inputs{account: Account{UID: os.Getuid(), Username: "tester", Home: filepath.Join(base, "realhome")}, paths: paths, home: filepath.Join(paths.Profiles, "demo"), objects: map[string]object{}, links: map[string]string{}}
	for _, p := range []string{in.account.Home, in.home, paths.SharedSkills} {
		if e := os.MkdirAll(p, 0700); e != nil {
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
	if e = os.Mkdir(in.home, 0700); e != nil {
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
func TestSkillsTargetsRejectSymlinksBeforeMutation(t *testing.T) {
	for _, target := range []string{".agents", ".agents/skills"} {
		t.Run(target, func(t *testing.T) {
			in := fixture(t)
			if target != ".agents" {
				if e := os.Mkdir(filepath.Join(in.home, ".agents"), 0700); e != nil {
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
}
func TestPrepareSkillsExactModeAndClosedOwner(t *testing.T) {
	in := fixture(t)
	if e := in.observeSkills(); e != nil {
		t.Fatal(e)
	}
	old := unix.Umask(0777)
	e := in.Prepare()
	unix.Umask(old)
	if e != nil {
		t.Fatal(e)
	}
	info, e := os.Stat(filepath.Join(in.home, ".agents"))
	if e != nil || info.Mode().Perm() != 0700 {
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
	if e := os.Chmod(in.home, 0755); e != nil {
		t.Fatal(e)
	}
	if e := in.Revalidate(); e == nil {
		t.Fatal("accepted changed profile permissions")
	}
	for _, dir := range []string{".local/bin", ".bun/bin"} {
		p := filepath.Join(in.home, dir, "demo")
		if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			t.Fatal(e)
		}
		mode := os.FileMode(0600)
		if dir == ".bun/bin" {
			mode = 0700
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
func TestDataRootLinkDoesNotModifyTarget(t *testing.T) {
	base := t.TempDir()
	target := t.TempDir()
	if e := os.Symlink(target, filepath.Join(base, "abx")); e != nil {
		t.Fatal(e)
	}
	p, e := ResolvePaths(Account{Home: base}, base)
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
	for _, mode := range []os.FileMode{0700 | os.ModeSetgid, 0700 | os.ModeSticky, 0700 | os.ModeSetuid} {
		if e = os.Chmod(in.home, mode); e != nil {
			t.Fatal(e)
		}
		info, e = os.Stat(in.home)
		if e != nil {
			t.Fatal(e)
		}
		if info.Mode()&(os.ModeSetgid|os.ModeSticky|os.ModeSetuid) == 0 {
			t.Skip("filesystem did not preserve special mode")
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
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, nil, 0001); e != nil {
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
	if err := os.Mkdir(project, 0700); err != nil {
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
	if err := os.Mkdir(project, 0700); err != nil {
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
