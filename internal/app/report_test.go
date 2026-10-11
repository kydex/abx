package app

import (
	"testing"

	"github.com/kydex/abx/internal/sandbox"
)

// mountName names every kind of mount in `abx inspect` output. It had no test at
// all, so a renamed or added kind would print "unknown" with nothing failing.
// The table also pins what an unknown kind prints, because the switch has no
// default branch of its own.
func TestMountNameCoversEveryKind(t *testing.T) {
	for _, tc := range []struct {
		kind sandbox.Kind
		want string
	}{
		{sandbox.Directory, "directory"},
		{sandbox.ReadOnly, "read-only"},
		{sandbox.Writable, "read-write"},
		{sandbox.Symlink, "symlink"},
		{sandbox.Tmpfs, "private-tmpfs"},
		{sandbox.Proc, "private-proc"},
		{sandbox.Dev, "private-dev"},
		{sandbox.Kind(99), "unknown"},
		{sandbox.Kind(200), "unknown"},
	} {
		if got := mountName(tc.kind); got != tc.want {
			t.Errorf("mountName(%d) = %q, want %q", tc.kind, got, tc.want)
		}
	}
}

// The names above are what a user reads to understand a plan, so they are also
// checked against the kinds the plan builder can actually produce: a kind that
// exists but prints "unknown" is as broken as a missing one.
func TestEveryPlanKindHasAName(t *testing.T) {
	kinds := []sandbox.Kind{
		sandbox.Directory, sandbox.ReadOnly, sandbox.Writable,
		sandbox.Symlink, sandbox.Tmpfs, sandbox.Proc, sandbox.Dev,
	}
	for _, k := range kinds {
		if mountName(k) == "unknown" {
			t.Errorf("plan kind %d has no name in inspect output", k)
		}
	}
}
