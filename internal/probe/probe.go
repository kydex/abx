// Package probe implements fixed observations made by ABX inside its sandbox.
// Its JSON input is private re-execution data, not a public protocol.
package probe

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

type Identity struct{ Device, Inode uint64 }

func Identify(path string) (Identity, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Identity{}, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return Identity{}, errors.New("filesystem identity unavailable")
	}
	return Identity{uint64(st.Dev), st.Ino}, nil
}

type Expected struct {
	Profile, Project     Identity
	Home, Sentinel, Link string
	Namespaces           map[string]string
	Objects              map[string]Identity
	Environment          []string
	ReadOnly             []string
	Skills               bool
}

const (
	statePass        = "PASS"
	stateUnavailable = "UNAVAILABLE"
	stateFail        = "FAIL"
	stateNA          = "N/A"
)

type Check struct{ Name, State, Detail string }

func observation(name string, ok bool, err error, detail string) Check {
	c := Check{Name: name, State: statePass}
	if err != nil {
		c.State = stateUnavailable
		c.Detail = err.Error()
	} else if !ok {
		c.State = stateFail
		c.Detail = detail
	}
	return c
}

func Run(raw string, out io.Writer) error {
	if err := gate(); err != nil {
		return err
	}
	e, err := decode(raw)
	if err != nil {
		return err
	}
	return report(out, check(e))
}

// probePath must match sandbox.ProbePath: the fixed mountpoint of this
// binary inside the sandbox.
const probePath = "/abx-probe"

// gate rejects host-side execution of the private probe entry point. Inside
// the sandbox /abx-probe is a read-only mount of this binary, so
// /proc/self/exe and /abx-probe identify the same object and the mountinfo
// entry carries the ro flag. Anywhere else the probe must not run. This
// guards against a non-sandboxed execution mode, not against privilege
// escalation: the probe runs with the caller's own rights either way.
func gate() error {
	self, err := Identify("/proc/self/exe")
	if err != nil {
		return fmt.Errorf("private probe is only available inside the sandbox: %w", err)
	}
	mounted, err := Identify(probePath)
	if err != nil {
		return fmt.Errorf("private probe is only available inside the sandbox: %w", err)
	}
	raw, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return fmt.Errorf("private probe is only available inside the sandbox: %w", err)
	}
	return checkGate(self, mounted, string(raw))
}

// checkGate is the pure part of gate: identity comparison and mount-flag
// inspection without filesystem reads, so it is fully unit-testable.
// gate itself only supplies the three observed inputs.
func checkGate(self, mounted Identity, mountinfo string) error {
	if self != mounted {
		return errors.New("private probe is only available inside the sandbox")
	}
	if detail := readOnlyDetail(mountinfo, probePath); detail != "" {
		return fmt.Errorf("private probe is only available inside the sandbox: %s", detail)
	}
	return nil
}

func decode(raw string) (Expected, error) {
	var e Expected
	if len(raw) > 65536 {
		return e, errors.New("probe input too large")
	}
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		return e, err
	}
	if !cleanPath(e.Home) || !cleanPath(e.Sentinel) || !cleanLink(e.Link) || len(e.Environment) == 0 || len(e.ReadOnly) == 0 {
		return e, errors.New("incomplete probe inputs")
	}
	for _, target := range e.ReadOnly {
		if !cleanPath(target) {
			return e, errors.New("incomplete probe inputs")
		}
	}
	for _, name := range NamespaceNames() {
		if e.Namespaces[name] == "" {
			return e, fmt.Errorf("missing namespace %q", name)
		}
	}
	for _, p := range []string{e.Home, "/tmp", "/var/tmp", "/run", "/dev"} {
		if _, ok := e.Objects[p]; !ok {
			return e, fmt.Errorf("missing identity %q", p)
		}
	}
	return e, nil
}

// cleanPath accepts only clean absolute host paths without NUL bytes.
func cleanPath(p string) bool {
	return filepath.IsAbs(p) && filepath.Clean(p) == p && !strings.ContainsRune(p, 0)
}

