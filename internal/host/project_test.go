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

func TestResolveAcceptsForeignOwnedAndWorldWritableProject(t *testing.T) {
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
	t.Run("foreign owner", func(t *testing.T) {
		if os.Getuid() != 0 {
			t.Skip("foreign-owned project fixture requires root")
		}
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
	})
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

func TestResolveRejectsMissingSensitiveFileBehindSymlinkedParent(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	store := filepath.Join(root, "store")
	data := filepath.Join(home, ".local", "share", "abx")
	for _, dir := range []string{home, store} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(store, filepath.Join(home, ".gem")); err != nil {
		t.Fatal(err)
	}
	// ~/.gem is redirected into the project, but credentials does not exist
	// yet. Resolving must still compare the future location, not the literal
	// path under the home directory.
	got, err := canonicalSensitiveTree(filepath.Join(home, ".gem", "credentials"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(store, "credentials"); got != want {
		t.Fatalf("canonical sensitive path = %q, want %q", got, want)
	}
	if _, err := ResolveProject(store, home, data); err == nil {
		t.Fatal("accepted project that will receive ~/.gem/credentials")
	}
	other := filepath.Join(root, "other")
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveProject(other, home, data); err != nil {
		t.Fatalf("unrelated project rejected: %v", err)
	}
}

func TestCanonicalSensitiveTreeAppendsAcrossDanglingParentLink(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	store := filepath.Join(root, "store")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(store, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(store, "future"), filepath.Join(home, ".gem")); err != nil {
		t.Fatal(err)
	}
	got, err := canonicalSensitiveTree(filepath.Join(home, ".gem", "credentials"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(store, "future", "credentials"); got != want {
		t.Fatalf("canonical sensitive path = %q, want %q", got, want)
	}
}

func TestResolveRejectsRelativeSensitiveLinkUnderSymlinkedParent(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	data := filepath.Join(home, ".local", "share", "abx")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, "gem"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(project, "gem"), filepath.Join(home, ".gem")); err != nil {
		t.Fatal(err)
	}
	// The final link is relative to its physical parent, not to ~/.gem's
	// lexical path in the home directory.
	if err := os.Symlink("../credentials", filepath.Join(project, "gem", "credentials")); err != nil {
		t.Fatal(err)
	}
	got, err := canonicalSensitiveTree(filepath.Join(home, ".gem", "credentials"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(project, "credentials"); got != want {
		t.Fatalf("canonical sensitive path = %q, want %q", got, want)
	}
	if _, err := ResolveProject(project, home, data); err == nil {
		t.Fatal("accepted project that will receive the relative link target")
	}
}

func TestResolveRejectsSensitiveTargetWithDotDotAfterSymlink(t *testing.T) {
	for _, absolute := range []bool{false, true} {
		name := "relative"
		if absolute {
			name = "absolute"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			store := filepath.Join(root, "store")
			project := filepath.Join(root, "project")
			data := filepath.Join(home, "data")
			for _, dir := range []string{home, store, project} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			target := "link/../project"
			if absolute {
				link := filepath.Join(root, "target-link")
				if err := os.Symlink(store, link); err != nil {
					t.Fatal(err)
				}
				target = link + "/../project"
			} else if err := os.Symlink(store, filepath.Join(home, "link")); err != nil {
				t.Fatal(err)
			}
			// The target contains ".." after a symlink component. The kernel
			// resolves the link first, so the real target is project, not a
			// lexically cleaned path under the link's parent.
			if err := os.Symlink(target, filepath.Join(home, ".gem")); err != nil {
				t.Fatal(err)
			}
			got, err := canonicalSensitiveTree(filepath.Join(home, ".gem", "credentials"))
			if err != nil {
				t.Fatal(err)
			}
			want := filepath.Join(project, "credentials")
			if got != want {
				t.Fatalf("canonical sensitive path = %q, want %q", got, want)
			}
			if _, err := ResolveProject(project, home, data); err == nil {
				t.Fatal("accepted project at the real sensitive target")
			}
			// The data base must not be placed at the real sensitive target
			// either; this goes through ResolvePaths rather than ResolveProject.
			if err := os.MkdirAll(want, 0o700); err != nil {
				t.Fatal(err)
			}
			account := Account{UID: os.Getuid(), Username: "tester", Home: home}
			if _, err := ResolvePaths(account, want); err == nil {
				t.Fatal("accepted data base at the real sensitive target")
			}
			// The lexical path under the home directory is not the target the
			// kernel reaches. It must remain usable for an unrelated project
			// and data base.
			legit := filepath.Join(home, "project")
			if err := os.MkdirAll(filepath.Join(legit, "credentials"), 0o700); err != nil {
				t.Fatal(err)
			}
			if got, err := ResolveProject(legit, home, data); err != nil || got != legit {
				t.Fatalf("ResolveProject(%q) = %q, %v", legit, got, err)
			}
			if _, err := ResolvePaths(account, filepath.Join(legit, "credentials")); err != nil {
				t.Fatalf("ResolvePaths rejected the lexical target: %v", err)
			}
		})
	}
}

func TestResolveRejectsSensitiveSymlinkTargetBlockedByFileOrCycle(t *testing.T) {
	for _, target := range []string{"file", "cycle"} {
		t.Run(target, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			project := filepath.Join(root, "project")
			data := filepath.Join(home, ".local", "share", "abx")
			if err := os.MkdirAll(home, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(project, 0o700); err != nil {
				t.Fatal(err)
			}
			gem := filepath.Join(project, "gem")
			if target == "file" {
				if err := os.WriteFile(gem, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Symlink("gem", gem); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(gem, filepath.Join(home, ".gem")); err != nil {
				t.Fatal(err)
			}
			if _, err := ResolveProject(project, home, data); err == nil {
				t.Fatalf("accepted project holding the %s target of ~/.gem", target)
			}
		})
	}
}

func TestResolveRejectsProjectThroughIntermediateSensitiveLink(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	outside := filepath.Join(root, "outside")
	for _, dir := range []string{home, filepath.Join(project, "gem"), outside} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "credentials"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "credentials"), filepath.Join(project, "gem", "credentials")); err != nil {
		t.Fatal(err)
	}
	// ~/.gem points into the project, and the project's final link points
	// outside it. The project is an intermediate target of the sensitive
	// path even though the final EvalSymlinks result is outside the project.
	if err := os.Symlink(filepath.Join(project, "gem"), filepath.Join(home, ".gem")); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveProject(project, home, filepath.Join(home, ".local", "share", "abx")); err == nil {
		t.Fatal("accepted project that is an intermediate sensitive-link target")
	}
}

func TestResolveRejectsSensitiveCycleThroughProject(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	outside := filepath.Join(root, "outside")
	for _, dir := range []string{home, project, outside} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	projectLink := filepath.Join(project, "link")
	outsideLink := filepath.Join(outside, "b")
	if err := os.Symlink(outsideLink, projectLink); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(projectLink, outsideLink); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(projectLink, filepath.Join(home, ".gem")); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveProject(project, home, filepath.Join(home, ".local", "share", "abx")); err == nil {
		t.Fatal("accepted project reached by a sensitive-tree cycle")
	}
}

func TestResolveRejectsMissingSensitivePathUnderNestedSymlinkedParents(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	data := filepath.Join(home, "data")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, "local"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(project, "local"), filepath.Join(home, ".local")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../share", filepath.Join(project, "local", "share")); err != nil {
		t.Fatal(err)
	}
	got, err := canonicalSensitiveTree(filepath.Join(home, ".local", "share", "keyrings"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(project, "share", "keyrings"); got != want {
		t.Fatalf("canonical sensitive path = %q, want %q", got, want)
	}
	if _, err := ResolveProject(project, home, data); err == nil {
		t.Fatal("accepted project that will receive the missing keyrings path")
	}
}

func TestCanonicalSensitiveTreeFollowsDanglingLinkChain(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	store := filepath.Join(root, "store")
	for _, dir := range []string{home, project, store} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(store, "link"), filepath.Join(home, ".gem")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../project/gem", filepath.Join(store, "link")); err != nil {
		t.Fatal(err)
	}
	got, err := canonicalSensitiveTree(filepath.Join(home, ".gem", "credentials"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(project, "gem", "credentials"); got != want {
		t.Fatalf("canonical sensitive path = %q, want %q", got, want)
	}
	if _, err := ResolveProject(project, home, filepath.Join(home, "data")); err == nil {
		t.Fatal("accepted project at the end of a dangling sensitive-link chain")
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

// A protected system tree that does not resolve says only that there is
// nothing to mount at that path: the project itself is already resolved, so
// refusing the session protects nothing. Dangling links, cycles, and paths
// through a non-directory must all leave project commands working.
func TestProjectSystemTreeToleratesUnresolvableRoot(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, base string) string
	}{
		{
			name: "dangling link",
			setup: func(t *testing.T, base string) string {
				protected := filepath.Join(base, "opt")
				if err := os.Symlink(filepath.Join(base, "missing"), protected); err != nil {
					t.Fatal(err)
				}
				return protected
			},
		},
		{
			name: "symlink cycle",
			setup: func(t *testing.T, base string) string {
				protected := filepath.Join(base, "opt")
				if err := os.Symlink(protected, protected); err != nil {
					t.Fatal(err)
				}
				return protected
			},
		},
		{
			name: "non-directory in the middle",
			setup: func(t *testing.T, base string) string {
				blocker := filepath.Join(base, "blocker")
				if err := os.WriteFile(blocker, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(blocker, "opt")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			home, project := filepath.Join(base, "home"), filepath.Join(base, "project")
			for _, p := range []string{home, project} {
				if err := os.Mkdir(p, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			protected := tc.setup(t, base)
			data := filepath.Join(home, "data", "abx")
			if _, err := resolveProject(project, home, data, []string{protected}); err != nil {
				t.Fatalf("unresolvable protected root blocked project: %v", err)
			}
		})
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

func TestExactProtectedRootFollowsSymlinkAlias(t *testing.T) {
	// The exact roots are compared against the canonical project, so an
	// aliased root must match by its resolved target too, not just by the
	// literal the way the system-tree loop already does.
	base := t.TempDir()
	target := filepath.Join(base, "real-tmp")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "tmp-link")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		project, exact string
		want           bool
	}{
		{target, target, true},
		{target, alias, true},
		{filepath.Join(target, "child"), alias, false},
		{filepath.Join(base, "elsewhere"), alias, false},
	} {
		got, err := isExactProtectedRoot(tc.project, tc.exact)
		if err != nil || got != tc.want {
			t.Errorf("isExactProtectedRoot(%q, %q) = %v, %v, want %v", tc.project, tc.exact, got, err, tc.want)
		}
	}
}
