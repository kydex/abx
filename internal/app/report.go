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
  mount namespace: private
  network: shared with host
  cgroup namespace: shared with host
  nested user namespaces: disabled
  lifetime: killed with the launcher after sandbox setup
`

// formatPlan returns a readable command description, never a reusable shell string.
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
		if m.Kind == sandbox.Tmpfs && m.Size != 0 {
			fmt.Fprintf(&b, " size=%d", m.Size)
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
	mounted := make(map[string]bool)
	for _, m := range plan.Mounts() {
		if m.Kind != sandbox.Directory {
			mounted[m.Target] = true
		}
	}
	b.WriteString("writable:\n")
	for _, m := range plan.Mounts() {
		if m.Kind == sandbox.Writable {
			fmt.Fprintf(b, "  %s\n", m.Target)
		}
	}
	// Base directories that no later mount covers stay writable as ephemeral
	// scratch; listing them keeps "writable:" from reading as the full set.
	b.WriteString("writable scratch:\n")
	for _, m := range plan.Mounts() {
		if m.Kind == sandbox.Directory && !mounted[m.Target] {
			fmt.Fprintf(b, "  %s\n", m.Target)
		}
	}
	b.WriteString("private:\n")
	for _, m := range plan.Mounts() {
		switch m.Kind {
		case sandbox.Tmpfs:
			if m.Size != 0 {
				fmt.Fprintf(b, "  %s (size=%d)\n", m.Target, m.Size)
				continue
			}
			fmt.Fprintf(b, "  %s\n", m.Target)
		case sandbox.Proc, sandbox.Dev:
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
