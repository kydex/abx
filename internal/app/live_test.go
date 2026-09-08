package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kydex/abx/internal/bwrap"
	"github.com/kydex/abx/internal/host"
)

// TestLiveShellRunAndWork checks project visibility and programs launched from both shell modes.
// It must be explicitly requested on a real supported non-root Linux host.
func TestLiveShellRunAndWork(t *testing.T) {
	if os.Getenv("ABX_LIVE") != "1" {
		t.Skip("opt-in: ABX_LIVE=1 go test -count=1 ./internal/app -run TestLiveShellRunAndWork")
	}
	if os.Getuid() == 0 {
		t.Fatal("live test requires a regular non-root account")
	}
	account, e := host.ResolveAccount()
	if e != nil {
		t.Fatal(e)
	}
	root := t.TempDir()
	data := filepath.Join(root, "data")
	t.Setenv("XDG_DATA_HOME", data)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var createOut, createErr bytes.Buffer
	if status := Run(ctx, []string{"profile", "create", "demo"}, bwrap.Stdio{Out: &createOut, Err: &createErr}); status != 0 {
		t.Fatalf("create profile: status=%d stdout=%q stderr=%q", status, createOut.String(), createErr.String())
	}
	profile := filepath.Join(data, "abx/profiles/demo")
	skills := filepath.Join(data, "abx/shared/agents/skills")
	project := filepath.Join(root, "project")
	for _, p := range []string{filepath.Join(profile, ".local/bin"), skills, project} {
		if e = os.MkdirAll(p, 0700); e != nil {
			t.Fatal(e)
		}
	}
	if e = os.WriteFile(filepath.Join(skills, "marker"), []byte("shared"), 0600); e != nil {
		t.Fatal(e)
	}
	sentinel := filepath.Join(root, "external-sentinel")
	if e = os.WriteFile(sentinel, []byte("outside"), 0600); e != nil {
		t.Fatal(e)
	}
	objects := map[string][2]uint64{}
	for _, p := range []string{account.Home, "/tmp", "/var/tmp", "/run", "/dev"} {
		info, e := os.Stat(p)
		if e != nil {
			t.Fatal(e)
		}
		s := info.Sys().(*syscall.Stat_t)
		objects[p] = [2]uint64{uint64(s.Dev), s.Ino}
	}
	ns, e := os.Readlink("/proc/self/ns/pid")
	if e != nil {
		t.Fatal(e)
	}
	expected := struct {
		Namespace, Home, Sentinel string
		Alternatives              []string
		Objects                   map[string][2]uint64
	}{Namespace: ns, Home: account.Home, Sentinel: sentinel, Objects: objects}
	for _, path := range []string{"/usr/bin/cc", "/usr/bin/editor"} {
		if target, err := os.Readlink(path); err == nil && strings.HasPrefix(target, "/etc/alternatives/") {
			if _, err := os.Stat(path); err != nil {
				t.Fatal(err)
			}
			expected.Alternatives = append(expected.Alternatives, path)
		}
	}
	raw, e := json.Marshal(expected)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(profile, "expect.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	build := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", filepath.Join(profile, ".local/bin/demo"), "./testdata/probe.go")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build fixture: %v %s", e, output)
	}
	t.Setenv("ABX_TEST_HOST_LEAK", "should-not-pass")
	t.Setenv("SSH_AUTH_SOCK", "/not-a-real-socket")
	t.Chdir(project)
	for _, mode := range []string{"run", "shell", "work"} {
		t.Run(mode, func(t *testing.T) {
			var out, stderr bytes.Buffer
			argv := []string{"run", "demo", "--", "run"}
			input := ""
			if mode == "shell" {
				argv = []string{"shell", "demo"}
				input = "/home/agent/.local/bin/demo shell\n"
			}
			if mode == "work" {
				// Work must not require the profile's matching executable.
				if err := os.Rename(filepath.Join(profile, ".local/bin/demo"), filepath.Join(profile, ".local/bin/probe")); err != nil {
					t.Fatal(err)
				}
				argv = []string{"work", "demo"}
				input = "/home/agent/.local/bin/probe work\n/home/agent/.local/bin/probe work\n"
			}
			status := Run(ctx, argv, bwrap.Stdio{In: strings.NewReader(input), Out: &out, Err: &stderr})
			if status != 0 || !strings.Contains(out.String(), "probe:"+mode+"\n") {
				t.Fatalf("%s: status=%d stdout=%q stderr=%q", mode, status, out.String(), stderr.String())
			}
			if mode == "work" && strings.Count(out.String(), "probe:work\n") != 2 {
				t.Fatalf("work shell did not continue after child exit: %q", out.String())
			}
		})
	}
}
