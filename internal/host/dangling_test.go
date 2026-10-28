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

// A ".." after a missing component used to be applied lexically: the target
// collapsed to its parent and the data-base overlap check refused every
// command. The kernel cannot take the parent of a directory that does not
// exist, so the path shelters nothing until the component appears. Once it
// does, the same link must be checked again and refuse the overlap.
func TestSensitiveDotDotAfterMissingComponentDoesNotCollapse(t *testing.T) {
	home := t.TempDir()
	dataBase := filepath.Join(home, "data")
	dataRoot := filepath.Join(dataBase, "abx")
	project := filepath.Join(dataBase, "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	// Build the target without filepath.Join: Join would clean the ".." away
	// before the link is created.
	if err := os.Symlink(home+"/missing/../data", filepath.Join(home, ".gem")); err != nil {
		t.Fatal(err)
	}
	account := accountWithHome(Account{UID: os.Getuid(), Username: "tester"}, home)
	if _, err := ResolvePaths(account, dataBase); err != nil {
		t.Fatalf("dangling dot-dot target blocked the data base: %v", err)
	}
	if got, err := ResolveProject(project, home, dataRoot); err != nil || got != project {
		t.Fatalf("dangling dot-dot target blocked an unrelated project: %q, %v", got, err)
	}

	// The component now exists, so the link really reaches home/data and the
	// overlap must be refused again.
	if err := os.Mkdir(filepath.Join(home, "missing"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolvePaths(account, dataBase); err == nil {
		t.Fatal("resolved dot-dot target accepted as the data base")
	}
	if _, err := ResolveProject(project, home, dataRoot); err == nil {
		t.Fatal("resolved dot-dot target accepted for an unrelated project")
	}
}

// Stopping at ".." must not discard ordinary descendants: a missing target
// still names the future path that the overlap check has to compare.
func TestSensitiveMissingTargetKeepsFutureDescendants(t *testing.T) {
	home := t.TempDir()
	if err := os.Symlink(home+"/missing/future", filepath.Join(home, ".gem")); err != nil {
		t.Fatal(err)
	}
	account := accountWithHome(Account{UID: os.Getuid(), Username: "tester"}, home)
	if _, err := ResolvePaths(account, filepath.Join(home, "missing", "future")); err == nil {
		t.Fatal("future descendant of a missing sensitive target was accepted")
	}
}

func accountWithHome(a Account, home string) Account {
	a.Home = home
	return a
}
