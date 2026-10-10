package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
