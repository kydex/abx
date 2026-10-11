package host

import (
	"os"
	"strings"
	"testing"
)

// The mode rule for trusted host objects was written out three times and
// drifted once. This table pins what the single predicate decides, so a change
// to it has to be a decision about this table too.
func TestUnsafeModeReason(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode os.FileMode
		want string
	}{
		{"plain readable", 0o644, ""},
		{"executable", 0o755, ""},
		{"read only", 0o444, ""},
		{"owner write only", 0o600, ""},
		{"group writable", 0o664, "is writable by group or other"},
		{"other writable", 0o646, "is writable by group or other"},
		{"world writable", 0o702, "is writable by group or other"},
		{"everyone writable", 0o777, "is writable by group or other"},
		{"setuid", 0o755 | os.ModeSetuid, "has setuid, setgid, or sticky bit"},
		{"setgid", 0o755 | os.ModeSetgid, "has setuid, setgid, or sticky bit"},
		{"sticky", 0o755 | os.ModeSticky, "has setuid, setgid, or sticky bit"},
		{"setuid on a data file", 0o644 | os.ModeSetuid, "has setuid, setgid, or sticky bit"},
		// Writability is reported first, matching the order the predicate
		// checks: a world-writable setuid file is one object with two faults,
		// and the message should name the one the operator can act on first.
		{"world writable and setuid", 0o777 | os.ModeSetuid, "is writable by group or other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := unsafeModeReason(tc.mode); got != tc.want {
				t.Fatalf("unsafeModeReason(%v) = %q, want %q", tc.mode, got, tc.want)
			}
		})
	}
}

// A call site must refuse the object for the reason the predicate gives, not
// merely refuse it: the message is what an operator acts on.
func TestTrustedObjectChecksReportThePredicateReason(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name string
		mode os.FileMode
		want string
	}{
		{"group writable", 0o660, "is writable by group or other"},
		{"setuid", 0o644 | os.ModeSetuid, "has setuid, setgid, or sticky bit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := dir + "/" + strings.ReplaceAll(tc.name, " ", "-")
			if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, tc.mode); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := unsafeModeReason(info.Mode()); got != tc.want {
				t.Fatalf("predicate disagrees with the case: %q vs %q", got, tc.want)
			}
			err = checkPasswdFile(path)
			if err == nil {
				t.Fatalf("checkPasswdFile accepted a %s object", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("checkPasswdFile: %v, want it to name %q", err, tc.want)
			}
		})
	}
}
