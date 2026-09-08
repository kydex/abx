package sandbox

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Validate checks the structural invariants of an in-memory sandbox plan without touching the host filesystem.
// It is the final structural guard before a plan is translated into Bubblewrap arguments.
func (p Plan) Validate() error {
	if !clean(p.expected.homeSource) || (p.expected.skillsSource != "" && !clean(p.expected.skillsSource)) {
		return errors.New("invalid plan expected source")
	}
	switch p.expected.kind {
	case planRun, planWork, planVerify:
		if p.cwd != "/workspace" || !clean(p.expected.projectSource) {
			return errors.New("invalid project plan expectation")
		}
	case planShell:
		if p.cwd != "/home/agent" || p.expected.projectSource != "" {
			return errors.New("invalid shell plan expectation")
		}
	default:
		return errors.New("invalid plan kind")
	}

	if len(p.command) == 0 || !clean(p.command[0]) {
		return errors.New("invalid plan command")
	}
	for _, arg := range p.command {
		if strings.ContainsRune(arg, 0) {
			return errors.New("plan command argument contains NUL")
		}
	}
	seenEnv := make(map[string]bool, len(p.environment))
	for _, e := range p.environment {
		if !validName(e.Name) || seenEnv[e.Name] || strings.ContainsRune(e.Value, 0) {
			return errors.New("invalid plan environment")
		}
		seenEnv[e.Name] = true
	}

	base := baseMounts()
	if len(p.mounts) < len(base) || !slices.Equal(p.mounts[:len(base)], base) {
		return errors.New("invalid plan base directories")
	}
	seenSystem := make(map[string]bool, len(p.expected.system))
	for _, m := range p.expected.system {
		compat := compatibilityTarget(m.Target)
		if !allowedSystemTarget(m.Target) || m.Mode != 0 || (m.Kind != ReadOnly && !(compat && m.Kind == Symlink)) {
			return fmt.Errorf("invalid expected system mount %q", m.Target)
		}
		if (m.Kind == ReadOnly && !clean(m.Source)) || (m.Kind == Symlink && (m.Source == "" || strings.ContainsRune(m.Source, 0))) {
			return errors.New("invalid expected system source")
		}
		if seenSystem[m.Target] {
			return fmt.Errorf("duplicate expected system mount %q", m.Target)
		}
		seenSystem[m.Target] = true
	}
	if !seenSystem["/usr"] {
		return errors.New("expected system /usr is required")
	}

	index := len(base)
	for _, want := range p.expected.system {
		if index >= len(p.mounts) || p.mounts[index] != want {
			return fmt.Errorf("invalid plan system mount %q", want.Target)
		}
		index++
	}
	private := privateMounts()
	for _, want := range private {
		if index >= len(p.mounts) || p.mounts[index] != want {
			return fmt.Errorf("invalid private mount %q", want.Target)
		}
		index++
	}

	wantHome := Mount{Kind: Writable, Source: p.expected.homeSource, Target: "/home/agent"}
	if index >= len(p.mounts) || p.mounts[index] != wantHome {
		return errors.New("invalid plan home mount")
	}
	index++

	if p.expected.skillsSource != "" {
		wantSkills := Mount{Kind: ReadOnly, Source: p.expected.skillsSource, Target: "/home/agent/.agents/skills"}
		if index >= len(p.mounts) || p.mounts[index] != wantSkills {
			return errors.New("invalid plan skills mount")
		}
		index++
	} else if index < len(p.mounts) && p.mounts[index].Target == "/home/agent/.agents/skills" {
		return errors.New("unexpected plan skills mount")
	}

	switch p.expected.kind {
	case planRun, planWork, planVerify:
		wantWorkspace := Mount{Kind: Writable, Source: p.expected.projectSource, Target: "/workspace"}
		if index >= len(p.mounts) || p.mounts[index] != wantWorkspace {
			return errors.New("invalid plan workspace mount")
		}
		index++
	case planShell:
		if index < len(p.mounts) && p.mounts[index].Target == "/workspace" {
			return errors.New("shell plan contains workspace")
		}
	}

	if p.expected.kind == planVerify {
		wantProbe := Mount{Kind: ReadOnly, Source: "/proc/self/exe", Target: ProbePath}
		if index >= len(p.mounts) || p.mounts[index] != wantProbe || len(p.command) != 3 || p.command[0] != ProbePath || p.command[1] != "__probe" {
			return errors.New("invalid verification probe")
		}
		index++
	} else if index < len(p.mounts) && p.mounts[index].Target == ProbePath {
		return errors.New("unexpected verification probe")
	}

	if index != len(p.mounts) {
		return fmt.Errorf("unexpected plan mount %q", p.mounts[index].Target)
	}
	return nil
}
