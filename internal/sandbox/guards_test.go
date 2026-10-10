package sandbox

import (
	"strings"
	"testing"
)

// The guards below had no test that could fail: removing the condition left the
// suite green. They are grouped by what they hold in place, not by file, because
// several of them are the same rule expressed twice — once while building the
// plan and once while validating it.

// Every plan has to reach validation, and validation is the only barrier left
// before the plan becomes Bubblewrap arguments. The allowlist is checked there
// and nowhere else: build only rejects malformed names, so a plan carrying a
// variable ABX never chose gets through unless Build really calls Validate.
func TestBuildRunsValidateAndRejectsNonAllowlistedVariable(t *testing.T) {
	for _, mode := range []Mode{Run, Shell, Work} {
		in := testInput(t, mode)
		in.Environment = append(append([]Entry{}, in.Environment...), Entry{Name: "EVIL", Value: "1"})
		if _, err := Build(in); err == nil || !strings.Contains(err.Error(), "not allowlisted") {
			t.Fatalf("mode %v accepted a non-allowlisted variable: %v", mode, err)
		}
	}
	// The verification plan has its own entry point and its own Validate call.
	probe := Input{
		Mode: Run, Home: "/profile", Project: "/project",
		Command:     []string{ProbePath, "__probe", "{}"},
		System:      []Mount{{Kind: ReadOnly, Source: "/usr", Target: "/usr"}},
		Environment: []Entry{{Name: "PWD", Value: "/workspace"}, {Name: "EVIL", Value: "1"}},
	}
	if _, err := BuildVerification(probe); err == nil || !strings.Contains(err.Error(), "not allowlisted") {
		t.Fatalf("verification plan accepted a non-allowlisted variable: %v", err)
	}
}

// Validate cross-checks the built mounts against the recorded expectations, so a
// plan edited after construction is rejected rather than translated.
func TestValidateRejectsPlanEditedAfterBuild(t *testing.T) {
	p, err := Build(testInput(t, Run))
	if err != nil {
		t.Fatal(err)
	}
	t.Run("working directory", func(t *testing.T) {
		q := p
		q.cwd = "/etc"
		if err := q.Validate(); err == nil || !strings.Contains(err.Error(), "invalid project plan expectation") {
			t.Fatalf("edited working directory accepted: %v", err)
		}
	})
	t.Run("non-allowlisted variable", func(t *testing.T) {
		q := p
		q.environment = append(append([]Entry{}, p.environment...), Entry{Name: "EVIL", Value: "1"})
		if err := q.Validate(); err == nil || !strings.Contains(err.Error(), "not allowlisted") {
			t.Fatalf("non-allowlisted variable accepted: %v", err)
		}
	})
	t.Run("mount out of order", func(t *testing.T) {
		q := p
		q.mounts = append([]Mount{}, p.mounts...)
		q.mounts[len(q.mounts)-1] = Mount{Kind: Writable, Source: "/elsewhere", Target: "/elsewhere"}
		if err := q.Validate(); err == nil {
			t.Fatal("plan with a rewritten mount accepted")
		}
	})
	t.Run("duplicate expected mount", func(t *testing.T) {
		q := p
		last := p.expected.system[len(p.expected.system)-1]
		q.expected.system = append(append([]Mount{}, p.expected.system...), last)
		q.mounts = append(append([]Mount{}, p.mounts...), last)
		if err := q.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate expected system mount") {
			t.Fatalf("duplicate expected mount accepted: %v", err)
		}
	})
	t.Run("size on expected mount", func(t *testing.T) {
		q := p
		q.expected.system = append([]Mount{}, p.expected.system...)
		q.mounts = append([]Mount{}, p.mounts...)
		for i := range q.expected.system {
			if q.expected.system[i].Target == "/usr" {
				q.expected.system[i].Size = 1 << 20
				q.mounts[len(baseMounts())+i].Size = 1 << 20
			}
		}
		if err := q.Validate(); err == nil || !strings.Contains(err.Error(), "invalid expected system mount") {
			t.Fatalf("size accepted on an expected non-tmpfs mount: %v", err)
		}
	})
}

