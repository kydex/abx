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
		if err := q.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate system mount") {
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
		if err := q.Validate(); err == nil || !strings.Contains(err.Error(), "invalid system mount") {
			t.Fatalf("size accepted on an expected non-tmpfs mount: %v", err)
		}
	})
	t.Run("relative system source", func(t *testing.T) {
		q := p
		q.expected.system = append([]Mount{}, p.expected.system...)
		q.mounts = append([]Mount{}, p.mounts...)
		q.expected.system[0].Source = "usr"
		q.mounts[len(baseMounts())].Source = "usr"
		if err := q.Validate(); err == nil || !strings.Contains(err.Error(), "invalid system source") {
			t.Fatalf("relative system source accepted: %v", err)
		}
	})
	t.Run("empty compatibility link source", func(t *testing.T) {
		q := p
		q.expected.system = append([]Mount{}, p.expected.system...)
		q.mounts = append([]Mount{}, p.mounts...)
		q.expected.system[1].Source = ""
		q.mounts[len(baseMounts())+1].Source = ""
		if err := q.Validate(); err == nil || !strings.Contains(err.Error(), "invalid system source") {
			t.Fatalf("empty compatibility link source accepted: %v", err)
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

// The three guards below are the first things build does, before anything is
// recorded in the plan. Nothing downstream re-checks them: Validate compares
// the built plan against expectations, and an empty home or a relative command
// recorded faithfully as an expectation passes every later comparison.

func TestBuildRejectsUnusableHomeAndCommand(t *testing.T) {
	for _, tc := range []struct {
		name  string
		apply func(*Input)
	}{
		{"empty home", func(in *Input) { in.Home = "" }},
		{"relative home", func(in *Input) { in.Home = "data/profiles/demo" }},
		{"home needs cleaning", func(in *Input) { in.Home = "/data/profiles/../profiles/demo" }},
		{"no command", func(in *Input) { in.Command = nil }},
		{"empty command", func(in *Input) { in.Command = []string{} }},
		{"relative command", func(in *Input) { in.Command = []string{"bin/demo"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := testInput(t, Run)
			tc.apply(&in)
			if _, err := Build(in); err == nil || !strings.Contains(err.Error(), "invalid session inputs") {
				t.Fatalf("accepted: %v", err)
			}
		})
	}
}

// Skills is optional but not unchecked: when it is named at all it has to be an
// absolute clean path, because it becomes a read-only mount source.
func TestBuildRejectsUnusableSkillsSource(t *testing.T) {
	for _, skills := range []string{"skills", "/skills/../elsewhere", "/skills\x00/x"} {
		in := testInput(t, Run)
		in.Skills = skills
		if _, err := Build(in); err == nil || !strings.Contains(err.Error(), "invalid skills source") {
			t.Fatalf("accepted %q: %v", skills, err)
		}
	}
	// Absent is a legitimate value, not a rejected one.
	in := testInput(t, Run)
	in.Skills = ""
	if _, err := Build(in); err != nil {
		t.Fatalf("no skills source rejected: %v", err)
	}
}

// Without /usr the sandbox has no userspace: no shell, no loader, nothing to run.
// Every other system entry is optional, so this is the one that cannot go missing.
//
// One shared rule set holds the requirement, and both entry points run it:
// build refuses before a plan with no /usr can exist, and Validate runs the
// same check before translation. Asserting both keeps either caller from
// dropping the call.
func TestBuildRequiresUsrInSystemLayout(t *testing.T) {
	in := testInput(t, Run)
	in.System = []Mount{{Kind: Symlink, Source: "usr/bin", Target: "/bin"}}
	if _, err := build(in); err == nil || !strings.Contains(err.Error(), "system /usr is required") {
		t.Fatalf("built a plan without /usr: %v", err)
	}
	if _, err := Build(in); err == nil || !strings.Contains(err.Error(), "system /usr is required") {
		t.Fatalf("accepted a system layout without /usr: %v", err)
	}
}

// The same rules must hold when build runs alone, before Validate sees the
// plan: a compatibility link that would resolve outside the mounted tree has
// to stop the construction, not only the later barrier.
func TestBuildAloneRefusesEscapingCompatibilityLink(t *testing.T) {
	in := testInput(t, Run)
	for i := range in.System {
		if in.System[i].Target == "/bin" {
			in.System[i].Source = "../../home/kdx/.ssh"
		}
	}
	if _, err := build(in); err == nil || !strings.Contains(err.Error(), "not a relative path inside /usr") {
		t.Fatalf("build let an escaping compatibility link through: %v", err)
	}
}

// The same rules must hold when build runs alone: a system source that is not
// a clean absolute path has to stop the construction, not only the later
// barrier.
func TestBuildAloneRefusesInvalidSystemSource(t *testing.T) {
	in := testInput(t, Run)
	in.System[0].Source = "usr"
	if _, err := build(in); err == nil || !strings.Contains(err.Error(), "invalid system source") {
		t.Fatalf("build accepted a relative system source: %v", err)
	}
}

// The compatibility link text is read from the host, not written here, and the
// sandbox mounts only /usr. The link sits at the root of the sandbox tree, so
// its text resolves from "/", not from the mount point: "usr/bin/../lib" is
// /usr/lib, while "usr/bin/../../etc" is /etc. A link pointing anywhere else
// resolves to a directory the sandbox creates empty, so the agent would find
// no binaries and nothing would say why.
func TestCompatibilityLinkMustStayInUsr(t *testing.T) {
	for _, src := range []string{
		"usr/bin", "usr/sbin", "usr/lib", "usr/lib64",
		"usr/bin/../lib", "./usr/bin",
	} {
		if !compatibilitySourceKeptInUsr(src) {
			t.Errorf("rejected a link that stays in /usr: %q", src)
		}
	}
	for _, src := range []string{
		"/etc", "/bin", "/usr/bin", "../etc", "../../home/kdx/.ssh", "usr/../../etc",
		"/", ".", "", "usr/../../../..",
		"usr/bin/../../etc", "usr/a/b/../../../etc",
		"usr/lib/x86_64-linux-gnu/../../../etc", "-foo", "usr",
	} {
		if compatibilitySourceKeptInUsr(src) {
			t.Errorf("accepted a link that leaves /usr: %q", src)
		}
	}
}

func TestBuildRejectsCompatibilityLinkEscapingUsr(t *testing.T) {
	in := testInput(t, Run)
	for i := range in.System {
		if in.System[i].Target == "/bin" {
			in.System[i].Source = "../../home/kdx/.ssh"
		}
	}
	if _, err := Build(in); err == nil || !strings.Contains(err.Error(), "not a relative path inside /usr") {
		t.Fatalf("accepted a compatibility link outside /usr: %v", err)
	}
	// The same link reaching Validate alone must be refused there too: Validate
	// is the barrier that stands between a built plan and Bubblewrap arguments.
	p, err := build(testInput(t, Run))
	if err != nil {
		t.Fatal(err)
	}
	// The first mounts are the base directories, so look the entry up by target rather than index.
	p.expected.system[1].Source = "/etc"
	for i := range p.mounts {
		if p.mounts[i].Target == "/bin" {
			p.mounts[i].Source = "/etc"
		}
	}
	if err = p.Validate(); err == nil || !strings.Contains(err.Error(), "not a relative path inside /usr") {
		t.Fatalf("Validate accepted a compatibility link outside /usr: %v", err)
	}
}

// Build refuses a command argument carrying NUL on its own, before Validate
// ever sees the plan. Validate refuses it too, with a different message, so a
// test that only expects any error proves nothing about this layer: disabling
// the check here leaves the refusal standing and the test green. The mutation
// sweep reported it uncovered for exactly that reason. Asserting the message
// this layer produces is what pins it.
//
// The case in the Validate table cannot do this: it edits a plan after
// construction, so it exercises only the second check.
func TestBuildRejectsNULInCommandArgument(t *testing.T) {
	for _, mode := range []Mode{Run, Shell, Work} {
		in := testInput(t, mode)
		in.Command = append(append([]string{}, in.Command...), "bad\x00arg")
		_, err := Build(in)
		if err == nil {
			t.Fatalf("mode %v accepted a NUL in a command argument", mode)
		}
		// "plan command argument contains NUL" is Validate's wording; this layer
		// says "command argument contains NUL" and nothing more.
		if got := err.Error(); got != "command argument contains NUL" {
			t.Fatalf("mode %v: NUL refused, but not by this check: %q", mode, got)
		}
	}
	// The same argument without NUL must still build, so the refusal above comes
	// from the byte and not from the argument count.
	in := testInput(t, Run)
	in.Command = append(append([]string{}, in.Command...), "ordinary-arg")
	if _, err := Build(in); err != nil {
		t.Fatalf("ordinary extra argument refused: %v", err)
	}
}
