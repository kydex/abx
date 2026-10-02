package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kydex/abx/internal/bwrap"
	"github.com/kydex/abx/internal/host"
	"github.com/kydex/abx/internal/sandbox"
)

func TestVerificationFilesCleanUpAfterSuccessOrLaterFailure(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	for range 2 {
		files := &probeFiles{}
		sentinel, link, err := files.create(home, project)
		if err != nil {
			t.Fatal(err)
		}
		actual := filepath.Join(project, filepath.Base(link))
		if target, err := os.Readlink(actual); err != nil || target != sentinel {
			t.Fatal(target, err)
		}
		if err = files.Close(); err != nil {
			t.Fatal(err)
		}
		if err = files.Close(); err != nil {
			t.Fatal(err)
		}
		for _, dir := range []string{home, project} {
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatal("probe residue", err)
			}
		}
	}
	files := &probeFiles{}
	if _, _, err := files.create(home, filepath.Join(project, "missing")); err == nil {
		t.Fatal("accepted nonexistent project")
	}
	if err := files.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatal("sentinel left after link failure", err)
	}
}

func TestVerificationCleanupDoesNotDeleteReplacement(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	files := &probeFiles{}
	_, link, err := files.create(home, project)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(project, filepath.Base(link))
	// Retain the old inode so this test doesn't depend on inode reuse timing.
	if err = os.Rename(path, path+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = files.Close(); err == nil || !strings.Contains(err.Error(), "replaced") {
		t.Fatal("replacement cleanup was silent", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "keep" {
		t.Fatal("replacement removed", err)
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatal("other cleanup abandoned", err)
	}
}

func TestObserveHostCollectsSandboxPayload(t *testing.T) {
	home := t.TempDir()
	profile := t.TempDir()
	project := t.TempDir()
	input := sandbox.Input{
		Home:    profile,
		Project: project,
		Mode:    sandbox.Run,
		System: []sandbox.Mount{
			{Kind: sandbox.ReadOnly, Source: "/sys", Target: "/usr"},
		},
		Environment: []sandbox.Entry{{Name: "A", Value: "1"}},
		Command:     []string{sandbox.ProbePath, "__probe", "{}"},
	}
	got, err := observeHost(home, input)
	if err != nil {
		t.Fatal(err)
	}
	if got.Home != home {
		t.Fatalf("home = %q", got.Home)
	}
	for _, p := range []string{home, "/tmp", "/var/tmp", "/run", "/dev"} {
		if _, ok := got.Objects[p]; !ok {
			t.Fatalf("missing identity %q", p)
		}
	}
	for _, name := range []string{"user", "mnt", "pid", "ipc", "uts"} {
		if got.Namespaces[name] == "" {
			t.Fatalf("missing namespace %q", name)
		}
	}
	if len(got.Environment) != 1 || got.Environment[0] != "A=1" {
		t.Fatalf("environment = %q", got.Environment)
	}
	for _, want := range []string{"/usr", sandbox.ProbePath} {
		found := false
		for _, target := range got.ReadOnly {
			found = found || target == want
		}
		if !found {
			t.Fatalf("read-only lacks %q: %q", want, got.ReadOnly)
		}
	}
	if got.Skills {
		t.Fatal("skills reported without a shared source")
	}
}

func TestVerifySuccessReportsCleanup(t *testing.T) {
	var out strings.Builder
	if err := verifySuccess(&out); err != nil || out.String() != "Verification passed; temporary probe files removed.\n" {
		t.Fatalf("success report = %q, %v", out.String(), err)
	}
}

func TestVerifyToolFailureDoesNotCreateProbeFiles(t *testing.T) {
	in := inputs(t, host.Run)
	home := t.TempDir()
	want := errors.New("tool not available")
	status, err := runVerify(context.Background(), in, home, nil, bwrap.Stdio{Out: io.Discard, Err: io.Discard}, func(context.Context) (*bwrap.Tool, error) { return nil, want })
	if status != 1 || !errors.Is(err, want) {
		t.Fatal(status, err)
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatal("sentinel created before tool check", err)
	}
	entries, err = os.ReadDir(in.Project())
	if err != nil || len(entries) != 0 {
		t.Fatal("link created before tool check", err)
	}
	if _, err = os.Lstat(filepath.Join(in.Home(), ".agents")); !os.IsNotExist(err) {
		t.Fatal("skills target prepared before tool check", err)
	}
}

func TestVerificationCleanupAggregatesFailuresAndContinues(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	files := &probeFiles{}
	first, firstLink, err := files.create(home, project)
	if err != nil {
		t.Fatal(err)
	}
	second, secondLink, err := files.create(home, project)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(first); err != nil {
		t.Fatal(err)
	}
	replaced := filepath.Join(project, filepath.Base(secondLink))
	// Keep the old object alive to avoid depending on inode reuse.
	if err := os.Rename(replaced, replaced+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replaced, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = files.Close()
	if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), "replaced") || !strings.Contains(err.Error(), first) || !strings.Contains(err.Error(), replaced) {
		t.Fatalf("lost cleanup failure: %v", err)
	}
	for _, path := range []string{second, filepath.Join(project, filepath.Base(firstLink))} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("cleanup abandoned %s: %v", path, err)
		}
	}
	if raw, err := os.ReadFile(replaced); err != nil || string(raw) != "keep" {
		t.Fatalf("replacement damaged: %q %v", raw, err)
	}
	if err := files.Close(); err != nil {
		t.Fatalf("second close retried failed cleanup: %v", err)
	}
}
