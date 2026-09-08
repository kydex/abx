// Account regressions adapted from abx 0.7.9.
package host

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"testing"
)

func TestAccountFromPasswd(t *testing.T) {
	home := t.TempDir()
	link := filepath.Join(filepath.Dir(home), "home-link")
	if err := os.Symlink(home, link); err != nil {
		t.Fatal(err)
	}
	a, err := accountFromPasswd(1000, "tester", link)
	if err != nil || a.Home != home {
		t.Fatalf("accountFromPasswd = %#v, %v", a, err)
	}
	for _, tc := range []struct {
		uid        int
		user, home string
	}{{0, "root", home}, {1000, "", home}, {1000, "u", "relative"}} {
		if _, err := accountFromPasswd(tc.uid, tc.user, tc.home); err == nil {
			t.Fatalf("accepted invalid account %#v", tc)
		}
	}
}

func TestResolveAccountLookupPaths(t *testing.T) {
	home := t.TempDir()
	account, err := resolveAccount(1000, func(id string) (*user.User, error) {
		if id != "1000" {
			t.Fatalf("lookup id = %q", id)
		}
		return &user.User{Uid: id, Username: "tester", HomeDir: home}, nil
	})
	if err != nil || account.Home != home {
		t.Fatalf("resolveAccount = %#v, %v", account, err)
	}
	if _, err := resolveAccount(1000, func(string) (*user.User, error) { return nil, errors.New("lookup failed") }); err == nil {
		t.Fatal("accepted lookup failure")
	}
	for _, tc := range []struct {
		name string
		user *user.User
	}{
		{name: "nil result", user: nil},
		{name: "malformed uid", user: &user.User{Uid: "not-a-uid", Username: "tester", HomeDir: home}},
		{name: "mismatched uid", user: &user.User{Uid: "1001", Username: "tester", HomeDir: home}},
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

func TestProfileStorageSelection(t *testing.T) {
	for _, mode := range []string{"unset", "relative", "absolute"} {
		t.Run(mode, func(t *testing.T) {
			base := t.TempDir()
			home := filepath.Join(base, "passwd-home")
			fakeHome := filepath.Join(base, "environment-home")
			if err := os.Mkdir(home, 0700); err != nil {
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
