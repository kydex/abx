package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveAllowsNormalProjectAndCanonicalizes(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(home, "code", "project")
	data := filepath.Join(home, ".local", "share", "abx")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "project-link")
	if err := os.Symlink(project, link); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveProject(link, home, data)
	if err != nil || got != project {
		t.Fatalf("Resolve = %q, %v", got, err)
	}
}

func TestResolveRejectsProtectedSystemTrees(t *testing.T) {
	// Pinned literals: changing systemRoots requires updating this list too.
	for _, root := range []string{
		"/bin", "/boot", "/dev", "/etc", "/lib", "/lib64", "/opt", "/proc",
		"/root", "/run", "/sbin", "/sys", "/usr", "/var",
		"/srv", "/mnt", "/media", "/snap",
	} {
		if _, err := ResolveProject(root, "/home/tester", "/home/tester/.local/share/abx"); err == nil {
			t.Errorf("accepted protected root %s", root)
		}
	}
	if _, err := ResolveProject("/", "/home/tester", "/home/tester/.local/share/abx"); err == nil {
		t.Error("accepted /")
	}
}

func TestResolveRejectsExactWorldWritableRoots(t *testing.T) {
	// The home must sit outside /tmp, and it does not have to exist: the refusal
	// is decided by path arithmetic alone. With a t.TempDir home every case below
	// was already refused by another rule -- /tmp as an ancestor of the home,
	// /var/tmp and /dev/shm as overlapping /var and /dev -- so the loop never
	// ran and the guard it covers was uncovered while this test stayed green.
	const home = "/opt/abx-exact-root-fixture"
	data := filepath.Join(home, ".local", "share", "abx")

	for _, exact := range []string{"/tmp", "/var/tmp", "/dev/shm"} {
		_, err := ResolveProject(exact, home, data)
		if err == nil {
			t.Fatalf("accepted exact protected root %s", exact)
		}
		// The specific refusal, not merely any: a different rule answering here
		// is what made the earlier version of this test say nothing.
		if !strings.Contains(err.Error(), "protected system path") {
			t.Errorf("root %s refused by the wrong rule: %v", exact, err)
		}
	}
	// Sanity check on the fixture itself: an overlap is still an overlap, so a
	// failure above cannot be blamed on the synthetic home.
	if _, err := ResolveProject("/etc", home, data); err == nil ||
		strings.Contains(err.Error(), "protected system path") {
		t.Errorf("fixture home does not isolate the exact rule: %v", err)
	}
}

func TestResolveRejectsHomeItself(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	data := filepath.Join(home, ".local", "share", "abx")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveProject(home, home, data); err == nil {
		t.Error("accepted the real home itself as project")
	}
}

