package host

import (
	"os"
	"path/filepath"
	"testing"
)

// A dangling symlink among the sensitive home trees used to abort every ABX
// command: the path existed as a link, so the missing-path branch did not
// apply, and resolving it failed. Uninstalling a program that owned
// ~/.mozilla or ~/.config leaves exactly that behind.
//
// A dangling link shelters nothing, so it has to be treated as a missing path;
// a cycle resolves to nothing ever and is handled the same way. The cases
// below pin which links still do their job.
func TestSensitiveHomeTreeToleratesDanglingSymlink(t *testing.T) {
	account := Account{UID: os.Getuid(), Username: "tester"}

	t.Run("dangling symlink does not block", func(t *testing.T) {
		home := t.TempDir()
		if err := os.Symlink(filepath.Join(home, "removed"), filepath.Join(home, ".netrc")); err != nil {
			t.Fatal(err)
		}
		if _, err := ResolvePaths(accountWithHome(account, home), filepath.Join(home, "data")); err != nil {
			t.Fatalf("dangling symlink blocked every command: %v", err)
		}
	})

	t.Run("symlink cycle does not block", func(t *testing.T) {
		home := t.TempDir()
		if err := os.Symlink(".ssh", filepath.Join(home, ".ssh")); err != nil {
			t.Fatal(err)
		}
		if _, err := ResolvePaths(accountWithHome(account, home), filepath.Join(home, "data")); err != nil {
			t.Fatalf("symlink cycle blocked every command: %v", err)
		}
	})

	t.Run("cycle below a sensitive tree does not block", func(t *testing.T) {
		home := t.TempDir()
		// .gem/credentials is the sensitive path, so this cycle sits above
		// the final component and Lstat itself fails with ELOOP.
		if err := os.Symlink(".gem", filepath.Join(home, ".gem")); err != nil {
			t.Fatal(err)
		}
		if _, err := ResolvePaths(accountWithHome(account, home), filepath.Join(home, "data")); err != nil {
			t.Fatalf("cycle below a sensitive tree blocked every command: %v", err)
		}
	})

	t.Run("file below a sensitive tree does not block", func(t *testing.T) {
		home := t.TempDir()
		// A regular file where .gem should be makes .gem/credentials
		// unreachable: Lstat returns ENOTDIR.
		if err := os.WriteFile(filepath.Join(home, ".gem"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ResolvePaths(accountWithHome(account, home), filepath.Join(home, "data")); err != nil {
			t.Fatalf("non-directory below a sensitive tree blocked every command: %v", err)
		}
	})

	t.Run("working symlink is still resolved", func(t *testing.T) {
		home, real := t.TempDir(), t.TempDir()
		if err := os.Symlink(real, filepath.Join(home, ".config")); err != nil {
			t.Fatal(err)
		}
		// The link points somewhere real, so the overlap rule has to follow
		// it: storing data behind that link must still be refused.
		if _, err := ResolvePaths(accountWithHome(account, home), filepath.Join(real, "abx")); err == nil {
			t.Fatal("data base behind a resolved sensitive symlink was accepted")
		}
	})

	t.Run("missing path is still missing", func(t *testing.T) {
		home := t.TempDir()
		if _, err := ResolvePaths(accountWithHome(account, home), filepath.Join(home, "data")); err != nil {
			t.Fatalf("plain missing sensitive tree blocked: %v", err)
		}
	})
}

// The storage layout is not the only place that used to stop: resolving a
// project walked the same trees, so `abx run` died on the same leftover link.
func TestResolveProjectToleratesDanglingSymlink(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	if err := os.Symlink(filepath.Join(home, "removed"), filepath.Join(home, ".netrc")); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(home, ".local", "share", "abx")
	got, err := ResolveProject(project, home, data)
	if err != nil {
		t.Fatalf("dangling symlink blocked the project: %v", err)
	}
	if got != project {
		t.Fatalf("project resolved to %q, want %q", got, project)
	}
}

func accountWithHome(a Account, home string) Account {
	a.Home = home
	return a
}
