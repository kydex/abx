package probe

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// fakeInfo is an os.FileInfo for observations no host filesystem would produce:
// a directory with no group and other bits is easy to find, one with sticky set
// is exactly what the runtime-directory check must reject.
type fakeInfo struct {
	mode os.FileMode
	dir  bool
}

func (f fakeInfo) Name() string       { return "fake" }
func (f fakeInfo) Size() int64        { return 0 }
func (f fakeInfo) Mode() os.FileMode  { return f.mode }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return f.dir }
func (f fakeInfo) Sys() any           { return nil }

// sandboxExpected describes a correct sandbox: every observation check makes
// would satisfy it, which is the state no ordinary host can be in.
func sandboxExpected() Expected {
	return Expected{
		Profile:  Identity{Device: 10, Inode: 100},
		Project:  Identity{Device: 10, Inode: 200},
		Home:     "/home/hostuser",
		Sentinel: "/home/hostuser/.abx-verify-1",
		Namespaces: map[string]string{
			"user": "user:[4026531833]", "mnt": "mnt:[4026531840]",
			"pid": "pid:[4026531836]", "ipc": "ipc:[4026531839]",
			"uts": "uts:[4026531838]",
		},
		Objects: map[string]Identity{
			"/home/hostuser": {Device: 8, Inode: 1}, "/tmp": {Device: 8, Inode: 2},
			"/var/tmp": {Device: 8, Inode: 3}, "/run": {Device: 8, Inode: 4},
			"/dev": {Device: 8, Inode: 5}, "/proc": {Device: 8, Inode: 6},
			"/dev/shm": {Device: 8, Inode: 7},
		},
		Environment: []string{"HOME=/home/agent", "PATH=/home/agent/.local/bin:/usr/bin:/bin"},
		ReadOnly:    []string{"/usr", "/etc/passwd"},
	}
}

// goodObserver returns observations that satisfy sandboxExpected. Every
// predicate below is checked against it, and then against one value changed at
// a time.
func goodObserver(e Expected) observer {
	private := func(p string) (Identity, error) {
		switch p {
		case "/home/agent":
			return e.Profile, nil
		case "/workspace":
			return e.Project, nil
		default:
			// Anything private differs from the host object it is compared to.
			return Identity{Device: 10, Inode: 999}, nil
		}
	}
	return observer{
		getwd:    func() (string, error) { return "/workspace", nil },
		hostname: func() (string, error) { return "abx", nil },
		environ:  func() []string { return append([]string(nil), e.Environment...) },
		stat: func(p string) (os.FileInfo, error) {
			switch p {
			case e.Sentinel, "/etc/shadow":
				return nil, os.ErrNotExist
			case "/run/user":
				return fakeInfo{mode: os.ModeDir | 0o700, dir: true}, nil
			}
			return fakeInfo{mode: os.ModeDir | 0o700, dir: true}, nil
		},
		identify: func(p string) (Identity, error) {
			if p == e.Home {
				return Identity{}, os.ErrNotExist
			}
			return private(p)
		},
		readlink: func(p string) (string, error) {
			// checkNamespaces requires the sandbox identity to differ from the
			// host one: sharing it would mean the sandbox is not its own.
			name := p[len("/proc/self/ns/"):]
			return e.Namespaces[name] + "-in-sandbox", nil
		},
		statfs: func(p string, fs *unix.Statfs_t) error {
			fs.Type = unix.TMPFS_MAGIC
			return nil
		},
		statDevice: func(p string, st *unix.Stat_t) error {
			st.Mode = unix.S_IFCHR
			st.Rdev = unix.Mkdev(1, 3)
			return nil
		},
		mountinfo: func() ([]byte, error) {
			var raw string
			for _, target := range e.ReadOnly {
				raw += "25 30 0:24 / " + target + " ro,relatime shared:1 - ext4 /dev/root rw\n"
			}
			return []byte(raw), nil
		},
		writeProbe: func(string) error { return nil },
	}
}

func state(t *testing.T, checks []Check, name string) string {
	t.Helper()
	return namedCheck(t, checks, name).State
}

