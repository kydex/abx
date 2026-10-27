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
	want := map[sandbox.Kind]string{
		sandbox.Directory: "directory",
		sandbox.ReadOnly:  "read-only",
		sandbox.Writable:  "read-write",
		sandbox.Symlink:   "symlink",
		sandbox.Tmpfs:     "private-tmpfs",
		sandbox.Proc:      "private-proc",
		sandbox.Dev:       "private-dev",
	}
	kinds := sandbox.AllKinds()
	if len(kinds) != int(sandbox.Dev)+1 {
		t.Fatalf("AllKinds returned %d kinds, want %d", len(kinds), int(sandbox.Dev)+1)
	}
	for _, kind := range kinds {
		name, ok := want[kind]
		if !ok {
			t.Errorf("mount kind %d has no expected name", kind)
			continue
		}
		if got := mountName(kind); got != name {
			t.Errorf("mountName(%d) = %q, want %q", kind, got, name)
		}
	}
	for _, kind := range []sandbox.Kind{99, 200} {
		if got := mountName(kind); got != "unknown" {
			t.Errorf("mountName(%d) = %q, want unknown", kind, got)
		}
	}
}

// The names above are what a user reads to understand a plan, so they are also
// checked against the kinds the plan builder can actually produce: a kind that
// exists but prints "unknown" is as broken as a missing one.
func TestEveryPlanKindHasAName(t *testing.T) {
	for _, kind := range sandbox.AllKinds() {
		if mountName(kind) == "unknown" {
			t.Errorf("plan kind %d has no name in inspect output", kind)
		}
	}
}
