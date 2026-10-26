package probe

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kydex/abx/internal/sandbox"
	"golang.org/x/sys/unix"
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
	if err := report(&bytes.Buffer{}, []Check{{Name: "required", State: "PASS"}, {Name: "future optional", State: "N/A", Optional: true}}); err != nil {
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

// Optionality is a field, not the name: an unconfigured optional check is a
// legitimate result, while one that ran and failed or came back unavailable
// still fails the run.
func TestOptionalChecksExemptOnlyUnconfiguredState(t *testing.T) {
	if err := report(&bytes.Buffer{}, []Check{{Name: "future optional", State: stateNA, Detail: "not configured", Optional: true}}); err != nil {
		t.Fatalf("unconfigured optional check failed the run: %v", err)
	}
	for _, state := range []string{stateFail, stateUnavailable} {
		if err := report(&bytes.Buffer{}, []Check{{Name: "future optional", State: state, Optional: true}}); err == nil {
			t.Fatalf("optional check in state %s passed the run", state)
		}
	}
}

// These five malformed inputs are decode's business. They used to go through
// Run, which checks the host gate first and on an ordinary machine never let
// decode see them, so a disabled decode check left this test green.
func TestPrivateProbeRejectsMissingObservations(t *testing.T) {
	for _, raw := range []string{"", "{}", "null", "[]", strings.Repeat("x", 65537)} {
		if _, err := decode(raw); err == nil {
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
	if _, err := decode(raw); err == nil || !strings.Contains(err.Error(), "probe input too large") {
		t.Fatalf("oversized valid JSON err = %v", err)
	}
}

func TestRunComposesGateDecodeAndReport(t *testing.T) {
	// The three glue lines of Run execute only on a live host; here the gate
	// and the observer are substituted so the composition itself is pinned:
	// a passing world must report PASS, a failing gate must win over decode,
	// and a failed decode must still stop before any report.
	runtime := t.TempDir()
	if err := os.Chmod(runtime, 0o700); err != nil {
		t.Fatal(err)
	}
	obs := observer{
		getwd:    func() (string, error) { return "/workspace", nil },
		hostname: func() (string, error) { return "abx", nil },
		environ:  func() []string { return []string{"HOME=/home/agent"} },
		stat: func(p string) (os.FileInfo, error) {
			if p == "/run/user" {
				return os.Stat(runtime)
			}
			return nil, os.ErrNotExist
		},
		identify: func(p string) (Identity, error) {
			switch p {
			case "/home/agent", "/workspace":
				return Identity{}, nil
			case "/host-home":
				return Identity{}, os.ErrNotExist
			default:
				return Identity{Device: 9, Inode: 9}, nil
			}
		},
		readlink: func(string) (string, error) { return "sandbox-ns", nil },
		statfs: func(_ string, fs *unix.Statfs_t) error {
			fs.Type = unix.TMPFS_MAGIC
			return nil
		},
		statDevice: func(_ string, st *unix.Stat_t) error {
			st.Mode = unix.S_IFCHR
			st.Rdev = unix.Mkdev(1, 3)
			return nil
		},
		mountinfo: func() ([]byte, error) {
			return []byte("1 2 0:1 / /usr rw,ro - ext4 /dev/sda1 rw\n"), nil
		},
		writeProbe: func(string) error { return nil },
	}
	valid := `{"Home":"/host-home","Sentinel":"/host-home/probe","Environment":["HOME=/home/agent"],"ReadOnly":["/usr"],"Namespaces":{"user":"u","mnt":"m","pid":"p","ipc":"i","uts":"t"},"Objects":{"/host-home":{},"/tmp":{},"/var/tmp":{},"/run":{},"/dev":{},"/proc":{},"/dev/shm":{}}}`
	var out bytes.Buffer
	if err := run(func() error { return nil }, valid, &out, obs); err != nil {
		t.Fatalf("glue = %v", err)
	}
	if !strings.Contains(out.String(), `PASS "working directory"`) {
		t.Fatalf("glue output = %q", out.String())
	}
	gateErr := errors.New("sealed")
	var gated bytes.Buffer
	if err := run(func() error { return gateErr }, "not json", &gated, obs); !errors.Is(err, gateErr) || gated.Len() != 0 {
		t.Fatalf("gate failure = %q, %v", gated.String(), err)
	}
	var malformed bytes.Buffer
	if err := run(func() error { return nil }, "not json", &malformed, obs); err == nil || !strings.Contains(err.Error(), "invalid character") || malformed.Len() != 0 {
		t.Fatalf("decode failure = %q, %v", malformed.String(), err)
	}
}

func TestProbeGateRunsBeforeDecode(t *testing.T) {
	var out bytes.Buffer
	err := Run("not json at all", &out)
	if err == nil || !strings.Contains(err.Error(), "only available inside the sandbox") || out.Len() != 0 {
		t.Fatalf("host Run without gate = %q, %v", out.String(), err)
	}
}

func TestPrivateProbeRejectsHostExecution(t *testing.T) {
	// Well-formed input that passes decode must still not run on the host:
	// /abx-probe is a read-only mount of this binary only inside the sandbox.
	// The positive path (inside the sandbox) is covered by live verify tests.
	valid := `{"Home":"/host-home","Sentinel":"/host-home/probe","Environment":["HOME=/home/agent"],"ReadOnly":["/usr"],"Namespaces":{"user":"u","mnt":"m","pid":"p","ipc":"i","uts":"t"},"Objects":{"/host-home":{},"/tmp":{},"/var/tmp":{},"/run":{},"/dev":{},"/proc":{},"/dev/shm":{}}}`
	var out bytes.Buffer
	err := Run(valid, &out)
	if err == nil || !strings.Contains(err.Error(), "only available inside the sandbox") || out.Len() != 0 {
		t.Fatalf("host probe execution = %q, %v", out.String(), err)
	}
}

func TestDecodeRejectsEachEmptyListSeparately(t *testing.T) {
	base := `{"Home":"/host-home","Sentinel":"/host-home/probe","Environment":["HOME=/home/agent"],"ReadOnly":["/usr"],"Namespaces":{"user":"u","mnt":"m","pid":"p","ipc":"i","uts":"t"},"Objects":{"/host-home":{},"/tmp":{},"/var/tmp":{},"/run":{},"/dev":{},"/proc":{},"/dev/shm":{}}}`
	for _, raw := range []string{
		strings.Replace(base, `"Environment":["HOME=/home/agent"]`, `"Environment":[]`, 1),
		strings.Replace(base, `"ReadOnly":["/usr"]`, `"ReadOnly":[]`, 1),
	} {
		if _, err := decode(raw); err == nil || !strings.Contains(err.Error(), "incomplete probe inputs") {
			t.Fatalf("decode with one empty list = %v", err)
		}
	}
}

func TestPrivatePathsCoverSandboxMounts(t *testing.T) {
	known := map[string]bool{runtimeDirectory: true}
	for _, p := range privateIdentityPaths {
		known[p] = true
	}
	for _, target := range sandbox.PrivateMountTargets() {
		if !known[target] {
			t.Fatalf("private mount target %q has no probe observation", target)
		}
	}
	if !slices.Contains(privateIdentityPaths, "/dev/shm") {
		t.Fatal("probe does not observe /dev/shm")
	}
}

func TestCleanPathMatchesSandbox(t *testing.T) {
	// Probe keeps its own copy of the predicate instead of importing it;
	// a one-sided edit must fail here rather than drift silently.
	for _, p := range []string{"/usr/bin/sh", "/", "relative", "/tmp/", "/x\x00y", "", "/a/../b", "//double"} {
		if cleanPath(p) != sandbox.CleanPath(p) {
			t.Fatalf("cleanPath(%q) disagrees with sandbox", p)
		}
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
		{Name: "shared skills", State: "N/A", Detail: "not configured", Optional: true},
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
	valid := `{"Home":"/host-home","Sentinel":"/host-home/probe","Environment":["HOME=/home/agent"],"ReadOnly":["/usr"],"Namespaces":{"user":"u","mnt":"m","pid":"p","ipc":"i","uts":"t"},"Objects":{"/host-home":{},"/tmp":{},"/var/tmp":{},"/run":{},"/dev":{},"/proc":{},"/dev/shm":{}}}`
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
		{"Objects", "/proc", "missing identity"},
		{"Objects", "/dev/shm", "missing identity"},
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
			_, err = decode(string(raw))
			if err == nil || !strings.Contains(err.Error(), tc.message) || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("incomplete input accepted: %v", err)
			}
		})
	}
}

