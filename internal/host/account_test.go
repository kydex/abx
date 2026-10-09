// Account regressions.
package host

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAccountFromPasswd(t *testing.T) {
	home := t.TempDir()
	link := filepath.Join(filepath.Dir(home), "home-link")
	if err := os.Symlink(home, link); err != nil {
		t.Fatal(err)
	}
	uid := os.Getuid()
	if uid == 0 {
		t.Skip("home permissions require a non-root fixture")
	}
	a, err := accountFromPasswd(uid, "tester", link)
	if err != nil || a.Home != home {
		t.Fatalf("accountFromPasswd = %#v, %v", a, err)
	}
	// Ownership is not required: root-owned homes (container images with
	// runAsUser) and NFS uid-mapping must keep working. Only a
	// world-writable home is refused.
	if _, err := accountFromPasswd(60000, "tester", link); err != nil {
		t.Errorf("foreign uid refused: %v", err)
	}
	wide := filepath.Join(filepath.Dir(home), "wide-home")
	if err := os.Mkdir(wide, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(wide, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := accountFromPasswd(uid, "tester", wide); err == nil {
		t.Error("accepted world-writable home")
	}
	for _, tc := range []struct {
		uid        int
		user, home string
	}{{0, "root", home}, {1000, "", home}, {1000, "u", "relative"}} {
		if _, err := accountFromPasswd(tc.uid, tc.user, tc.home); err == nil {
			t.Fatalf("accepted invalid account %#v", tc)
		}
	}
	dangling := filepath.Join(filepath.Dir(home), "dangling-home")
	if err := os.Symlink(filepath.Join(filepath.Dir(home), "missing"), dangling); err != nil {
		t.Fatal(err)
	}
	if _, err := accountFromPasswd(1000, "tester", dangling); err == nil {
		t.Error("accepted dangling symlink as home")
	}
	fileHome := filepath.Join(filepath.Dir(home), "file-home")
	if err := os.WriteFile(fileHome, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := accountFromPasswd(1000, "tester", fileHome); err == nil {
		t.Error("accepted regular file as home")
	}
	if os.Getuid() != 0 {
		lockedParent := filepath.Join(filepath.Dir(home), "locked-parent")
		lockedHome := filepath.Join(lockedParent, "home")
		if err := os.MkdirAll(lockedHome, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(lockedParent, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(lockedParent, 0o700) })
		if _, err := accountFromPasswd(1000, "tester", lockedHome); err == nil {
			t.Error("accepted unreachable home")
		}
	}
}

func TestResolveAccountLookupPaths(t *testing.T) {
	uid := os.Getuid()
	if uid == 0 {
		t.Skip("home ownership requires a non-root fixture")
	}
	suid := strconv.Itoa(uid)
	home := t.TempDir()
	account, err := resolveAccount(uid, func(id string) (*user.User, error) {
		if id != suid {
			t.Fatalf("lookup id = %q", id)
		}
		return &user.User{Uid: id, Username: "tester", HomeDir: home}, nil
	})
	if err != nil || account.Home != home {
		t.Fatalf("resolveAccount = %#v, %v", account, err)
	}
	if _, err := resolveAccount(uid, func(string) (*user.User, error) { return nil, errors.New("lookup failed") }); err == nil {
		t.Fatal("accepted lookup failure")
	}
	for _, tc := range []struct {
		name string
		user *user.User
	}{
		{name: "nil result", user: nil},
		{name: "malformed uid", user: &user.User{Uid: "not-a-uid", Username: "tester", HomeDir: home}},
		{name: "mismatched uid", user: &user.User{Uid: strconv.Itoa(uid + 1), Username: "tester", HomeDir: home}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := resolveAccount(1000, func(string) (*user.User, error) { return tc.user, nil }); err == nil {
				t.Fatalf("accepted passwd lookup result %#v", tc.user)
			}
		})
	}
}

func TestResolveAccountRejectsRoot(t *testing.T) {
	if _, err := resolveAccount(0, func(string) (*user.User, error) {
		t.Fatal("root rejection performed a passwd lookup")
		return nil, nil
	}); err == nil {
		t.Fatal("resolveAccount accepted root")
	}
	if os.Getuid() != 0 {
		return
	}
	if _, err := ResolveAccount(); err == nil {
		t.Fatal("ResolveAccount accepted root")
	}
}

func TestResolvePathsRejectsUnsafeDataBase(t *testing.T) {
	uid := os.Getuid()
	if uid == 0 {
		uid = 1000
	}
	mkhome := func(t *testing.T) string {
		t.Helper()
		home := filepath.Join(t.TempDir(), "passwd-home")
		if err := os.Mkdir(home, 0o700); err != nil {
			t.Fatal(err)
		}
		return home
	}
	t.Run("filesystem root", func(t *testing.T) {
		account := Account{UID: uid, Username: "tester", Home: mkhome(t)}
		if _, err := ResolvePaths(account, "/"); err == nil || !strings.Contains(err.Error(), "filesystem root") {
			t.Fatalf("data base at root accepted: %v", err)
		}
	})
	t.Run("sensitive home tree", func(t *testing.T) {
		home := mkhome(t)
		if err := os.Mkdir(filepath.Join(home, ".config"), 0o700); err != nil {
			t.Fatal(err)
		}
		account := Account{UID: uid, Username: "tester", Home: home}
		if _, err := ResolvePaths(account, filepath.Join(home, ".config", "custom-data")); err == nil || !strings.Contains(err.Error(), "sensitive home tree") {
			t.Fatalf("data base in sensitive tree accepted: %v", err)
		}
	})
	t.Run("unowned ancestor", func(t *testing.T) {
		account := Account{UID: uid, Username: "tester", Home: mkhome(t)}
		if _, err := ResolvePaths(account, "/usr/abx-test-unowned-data"); err == nil || !strings.Contains(err.Error(), "not owned") {
			t.Fatalf("data base under unowned ancestor accepted: %v", err)
		}
	})
	t.Run("file as base", func(t *testing.T) {
		account := Account{UID: uid, Username: "tester", Home: mkhome(t)}
		base := filepath.Join(t.TempDir(), "filebase")
		if err := os.WriteFile(base, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ResolvePaths(account, base); err == nil {
			t.Fatalf("file data base accepted: %v", err)
		}
	})
	t.Run("group-writable ancestor allowed", func(t *testing.T) {
		wide := filepath.Join(t.TempDir(), "wide")
		if err := os.Mkdir(wide, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(wide, 0o775); err != nil {
			t.Fatal(err)
		}
		account := Account{UID: os.Getuid(), Username: "tester", Home: mkhome(t)}
		if _, err := ResolvePaths(account, filepath.Join(wide, "deep")); err != nil {
			t.Fatalf("group-writable ancestor refused: %v", err)
		}
	})
	t.Run("world-writable ancestor", func(t *testing.T) {
		wide := filepath.Join(t.TempDir(), "wide")
		if err := os.Mkdir(wide, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(wide, 0o777); err != nil {
			t.Fatal(err)
		}
		account := Account{UID: os.Getuid(), Username: "tester", Home: mkhome(t)}
		if _, err := ResolvePaths(account, filepath.Join(wide, "deep")); err == nil {
			t.Fatal("world-writable ancestor accepted")
		}
	})
	t.Run("sticky ancestor", func(t *testing.T) {
		wide := filepath.Join(t.TempDir(), "wide")
		if err := os.Mkdir(wide, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(wide, 0o777|os.ModeSticky); err != nil {
			t.Fatal(err)
		}
		account := Account{UID: os.Getuid(), Username: "tester", Home: mkhome(t)}
		if _, err := ResolvePaths(account, filepath.Join(wide, "deep")); err != nil {
			t.Fatalf("sticky ancestor rejected: %v", err)
		}
	})
}

func TestProfileStorageSelection(t *testing.T) {
	for _, mode := range []string{"unset", "relative", "absolute"} {
		t.Run(mode, func(t *testing.T) {
			base := t.TempDir()
			home := filepath.Join(base, "passwd-home")
			fakeHome := filepath.Join(base, "environment-home")
			if err := os.Mkdir(home, 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", fakeHome)
			account := Account{UID: os.Getuid(), Username: "tester", Home: home}
			xdg := ""
			want := filepath.Join(home, ".local", "share", "abx")
			switch mode {
			case "relative":
				xdg = "relative-data"
			case "absolute":
				xdg = filepath.Join(base, "custom-data")
				want = filepath.Join(xdg, "abx")
			}
			paths, err := ResolvePaths(account, xdg)
			if err != nil || paths.Root != want {
				t.Fatalf("storage: %q, %v; want %q", paths.Root, err, want)
			}
			created, err := CreateProfile(account, paths, "demo")
			if err != nil {
				t.Fatal(err)
			}
			if created != filepath.Join(want, "profiles", "demo") {
				t.Fatal(created)
			}
			status, err := ShowProfile(account, paths, "demo")
			if err != nil || !status.Exists || status.Home != created {
				t.Fatalf("profile not found in selected storage: %#v, %v", status, err)
			}
			if _, err := os.Lstat(fakeHome); !os.IsNotExist(err) {
				t.Fatalf("environment HOME used: %v", err)
			}
		})
	}
}
