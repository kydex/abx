package probe

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kydex/abx/internal/sandbox"
)

func TestProbePathMatchesSandbox(t *testing.T) {
	if probePath != sandbox.ProbePath {
		t.Fatalf("probePath = %q, sandbox.ProbePath = %q", probePath, sandbox.ProbePath)
	}
}

func TestReadOnlyRequiresObservedMountFlags(t *testing.T) {
	for _, tc := range []struct {
		raw string
		ok  bool
	}{
		{"23 1 0:2 / /usr ro,nosuid - ext4 /dev/x rw\n", true},
		{"23 1 0:2 / /usr rw,nosuid - ext4 /dev/x ro\n", false},
		{"23 1 0:2 / /usr ro,nosuid - ext4 /dev/x rw\n24 1 0:2 / /usr rw - ext4 /dev/x rw", false},
		{"23 1 0:2 / /usrx ro - ext4 /dev/x ro\n", false},
		{"malformed", false},
		{"", false},
	} {
		if got := readOnlyDetail(tc.raw, "/usr") == ""; got != tc.ok {
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

func TestNamespaceNamesPinned(t *testing.T) {
	// Keep in sync with the Translate flag prefix in internal/bwrap (no import:
	// probe must not depend on the executor). "mnt" is implicit in Bubblewrap.
	want := []string{"user", "mnt", "pid", "ipc", "uts"}
	if !slices.Equal(NamespaceNames(), want) {
		t.Fatalf("NamespaceNames() = %q, want %q", NamespaceNames(), want)
	}
}

func TestPrivateProbeRejectsOversizedValidJSON(t *testing.T) {
	raw := `{"Home":"` + strings.Repeat("x", 70000) + `"}`
	var out bytes.Buffer
	err := Run(raw, &out)
	if err == nil || !strings.Contains(err.Error(), "probe input too large") || out.Len() != 0 {
		t.Fatalf("oversized valid JSON = %q, %v", out.String(), err)
	}
}

func TestPrivateProbeRejectsHostExecution(t *testing.T) {
	// Well-formed input that passes decode must still not run on the host:
	// /abx-probe is a read-only mount of this binary only inside the sandbox.
	// The positive path (inside the sandbox) is covered by live verify tests.
	valid := `{"Home":"/host-home","Sentinel":"/host-home/probe","Link":"/workspace/.abx-verify-test","Environment":["HOME=/home/agent"],"ReadOnly":["/usr"],"Namespaces":{"user":"u","mnt":"m","pid":"p","ipc":"i","uts":"t"},"Objects":{"/host-home":{},"/tmp":{},"/var/tmp":{},"/run":{},"/dev":{}}}`
	var out bytes.Buffer
	err := Run(valid, &out)
	if err == nil || !strings.Contains(err.Error(), "only available inside the sandbox") || out.Len() != 0 {
		t.Fatalf("host probe execution = %q, %v", out.String(), err)
	}
}

func TestGateChecksIdentityAndReadOnlyMount(t *testing.T) {
	self := Identity{Device: 7, Inode: 42}
	ro := "1 2 3:4 / /abx-probe ro,nosuid - ext4 /dev/x rw\n"
	rw := "1 2 3:4 / /abx-probe rw,nosuid - ext4 /dev/x rw\n"
	for _, tc := range []struct {
		name             string
		mounted          Identity
		mountinfo        string
		wantErrSubstring string
	}{
		{"matching identity and ro mount", self, ro, ""},
		{"different inode", Identity{Device: 7, Inode: 43}, ro, "only available inside the sandbox"},
		{"different device", Identity{Device: 8, Inode: 42}, ro, "only available inside the sandbox"},
		{"writable mount", self, rw, "only available inside the sandbox"},
		{"missing mountpoint", self, "1 2 3:4 / /other ro - ext4 /dev/x rw\n", "only available inside the sandbox"},
		{"empty mountinfo", self, "", "only available inside the sandbox"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkGate(self, tc.mounted, tc.mountinfo)
			if tc.wantErrSubstring == "" {
				if err != nil {
					t.Fatalf("gate = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErrSubstring) {
				t.Fatalf("gate = %v, want %q", err, tc.wantErrSubstring)
			}
		})
	}
}

func TestIdentifyReportsFilesystemObject(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "object")
	if err := os.WriteFile(file, []byte("probe"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := Identify(file)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Identify(file)
	if err != nil || first != second || first == (Identity{}) {
		t.Fatalf("identify = %#v, %#v, %v", first, second, err)
	}
	if _, err := Identify(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("identified a missing path")
	}
}

func TestWriteProbeRemovesOnlyItsTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "keep")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
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

func TestReportOmitsOnlyEmptyDetails(t *testing.T) {
	var out bytes.Buffer
	err := report(&out, []Check{
		{Name: "working directory", State: "PASS"},
		{Name: "shared skills", State: "N/A", Detail: "not configured"},
		{Name: "hostname", State: "FAIL", Detail: "unexpected sandbox state"},
	})
	want := "PASS \"working directory\"\nN/A \"shared skills\": \"not configured\"\nFAIL \"hostname\": \"unexpected sandbox state\"\n"
	if err == nil || out.String() != want {
		t.Fatalf("report = %q, %v", out.String(), err)
	}
}

func TestEnvironmentDiagnosticsPreserveExactComparisonWithoutValues(t *testing.T) {
	for _, tc := range []struct {
		name             string
		actual, expected []string
		state, detail    string
	}{
		{"order", []string{"B=secret-two", "A=secret-one"}, []string{"A=secret-one", "B=secret-two"}, "PASS", ""},
		{"missing extra changed", []string{"A=secret-new", "C=secret-extra"}, []string{"A=secret-old", "B=secret-missing"}, "FAIL", `differing variable names: ["A" "B" "C"]`},
		{"duplicate", []string{"A=secret-one", "A=secret-one"}, []string{"A=secret-one"}, "FAIL", `differing variable names: ["A"]`},
		{"empty value versus absent", []string{"A="}, nil, "FAIL", `differing variable names: ["A"]`},
		{"malformed", []string{"secret-malformed"}, nil, "FAIL", `differing variable names: ["<malformed assignment>"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			beforeActual, beforeExpected := strings.Join(tc.actual, "\x00"), strings.Join(tc.expected, "\x00")
			c := checkEnvironment(tc.actual, tc.expected)
			if c.State != tc.state || c.Detail != tc.detail {
				t.Fatalf("check = %#v", c)
			}
			if strings.Join(tc.actual, "\x00") != beforeActual || strings.Join(tc.expected, "\x00") != beforeExpected {
				t.Fatal("modified input environment")
			}
			var out bytes.Buffer
			err := report(&out, []Check{c})
			if (err == nil) != (tc.state == "PASS") || strings.Contains(out.String(), "secret-") {
				t.Fatalf("report = %q, %v", out.String(), err)
			}
		})
	}
}

func TestObservationDetailsDoNotOverrideStateOrErrors(t *testing.T) {
	for _, tc := range []struct {
		ok            bool
		err           error
		state, detail string
	}{
		{true, nil, "PASS", ""},
		{false, nil, "FAIL", `expected "/workspace"; observed "/home/agent"`},
		{false, os.ErrPermission, "UNAVAILABLE", os.ErrPermission.Error()},
		{true, os.ErrPermission, "UNAVAILABLE", os.ErrPermission.Error()},
	} {
		c := observation("working directory", tc.ok, tc.err, `expected "/workspace"; observed "/home/agent"`)
		if c.State != tc.state || c.Detail != tc.detail {
			t.Fatalf("check = %#v", c)
		}
	}
}

func TestReadOnlyDiagnosticsDistinguishMissingAndWritableMount(t *testing.T) {
	for _, tc := range []struct{ raw, detail string }{
		{"23 1 0:2 / /usr ro,nosuid - ext4 /dev/x rw\n", ""},
		{"23 1 0:2 / /usr rw,nosuid - ext4 /dev/x ro\n", `expected ro mount flag; observed flags "rw,nosuid"`},
		{"23 1 0:2 / /usrx ro - ext4 /dev/x rw\n", "expected read-only mountpoint; no matching mountinfo entry"},
	} {
		if got := readOnlyDetail(tc.raw, "/usr"); got != tc.detail {
			t.Fatalf("detail = %q, want %q", got, tc.detail)
		}
	}
}

func TestDecodeRequiresEveryNamespaceAndHostIdentity(t *testing.T) {
	valid := `{"Home":"/host-home","Sentinel":"/host-home/probe","Link":"/workspace/.abx-verify-test","Environment":["HOME=/home/agent"],"ReadOnly":["/usr"],"Namespaces":{"user":"u","mnt":"m","pid":"p","ipc":"i","uts":"t"},"Objects":{"/host-home":{},"/tmp":{},"/var/tmp":{},"/run":{},"/dev":{}}}`
	if _, err := decode(valid); err != nil {
		t.Fatalf("valid fixture rejected: %v", err)
	}
	for _, tc := range []struct{ field, key, message string }{
		{"Namespaces", "user", "missing namespace"},
		{"Namespaces", "mnt", "missing namespace"},
		{"Namespaces", "pid", "missing namespace"},
		{"Namespaces", "ipc", "missing namespace"},
		{"Namespaces", "uts", "missing namespace"},
		{"Objects", "/host-home", "missing identity"},
		{"Objects", "/tmp", "missing identity"},
		{"Objects", "/var/tmp", "missing identity"},
		{"Objects", "/run", "missing identity"},
		{"Objects", "/dev", "missing identity"},
	} {
		t.Run(tc.field+"/"+tc.key, func(t *testing.T) {
			var input map[string]json.RawMessage
			if err := json.Unmarshal([]byte(valid), &input); err != nil {
				t.Fatal(err)
			}
			var entries map[string]json.RawMessage
			if err := json.Unmarshal(input[tc.field], &entries); err != nil {
				t.Fatal(err)
			}
			delete(entries, tc.key)
			raw, err := json.Marshal(entries)
			if err != nil {
				t.Fatal(err)
			}
			input[tc.field] = raw
			raw, err = json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			err = Run(string(raw), &out)
			if err == nil || !strings.Contains(err.Error(), tc.message) || !strings.Contains(err.Error(), tc.key) || out.Len() != 0 {
				t.Fatalf("incomplete input reached observations: output=%q err=%v", out.String(), err)
			}
		})
	}
}

func TestDecodeRejectsNonCanonicalProbePaths(t *testing.T) {
	valid := `{"Home":"/host-home","Sentinel":"/host-home/probe","Link":"/workspace/.abx-verify-test","Environment":["HOME=/home/agent"],"ReadOnly":["/usr"],"Namespaces":{"user":"u","mnt":"m","pid":"p","ipc":"i","uts":"t"},"Objects":{"/host-home":{},"/tmp":{},"/var/tmp":{},"/run":{},"/dev":{}}}`
	for _, tc := range []struct{ field, value string }{
		{"Link", "/workspace/.abx-verify-test/../../etc/shadow"},
		{"Link", "/tmp/.abx-verify-test"},
		{"Link", "/workspace/other-name"},
		{"Link", "/workspace/.abx-verify-test\x00"},
		{"Home", "/host-home\x00"},
		{"Home", "/host-home/"},
		{"Sentinel", "/host-home/probe/../other"},
	} {
		t.Run(tc.field+"/"+tc.value, func(t *testing.T) {
			var input map[string]json.RawMessage
			if err := json.Unmarshal([]byte(valid), &input); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			input[tc.field] = raw
			raw, err = json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if err := Run(string(raw), &out); err == nil || !strings.Contains(err.Error(), "incomplete probe inputs") || out.Len() != 0 {
				t.Fatalf("non-canonical path reached observations: output=%q err=%v", out.String(), err)
			}
		})
	}
}

func TestDecodeRejectsNonCleanReadOnlyTargets(t *testing.T) {
	valid := `{"Home":"/host-home","Sentinel":"/host-home/probe","Link":"/workspace/.abx-verify-test","Environment":["HOME=/home/agent"],"ReadOnly":["/usr"],"Namespaces":{"user":"u","mnt":"m","pid":"p","ipc":"i","uts":"t"},"Objects":{"/host-home":{},"/tmp":{},"/var/tmp":{},"/run":{},"/dev":{}}}`
	for _, targets := range [][]string{{"relative"}, {"/usr/"}, {"/usr\x00"}, {""}, {"/usr", "relative"}} {
		t.Run(strings.Join(targets, ","), func(t *testing.T) {
			var input map[string]json.RawMessage
			if err := json.Unmarshal([]byte(valid), &input); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(targets)
			if err != nil {
				t.Fatal(err)
			}
			input["ReadOnly"] = raw
			raw, err = json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if err := Run(string(raw), &out); err == nil || !strings.Contains(err.Error(), "incomplete probe inputs") || out.Len() != 0 {
				t.Fatalf("unclean read-only target reached observations: output=%q err=%v", out.String(), err)
			}
		})
	}
}

type reportFailureWriter struct {
	err   error
	calls int
}

func (w *reportFailureWriter) Write([]byte) (int, error) { w.calls++; return 0, w.err }

func TestReportStopsOnOutputFailure(t *testing.T) {
	want := errors.New("report output failed")
	for _, state := range []string{"PASS", "FAIL", "UNAVAILABLE"} {
		w := &reportFailureWriter{err: want}
		err := report(w, []Check{{Name: "first", State: state}, {Name: "second", State: "PASS"}})
		if !errors.Is(err, want) || w.calls != 1 {
			t.Fatalf("state=%s calls=%d err=%v", state, w.calls, err)
		}
	}
}
