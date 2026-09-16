package app

import (
	"fmt"
	"strings"

	"github.com/kydex/abx/internal/sandbox"
)

const isolationSummary = `isolation:
  user namespace: private
  pid namespace: private
  ipc namespace: private
  uts namespace: private
  network: shared with host
`

// Format returns a readable command description, never a reusable shell string.
func formatPlan(plan sandbox.Plan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "cwd: %q\ncommand: %q\n", plan.WorkingDirectory(), plan.Command())
	for _, m := range plan.Mounts() {
		fmt.Fprintf(&b, "mount: %s", mountName(m.Kind))
		if m.Source != "" {
			fmt.Fprintf(&b, " %q ->", m.Source)
		}
		fmt.Fprintf(&b, " %q", m.Target)
		if m.Mode != 0 {
			fmt.Fprintf(&b, " mode=%04o", m.Mode)
		}
		b.WriteByte('\n')
	}
	for _, e := range plan.Environment() {
		fmt.Fprintf(&b, "env: %s=%q\n", e.Name, e.Value)
	}
	appendPlanSummary(&b, plan)
	return b.String()
}

func appendPlanSummary(b *strings.Builder, plan sandbox.Plan) {
	b.WriteByte('\n')
	b.WriteString(isolationSummary)
	b.WriteString("writable:\n")
	for _, m := range plan.Mounts() {
		if m.Kind == sandbox.Writable {
			fmt.Fprintf(b, "  %s\n", m.Target)
		}
	}
	b.WriteString("private:\n")
	for _, m := range plan.Mounts() {
		switch m.Kind {
		case sandbox.Tmpfs, sandbox.Proc, sandbox.Dev:
			fmt.Fprintf(b, "  %s\n", m.Target)
		}
	}
}

func mountName(k sandbox.Kind) string {
	switch k {
	case sandbox.Directory:
		return "directory"
	case sandbox.ReadOnly:
		return "read-only"
	case sandbox.Writable:
		return "read-write"
	case sandbox.Symlink:
		return "symlink"
	case sandbox.Tmpfs:
		return "private-tmpfs"
	case sandbox.Proc:
		return "private-proc"
	case sandbox.Dev:
		return "private-dev"
	}
	return "unknown"
}