// cleanLink accepts only the fixed verification link layout: a canonical
// path directly inside /workspace with the reserved file name prefix.
func cleanLink(p string) bool {
	return cleanPath(p) && filepath.Dir(p) == "/workspace" && strings.HasPrefix(filepath.Base(p), ".abx-verify-")
}

func report(out io.Writer, checks []Check) error {
	if len(checks) == 0 {
		return errors.New("no verification results")
	}
	failed := false
	for _, c := range checks {
		if c.State != statePass && !(c.State == stateNA && c.Name == "shared skills") {
			failed = true
		}
		line := fmt.Sprintf("%s %q", c.State, c.Name)
		if c.Detail != "" {
			line += fmt.Sprintf(": %q", c.Detail)
		}
		if _, err := fmt.Fprintln(out, line); err != nil {
			return err
		}
	}
	if failed {
		return errors.New("verification failed or required observations are unavailable")
	}
	return nil
}

func check(e Expected) []Check {
	var checks []Check
	add := func(name string, ok bool, err error, detail string) {
		checks = append(checks, observation(name, ok, err, detail))
	}
	cwd, err := os.Getwd()
	add("working directory", cwd == "/workspace", err, fmt.Sprintf("expected %q; observed %q", "/workspace", cwd))
	hostname, err := os.Hostname()
	add("hostname", hostname == "abx", err, fmt.Sprintf("expected %q; observed %q", "abx", hostname))
	checks = append(checks, checkEnvironment(os.Environ(), e.Environment))
	for _, p := range []string{e.Sentinel, e.Link, "/etc/shadow"} {
		_, err := os.Stat(p)
		if errors.Is(err, os.ErrNotExist) {
			add("hidden "+p, true, nil, "")
		} else {
			add("hidden "+p, false, err, "expected path to be absent; path is accessible")
		}
	}
	for _, target := range []struct {
		path string
		id   Identity
	}{{"/home/agent", e.Profile}, {"/workspace", e.Project}} {
		id, err := Identify(target.path)
		add("selected "+target.path, id == target.id, err, fmt.Sprintf("expected device=%d inode=%d; observed device=%d inode=%d", target.id.Device, target.id.Inode, id.Device, id.Inode))
	}
	id, err := Identify(e.Home)
	if errors.Is(err, os.ErrNotExist) {
		add("host home hidden", true, nil, "")
	} else {
		add("host home hidden", id != e.Objects[e.Home], err, "expected host home to be hidden; observed the host home filesystem object")
	}
	checks = append(checks, checkNamespaces(e.Namespaces, os.Readlink)...)
	for _, p := range []string{"/tmp", "/var/tmp", "/run", "/dev"} {
		id, err := Identify(p)
		add("private "+p, id != e.Objects[p], err, "expected a private filesystem object; observed the host object")
	}
	for _, p := range []string{"/tmp", "/var/tmp", "/run"} {
		var fs unix.Statfs_t
		err := unix.Statfs(p, &fs)
		add("tmpfs "+p, fs.Type == unix.TMPFS_MAGIC, err, fmt.Sprintf("expected tmpfs type=%#x; observed type=%#x", unix.TMPFS_MAGIC, fs.Type))
	}
	info, err := os.Stat("/run/user")
	ok := err == nil && info.IsDir() && info.Mode().Perm() == 0o700 && info.Mode()&(os.ModeSticky|os.ModeSetuid|os.ModeSetgid) == 0
	detail := ""
	if err == nil {
		detail = fmt.Sprintf("expected directory with mode 0700 and no special bits; observed %s (permissions %04o)", info.Mode(), info.Mode().Perm())
	}
	add("runtime directory mode", ok, err, detail)
	var st unix.Stat_t
	err = unix.Stat("/dev/null", &st)
	add("null device", st.Mode&unix.S_IFMT == unix.S_IFCHR && unix.Major(st.Rdev) == 1 && unix.Minor(st.Rdev) == 3, err, fmt.Sprintf("expected character device 1:3; observed file type=%#o device=%d:%d", st.Mode&unix.S_IFMT, unix.Major(st.Rdev), unix.Minor(st.Rdev)))
	raw, err := os.ReadFile("/proc/self/mountinfo")
	for _, target := range e.ReadOnly {
		detail := readOnlyDetail(string(raw), target)
		add("read-only "+target, detail == "", err, detail)
	}
	if !e.Skills {
		checks = append(checks, Check{Name: "shared skills", State: stateNA, Detail: "not configured"})
	}
	for _, p := range []string{"/home/agent", "/workspace"} {
		err := writeAndRemove(p)
		add("write and cleanup "+p, err == nil, err, "")
	}
	return checks
}

