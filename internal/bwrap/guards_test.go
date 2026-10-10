package bwrap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const workingProbe = `if [ "$1" = "--version" ]; then
  printf 'bubblewrap 0.12.0\n'
  exit 0
fi
exit 23`

// Revalidate is the check between "we trusted this file" and "we are about to
// hand its descriptor to Bubblewrap". Nothing tested the identity branch: the
// suite had a case for an ownership change, which is a different check.
func TestRevalidateRejectsReplacedBinary(t *testing.T) {
	path := writeScript(t, workingProbe)
	tool, err := open(context.Background(), path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer tool.Close()
	// Replace the path with a different object. Same path, same mode, different
	// inode: exactly the swap the identity check exists for.
	other := filepath.Join(filepath.Dir(path), "other")
	if err := os.WriteFile(other, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(other, path); err != nil {
		t.Fatal(err)
	}
	if err := tool.Revalidate(); err == nil || !strings.Contains(err.Error(), "bubblewrap changed") {
		t.Fatalf("replaced binary accepted by revalidate: %v", err)
	}
}

// The trusted file has to be a regular file. A directory or anything else at
// that path is refused before it is ever executed.
func TestOpenRejectsNonRegularBubblewrap(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "notafile")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := open(context.Background(), dir, false); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("directory accepted as the Bubblewrap executable: %v", err)
	}
}

// The minimum version is a documented host requirement, and the probe is what
// reads it. Without this check an old Bubblewrap would be trusted the same way
// a current one is.
func TestOpenRejectsOldBubblewrapVersion(t *testing.T) {
	for _, banner := range []string{"bubblewrap 0.11.0\n", "bubblewrap 0.9.1\n", "not bubblewrap at all\n"} {
		path := writeScript(t, `if [ "$1" = "--version" ]; then
  printf '`+banner+`'
  exit 0
fi
exit 23`)
		if _, err := open(context.Background(), path, false); err == nil {
			t.Fatalf("accepted %q as a supported Bubblewrap", strings.TrimSpace(banner))
		}
	}
}
