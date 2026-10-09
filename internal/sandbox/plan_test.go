package sandbox

import (
	"reflect"
	"strings"
	"testing"
)

func testInput(t *testing.T, mode Mode) Input {
	t.Helper()
	cwd := "/workspace"
	project := "/host/project"
	cmd := []string{"/home/agent/bin/demo", "a b", "$(literal)"}
	if mode == Shell {
		cwd = "/home/agent"
		project = ""
	}
	if mode != Run {
		cmd = []string{"/usr/bin/bash", "-l"}
	}
	env, err := Session("tester", "/usr/bin/bash", cwd, []string{"LD_PRELOAD=bad", "SSH_AUTH_SOCK=/sock", "TERM=xterm"})
	if err != nil {
		t.Fatal(err)
	}
	return Input{Home: "/data/profiles/demo", Project: project, Skills: "/data/shared/agents/skills", Mode: mode, System: []Mount{{Kind: ReadOnly, Source: "/usr", Target: "/usr"}, {Kind: Symlink, Source: "usr/bin", Target: "/bin"}}, Environment: env, Command: cmd}
}

func mountIndex(t *testing.T, p Plan, kind Kind, target string) int {
	t.Helper()
	for i, m := range p.mounts {
		if m.Kind == kind && m.Target == target {
			return i
		}
	}
	t.Fatalf("missing mount kind=%d target=%q", kind, target)
	return -1
}

func TestShellAndAgentHaveIdenticalIsolation(t *testing.T) {
	run, e := Build(testInput(t, Run))
	if e != nil {
		t.Fatal(e)
	}
	shell, e := Build(testInput(t, Shell))
	if e != nil {
		t.Fatal(e)
	}
	mounts := run.Mounts()
	last := mounts[len(mounts)-1]
	if last != (Mount{Kind: Writable, Source: "/host/project", Target: "/workspace"}) {
		t.Fatalf("project mount: %#v", last)
	}
	if !reflect.DeepEqual(mounts[:len(mounts)-1], shell.Mounts()) {
		t.Fatal("shell has a different base isolation policy")
	}
	for _, p := range []Plan{run, shell} {
		writable := 0
		for _, m := range p.Mounts() {
			if m.Kind == Writable {
				writable++
			}
			if m.Target == "/home/agent/.agents/skills" && m.Kind != ReadOnly {
				t.Fatal("skills writable")
			}
		}
		want := 2
		if p.cwd == "/home/agent" {
			want = 1
		}
		if writable != want {
			t.Fatalf("writable=%d", writable)
		}
		env := toMap(p.Environment())
		for _, key := range []string{"LD_PRELOAD", "SSH_AUTH_SOCK"} {
			if _, ok := env[key]; ok {
				t.Fatal("host injection inherited")
			}
		}
		if env["HOME"] != "/home/agent" || env["PWD"] != p.cwd {
			t.Fatal("host home or incorrect cwd")
		}
	}
}

func TestIndependentPrivateMountExpectation(t *testing.T) {
	for _, mode := range []Mode{Run, Shell, Work} {
		p, e := Build(testInput(t, mode))
		if e != nil {
			t.Fatal(e)
		}
		expected := []Mount{{Kind: Tmpfs, Target: "/tmp", Size: tmpfsSize}, {Kind: Tmpfs, Target: "/var/tmp", Size: tmpfsSize}, {Kind: Tmpfs, Target: "/run", Size: tmpfsSize}, {Kind: Directory, Target: "/run/user", Mode: 0o700}, {Kind: Proc, Target: "/proc"}, {Kind: Dev, Target: "/dev"}}
		var actual []Mount
		for _, m := range p.Mounts() {
			if m.Kind == Tmpfs || m.Kind == Proc || m.Kind == Dev || m.Target == "/run/user" {
				actual = append(actual, m)
			}
		}
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("private views: %#v", actual)
		}
	}
}

func TestRejectSystemPolicyEscape(t *testing.T) {
	for _, m := range []Mount{{Kind: Writable, Source: "/usr", Target: "/usr"}, {Kind: ReadOnly, Source: "/", Target: "/"}, {Kind: ReadOnly, Source: "/run", Target: "/run"}, {Kind: ReadOnly, Source: "/etc", Target: "/etc"}, {Kind: Symlink, Source: "/host", Target: "/usr"}} {
		in := testInput(t, Shell)
		in.System = append(in.System, m)
		if _, e := Build(in); e == nil {
			t.Fatalf("accepted %#v", m)
		}
	}
	in := testInput(t, Shell)
	in.Project = "/project"
	if _, e := Build(in); e == nil {
		t.Fatal("shell accepted project")
	}
}

