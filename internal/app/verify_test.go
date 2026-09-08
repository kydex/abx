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
	if err = os.WriteFile(path, []byte("keep"), 0600); err != nil {
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
