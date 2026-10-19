package host

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

func profileStorage(t *testing.T) (Account, Paths) {
	t.Helper()
	base := t.TempDir()
	account := Account{UID: os.Getuid(), Username: "tester", Home: base}
	paths, err := ResolvePaths(account, filepath.Join(base, "missing/data"))
	if err != nil {
		t.Fatal(err)
	}
	return account, paths
}

func TestProfileReadDoesNotCreateStorage(t *testing.T) {
	a, p := profileStorage(t)
	s, err := ShowProfile(a, p, "demo")
	if err != nil || s.Exists || s.Name != "demo" {
		t.Fatalf("show: %+v %v", s, err)
	}
	list, err := ListProfiles(a, p)
	if err != nil || len(list) != 0 {
		t.Fatalf("list: %+v %v", list, err)
	}
	if _, err = os.Lstat(filepath.Dir(p.Root)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read created storage: %v", err)
	}
}

func TestCreateProfileExactModesAndExclusive(t *testing.T) {
	for _, mask := range []int{0, 0o022, 0o777} {
		t.Run(strings.ReplaceAll(os.FileMode(mask).String(), "/", "_"), func(t *testing.T) {
			a, p := profileStorage(t)
			old := unix.Umask(mask)
			home, err := CreateProfile(a, p, "run")
			unix.Umask(old)
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{filepath.Dir(p.Root), p.Root, p.Profiles, home} {
				info, err := os.Lstat(path)
				if err != nil {
					t.Fatal(err)
				}
				if err = privateInfo(path, info, a.UID); err != nil {
					t.Fatal(err)
				}
			}
			entries, err := os.ReadDir(home)
			if err != nil || len(entries) != 0 {
				t.Fatal("new profile is not empty", err)
			}
			marker := filepath.Join(home, "keep")
			if err = os.WriteFile(marker, []byte("preserve"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err = CreateProfile(a, p, "run"); !errors.Is(err, ErrProfileExists) {
				t.Fatalf("duplicate: %v", err)
			}
			raw, err := os.ReadFile(marker)
			if err != nil || string(raw) != "preserve" {
				t.Fatal("changed existing profile", err)
			}
			if _, err = os.Lstat(p.Shared); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("created optional skills", err)
			}
		})
	}
}

func TestProfilesRejectUnsafeLayoutWithoutRepair(t *testing.T) {
	for _, level := range []string{"root", "profiles", "profile"} {
		for _, kind := range []string{"symlink", "file", "mode", "sticky"} {
			t.Run(level+"/"+kind, func(t *testing.T) {
				a, p := profileStorage(t)
				home, err := CreateProfile(a, p, "demo")
				if err != nil {
					t.Fatal(err)
				}
				path := map[string]string{"root": p.Root, "profiles": p.Profiles, "profile": home}[level]
				switch kind {
				case "mode", "sticky":
					mode := os.FileMode(0o755)
					if kind == "sticky" {
						mode = 0o700 | os.ModeSticky
					}
					if err = os.Chmod(path, mode); err != nil {
						t.Fatal(err)
					}
				default:
					if err = os.Rename(path, path+"-saved"); err != nil {
						t.Fatal(err)
					}
					if kind == "symlink" {
						err = os.Symlink(path+"-saved", path)
					} else {
						err = os.WriteFile(path, []byte("keep"), 0o600)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				before, err := os.Lstat(path)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = ShowProfile(a, p, "demo"); err == nil {
					t.Fatal("show accepted unsafe layout")
				}
				if _, err = ListProfiles(a, p); err == nil {
					t.Fatal("list silently accepted unsafe entry")
				}
				if _, err = CreateProfile(a, p, "demo"); err == nil {
					t.Fatal("create accepted unsafe existing object")
				}
				after, err := os.Lstat(path)
				if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
					t.Fatal("repaired or replaced existing object", err)
				}
			})
		}
	}
}

func TestConcurrentProfileCreationHasOneWinner(t *testing.T) {
	a, p := profileStorage(t)
	// Initialize only storage so the test isolates exclusive profile creation.
	if _, err := CreateProfile(a, p, "initial"); err != nil {
		t.Fatal(err)
	}
	const count = 8
	results := make(chan error, count)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range count {
		wg.Go(func() { <-start; _, err := CreateProfile(a, p, "demo"); results <- err })
	}
	close(start)
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		} else if !errors.Is(err, ErrProfileExists) {
			t.Fatal(err)
		}
	}
	if wins != 1 {
		t.Fatalf("successful creators: %d", wins)
	}
}