func TestPlanCopiesInputsAndOutputs(t *testing.T) {
	in := testInput(t, Run)
	p, e := Build(in)
	if e != nil {
		t.Fatal(e)
	}
	in.Command[0] = "/wrong"
	in.System[0].Source = "/wrong"
	in.Environment[0].Value = "wrong"
	p.Mounts()[0].Target = "/wrong"
	p.Command()[0] = "/wrong"
	p.Environment()[0].Value = "wrong"
	if p.Command()[0] != "/home/agent/bin/demo" || p.Mounts()[0].Target != "/usr" || p.Environment()[0].Value == "wrong" || p.expected.system[0].Source != "/usr" {
		t.Fatal("mutable plan")
	}
}

func TestEnvironmentLastAllowedValueWins(t *testing.T) {
	e, err := Session("u", "/bin/sh", "/home/agent", []string{"TERM=old", "TERM=new", "HOME=/host"})
	if err != nil {
		t.Fatal(err)
	}
	m := toMap(e)
	if m["TERM"] != "new" || m["HOME"] != "/home/agent" {
		t.Fatal(m)
	}
}

func TestVerificationAddsOnlyFixedReadOnlyProbe(t *testing.T) {
	input := Input{Mode: Run, Home: "/profile", Project: "/project", Command: []string{ProbePath, "__probe", "{}"}, System: []Mount{{Kind: ReadOnly, Source: "/usr", Target: "/usr"}}, Environment: []Entry{{Name: "PWD", Value: "/workspace"}}}
	normal, err := Build(input)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := BuildVerification(input)
	if err != nil {
		t.Fatal(err)
	}
	mounts := verified.Mounts()
	if !reflect.DeepEqual(normal.Mounts(), mounts[:len(mounts)-1]) {
		t.Fatal("verification changed run policy")
	}
	if got := mounts[len(mounts)-1]; got != (Mount{Kind: ReadOnly, Source: "/proc/self/exe", Target: "/abx-probe"}) {
		t.Fatal(got)
	}
	input.Mode = Shell
	input.Project = ""
	if _, err = BuildVerification(input); err == nil {
		t.Fatal("accepted shell probe plan")
	}
	input.Mode = Run
	input.Project = "/project"
	input.Command = []string{"/bin/sh", "-c", "anything"}
	if _, err = BuildVerification(input); err == nil {
		t.Fatal("accepted arbitrary probe command")
	}
}