func TestDecodeRejectsNonCanonicalProbePaths(t *testing.T) {
	valid := `{"Home":"/host-home","Sentinel":"/host-home/probe","Environment":["HOME=/home/agent"],"ReadOnly":["/usr"],"Namespaces":{"user":"u","mnt":"m","pid":"p","ipc":"i","uts":"t"},"Objects":{"/host-home":{},"/tmp":{},"/var/tmp":{},"/run":{},"/dev":{},"/proc":{},"/dev/shm":{}}}`
	for _, tc := range []struct{ field, value string }{
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
			if _, err := decode(string(raw)); err == nil || !strings.Contains(err.Error(), "incomplete probe inputs") {
				t.Fatalf("non-canonical path accepted: %v", err)
			}
		})
	}
}

func TestDecodeRejectsNonCleanReadOnlyTargets(t *testing.T) {
	valid := `{"Home":"/host-home","Sentinel":"/host-home/probe","Environment":["HOME=/home/agent"],"ReadOnly":["/usr"],"Namespaces":{"user":"u","mnt":"m","pid":"p","ipc":"i","uts":"t"},"Objects":{"/host-home":{},"/tmp":{},"/var/tmp":{},"/run":{},"/dev":{},"/proc":{},"/dev/shm":{}}}`
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
			if _, err := decode(string(raw)); err == nil || !strings.Contains(err.Error(), "incomplete probe inputs") {
				t.Fatalf("unclean read-only target accepted: %v", err)
			}
		})
	}
}