func TestProfileListAndStatus(t *testing.T) {
	a, p := profileStorage(t)
	for _, name := range []string{"zeta", "alpha"} {
		if _, err := CreateProfile(a, p, name); err != nil {
			t.Fatal(err)
		}
	}
	home := filepath.Join(p.Profiles, "alpha")
	if err := os.MkdirAll(filepath.Join(home, ".bun/bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".bun/bin/alpha"), nil, 0o700); err != nil {
		t.Fatal(err)
	}
	list, err := ListProfiles(a, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "alpha" || list[1].Name != "zeta" || list[0].Executable != "/home/agent/.bun/bin/alpha" || list[1].AgentError == nil {
		t.Fatalf("list: %+v", list)
	}
	if list[0].Skills != "not-configured" {
		t.Fatal("missing skills misreported")
	}
	if err = os.MkdirAll(p.SharedSkills, 0o700); err != nil {
		t.Fatal(err)
	}
	s, err := ShowProfile(a, p, "alpha")
	if err != nil || s.Skills != "configured" {
		t.Fatalf("skills: %+v %v", s, err)
	}
	if _, err = os.Lstat(filepath.Join(home, ".agents")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("show prepared a mount target", err)
	}
	if err = os.Chmod(p.Shared, 0o755); err != nil {
		t.Fatal(err)
	}
	s, err = ShowProfile(a, p, "alpha")
	if err != nil || s.Skills != "invalid" || s.SkillsError == nil {
		t.Fatalf("unsafe optional skills: %+v %v", s, err)
	}
}

func TestProfileInvalidNamesAndOwners(t *testing.T) {
	a, p := profileStorage(t)
	for _, name := range []string{"../escape", "", "bad\nname", "-hidden"} {
		if _, err := CreateProfile(a, p, name); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	if _, err := os.Lstat(p.Root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid names changed storage", err)
	}
	if _, err := CreateProfile(a, p, "demo"); err != nil {
		t.Fatal(err)
	}
	wrong := a
	wrong.UID++
	if _, err := ShowProfile(wrong, p, "demo"); err == nil {
		t.Fatal("wrong owner accepted")
	}
	if _, err := CreateProfile(wrong, p, "other"); err == nil {
		t.Fatal("create ignored storage owner")
	}
	if err := os.Mkdir(filepath.Join(p.Profiles, "bad\nname"), 0o700); err != nil {
		t.Fatal(err)
	}
	list, err := ListProfiles(a, p)
	if err != nil {
		t.Fatalf("invalid entry broke listing: %v", err)
	}
	if len(list) != 1 || list[0].Name != "demo" {
		t.Fatalf("invalid entry not skipped: %+v", list)
	}
}

func TestAgentLookupSkipsUnresolvableEarlierCandidate(t *testing.T) {
	in := fixture(t)
	bad := filepath.Join(in.home, ".local/bin/demo")
	good := filepath.Join(in.home, ".bun/bin/demo")
	for _, p := range []string{bad, good} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("demo", bad); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(good, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := resolveAgent(in.home, "demo")
	if err != nil || got != "/home/agent/.bun/bin/demo" {
		t.Fatalf("fallback: %q %v", got, err)
	}
}

func TestCreateErrorIncludesFullDirectoryPath(t *testing.T) {
	a, paths := profileStorage(t)
	if _, err := CreateProfile(a, paths, "demo"); err != nil {
		t.Fatal(err)
	}
	_, err := CreateProfile(a, paths, "demo")
	if !errors.Is(err, ErrProfileExists) || !strings.Contains(err.Error(), filepath.Join(paths.Profiles, "demo")) {
		t.Fatalf("missing full path or error classification: %v", err)
	}
	// A symlink reports 0777 on its own, so its refusal must name the kind
	// without a mode that would misread as real permissions.
	profile := filepath.Join(paths.Profiles, "demo")
	if err := os.Rename(profile, profile+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(profile+"-saved", profile); err != nil {
		t.Fatal(err)
	}
	if _, err = CreateProfile(a, paths, "demo"); !errors.Is(err, ErrProfileExists) ||
		!strings.Contains(err.Error(), "(existing symlink)") || strings.Contains(err.Error(), "mode 0777") {
		t.Fatalf("symlink refusal text: %v", err)
	}
}

// Between Mkdirat's EEXIST and the diagnostic open, another process of the
// same user can remove the entry; the refusal must say the entry vanished
// instead of claiming it still exists.
func TestCreateProfileNamesVanishedEntry(t *testing.T) {
	parent, err := openDirectoryAt(unix.AT_FDCWD, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = parent.Close() }()
	err = existingObjectError(int(parent.Fd()), "gone")
	if err == nil || errors.Is(err, ErrProfileExists) || !strings.Contains(err.Error(), "disappeared during creation") {
		t.Fatalf("vanished entry reported as %v", err)
	}
}

// profile show and profile list go through ShowProfile, which resolved the
// agent but stopped before the placement check that run performs. A link ending
// outside the profile and outside every mounted system source therefore showed
// up as an available command, and `abx run` refused the same profile.
func TestShowProfileRefusesAgentThatRunWouldRefuse(t *testing.T) {
	a, p := profileStorage(t)
	if _, err := CreateProfile(a, p, "demo"); err != nil {
		t.Fatal(err)
	}
	// A real executable outside the profile, reached by a relative link from
	// the search directory: resolveAgent accepts it, checkAgentPlacement does not.
	outside := filepath.Join(a.Home, "agent-bin")
	if err := os.WriteFile(outside, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(p.Profiles, "demo", ".local", "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	// An absolute link to a real executable outside the profile: resolveAgent
	// accepts it (a regular executable is what a package manager produces),
	// and checkAgentPlacement refuses it because nothing mounts that path
	// inside the sandbox.
	link := filepath.Join(bin, "demo")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	status, err := ShowProfile(a, p, "demo")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if status.Executable != "" {
		t.Fatalf("show reports the agent as available at %q, run would refuse it", status.Executable)
	}
	if status.AgentError == nil {
		t.Fatal("show accepted an agent run refuses, and named no reason")
	}
	list, err := ListProfiles(a, p)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list returned %d entries", len(list))
	}
	if list[0].Executable != "" || list[0].AgentError == nil {
		t.Fatalf("list reports %+v, show reported %+v", list[0], status)
	}
}

// A package manager installs the real binary under a mounted system path and
// points the profile entry at it; the security guide calls this supported and
// run accepts it. The placement check reads the mounted system sources, and
// with none discovered it refused exactly that supported layout.
func TestShowProfileAcceptsAgentInstalledUnderSystemPath(t *testing.T) {
	a, p := profileStorage(t)
	if _, err := CreateProfile(a, p, "demo"); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(p.Profiles, "demo", ".local", "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/usr/bin/true", filepath.Join(bin, "demo")); err != nil {
		t.Fatal(err)
	}
	status, err := ShowProfile(a, p, "demo")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if status.AgentError != nil || status.Executable != "/home/agent/.local/bin/demo" {
		t.Fatalf("show refused an agent run accepts: %+v", status)
	}
	list, err := ListProfiles(a, p)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].AgentError != nil || list[0].Executable != status.Executable {
		t.Fatalf("list reports %+v, show reported %+v", list, status)
	}
}
