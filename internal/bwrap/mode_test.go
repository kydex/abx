package bwrap

import (
	"os"
	"testing"
)

// internal/host applies the same two conditions to the objects it trusts, in a
// predicate it does not export and this package must not depend on. The rules
// are deliberately kept in two places -- this one adds the execute bit because
// its objects are executed -- but the tables are independent: this one pins
// only this package's copy, and a change to the host copy fails nothing here.
// Keeping the shared half in step is a review duty, not a test guarantee.
func TestUnsafeModeReason(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode os.FileMode
		want string
	}{
		{"executable", 0o755, ""},
		{"owner execute only", 0o700, ""},
		{"execute for group", 0o710, ""},
		// The execute bit is this package's addition to the shared rule, and it
		// is checked first, so a non-executable object is reported before its
		// permissions are considered at all.
		{"plain readable", 0o644, "not executable"},
		{"read only", 0o444, "not executable"},
		{"group writable", 0o775, "writable by group or other"},
		{"other writable", 0o757, "writable by group or other"},
		{"world writable", 0o777, "writable by group or other"},
		// Go mode bits, not Unix ones: 0o4755 carries no os.ModeSetuid.
		{"setuid", 0o755 | os.ModeSetuid, "has setuid, setgid, or sticky bit"},
		{"setgid", 0o755 | os.ModeSetgid, "has setuid, setgid, or sticky bit"},
		{"sticky", 0o755 | os.ModeSticky, "has setuid, setgid, or sticky bit"},
		// Order is the shared order: not executable, then writable, then bits.
		{"world writable and setuid", 0o777 | os.ModeSetuid, "writable by group or other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := unsafeModeReason(tc.mode); got != tc.want {
				t.Fatalf("unsafeModeReason(%v) = %q, want %q", tc.mode, got, tc.want)
			}
		})
	}
}
