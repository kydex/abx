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

func TestLiveVerify(t *testing.T) {
	if os.Getenv("ABX_LIVE") != "1" {
		t.Skip("opt-in: ABX_LIVE=1 go test ./internal/app -run TestLiveVerify -v")
	}
	if os.Getuid() == 0 {
		t.Fatal("requires a regular non-root account")
	}
	base := t.TempDir()
	binary := filepath.Join(base, "abx")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-trimpath", "-buildvcs=false", "-o", binary, "../../cmd/abx")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if raw, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build %v %s", err, raw)
	}
	data := filepath.Join(base, "data")
	project := filepath.Join(base, "project")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir = project
		cmd.Env = []string{"XDG_DATA_HOME=" + data, "PATH=/no-tools", "ABX_TEST_HOST_LEAK=secret", "LD_PRELOAD=/missing"}
		raw, err := cmd.CombinedOutput()
		return string(raw), err
	}
	if raw, err := run("profile", "create", "empty"); err != nil {
		t.Fatalf("create %v %s", err, raw)
	}
	for _, skills := range []bool{false, true} {
		if skills {
			if err := os.MkdirAll(filepath.Join(data, "abx/shared/agents/skills"), 0700); err != nil {
				t.Fatal(err)
			}
		}
		raw, err := run("verify", "empty")
		if err != nil || !strings.Contains(raw, "Verification passed; temporary probe files removed.") {
			t.Fatalf("skills=%v: %v\n%s", skills, err, raw)
		}
		for _, name := range []string{"user", "mnt", "pid", "ipc", "uts"} {
			if !strings.Contains(raw, `PASS "`+name+` namespace"`) {
				t.Fatal("missing namespace check", raw)
			}
		}
		if !skills && !strings.Contains(raw, `N/A "shared skills"`) {
			t.Fatal("missing optional-skills status", raw)
		}
		if skills && !strings.Contains(raw, `PASS "read-only /home/agent/.agents/skills"`) {
			t.Fatal("missing skills check", raw)
		}
		entries, err := os.ReadDir(project)
		if err != nil || len(entries) != 0 {
			t.Fatal("project residue", err)
		}
		entries, err = os.ReadDir(filepath.Join(data, "abx/profiles/empty"))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.Name() != ".agents" {
				t.Fatal("profile residue", entry.Name())
			}
		}
	}
	// Use a fresh profile: a successful bind may leave its empty mountpoint
	// in the writable profile. The negative fixture must not depend on that state.
	if raw, err := run("profile", "create", "unsafe"); err != nil {
		t.Fatalf("create negative fixture: %v %s", err, raw)
	}
	target := filepath.Join(data, "abx/profiles/unsafe/.agents/skills")
	if err := os.Mkdir(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(project, target); err != nil {
		t.Fatal(err)
	}
	if raw, err := run("verify", "unsafe"); err == nil || !strings.Contains(raw, "unsafe skills target") || strings.Contains(raw, "Verification passed;") {
		t.Fatal("unsafe target accepted", err, raw)
	}
	if got, err := os.Readlink(target); err != nil || got != project {
		t.Fatal("unsafe target was modified", got, err)
	}
}