func readOnlyDetail(raw, target string) string {
	found := false
	for _, line := range strings.Split(raw, "\n") {
		f := strings.Fields(line)
		if len(f) < 10 || f[4] != target {
			continue
		}
		found = true
		if !strings.Contains(","+f[5]+",", ",ro,") {
			return fmt.Sprintf("expected ro mount flag; observed flags %q", f[5])
		}
	}
	if !found {
		return "expected read-only mountpoint; no matching mountinfo entry"
	}
	return ""
}

func writeAndRemove(dir string) error {
	f, err := os.CreateTemp(dir, ".abx-write-*")
	if err != nil {
		return err
	}
	_, writeErr := f.WriteString("abx verification\n")
	return errors.Join(writeErr, f.Close(), os.Remove(f.Name()))
}

// NamespaceNames is the fixed set required by the common execution policy.
func NamespaceNames() []string { return []string{"user", "mnt", "pid", "ipc", "uts"} }

func checkNamespaces(host map[string]string, readlink func(string) (string, error)) []Check {
	var checks []Check
	for _, name := range NamespaceNames() {
		ns, err := readlink("/proc/self/ns/" + name)
		checks = append(checks, observation(name+" namespace", host[name] != "" && ns != "" && ns != host[name], err, fmt.Sprintf("expected nonempty namespace identities to differ; host=%q sandbox=%q", host[name], ns)))
	}
	return checks
}

// checkEnvironment compares full assignments, including duplicates, but discloses only variable names.

// sanitizeEnvironment puts both sides of the comparison through the same lossy
// transform. The expected values reach the probe by being marshalled to JSON,
// and the JSON encoder replaces every byte of an invalid UTF-8 sequence with
// U+FFFD; without this the host value and its own round trip would differ
// forever, and verify would report a difference that no rendering of the
// environment could show. Anything that still differs afterwards differs in the
// bytes a user could read.
func sanitizeEnvironment(entries []string) []string {
	out := make([]string, len(entries))
	for i, entry := range entries {
		if utf8.ValidString(entry) {
			out[i] = entry
			continue
		}
		var b strings.Builder
		b.Grow(len(entry) + 3)
		for j := 0; j < len(entry); {
			c, size := utf8.DecodeRuneInString(entry[j:])
			if c == utf8.RuneError && size == 1 {
				b.WriteRune(utf8.RuneError)
			} else {
				b.WriteString(entry[j : j+size])
			}
			j += size
		}
		out[i] = b.String()
	}
	return out
}

func checkEnvironment(actual, expected []string) Check {
	actual, expected = sanitizeEnvironment(actual), sanitizeEnvironment(expected)
	slices.Sort(actual)
	slices.Sort(expected)
	if slices.Equal(actual, expected) {
		return observation("exact environment", true, nil, "")
	}
	group := func(entries []string) map[string][]string {
		out := make(map[string][]string)
		for _, entry := range entries {
			name, _, ok := strings.Cut(entry, "=")
			if !ok || name == "" {
				name = "<malformed assignment>"
			}
			out[name] = append(out[name], entry)
		}
		return out
	}
	got, want := group(actual), group(expected)
	var names []string
	for name, entries := range got {
		if !slices.Equal(entries, want[name]) {
			names = append(names, name)
		}
	}
	for name := range want {
		if _, exists := got[name]; !exists {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return observation("exact environment", false, nil, fmt.Sprintf("differing variable names: %q", names))
}
