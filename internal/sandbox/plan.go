// Package sandbox builds the same isolation policy for run, shell and work sessions.
package sandbox

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

type Kind uint8

const (
	Directory Kind = iota
	ReadOnly
	Writable
	Symlink
	Tmpfs
	Proc
	Dev
)

type Mount struct {
	Kind           Kind
	Source, Target string
	Mode           uint32
}

// Mode describes the visibility and working directory of a normal session.
type Mode uint8

const (
	Run Mode = iota + 1
	Shell
	Work
)

type Input struct {
	Home, Project, Skills string
	Mode                  Mode
	System                []Mount
	Environment           []Entry
	Command               []string
}

type planKind uint8

const (
	planRun planKind = iota + 1
	planShell
	planWork
	planVerify
)

type expectations struct {
	kind          planKind
	homeSource    string
	projectSource string
	skillsSource  string
	system        []Mount
}

type Plan struct {
	mounts      []Mount
	environment []Entry
	command     []string
	cwd         string
	expected    expectations
}

func (p Plan) Mounts() []Mount          { return slices.Clone(p.mounts) }
func (p Plan) Environment() []Entry     { return slices.Clone(p.environment) }
func (p Plan) Command() []string        { return slices.Clone(p.command) }
func (p Plan) WorkingDirectory() string { return p.cwd }
func clean(p string) bool {
	return filepath.IsAbs(p) && filepath.Clean(p) == p && !strings.ContainsRune(p, 0)
}

func baseMounts() []Mount {
	mounts := make([]Mount, 0, 10)
	for _, dir := range []string{"/usr", "/etc", "/home", "/opt", "/var", "/tmp", "/var/tmp", "/run", "/proc", "/dev"} {
		mounts = append(mounts, Mount{Kind: Directory, Target: dir})
	}
	return mounts
}

func privateMounts() []Mount {
	return []Mount{
		{Kind: Tmpfs, Target: "/tmp"},
		{Kind: Tmpfs, Target: "/var/tmp"},
		{Kind: Tmpfs, Target: "/run"},
		{Kind: Directory, Target: "/run/user", Mode: 0700},
		{Kind: Proc, Target: "/proc"},
		{Kind: Dev, Target: "/dev"},
	}
}

func allowedSystemTarget(target string) bool {
	switch target {
	case "/usr", "/bin", "/sbin", "/lib", "/lib64",
		"/etc/hosts", "/etc/resolv.conf", "/etc/nsswitch.conf", "/etc/gai.conf",
		"/etc/passwd", "/etc/group", "/etc/ssl", "/etc/ca-certificates", "/etc/pki",
		"/etc/ld.so.cache", "/etc/ld.so.conf", "/etc/ld.so.conf.d",
		"/etc/localtime", "/etc/timezone", "/etc/os-release", "/etc/shells", "/etc/alternatives":
		return true
	default:
		return false
	}
}

func compatibilityTarget(target string) bool {
	return target == "/bin" || target == "/sbin" || target == "/lib" || target == "/lib64"
}

// Build varies only the writable project and command; namespace and system policy are shared.
func Build(in Input) (Plan, error) {
	p, err := build(in)
	if err != nil {
		return Plan{}, err
	}
	if err := p.Validate(); err != nil {
		return Plan{}, err
	}
	return p, nil
}

func build(in Input) (Plan, error) {
	if !clean(in.Home) || len(in.Command) == 0 || !clean(in.Command[0]) {
		return Plan{}, errors.New("invalid session inputs")
	}
	if (in.Mode == Shell && in.Project != "") || (in.Mode != Shell && !clean(in.Project)) {
		return Plan{}, errors.New("project does not match session mode")
	}
	if in.Skills != "" && !clean(in.Skills) {
		return Plan{}, errors.New("invalid skills source")
	}
	var kind planKind
	switch in.Mode {
	case Run:
		kind = planRun
	case Shell:
		kind = planShell
	case Work:
		kind = planWork
	default:
		return Plan{}, errors.New("invalid session mode")
	}
	p := Plan{
		cwd:         "/home/agent",
		command:     slices.Clone(in.Command),
		environment: slices.Clone(in.Environment),
		expected: expectations{
			kind:          kind,
			homeSource:    in.Home,
			projectSource: in.Project,
			skillsSource:  in.Skills,
			system:        slices.Clone(in.System),
		},
	}
	for _, arg := range p.command {
		if strings.ContainsRune(arg, 0) {
			return Plan{}, errors.New("command argument contains NUL")
		}
	}
	seenEnv := map[string]bool{}
	for _, e := range p.environment {
		if !validName(e.Name) || seenEnv[e.Name] || strings.ContainsRune(e.Value, 0) {
			return Plan{}, errors.New("invalid session environment")
		}
		seenEnv[e.Name] = true
	}
	p.mounts = append(p.mounts, baseMounts()...)
	seen := map[string]bool{}
	for _, m := range in.System {
		compat := compatibilityTarget(m.Target)
		if !allowedSystemTarget(m.Target) || seen[m.Target] || m.Mode != 0 || (m.Kind != ReadOnly && !(compat && m.Kind == Symlink)) {
			return Plan{}, fmt.Errorf("invalid system mount %q", m.Target)
		}
		if (m.Kind == ReadOnly && !clean(m.Source)) || (m.Kind == Symlink && (m.Source == "" || strings.ContainsRune(m.Source, 0))) {
			return Plan{}, errors.New("invalid system source")
		}
		seen[m.Target] = true
		p.mounts = append(p.mounts, m)
	}
	if !seen["/usr"] {
		return Plan{}, errors.New("system /usr is required")
	}
	p.mounts = append(p.mounts, privateMounts()...)
	p.mounts = append(p.mounts, Mount{Kind: Writable, Source: in.Home, Target: "/home/agent"})
	if in.Skills != "" {
		p.mounts = append(p.mounts, Mount{Kind: ReadOnly, Source: in.Skills, Target: "/home/agent/.agents/skills"})
	}
	if in.Mode != Shell {
		p.cwd = "/workspace"
		p.mounts = append(p.mounts, Mount{Kind: Writable, Source: in.Project, Target: "/workspace"})
	}
	return p, nil
}

const ProbePath = "/abx-probe"

// BuildVerification adds exactly one fixed read-only executable to the run plan.
func BuildVerification(in Input) (Plan, error) {
	if in.Mode != Run || len(in.Command) != 3 || in.Command[0] != ProbePath || in.Command[1] != "__probe" {
		return Plan{}, errors.New("invalid verification command")
	}
	p, err := build(in)
	if err != nil {
		return Plan{}, err
	}
	p.expected.kind = planVerify
	p.mounts = append(p.mounts, Mount{Kind: ReadOnly, Source: "/proc/self/exe", Target: ProbePath})
	if err := p.Validate(); err != nil {
		return Plan{}, err
	}
	return p, nil
}
