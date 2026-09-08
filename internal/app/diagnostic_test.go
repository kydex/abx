package app

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/kydex/abx/internal/bwrap"
	"github.com/kydex/abx/internal/sandbox"
)

func TestEscape(t *testing.T) {
	tests := []struct {
		name, input, want string
	}{
		{"printable", `quoted "value"`, `quoted "value"`},
		{"named controls", "\a\b\f\n\r\t\v", `\a\b\f\n\r\t\v`},
		{"ASCII control", "a\x1bb", `a\u001bb`},
		{"Unicode controls", "a\u202eb\u2028c\U000e0001d", `a\u202eb\u2028c\U000e0001d`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Escape(test.input); got != test.want {
				t.Fatalf("Escape(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func diagnosticPlan(t *testing.T, mode sandbox.Mode) sandbox.Plan {
	t.Helper()
	project := "/project"
	command := []string{"/home/agent/.local/bin/demo"}
	if mode == sandbox.Shell {
		project = ""
	}
	if mode != sandbox.Run {
		command = []string{"/bin/sh", "-l"}
	}
	plan, err := sandbox.Build(sandbox.Input{
		Home:    "/profile",
		Project: project,
		Mode:    mode,
		System:  []sandbox.Mount{{Kind: sandbox.ReadOnly, Source: "/usr", Target: "/usr"}},
		Command: command,
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestFormatPlanAppendsIsolationSummary(t *testing.T) {
	for _, mode := range []sandbox.Mode{sandbox.Run, sandbox.Shell, sandbox.Work} {
		plan := diagnosticPlan(t, mode)
		got := formatPlan(plan)
		detailEnd := strings.Index(got, "\nisolation:\n")
		if detailEnd < 0 {
			t.Fatalf("missing isolation summary: %q", got)
		}
		detail := got[:detailEnd+1]
		if !strings.HasPrefix(detail, "cwd: ") || !strings.Contains(detail, "\ncommand: ") || !strings.Contains(detail, "\nmount: ") {
			t.Fatalf("detailed plan prefix changed: %q", detail)
		}
		wantWritable := "writable:\n  /home/agent\n"
		if mode != sandbox.Shell {
			wantWritable += "  /workspace\n"
		}
		if !strings.Contains(got, wantWritable+"private:\n  /tmp\n  /var/tmp\n  /run\n  /proc\n  /dev\n") {
			t.Fatalf("unexpected mount summary: %q", got)
		}
		if mode == sandbox.Shell && strings.Contains(got[strings.Index(got, "\nisolation:\n"):], "  /workspace\n") {
			t.Fatalf("shell summary contains workspace: %q", got)
		}
	}
}

func TestIsolationSummaryMatchesBubblewrapNamespacePolicy(t *testing.T) {
	plan := diagnosticPlan(t, sandbox.Shell)
	f, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	args, _, err := bwrap.Translate(plan, func(string) (*os.File, error) { return f, nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--unshare-user", "--unshare-pid", "--unshare-ipc", "--unshare-uts"} {
		if !slices.Contains(args, flag) {
			t.Fatalf("summary claims private namespace without %s: %v", flag, args)
		}
	}
	if slices.Contains(args, "--unshare-net") {
		t.Fatalf("summary claims shared network but translation unshares it: %v", args)
	}
	for _, want := range []string{
		"user namespace: private",
		"pid namespace: private",
		"ipc namespace: private",
		"uts namespace: private",
		"network: shared with host",
	} {
		if !strings.Contains(isolationSummary, want) {
			t.Fatalf("missing isolation summary line %q", want)
		}
	}
}
