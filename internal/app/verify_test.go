package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kydex/abx/internal/bwrap"
	"github.com/kydex/abx/internal/cli"
	"github.com/kydex/abx/internal/host"
	"github.com/kydex/abx/internal/probe"
	"github.com/kydex/abx/internal/sandbox"
)

func TestVerificationFilesCleanUpAfterSuccessOrLaterFailure(t *testing.T) {
	home := t.TempDir()
	for range 2 {
		files := &probeFiles{}
		sentinel, err := files.create(home)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(filepath.Base(sentinel), ".abx-verify-") {
			t.Fatalf("sentinel %q left the reserved probe prefix", sentinel)
		}
		if err = files.Close(); err != nil {
			t.Fatal(err)
		}
		if err = files.Close(); err != nil {
			t.Fatal(err)
		}
		if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
			t.Fatal("probe residue", err)
		}
	}
	files := &probeFiles{}
	if _, err := files.create(filepath.Join(home, "missing")); err == nil {
		t.Fatal("accepted a nonexistent home")
	}
	if err := files.Close(); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
		t.Fatal("sentinel left behind by a failed create", err)
	}
}

func TestVerificationCleanupDoesNotDeleteReplacement(t *testing.T) {
	home := t.TempDir()
	files := &probeFiles{}
	sentinel, err := files.create(home)
	if err != nil {
		t.Fatal(err)
	}
	// Retain the old inode so this test doesn't depend on inode reuse timing.
	if err = os.Rename(sentinel, sentinel+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = files.Close(); err == nil || !strings.Contains(err.Error(), "replaced") {
		t.Fatal("replacement cleanup was silent", err)
	}
	raw, err := os.ReadFile(sentinel)
	if err != nil || string(raw) != "keep" {
		t.Fatal("replacement removed", err)
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) == 0 {
		t.Fatal("other cleanup abandoned", err)
	}
}

func TestObserveHostCollectsSandboxPayload(t *testing.T) {
	home := t.TempDir()
	profile := t.TempDir()
	project := t.TempDir()
	input := sandbox.Input{
		Home:        profile,
		Project:     project,
		Mode:        sandbox.Run,
		System:      []sandbox.Mount{{Kind: sandbox.ReadOnly, Source: "/sys", Target: "/usr"}},
		Environment: []sandbox.Entry{{Name: "A", Value: "1"}},
		Command:     []string{sandbox.ProbePath, "__probe", "{}"},
	}
	got, err := observeHost(home, input)
	if err != nil {
		t.Fatal(err)
	}
	if got.Home != home {
		t.Fatalf("home = %q", got.Home)
	}
	for _, p := range []string{home, "/tmp", "/var/tmp", "/run", "/dev"} {
		if _, ok := got.Objects[p]; !ok {
			t.Fatalf("missing identity %q", p)
		}
	}
	for _, name := range []string{"user", "mnt", "pid", "ipc", "uts"} {
		if got.Namespaces[name] == "" {
			t.Fatalf("missing namespace %q", name)
		}
	}
	if len(got.Environment) != 1 || got.Environment[0] != "A=1" {
		t.Fatalf("environment = %q", got.Environment)
	}
	for _, want := range []string{"/usr", sandbox.ProbePath} {
		found := false
		for _, target := range got.ReadOnly {
			found = found || target == want
		}
		if !found {
			t.Fatalf("read-only lacks %q: %q", want, got.ReadOnly)
		}
	}
	if got.Skills {
		t.Fatal("skills reported without a shared source")
	}
}

func TestVerifySuccessReportsCleanup(t *testing.T) {
	var out strings.Builder
	if err := verifySuccess(&out); err != nil || out.String() != "Verification passed; temporary probe files removed.\n" {
		t.Fatalf("success report = %q, %v", out.String(), err)
	}
}

// A write failure after the check passed must say the check passed; the raw
// write error alone reads as if the verification itself had failed.
func TestVerifySuccessNamesWriteFailure(t *testing.T) {
	err := verifySuccess(brokenWriter{})
	if err == nil || !strings.Contains(err.Error(), "verification passed, but its report could not be written") {
		t.Fatalf("write failure = %v", err)
	}
}

func TestReportVerificationOutcome(t *testing.T) {
	for _, tc := range []struct {
		name   string
		kind   cli.Kind
		status int
		want   string
	}{
		{"verify success prints", cli.Verify, 0, "Verification passed; temporary probe files removed.\n"},
		{"verify failure stays silent", cli.Verify, 1, ""},
		{"run success stays silent", cli.Run, 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out strings.Builder
			if err := reportVerificationOutcome(tc.kind, tc.status, &out); err != nil {
				t.Fatal(err)
			}
			if out.String() != tc.want {
				t.Fatalf("output = %q, want %q", out.String(), tc.want)
			}
		})
	}
}

