package cli

import (
	"reflect"
	"testing"
)

func TestStructuredArgumentsAndNames(t *testing.T) {
	c, e := Parse([]string{"run", "shell", "--", "a b", "$(literal)", "", "--help"})
	if e != nil {
		t.Fatal(e)
	}
	if c.Profile != "shell" || !reflect.DeepEqual(c.Args, []string{"a b", "$(literal)", "", "--help"}) {
		t.Fatal(c)
	}
	for _, argv := range [][]string{{"shell", "run"}, {"work", "tools"}, {"inspect", "agent"}, {"verify", "agent"}, {"help"}, {"-h"}, {"version"}} {
		if _, e := Parse(argv); e != nil {
			t.Fatal(e)
		}
	}
	for _, argv := range [][]string{nil, {"agent"}, {"work"}, {"work", "../bad"}, {"work", "tools", "--"}, {"work", "tools", "arg"}, {"shell", "--create", "agent"}, {"shell", "agent", "--"}, {"run", "../bad"}, {"run", "agent", "arg"}, {"help", "extra"}, {"verify", "agent", "extra"}} {
		if _, e := Parse(argv); e == nil {
			t.Fatalf("accepted %q", argv)
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