func TestAlternativesOnlyReadOnlyInAllModes(t *testing.T) {
	for _, mode := range []Mode{Run, Shell, Work} {
		for _, kind := range []Kind{ReadOnly, Writable, Symlink} {
			in := testInput(t, mode)
			mount := Mount{Kind: kind, Source: "/etc/alternatives", Target: "/etc/alternatives"}
			in.System = append(in.System, mount)
			plan, err := Build(in)
			if kind != ReadOnly {
				if err == nil {
					t.Fatal("accepted unsafe alternatives mount", kind)
				}
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, m := range plan.Mounts() {
				if m == mount {
					found = true
				}
			}
			if !found {
				t.Fatal("missing alternatives")
			}
		}
	}
}

func TestValidateAcceptsMinimalOptionalPolicy(t *testing.T) {
	in := testInput(t, Shell)
	in.Skills = ""
	in.System = []Mount{{Kind: ReadOnly, Source: "/usr", Target: "/usr"}}
	p, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsPolicyMutations(t *testing.T) {
	insertMount := func(p *Plan, index int, m Mount) {
		p.mounts = append(p.mounts, Mount{})
		copy(p.mounts[index+1:], p.mounts[index:])
		p.mounts[index] = m
	}

	tests := []struct {
		name   string
		modes  []Mode
		mutate func(*testing.T, *Plan)
	}{
		{name: "relative command", mutate: func(_ *testing.T, p *Plan) { p.command[0] = "relative" }},
		{name: "NUL command argument", mutate: func(_ *testing.T, p *Plan) { p.command = append(p.command, "bad\x00arg") }},
		{name: "unexpected cwd", mutate: func(_ *testing.T, p *Plan) { p.cwd = "/tmp" }},
		{name: "duplicate environment", mutate: func(_ *testing.T, p *Plan) { p.environment = append(p.environment, p.environment[0]) }},
		{name: "invalid environment name", mutate: func(_ *testing.T, p *Plan) { p.environment[0].Name = "bad-name" }},
		{name: "NUL environment", mutate: func(_ *testing.T, p *Plan) { p.environment[0].Value = "bad\x00value" }},
		{name: "changed base directory", mutate: func(_ *testing.T, p *Plan) { p.mounts[0].Kind = Tmpfs }},
		{name: "writable usr", mutate: func(t *testing.T, p *Plan) {
			p.mounts[mountIndex(t, *p, ReadOnly, "/usr")].Kind = Writable
		}},
		{name: "relative usr source", mutate: func(t *testing.T, p *Plan) {
			p.mounts[mountIndex(t, *p, ReadOnly, "/usr")].Source = "relative"
		}},
		{name: "symlink usr", mutate: func(t *testing.T, p *Plan) {
			i := mountIndex(t, *p, ReadOnly, "/usr")
			p.mounts[i].Kind = Symlink
			p.mounts[i].Source = "usr"
		}},
		{name: "empty compatibility symlink", mutate: func(t *testing.T, p *Plan) {
			p.mounts[mountIndex(t, *p, Symlink, "/bin")].Source = ""
		}},
		{name: "missing system usr", mutate: func(t *testing.T, p *Plan) {
			p.mounts[mountIndex(t, *p, ReadOnly, "/usr")].Target = "/etc/hosts"
		}},
		{name: "duplicate system target", mutate: func(t *testing.T, p *Plan) {
			i := mountIndex(t, *p, ReadOnly, "/usr")
			insertMount(p, i+1, p.mounts[i])
		}},
		{name: "changed private tmp", mutate: func(t *testing.T, p *Plan) {
			p.mounts[mountIndex(t, *p, Tmpfs, "/tmp")].Target = "/tmp-other"
		}},
		{name: "tmpfs without size limit", mutate: func(t *testing.T, p *Plan) {
			p.mounts[mountIndex(t, *p, Tmpfs, "/tmp")].Size = 0
		}},
		{name: "run user permissions", mutate: func(t *testing.T, p *Plan) {
			p.mounts[mountIndex(t, *p, Directory, "/run/user")].Mode = 0o755
		}},
		{name: "readonly home", mutate: func(t *testing.T, p *Plan) {
			p.mounts[mountIndex(t, *p, Writable, "/home/agent")].Kind = ReadOnly
		}},
		{name: "relative home source", mutate: func(t *testing.T, p *Plan) {
			p.mounts[mountIndex(t, *p, Writable, "/home/agent")].Source = "relative"
		}},
		{name: "writable skills", mutate: func(t *testing.T, p *Plan) {
			p.mounts[mountIndex(t, *p, ReadOnly, "/home/agent/.agents/skills")].Kind = Writable
		}},
		{name: "relative skills source", mutate: func(t *testing.T, p *Plan) {
			p.mounts[mountIndex(t, *p, ReadOnly, "/home/agent/.agents/skills")].Source = "relative"
		}},
		{name: "missing workspace", modes: []Mode{Run, Work}, mutate: func(t *testing.T, p *Plan) {
			i := mountIndex(t, *p, Writable, "/workspace")
			p.mounts = append(p.mounts[:i], p.mounts[i+1:]...)
		}},
		{name: "relative workspace source", modes: []Mode{Run, Work}, mutate: func(t *testing.T, p *Plan) {
			p.mounts[mountIndex(t, *p, Writable, "/workspace")].Source = "relative"
		}},
		{name: "unexpected extra writable mount", mutate: func(_ *testing.T, p *Plan) {
			p.mounts = append(p.mounts, Mount{Kind: Writable, Source: "/host/etc", Target: "/etc"})
		}},
		{name: "shell workspace", modes: []Mode{Shell}, mutate: func(_ *testing.T, p *Plan) {
			p.mounts = append(p.mounts, Mount{Kind: Writable, Source: "/project", Target: "/workspace"})
		}},
	}
	for _, tc := range tests {
		modes := tc.modes
		if len(modes) == 0 {
			modes = []Mode{Run, Shell, Work}
		}
		for _, mode := range modes {
			name := map[Mode]string{Run: "run", Shell: "shell", Work: "work"}[mode]
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				p, err := Build(testInput(t, mode))
				if err != nil {
					t.Fatal(err)
				}
				tc.mutate(t, &p)
				if err := p.Validate(); err == nil {
					t.Fatal("accepted mutated plan")
				}
			})
		}
	}
}