func TestVerifyInputRefusesOversizedPayload(t *testing.T) {
	raw, err := verifyInput(probe.Expected{Home: "/home/agent"})
	if err != nil || len(raw) == 0 {
		t.Fatalf("small input = %d bytes, %v", len(raw), err)
	}
	big := probe.Expected{Environment: []string{"LC_A=" + strings.Repeat("a", probe.MaxInputBytes)}}
	if _, err := verifyInput(big); err == nil || !strings.Contains(err.Error(), "byte probe limit") {
		t.Fatalf("oversized input = %v", err)
	}
}

// The per-value bound does not bound the sum, so a valid environment whose
// total payload crosses the probe limit has to be refused before execve
// turns it into a raw E2BIG or a late probe refusal.
func TestVerifyRefusesOversizedEnvironmentBeforeExecute(t *testing.T) {
	in := inputs(t, host.Run)
	home := t.TempDir()
	env := []string{
		"LC_A=" + strings.Repeat("a", 40000),
		"LC_B=" + strings.Repeat("b", 40000),
	}
	mustNotExecute := func(context.Context, *bwrap.Tool, sandbox.Plan, func(string) (*os.File, error), bwrap.Stdio) (int, error) {
		t.Error("execute ran for oversized verification input")
		return 0, nil
	}
	status, err := runVerify(context.Background(), in, home, env, bwrap.Stdio{Out: io.Discard, Err: io.Discard}, openStubTool(t), mustNotExecute)
	if status != 1 || err == nil || !strings.Contains(err.Error(), "byte probe limit") {
		t.Fatalf("oversized environment = status %d, %v", status, err)
	}
	entries, readErr := os.ReadDir(home)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("probe residue after refusal: %v, %v", entries, readErr)
	}
}

// The deadline can expire while a host-side observation is running; those
// phases are not interruptible, so the run has to report the timeout the
// guide names instead of the raw context error from inside execute.
func TestVerifyExpiredDeadlineReportsCancellation(t *testing.T) {
	in := inputs(t, host.Run)
	home := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	mustNotExecute := func(context.Context, *bwrap.Tool, sandbox.Plan, func(string) (*os.File, error), bwrap.Stdio) (int, error) {
		t.Error("execute ran after the deadline expired")
		return 0, nil
	}
	status, err := runVerify(ctx, in, home, nil, bwrap.Stdio{Out: io.Discard, Err: io.Discard}, openStubTool(t), mustNotExecute)
	if status != 1 || err == nil || !strings.Contains(err.Error(), "execution cancelled") {
		t.Fatalf("expired deadline = status %d, %v", status, err)
	}
}

func TestVerifyToolFailureDoesNotCreateProbeFiles(t *testing.T) {
	in := inputs(t, host.Run)
	home := t.TempDir()
	want := errors.New("tool not available")
	status, err := runVerify(context.Background(), in, home, nil, bwrap.Stdio{Out: io.Discard, Err: io.Discard}, func(context.Context) (*bwrap.Tool, error) { return nil, want }, func(context.Context, *bwrap.Tool, sandbox.Plan, func(string) (*os.File, error), bwrap.Stdio) (int, error) {
		t.Error("execute ran after the tool failure")
		return 0, nil
	})
	if status != 1 || !errors.Is(err, want) {
		t.Fatal(status, err)
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatal("sentinel created before tool check", err)
	}
	// Verification must not leave anything behind in the user's project either.
	entries, err = os.ReadDir(in.Project())
	if err != nil || len(entries) != 0 {
		t.Fatal("project touched before tool check", err)
	}
	if _, err = os.Lstat(filepath.Join(in.Home(), ".agents")); !os.IsNotExist(err) {
		t.Fatal("skills target prepared before tool check", err)
	}
}

func TestVerificationCleanupAggregatesFailuresAndContinues(t *testing.T) {
	home := t.TempDir()
	files := &probeFiles{}
	first, err := files.create(home)
	if err != nil {
		t.Fatal(err)
	}
	second, err := files.create(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(first); err != nil {
		t.Fatal(err)
	}
	// Keep the old object alive to avoid depending on inode reuse.
	if err := os.Rename(second, second+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = files.Close()
	if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), "replaced") || !strings.Contains(err.Error(), first) || !strings.Contains(err.Error(), second) {
		t.Fatalf("lost cleanup failure: %v", err)
	}
	if _, err := os.Lstat(first); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleanup abandoned %s: %v", first, err)
	}
	if raw, err := os.ReadFile(second); err != nil || string(raw) != "keep" {
		t.Fatalf("replacement damaged: %q %v", raw, err)
	}
	if err := files.Close(); err != nil {
		t.Fatalf("second close retried failed cleanup: %v", err)
	}
}

