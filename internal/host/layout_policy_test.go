package host

import (
	"os"
	"path/filepath"
	"testing"
)

// Pre-release review lesson: project and home checks tightened after v0.8.21
// began refusing legitimate layouts. Filesystems without POSIX modes report
// 0777 for everything, team checkouts are root:dev 2775, container images
// own the home as root, and NFS with root_squash reports an unrelated uid.
// Refusing those is worse than the attack they prevent, which is outside the
// documented same-UID threat model anyway.
func TestProjectPermissionsAreNotConstrained(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	data := filepath.Join(home, ".local", "share", "abx")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []os.FileMode{0o700, 0o755, 0o775, 0o777} {
		p := filepath.Join(base, "p"+mode.String())
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := ResolveProject(p, home, data); err != nil {
			t.Errorf("project with mode %v refused: %v", mode, err)
		}
	}
	if os.Getuid() == 0 {
		foreign := filepath.Join(base, "foreign")
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
}

func TestDataBaseAncestorWritablePolicy(t *testing.T) {
	uid := os.Getuid()
	if uid == 0 {
		uid = 1000
	}
	home := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	account := Account{UID: uid, Username: "tester", Home: home}

	// Group-writable is a team policy (umask 002, shared groups), not an
	// attack; only world-writable without sticky is refused.
	allowed := map[string]os.FileMode{
		"normal":   0o755,
		"umask002": 0o775,
		"sticky":   0o777 | os.ModeSticky,
	}
	for name, mode := range allowed {
		b := filepath.Join(t.TempDir(), name)
		if err := os.Mkdir(b, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(b, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := ResolvePaths(account, filepath.Join(b, "deep")); err != nil {
			t.Errorf("%s ancestor %v refused: %v", name, mode, err)
		}
	}
	b := filepath.Join(t.TempDir(), "world")
	if err := os.Mkdir(b, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(b, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolvePaths(account, filepath.Join(b, "deep")); err == nil {
		t.Error("world-writable ancestor accepted")
	}
}
