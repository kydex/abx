package probe

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadOnlyRequiresObservedMountFlags(t *testing.T) {
	for _, tc := range []struct {
		raw string
		ok  bool
	}{
		{"23 1 0:2 / /usr ro,nosuid - ext4 /dev/x rw\n", true},
		{"23 1 0:2 / /usr rw,nosuid - ext4 /dev/x ro\n", false},
		{"23 1 0:2 / /usr ro,nosuid - ext4 /dev/x rw\n24 1 0:2 / /usr rw - ext4 /dev/x rw", false},
		{"23 1 0:2 / /usrx ro - ext4 /dev/x ro\n", false},
		{"malformed", false}, {"", false},
	} {
		if got := readOnly(tc.raw, "/usr"); got != tc.ok {
			t.Fatalf("%q: %v", tc.raw, got)
		}
	}
}
func TestIncompleteResultsNeverPass(t *testing.T) {
	for _, state := range []string{"FAIL", "UNAVAILABLE", "unknown", "N/A"} {
		if err := report(&bytes.Buffer{}, []Check{{Name: "required", State: state}}); err == nil {
			t.Fatalf("accepted %s", state)
		}
	}
	if err := report(&bytes.Buffer{}, nil); err == nil {
		t.Fatal("accepted empty report")
	}
	if err := report(&bytes.Buffer{}, []Check{{Name: "required", State: "PASS"}, {Name: "shared skills", State: "N/A"}}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := report(&out, []Check{{Name: "path\n\x1b[31m", State: "PASS", Detail: "bad\rtext"}}); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), "\n") != 1 || strings.ContainsAny(out.String(), "\x1b\r") {
		t.Fatalf("unsafe output %q", out.String())
	}
}
func TestPrivateProbeRejectsMissingObservations(t *testing.T) {
	for _, raw := range []string{"", "{}", "null", "[]", strings.Repeat("x", 65537)} {
		var out bytes.Buffer
		if err := Run(raw, &out); err == nil || out.Len() != 0 {
			t.Fatal("accepted incomplete input")
		}
	}
}
func TestWriteProbeRemovesOnlyItsTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "keep")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeAndRemove(dir); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "keep" {
		t.Fatal("unexpected remaining files", err)
	}
	if err := writeAndRemove(marker); err == nil {
		t.Fatal("write accepted a file as directory")
	}
}

func TestNamespaceObservationsFailClosed(t *testing.T) {
	for _, broken := range NamespaceNames() {
		for _, mode := range []string{"same", "missing", "unavailable"} {
			t.Run(broken+"/"+mode, func(t *testing.T) {
				host := map[string]string{}
				for _, name := range NamespaceNames() {
					host[name] = name + "-host"
				}
				if mode == "missing" {
					delete(host, broken)
				}
				checks := checkNamespaces(host, func(path string) (string, error) {
					name := filepath.Base(path)
					if name == broken {
						if mode == "unavailable" {
							return "", os.ErrPermission
						}
						if mode == "same" {
							return host[name], nil
						}
					}
					return name + "-sandbox", nil
				})
				if len(checks) != 5 || report(&bytes.Buffer{}, checks) == nil {
					t.Fatal(checks)
				}
				for _, c := range checks {
					if c.Name != broken+" namespace" && c.State != "PASS" {
						t.Fatal(c)
					}
				}
			})
		}
	}
	host := map[string]string{}
	for _, name := range NamespaceNames() {
		host[name] = "host"
	}
	if err := report(&bytes.Buffer{}, checkNamespaces(host, func(string) (string, error) { return "sandbox", nil })); err != nil {
		t.Fatal(err)
	}
}