func TestValidateRejectsEnvironmentPolicyMutations(t *testing.T) {
	tests := []struct {
		name   string
		want   string
		mutate func(*Plan)
	}{
		{name: "LD_PRELOAD environment", want: "not allowlisted", mutate: func(p *Plan) {
			p.environment = append(p.environment, Entry{Name: "LD_PRELOAD", Value: "/tmp/x.so"})
		}},
		{name: "PWD mismatch", want: "does not match working directory", mutate: func(p *Plan) {
			for i := range p.environment {
				if p.environment[i].Name == "PWD" {
					p.environment[i].Value = "/elsewhere"
				}
			}
		}},
		{name: "missing PWD", want: "exactly once", mutate: func(p *Plan) {
			kept := make([]Entry, 0, len(p.environment))
			for _, e := range p.environment {
				if e.Name != "PWD" {
					kept = append(kept, e)
				}
			}
			p.environment = kept
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := Build(testInput(t, Run))
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(&p)
			if err := p.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestSystemMountSourceMustMatchTarget(t *testing.T) {
	usr := Mount{Kind: ReadOnly, Source: "/usr", Target: "/usr"}
	for _, m := range []Mount{
		{Kind: ReadOnly, Source: "/etc", Target: "/etc/ssl"},
		{Kind: ReadOnly, Source: "/home/u/.ssh", Target: "/etc/pki"},
		{Kind: ReadOnly, Source: "/etc/hosts", Target: "/etc/hosts", Size: 1},
	} {
		in := testInput(t, Run)
		in.System = []Mount{usr, m}
		if _, err := Build(in); err == nil {
			t.Fatalf("accepted system mount %+v", m)
		}
	}
}

func TestValidateRejectsExpectedMutations(t *testing.T) {
	usrExpected := func(t *testing.T, p *Plan) int {
		t.Helper()
		for i, m := range p.expected.system {
			if m.Target == "/usr" {
				return i
			}
		}
		t.Fatal("no expected /usr")
		return -1
	}
	tests := []struct {
		name   string
		mode   Mode
		want   string
		mutate func(*testing.T, *Plan)
	}{
		{name: "relative home source", want: "invalid plan expected source", mutate: func(_ *testing.T, p *Plan) { p.expected.homeSource = "relative" }},
		{name: "relative skills source", want: "invalid plan expected source", mutate: func(_ *testing.T, p *Plan) { p.expected.skillsSource = "relative" }},
		{name: "unknown kind", want: "invalid plan kind", mutate: func(_ *testing.T, p *Plan) { p.expected.kind = planKind(0) }},
		{name: "empty command", want: "invalid plan command", mutate: func(_ *testing.T, p *Plan) { p.command = nil }},
		{name: "writable expected usr", want: "invalid expected system mount", mutate: func(t *testing.T, p *Plan) { p.expected.system[usrExpected(t, p)].Kind = Writable }},
		{name: "usr with mode", want: "invalid expected system mount", mutate: func(t *testing.T, p *Plan) { p.expected.system[usrExpected(t, p)].Mode = 0o755 }},
		{name: "missing expected usr", want: "expected system /usr is required", mutate: func(t *testing.T, p *Plan) {
			i := usrExpected(t, p)
			p.expected.system = append(p.expected.system[:i], p.expected.system[i+1:]...)
		}},
		{name: "reordered system mount", want: "invalid plan system mount", mutate: func(_ *testing.T, p *Plan) { p.mounts[len(baseMounts())].Target = "/etc/hosts" }},
		{name: "shell workspace mount", mode: Shell, want: "shell plan contains workspace", mutate: func(_ *testing.T, p *Plan) {
			p.mounts = append(p.mounts, Mount{Kind: Writable, Source: "/project", Target: "/workspace"})
		}},
	}
	for _, tc := range tests {
		mode := tc.mode
		if mode == 0 {
			mode = Run
		}
		t.Run(tc.name, func(t *testing.T) {
			p, err := Build(testInput(t, mode))
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(t, &p)
			err = p.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestAllowedSystemTargetsPinned(t *testing.T) {
	// Independent literals: changing allowedSystemTarget requires updating
	// this list too, in both directions.
	for _, target := range []string{"/usr", "/bin", "/sbin", "/lib", "/lib64", "/etc/hosts", "/etc/resolv.conf", "/etc/nsswitch.conf", "/etc/gai.conf", "/etc/passwd", "/etc/group", "/etc/ssl", "/etc/ca-certificates", "/etc/pki", "/etc/ld.so.cache", "/etc/ld.so.conf", "/etc/ld.so.conf.d", "/etc/localtime", "/etc/timezone", "/etc/os-release", "/etc/shells", "/etc/alternatives"} {
		if !allowedSystemTarget(target) {
			t.Errorf("pinned target %q rejected", target)
		}
	}
	for _, target := range []string{"", "/", "/etc/shadow", "/etc/gshadow", "/etc/sudoers", "/etc/passwd.old", "/etc", "/root", "/home/agent"} {
		if allowedSystemTarget(target) {
			t.Errorf("non-policy target %q accepted", target)
		}
	}
}

func TestValidateRejectsRoleSourceAndKindMutations(t *testing.T) {
	tests := []struct {
		name   string
		modes  []Mode
		mutate func(*testing.T, *Plan)
	}{
		{name: "home source substitution", mutate: func(t *testing.T, p *Plan) {
			p.mounts[mountIndex(t, *p, Writable, "/home/agent")].Source = "/usr"
		}},
		{name: "workspace source substitution", modes: []Mode{Run, Work}, mutate: func(t *testing.T, p *Plan) {
			p.mounts[mountIndex(t, *p, Writable, "/workspace")].Source = "/usr"
		}},
		{name: "skills source substitution", mutate: func(t *testing.T, p *Plan) {
			p.mounts[mountIndex(t, *p, ReadOnly, "/home/agent/.agents/skills")].Source = "/usr"
		}},
		{name: "home expectation substitution", mutate: func(_ *testing.T, p *Plan) { p.expected.homeSource = "/usr" }},
		{name: "project expectation substitution", mutate: func(_ *testing.T, p *Plan) { p.expected.projectSource = "/usr" }},
		{name: "skills expectation substitution", mutate: func(_ *testing.T, p *Plan) { p.expected.skillsSource = "/usr" }},
		{name: "system source substitution", mutate: func(t *testing.T, p *Plan) {
			p.mounts[mountIndex(t, *p, ReadOnly, "/usr")].Source = "/opt"
		}},
		{name: "system expectation substitution", mutate: func(_ *testing.T, p *Plan) { p.expected.system[0].Source = "/opt" }},
		{name: "system expectation reordered", mutate: func(_ *testing.T, p *Plan) {
			p.expected.system[0], p.expected.system[1] = p.expected.system[1], p.expected.system[0]
		}},
		{name: "project plan relabeled shell", modes: []Mode{Run, Work}, mutate: func(_ *testing.T, p *Plan) { p.expected.kind = planShell }},
		{name: "shell relabeled run", modes: []Mode{Shell}, mutate: func(_ *testing.T, p *Plan) { p.expected.kind = planRun }},
		{name: "unknown kind", mutate: func(_ *testing.T, p *Plan) { p.expected.kind = 0 }},
	}

	for _, tc := range tests {
		modes := tc.modes
		if len(modes) == 0 {
			modes = []Mode{Run, Shell, Work}
		}
		for _, mode := range modes {
			name := map[Mode]string{Run: "run", Shell: "shell", Work: "work"}[mode]
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				p, err := Build(testInput(t, mode))
				if err != nil {
					t.Fatal(err)
				}
				tc.mutate(t, &p)
				if err := p.Validate(); err == nil {
					t.Fatal("accepted mutated plan")
				}
			})
		}
	}
}

func TestValidateKindPreservesBuildSemantics(t *testing.T) {
	input := Input{Mode: Run, Home: "/profile", Project: "/project", Command: []string{ProbePath, "__probe", "{}"}, System: []Mount{{Kind: ReadOnly, Source: "/usr", Target: "/usr"}}, Environment: []Entry{{Name: "PWD", Value: "/workspace"}}}
	normal, err := Build(input)
	if err != nil {
		t.Fatal(err)
	}
	if normal.expected.kind != planRun {
		t.Fatalf("normal kind = %d", normal.expected.kind)
	}
	if err := normal.Validate(); err != nil {
		t.Fatal(err)
	}

	verified, err := BuildVerification(input)
	if err != nil {
		t.Fatal(err)
	}
	if verified.expected.kind != planVerify {
		t.Fatalf("verification kind = %d", verified.expected.kind)
	}
	if err := verified.Validate(); err != nil {
		t.Fatal(err)
	}

	shell, err := Build(testInput(t, Shell))
	if err != nil {
		t.Fatal(err)
	}
	if shell.expected.kind != planShell {
		t.Fatalf("shell kind = %d", shell.expected.kind)
	}
}

func TestValidateVerificationProbePolicy(t *testing.T) {
	input := Input{Mode: Run, Home: "/profile", Project: "/project", Command: []string{ProbePath, "__probe", "{}"}, System: []Mount{{Kind: ReadOnly, Source: "/usr", Target: "/usr"}}, Environment: []Entry{{Name: "PWD", Value: "/workspace"}}}
	verified, err := BuildVerification(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := verified.Validate(); err != nil {
		t.Fatal(err)
	}
	relabeled := verified
	relabeled.expected.kind = planRun
	if err := relabeled.Validate(); err == nil {
		t.Fatal("accepted verification plan relabeled as run")
	}

	for _, mutate := range []func(*Plan){
		func(p *Plan) { p.mounts[len(p.mounts)-1].Source = "/tmp/probe" },
		func(p *Plan) { p.mounts[len(p.mounts)-1].Kind = Writable },
		func(p *Plan) { p.command[1] = "other" },
	} {
		p, err := BuildVerification(input)
		if err != nil {
			t.Fatal(err)
		}
		mutate(&p)
		if err := p.Validate(); err == nil {
			t.Fatal("accepted invalid verification probe")
		}
	}

	normal, err := Build(testInput(t, Run))
	if err != nil {
		t.Fatal(err)
	}
	normal.mounts = append(normal.mounts, Mount{Kind: ReadOnly, Source: "/proc/self/exe", Target: ProbePath})
	if err := normal.Validate(); err == nil {
		t.Fatal("accepted probe mount for normal command")
	}
}

func TestWorkSharesRunPolicyAndShellCommand(t *testing.T) {
	run, err := Build(testInput(t, Run))
	if err != nil {
		t.Fatal(err)
	}
	work, err := Build(testInput(t, Work))
	if err != nil {
		t.Fatal(err)
	}
	if work.expected.kind != planWork || work.WorkingDirectory() != "/workspace" ||
		!reflect.DeepEqual(work.Mounts(), run.Mounts()) ||
		!reflect.DeepEqual(work.Environment(), run.Environment()) ||
		!reflect.DeepEqual(work.Command(), testInput(t, Shell).Command) {
		t.Fatal("work must use run visibility/environment and shell argv")
	}
}

func TestWorkRejectsPolicyMutations(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Plan)
	}{
		{"missing workspace", func(p *Plan) { p.mounts = p.mounts[:len(p.mounts)-1] }},
		{"read-only workspace", func(p *Plan) { p.mounts[len(p.mounts)-1].Kind = ReadOnly }},
		{"project substitution", func(p *Plan) { p.mounts[len(p.mounts)-1].Source = "/other" }},
		{"wrong cwd", func(p *Plan) { p.cwd = "/home/agent" }},
		{"shell kind", func(p *Plan) { p.expected.kind = planShell }},
		{"home substitution", func(p *Plan) { p.mounts[mountIndex(t, *p, Writable, "/home/agent")].Source = "/other" }},
		{"writable skills", func(p *Plan) { p.mounts[mountIndex(t, *p, ReadOnly, "/home/agent/.agents/skills")].Kind = Writable }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := Build(testInput(t, Work))
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(&p)
			if err := p.Validate(); err == nil {
				t.Fatal("accepted mutated work plan")
			}
		})
	}
	in := testInput(t, Work)
	in.Project = ""
	if _, err := Build(in); err == nil {
		t.Fatal("accepted work without project")
	}
	in = testInput(t, Work)
	in.Mode = 0
	if _, err := Build(in); err == nil {
		t.Fatal("accepted unspecified session mode")
	}
	in = testInput(t, Work)
	in.Command = []string{ProbePath, "__probe", "{}"}
	if _, err := BuildVerification(in); err == nil {
		t.Fatal("accepted work as verification input")
	}
}
