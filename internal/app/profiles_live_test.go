package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exercise the shipped entry point, not just app.Run inside a test binary.
func TestLiveProfilesCLI(t *testing.T) {
	if os.Getenv("ABX_LIVE") != "1" {
		t.Skip("opt-in: ABX_LIVE=1 go test ./internal/app -run TestLiveProfilesCLI -v")
	}
	if os.Getuid() == 0 {
		t.Fatal("live CLI test requires a regular non-root account")
	}
	base := t.TempDir()
	binary := filepath.Join(base, "abx")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-trimpath", "-buildvcs=false", "-o", binary, "../../cmd/abx")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if raw, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, raw)
	}
	data := filepath.Join(base, "data")
	for _, tc := range []struct {
		args     []string
		code     int
		contains string
	}{
		{[]string{"profile", "list"}, 0, ""},
		{[]string{"profile", "show", "demo"}, 0, "State: absent"},
		{[]string{"profile", "create", "demo"}, 0, "Created profile demo"},
		{[]string{"profile", "create", "demo"}, 1, "profile already exists"},
		{[]string{"profile", "show", "demo"}, 0, "Command: unavailable"},
		{[]string{"profile", "list"}, 0, "demo  unavailable  not-configured"},
		{[]string{"profile", "create", "../bad"}, 2, "invalid profile name"},
	} {
		cmd := exec.CommandContext(ctx, binary, tc.args...)
		// Profile management needs neither PATH executables nor an ambient shell.
		cmd.Env = []string{"XDG_DATA_HOME=" + data, "PATH=/no-tools", "SHELL=/no-shell"}
		cmd.Dir = base
		raw, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			if cmd.ProcessState == nil {
				t.Fatal(err)
			}
			code = cmd.ProcessState.ExitCode()
		}
		if code != tc.code || !strings.Contains(string(raw), tc.contains) {
			t.Fatalf("%q: code=%d err=%v output=%q", tc.args, code, err, raw)
		}
	}
}
