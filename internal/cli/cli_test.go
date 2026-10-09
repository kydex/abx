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
	for _, argv := range [][]string{{"shell", "run"}, {"work", "tools"}, {"inspect", "agent"}, {"verify", "agent"}, {"help"}, {"-h"}, {"--help"}, {"-v"}, {"--version"}, {"shell", "agent", "--help"}, {"verify", "agent", "--help"}, {"run", "--help"}} {
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
	for _, argv := range [][]string{{"-v"}, {"--version"}, {"version", "--version"}, {"version", "-v"}, {"help", "--version"}} {
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