func TestResolveRejectsForeignOwnedAndWorldWritableProject(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	data := filepath.Join(home, ".local", "share", "abx")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	own := filepath.Join(root, "own")
	if err := os.Mkdir(own, 0o700); err != nil {
		t.Fatal(err)
	}
	// Project ownership and mode are NOT constrained: team checkouts
	// (root:dev 2775), shared volumes, and filesystems without POSIX modes
	// (FAT32/exFAT/CIFS/sshfs, always 0777) are legitimate. Nothing here
	// should refuse a writable project directory.
	for _, mode := range []os.FileMode{0o755, 0o775, 0o777} {
		wide := filepath.Join(root, "mode-"+mode.String())
		if err := os.Mkdir(wide, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(wide, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := ResolveProject(wide, home, data); err != nil {
			t.Errorf("project with mode %v refused: %v", mode, err)
		}
	}
	if os.Getuid() == 0 {
		foreign := filepath.Join(root, "foreign")
		if err := os.Mkdir(foreign, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(foreign, 65534, 65534); err != nil {
			t.Fatal(err)
		}
		if _, err := ResolveProject(foreign, home, data); err != nil {
			t.Errorf("foreign-owned project refused: %v", err)
		}
	}
	if _, err := ResolveProject(own, home, data); err != nil {
		t.Errorf("owned project rejected: %v", err)
	}
}

func TestResolveRejectsHomeAndSensitiveOverlaps(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "users", "tester")
	data := filepath.Join(home, ".local", "share", "abx")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	candidates := []string{filepath.Dir(home), home, data, filepath.Dir(data), filepath.Join(data, "profiles")}
	for _, relative := range []string{".config", ".ssh", ".gnupg", "dotfiles", ".aws", ".docker", ".kube", ".pki", ".cache", ".netrc", ".git-credentials", ".password-store", ".npmrc", ".gem/credentials", ".local/share/keyrings", ".mozilla", ".thunderbird"} {
		sensitive := filepath.Join(home, relative)
		candidates = append(candidates, sensitive, filepath.Join(sensitive, "child"))
	}
	for _, candidate := range candidates {
		if err := os.MkdirAll(candidate, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := ResolveProject(candidate, home, data); err == nil {
			t.Errorf("accepted %s", candidate)
		}
	}
}

func TestResolveRejectsCanonicalSensitiveSymlinkTarget(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	target := filepath.Join(root, "external-config")
	project := filepath.Join(target, "project")
	data := filepath.Join(home, ".local", "share", "abx")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(home, ".config")); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveProject(project, home, data); err == nil {
		t.Fatal("accepted project inside canonical target of ~/.config")
	}
}

func TestResolveRejectsDanglingSensitiveSymlinkTargetAncestors(t *testing.T) {
	for _, relative := range []string{".config", ".ssh", ".gnupg", "dotfiles", ".aws", ".docker", ".kube", ".pki", ".cache", ".netrc", ".git-credentials", ".password-store", ".npmrc", ".gem/credentials", ".local/share/keyrings", ".mozilla", ".thunderbird"} {
		for _, targetSuffix := range []string{"future", filepath.Join("missing", "future")} {
			t.Run(relative+"-"+targetSuffix, func(t *testing.T) {
				root := t.TempDir()
				home := filepath.Join(root, "home")
				external := filepath.Join(root, "external")
				if err := os.Mkdir(home, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(external, 0o700); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(external, targetSuffix)
				link := filepath.Join(home, relative)
				if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, link); err != nil {
					t.Fatal(err)
				}
				data := filepath.Join(home, ".local", "share", "abx")
				if _, err := ResolveProject(external, home, data); err == nil {
					t.Fatalf("accepted project above dangling %s target %q", relative, target)
				}
			})
		}
	}
}

func TestResolveRejectsRelativeAndFile(t *testing.T) {
	if _, err := ResolveProject("relative", "/home/u", "/data"); err == nil {
		t.Error("accepted relative cwd")
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveProject(file, "/home/u", "/data"); err == nil {
		t.Error("accepted file")
	}
}

func TestWithinBoundaries(t *testing.T) {
	if !within("/usr", "/usr") || !within("/usr/bin", "/usr") || within("/usrx", "/usr") || within("/us", "/usr") {
		t.Fatal("within boundary behavior is incorrect")
	}
}

// A relocated system tree must not become an allowed writable project.
func TestProjectRejectsRelocatedSystemTree(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	target := filepath.Join(base, "storage", "system")
	alias := filepath.Join(base, "opt")
	sibling := filepath.Join(base, "storage", "system-project")
	for _, p := range []string{home, filepath.Join(target, "child"), sibling} {
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(home, "data", "abx")
	for _, project := range []string{alias, target, filepath.Join(alias, "child"), filepath.Join(target, "child"), filepath.Dir(target)} {
		if _, err := resolveProject(project, home, data, []string{alias}); err == nil {
			t.Errorf("accepted project exposing relocated system tree: %s", project)
		}
	}
	if got, err := resolveProject(sibling, home, data, []string{alias}); err != nil || got != sibling {
		t.Fatalf("unrelated sibling rejected: %q, %v", got, err)
	}
}

func TestProjectSystemTreeResolutionFailures(t *testing.T) {
	base := t.TempDir()
	home, project := filepath.Join(base, "home"), filepath.Join(base, "project")
	for _, p := range []string{home, project} {
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	protected := filepath.Join(base, "opt")
	data := filepath.Join(home, "data", "abx")
	if _, err := resolveProject(project, home, data, []string{protected}); err != nil {
		t.Fatalf("absent optional system tree blocked project: %v", err)
	}
	if err := os.Symlink(filepath.Join(base, "missing"), protected); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveProject(project, home, data, []string{protected}); err == nil {
		t.Fatal("unresolved existing system link was ignored")
	}
}

func TestSiblingOutsideHomeWithSymlinkedHomeRoot(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "realhome")
	alice := filepath.Join(real, "alice")
	if err := os.MkdirAll(alice, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "home")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if !siblingOutsideHome(alice, filepath.Join(real, "bob", "proj"), link) {
		t.Error("sibling project accepted under symlinked home root")
	}
	if siblingOutsideHome(alice, filepath.Join(alice, "proj"), link) {
		t.Error("own project rejected under symlinked home root")
	}
}

// docs/security.md names a sibling home as a rejected project, and the rule
// lives in resolveProject rather than in the predicate. resolveProject scopes it
// to the literal /home so that isolated trees under /tmp keep working, so the
// case has to be built there: an integration test anywhere else would pass
// without ever reaching the check.
func TestProjectRejectsSiblingHome(t *testing.T) {
	root, err := os.MkdirTemp("/home", "abx-sibling-")
	if err != nil {
		t.Skipf("cannot build a test tree under /home: %v", err)
	}
	defer func() {
		if e := os.RemoveAll(root); e != nil {
			t.Errorf("remove test tree: %v", e)
		}
	}()
	alice, bobProj, own := filepath.Join(root, "alice"), filepath.Join(root, "bob", "proj"), filepath.Join(root, "alice", "proj")
	for _, p := range []string{alice, own, filepath.Dir(bobProj), bobProj} {
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	data := filepath.Join(root, "data", "abx")
	if _, err := resolveProject(bobProj, alice, data, nil); err == nil {
		t.Error("accepted a project in another user's home")
	}
	if got, err := resolveProject(own, alice, data, nil); err != nil || got != own {
		t.Fatalf("own project rejected: %q, %v", got, err)
	}
}