// FuzzDecode runs arbitrary payloads through decode and then through check over
// an observer that touches nothing. Anything decode accepts must survive its
// own encoding, and a hostile value -- an environment entry, a path -- must not
// drive check into a panic or an observation outside the four legal states.
func FuzzDecode(f *testing.F) {
	seed, err := json.Marshal(sandboxExpected())
	if err != nil {
		f.Fatal(err)
	}
	f.Add(string(seed))
	f.Add("")
	f.Add("{}")
	f.Add("null")
	f.Add("[]")
	f.Add(`{"Home":"/host-home/","Environment":["X=\u0000"],"ReadOnly":["relative"]}`)
	stubErr := errors.New("stub observer")
	quiet := observer{
		getwd:      func() (string, error) { return "", nil },
		hostname:   func() (string, error) { return "", nil },
		environ:    func() []string { return nil },
		stat:       func(string) (os.FileInfo, error) { return nil, stubErr },
		identify:   func(string) (Identity, error) { return Identity{}, stubErr },
		readlink:   func(string) (string, error) { return "", stubErr },
		statfs:     func(string, *unix.Statfs_t) error { return stubErr },
		statDevice: func(string, *unix.Stat_t) error { return stubErr },
		mountinfo:  func() ([]byte, error) { return nil, stubErr },
		writeProbe: func(string) error { return nil },
	}
	f.Fuzz(func(t *testing.T, raw string) {
		e, err := decode(raw)
		if err != nil {
			return
		}
		encoded, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("accepted payload does not marshal: %v", err)
		}
		if _, err := decode(string(encoded)); err != nil {
			t.Fatalf("accepted payload rejects its own encoding: %v", err)
		}
		checks := check(e, quiet)
		for _, c := range checks {
			switch c.State {
			case statePass, stateUnavailable, stateFail, stateNA:
			default:
				t.Fatalf("check returned unknown state %q for %q", c.State, raw)
			}
			if c.Name == "" {
				t.Fatalf("check returned an unnamed observation for %q", raw)
			}
		}
		_ = report(io.Discard, checks)
	})
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
