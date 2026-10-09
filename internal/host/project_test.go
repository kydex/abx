package host

import (
	"os"
	"path/filepath"
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
	for _, root := range systemRoots {
		if _, err := ResolveProject(root, "/home/tester", "/home/tester/.local/share/abx"); err == nil {
			t.Errorf("accepted protected root %s", root)
		}
	}
	if _, err := ResolveProject("/", "/home/tester", "/home/tester/.local/share/abx"); err == nil {
		t.Error("accepted /")
	}
}

func TestResolveRejectsExactWorldWritableRoots(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	data := filepath.Join(home, ".local", "share", "abx")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, exact := range exactProtectedRoots {
		if _, err := os.Stat(exact); err != nil {
			t.Skipf("protected root %s is absent on this host", exact)
		}
		if _, err := ResolveProject(exact, home, data); err == nil {
			t.Errorf("accepted exact protected root %s", exact)
		}
	}
}

func TestResolveRejectsSiblingHome(t *testing.T) {
	// The sibling-home rule is scoped to the literal /home tree, so the
	// fixture must live there. Use a unique base under /home and remove it.
	base, err := os.MkdirTemp("/home", "abx-sibling-*")
	if err != nil {
		t.Skipf("cannot stage /home fixture: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	realHome := filepath.Join(base, "alice")
	sibling := filepath.Join(base, "bob", "proj")
	own := filepath.Join(realHome, "code", "proj")
	for _, p := range []string{realHome, sibling, own} {
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	data := filepath.Join(realHome, ".local", "share", "abx")
	if _, err := ResolveProject(sibling, realHome, data); err == nil {
		t.Fatalf("accepted sibling home project %s", sibling)
	}
	if got, err := ResolveProject(own, realHome, data); err != nil || got != own {
		t.Fatalf("own-home project rejected: %q, %v", got, err)
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
	for _, relative := range []string{".config", ".ssh", ".gnupg", "dotfiles"} {
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
	for _, relative := range []string{".config", ".ssh", ".gnupg", "dotfiles"} {
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
				if err := os.Symlink(target, filepath.Join(home, relative)); err != nil {
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
