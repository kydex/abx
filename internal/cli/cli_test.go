package cli

import (
	"reflect"
	"testing"

	"github.com/kydex/abx/internal/profile"
)

func TestStructuredArgumentsAndNames(t *testing.T) {
	c, e := Parse([]string{"run", "shell", "--", "a b", "$(literal)", "", "--help"})
	if e != nil {
		t.Fatal(e)
	}
	if c.Profile != "shell" || !reflect.DeepEqual(c.Args, []string{"a b", "$(literal)", "", "--help"}) {
		t.Fatal(c)
	}
	for _, argv := range [][]string{{"shell", "run"}, {"work", "tools"}, {"inspect", "agent"}, {"verify", "agent"}, {"help"}, {"-h"}, {"--help"}, {"-v"}, {"--version"}, {"shell", "agent"}, {"verify", "agent"}} {
		if _, e := Parse(argv); e != nil {
			t.Fatal(e)
		}
	}
	for _, argv := range [][]string{{"agent"}, {"work"}, {"work", "../bad"}, {"work", "tools", "--"}, {"work", "tools", "arg"}, {"shell", "--create", "agent"}, {"shell", "agent", "--"}, {"run", "../bad"}, {"run", "agent", "arg"}, {"help", "extra"}, {"verify", "agent", "extra"}} {
		if _, e := Parse(argv); e == nil {
			t.Fatalf("accepted %q", argv)
		}
	}
}

func TestBareAndHelpShortcuts(t *testing.T) {
	for _, argv := range [][]string{nil, {}} {
		c, err := Parse(argv)
		if err != nil || c.Kind != Help {
			t.Fatalf("%q: %+v %v", argv, c, err)
		}
	}
	for _, argv := range [][]string{{"-v"}, {"--version"}, {"version"}} {
		c, err := Parse(argv)
		if err != nil || c.Kind != Version {
			t.Fatalf("%q: %+v %v", argv, c, err)
		}
	}
	// Agent arguments after "--" must never be read as ABX flags.
	c, err := Parse([]string{"run", "demo", "--", "--version", "-v", "--help"})
	if err != nil || c.Kind != Run || !reflect.DeepEqual(c.Args, []string{"--version", "-v", "--help"}) {
		t.Fatalf("run args after separator: %+v %v", c, err)
	}
	// A bare separator with nothing after it is accepted as a no-op.
	c, err = Parse([]string{"run", "demo", "--"})
	if err != nil || c.Kind != Run || c.Profile != "demo" || len(c.Args) != 0 {
		t.Fatalf("bare separator: %+v %v", c, err)
	}
	// Help and version are only commands in the first position. Anywhere else
	// they are arguments, and must not turn a real command into a silent no-op.
	for _, argv := range [][]string{
		{"profile", "create", "demo", "--version"},
		{"profile", "create", "demo", "-h"},
		{"run", "--version"},
		{"inspect", "--help"},
		{"version", "--version"},
		{"help", "-h"},
		{"profile", "list", "extra"},
	} {
		c, err := Parse(argv)
		if err == nil {
			t.Fatalf("accepted %q as %q with no error", argv, c.Kind)
		}
	}
}

func TestOptionLikeProfileNameGetsAHint(t *testing.T) {
	_, err := Parse([]string{"run", "--version"})
	if err == nil || err.Error() != `unknown option "--version" for run; run "abx --help"` {
		t.Fatalf("option-like profile name: %v", err)
	}
	_, err = Parse([]string{"profile", "create", "-x"})
	if err == nil || err.Error() != `unknown option "-x" for profile create; run "abx --help"` {
		t.Fatalf("option-like profile create name: %v", err)
	}
	_, err = Parse([]string{"profile", "show", "--version"})
	if err == nil || err.Error() != `unknown option "--version" for profile show; run "abx --help"` {
		t.Fatalf("option-like profile show name: %v", err)
	}
	_, err = Parse([]string{"profile", "--help"})
	if err == nil || err.Error() != `unknown option "--help" for profile; run "abx --help"` {
		t.Fatalf("option-like profile subcommand: %v", err)
	}
	_, err = Parse([]string{"-h", "extra"})
	if err == nil || err.Error() != "-h takes no arguments" {
		t.Fatalf("-h with an argument: %v", err)
	}
}

// A bare dash is not an option: the message names the missing profile instead
// of reciting option syntax.
func TestBareDashesAreNotOptions(t *testing.T) {
	for _, tc := range []struct {
		argv []string
		want string
	}{
		{[]string{"run", "-"}, `run requires a profile name; "-" is not one`},
		{[]string{"run", "--"}, `run requires a profile name; "--" is not one`},
		{[]string{"profile", "create", "-"}, `profile create requires a profile name; "-" is not one`},
		{[]string{"profile", "show", "--"}, `profile show requires a profile name; "--" is not one`},
	} {
		_, err := Parse(tc.argv)
		if err == nil || err.Error() != tc.want {
			t.Fatalf("%q: %v", tc.argv, err)
		}
	}
}

func TestProfileCommands(t *testing.T) {
	for _, tc := range []struct {
		args []string
		kind Kind
		name string
	}{
		{[]string{"profile", "create", "shell"}, ProfileCreate, "shell"},
		{[]string{"profile", "show", "demo"}, ProfileShow, "demo"},
		{[]string{"profile", "list"}, ProfileList, ""},
	} {
		c, err := Parse(tc.args)
		if err != nil || c.Kind != tc.kind || c.Profile != tc.name {
			t.Fatalf("%q: %+v %v", tc.args, c, err)
		}
	}
	for _, args := range [][]string{{"profile"}, {"profile", "remove", "demo"}, {"profile", "create"}, {"profile", "create", "../x"}, {"profile", "create", "demo", "--"}, {"profile", "show", "demo", "extra"}, {"profile", "list", "demo"}} {
		if _, err := Parse(args); err == nil {
			t.Fatalf("accepted %q", args)
		}
	}
}

func TestWorkCommand(t *testing.T) {
	c, err := Parse([]string{"work", "tools"})
	if err != nil || c.Kind != Work || c.Profile != "tools" || len(c.Args) != 0 {
		t.Fatalf("work: %+v %v", c, err)
	}
}

func TestRunArgumentsRequireSeparator(t *testing.T) {
	_, err := Parse([]string{"run", "demo", "argument"})
	if err == nil || err.Error() != "run arguments must follow --: abx run <profile> -- <argument> [additional arguments]" {
		t.Fatalf("error = %v", err)
	}
}

func FuzzParse(f *testing.F) {
	f.Add("run", "demo")
	f.Add("", "")
	f.Add("run", "--")
	f.Add("profile", "create")
	f.Fuzz(func(t *testing.T, a, b string) {
		for _, argv := range [][]string{{a}, {a, b}} {
			c, err := Parse(argv)
			if err != nil {
				continue
			}
			switch c.Kind {
			case Run, Shell, Work, Inspect, Verify, Help, Version,
				ProfileCreate, ProfileShow, ProfileList:
			default:
				t.Fatalf("unknown command kind %q from %q", c.Kind, argv)
			}
			if c.Profile != "" {
				if err := profile.ValidateName(c.Profile); err != nil {
					t.Fatalf("invalid profile %q from %q: %v", c.Profile, argv, err)
				}
			}
		}
	})
}