func namedCheck(t *testing.T, checks []Check, name string) Check {
	t.Helper()
	for _, c := range checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no observation named %q in %v", name, checks)
	return Check{}
}

// With consistent observations every observation passes. Without this the
// per-predicate table below would pass for the wrong reason: a test that never
// sees a PASS cannot tell a working check from a broken one.
func TestCheckPassesWithConsistentObservations(t *testing.T) {
	e := sandboxExpected()
	checks := check(e, goodObserver(e))
	for _, c := range checks {
		if c.State != statePass && !c.Optional {
			t.Errorf("%q = %s: %s", c.Name, c.State, c.Detail)
		}
	}
}

// Each predicate must FAIL when the single observation it reads is wrong, and
// leave every other observation alone. Inverting any one of these comparisons
// used to leave the whole suite green.
func TestCheckFailsEachPredicateWhenItsObservationIsWrong(t *testing.T) {
	const (
		profileID    = "selected /home/agent"
		projectID    = "selected /workspace"
		privateTmp   = "private /tmp"
		privateProc  = "private /proc"
		privateShm   = "private /dev/shm"
		writeHome    = "write and cleanup /home/agent"
		runtimeMode  = "runtime directory mode"
		runtimeMount = "runtime directory mount"
	)
	for _, tc := range []struct {
		observation      string
		breakObservation func(observer) observer
	}{
		{"working directory", func(o observer) observer {
			o.getwd = func() (string, error) { return "/home/agent", nil }
			return o
		}},
		{"hostname", func(o observer) observer {
			o.hostname = func() (string, error) { return "workstation", nil }
			return o
		}},
		{"exact environment", func(o observer) observer {
			o.environ = func() []string { return []string{"HOME=/home/agent"} }
			return o
		}},
		{"user namespace", func(o observer) observer {
			o.readlink = func(p string) (string, error) {
				return sandboxExpected().Namespaces[p[len("/proc/self/ns/"):]], nil
			}
			return o
		}},
		{profileID, func(o observer) observer {
			o.identify = func(p string) (Identity, error) {
				if p == "/home/agent" {
					return Identity{Device: 10, Inode: 101}, nil
				}
				return goodObserver(sandboxExpected()).identify(p)
			}
			return o
		}},
		{projectID, func(o observer) observer {
			o.identify = func(p string) (Identity, error) {
				if p == "/workspace" {
					return Identity{Device: 10, Inode: 201}, nil
				}
				return goodObserver(sandboxExpected()).identify(p)
			}
			return o
		}},
		{"host home hidden", func(o observer) observer {
			o.identify = func(p string) (Identity, error) {
				if p == "/home/hostuser" {
					return Identity{Device: 8, Inode: 1}, nil
				}
				return goodObserver(sandboxExpected()).identify(p)
			}
			return o
		}},
		{privateTmp, func(o observer) observer {
			o.identify = func(p string) (Identity, error) {
				if p == "/tmp" {
					return Identity{Device: 8, Inode: 2}, nil
				}
				return goodObserver(sandboxExpected()).identify(p)
			}
			return o
		}},
		{privateProc, func(o observer) observer {
			o.identify = func(p string) (Identity, error) {
				if p == "/proc" {
					return Identity{Device: 8, Inode: 6}, nil
				}
				return goodObserver(sandboxExpected()).identify(p)
			}
			return o
		}},
		{privateShm, func(o observer) observer {
			o.identify = func(p string) (Identity, error) {
				if p == "/dev/shm" {
					return Identity{Device: 8, Inode: 7}, nil
				}
				return goodObserver(sandboxExpected()).identify(p)
			}
			return o
		}},
		{"tmpfs /tmp", func(o observer) observer {
			o.statfs = func(p string, fs *unix.Statfs_t) error {
				fs.Type = unix.TMPFS_MAGIC
				if p == "/tmp" {
					fs.Type = unix.OVERLAYFS_SUPER_MAGIC
				}
				return nil
			}
			return o
		}},
		{runtimeMode, func(o observer) observer {
			o.stat = func(p string) (os.FileInfo, error) {
				if p == "/run/user" {
					return fakeInfo{mode: os.ModeDir | 0o755, dir: true}, nil
				}
				return goodObserver(sandboxExpected()).stat(p)
			}
			return o
		}},
		{runtimeMode, func(o observer) observer {
			o.stat = func(p string) (os.FileInfo, error) {
				if p == "/run/user" {
					return fakeInfo{mode: 0o700, dir: false}, nil
				}
				return goodObserver(sandboxExpected()).stat(p)
			}
			return o
		}},
		{runtimeMode, func(o observer) observer {
			o.stat = func(p string) (os.FileInfo, error) {
				if p == "/run/user" {
					return fakeInfo{mode: os.ModeDir | os.ModeSticky | os.ModeSetuid | os.ModeSetgid | 0o700, dir: true}, nil
				}
				return goodObserver(sandboxExpected()).stat(p)
			}
			return o
		}},
		{runtimeMount, func(o observer) observer {
			o.mountinfo = func() ([]byte, error) {
				return []byte("25 30 0:24 / /run/user rw,relatime shared:1 - tmpfs tmpfs rw\n"), nil
			}
			return o
		}},
		{"null device", func(o observer) observer {
			o.statDevice = func(p string, st *unix.Stat_t) error {
				st.Mode = unix.S_IFREG
				st.Rdev = unix.Mkdev(1, 3)
				return nil
			}
			return o
		}},
		{"read-only /usr", func(o observer) observer {
			o.mountinfo = func() ([]byte, error) {
				return []byte("25 30 0:24 / /usr rw,relatime shared:1 - ext4 /dev/root rw\n"), nil
			}
			return o
		}},
		{writeHome, func(o observer) observer {
			o.writeProbe = func(p string) error {
				if p == "/home/agent" {
					return errors.New("read-only filesystem")
				}
				return nil
			}
			return o
		}},
	} {
		t.Run(tc.observation, func(t *testing.T) {
			e := sandboxExpected()
			checks := check(e, tc.breakObservation(goodObserver(e)))
			// observation() reports an error as UNAVAILABLE, not FAIL: the two
			// say different things to the reader. Only a comparison that
			// succeeded and disagreed is a FAIL.
			want := stateFail
			if tc.observation == writeHome {
				want = stateUnavailable
			}
			if got := state(t, checks, tc.observation); got != want {
				t.Fatalf("%q = %s, want %s", tc.observation, got, want)
			}
			got := namedCheck(t, checks, tc.observation)
			if got.Detail == "" {
				t.Errorf("%q has an empty Detail", tc.observation)
			}
			if tc.observation == runtimeMode &&
				(!strings.Contains(got.Detail, "expected") || !strings.Contains(got.Detail, "observed")) {
				t.Errorf("runtime mode Detail lacks expected/observed: %q", got.Detail)
			}
			// An observation the case did not touch must still hold, otherwise
			// one shared stub could be failing everything at once.
			for _, other := range []string{"hostname", "null device", "runtime directory mode"} {
				if other == tc.observation {
					continue
				}
				if got := state(t, checks, other); got != statePass {
					t.Errorf("unrelated observation %q = %s, want %s", other, got, statePass)
				}
			}
		})
	}
}

// A hidden path that exists is a failure, not an unavailable observation.
func TestCheckHiddenPathThatExistsFails(t *testing.T) {
	e := sandboxExpected()
	o := goodObserver(e)
	o.stat = func(string) (os.FileInfo, error) { return fakeInfo{mode: 0o600}, nil }
	checks := check(e, o)
	if got := state(t, checks, "hidden /etc/shadow"); got != stateFail {
		t.Fatalf("hidden /etc/shadow = %s, want %s", got, stateFail)
	}
}

// The two write-and-cleanup observations read the probe's own side effect, so
// they are the only ones an ordinary host can exercise without writing anything.
func TestCheckDoesNotTouchTheRealFilesystem(t *testing.T) {
	e := sandboxExpected()
	o := goodObserver(e)
	touched := make([]string, 0, 2)
	o.writeProbe = func(p string) error { touched = append(touched, p); return nil }
	check(e, o)
	if len(touched) != 2 {
		t.Fatalf("write probe called for %v", touched)
	}
}