// A sandbox with no /usr has nothing to run. The mount may come from anywhere in
// the system list; what matters is that /usr ends up in the plan.
func TestBuildRequiresUsr(t *testing.T) {
	in := testInput(t, Shell)
	in.System = []Mount{{Kind: Symlink, Source: "usr/bin", Target: "/bin"}}
	if _, err := Build(in); err == nil || !strings.Contains(err.Error(), "/usr is required") {
		t.Fatalf("plan without /usr accepted: %v", err)
	}
}

// shell is documented to omit the project. If the plan builder stops checking,
// a shell session would mount a project it was told not to.
func TestBuildRejectsProjectInShellMode(t *testing.T) {
	in := testInput(t, Shell)
	in.Project = "/host/project"
	if _, err := Build(in); err == nil || !strings.Contains(err.Error(), "project does not match session mode") {
		t.Fatalf("shell session accepted a project: %v", err)
	}
	in = testInput(t, Work)
	in.Project = ""
	if _, err := Build(in); err == nil || !strings.Contains(err.Error(), "project does not match session mode") {
		t.Fatalf("work session accepted an empty project: %v", err)
	}
}

func TestBuildRejectsMalformedSessionInputs(t *testing.T) {
	for name, mutate := range map[string]func(*Input){
		"home not clean":    func(in *Input) { in.Home = "/data/../data/profiles/demo" },
		"home relative":     func(in *Input) { in.Home = "relative/path" },
		"no command":        func(in *Input) { in.Command = nil },
		"command not clean": func(in *Input) { in.Command = []string{"/home/agent/../bin/demo"} },
		"skills not clean":  func(in *Input) { in.Skills = "/data/shared/../elsewhere" },
	} {
		t.Run(name, func(t *testing.T) {
			in := testInput(t, Shell)
			mutate(&in)
			if _, err := Build(in); err == nil {
				t.Fatalf("malformed session inputs accepted: %v", in)
			}
		})
	}
}

// The environment allowlist is the whole reason the payload cannot be handed a
// variable ABX did not choose. Duplicate names let the last value win over an
// earlier one, and a NUL truncates the value the agent actually receives.
func TestBuildRejectsMalformedEnvironment(t *testing.T) {
	for name, entry := range map[string]Entry{
		"bad name":     {Name: "1BAD", Value: "x"},
		"nul in value": {Name: "TERM", Value: "xterm\x00evil"},
		"empty name":   {Name: "", Value: "x"},
	} {
		t.Run(name, func(t *testing.T) {
			in := testInput(t, Shell)
			in.Environment = append(append([]Entry{}, in.Environment...), entry)
			if _, err := Build(in); err == nil || !strings.Contains(err.Error(), "invalid session environment") {
				t.Fatalf("malformed environment accepted: %v", err)
			}
		})
	}
	t.Run("duplicate name", func(t *testing.T) {
		in := testInput(t, Shell)
		in.Environment = append(append([]Entry{}, in.Environment...), Entry{Name: "TERM", Value: "second"})
		if _, err := Build(in); err == nil || !strings.Contains(err.Error(), "invalid session environment") {
			t.Fatalf("duplicate environment name accepted: %v", err)
		}
	})
}

// A size on anything but a tmpfs has nowhere to apply, and passing it through
// produces an argument Bubblewrap rejects at a much later point.
func TestBuildRejectsSizeOutsideTmpfs(t *testing.T) {
	in := testInput(t, Shell)
	in.System = append(in.System, Mount{Kind: ReadOnly, Source: "/etc/hosts", Target: "/etc/hosts", Size: 1 << 20})
	if _, err := Build(in); err == nil || !strings.Contains(err.Error(), "invalid system mount") {
		t.Fatalf("size accepted on a non-tmpfs mount: %v", err)
	}
}

func TestValidNameRejectsEmptyKey(t *testing.T) {
	if validName("") {
		t.Fatal("empty environment variable name accepted")
	}
	if !validName("TERM") {
		t.Fatal("a valid environment variable name was rejected")
	}
}