// The whole verification cycle used to be untested: runVerify went straight to
// bwrap.Execute, so nothing observed what it passed in or how it read the
// status. Every control below is the behaviour the code claims, and each one is
// something a regression would otherwise change silently.
func TestVerifyRunCycle(t *testing.T) {
	neverExecute := func(context.Context, *bwrap.Tool, sandbox.Plan, func(string) (*os.File, error), bwrap.Stdio) (int, error) {
		t.Error("execute ran without a usable tool")
		return 0, nil
	}
	t.Run("sentinel is created before execute and marshalled into the command", func(t *testing.T) {
		in := inputs(t, host.Run)
		home := t.TempDir()
		var payload string
		status, err := runVerify(context.Background(), in, home, nil,
			bwrap.Stdio{Out: io.Discard, Err: io.Discard},
			openStubTool(t),
			func(_ context.Context, _ *bwrap.Tool, plan sandbox.Plan, _ func(string) (*os.File, error), _ bwrap.Stdio) (int, error) {
				payload = plan.Command()[2]
				// The sentinel must already exist when Execute is called:
				// the probe checks that this path is absent inside the
				// sandbox, which means nothing unless the file exists
				// outside it. The host side removes it after Execute and
				// reports a missing or replaced file as a cleanup failure.
				entries, readErr := os.ReadDir(home)
				if readErr != nil || len(entries) != 1 {
					t.Errorf("sentinel present at Execute: %d entries, %v", len(entries), readErr)
				}
				return 0, nil
			})
		if err != nil || status != 0 {
			t.Fatalf("successful run = status %d, %v", status, err)
		}
		var expected probe.Expected
		if json.Unmarshal([]byte(payload), &expected) != nil {
			t.Errorf("command payload %q is not the marshalled expectation", payload)
		} else if expected.Sentinel == "" {
			t.Errorf("marshalled expectation carries no sentinel path: %s", payload)
		}
		entries, readErr := os.ReadDir(home)
		if readErr != nil || len(entries) != 0 {
			t.Errorf("sentinel left behind: %d entries, %v", len(entries), readErr)
		}
	})
	t.Run("non-zero exit status is a failure", func(t *testing.T) {
		in := inputs(t, host.Run)
		home := t.TempDir()
		status, err := runVerify(context.Background(), in, home, nil,
			bwrap.Stdio{Out: io.Discard, Err: io.Discard},
			openStubTool(t),
			func(context.Context, *bwrap.Tool, sandbox.Plan, func(string) (*os.File, error), bwrap.Stdio) (int, error) {
				return 3, nil
			})
		if err == nil || status != 1 || !strings.Contains(err.Error(), "status 3") {
			t.Fatalf("non-zero payload status = %d, %v", status, err)
		}
	})
	t.Run("execute error is a failure and leaves nothing behind", func(t *testing.T) {
		in := inputs(t, host.Run)
		home := t.TempDir()
		want := errors.New("execute failed")
		status, err := runVerify(context.Background(), in, home, nil,
			bwrap.Stdio{Out: io.Discard, Err: io.Discard},
			openStubTool(t),
			func(context.Context, *bwrap.Tool, sandbox.Plan, func(string) (*os.File, error), bwrap.Stdio) (int, error) {
				return 0, want
			})
		if err == nil || status != 1 || !errors.Is(err, want) {
			t.Fatalf("execute error = status %d, %v", status, err)
		}
		entries, readErr := os.ReadDir(home)
		if readErr != nil || len(entries) != 0 {
			t.Errorf("sentinel left behind after execute error: %d entries, %v", len(entries), readErr)
		}
	})
	t.Run("tool failure never reaches execute", func(t *testing.T) {
		in := inputs(t, host.Run)
		home := t.TempDir()
		want := errors.New("tool not available")
		status, err := runVerify(context.Background(), in, home, nil,
			bwrap.Stdio{Out: io.Discard, Err: io.Discard},
			func(context.Context) (*bwrap.Tool, error) { return nil, want }, neverExecute)
		if err == nil || status != 1 || !errors.Is(err, want) {
			t.Fatalf("tool failure = status %d, %v", status, err)
		}
	})
}

// openStubTool hands back a Tool that exists but is never used: runVerify only
// passes it to the execute stub, which these tests replace.
func openStubTool(t *testing.T) func(context.Context) (*bwrap.Tool, error) {
	t.Helper()
	return func(context.Context) (*bwrap.Tool, error) { return &bwrap.Tool{}, nil }
}
