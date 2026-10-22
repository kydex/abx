package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kydex/abx/internal/bwrap"
	"github.com/kydex/abx/internal/cli"
	"github.com/kydex/abx/internal/host"
	"github.com/kydex/abx/internal/sandbox"
)

func inputs(t *testing.T, mode host.Mode) *host.Inputs {
	t.Helper()
	base := t.TempDir()
	home := filepath.Join(base, "realhome")
	project := filepath.Join(home, "code/project")
	paths, e := host.ResolvePaths(host.Account{UID: os.Getuid(), Home: home}, filepath.Join(base, "data"))
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{project, filepath.Join(paths.Profiles, "demo/.local/bin"), paths.SharedSkills} {
		if e = os.MkdirAll(p, 0o700); e != nil {
			t.Fatal(e)
		}
	}
	if e = os.WriteFile(filepath.Join(paths.Profiles, "demo/.local/bin/demo"), []byte("#!/bin/sh\nexit 0\n"), 0o700); e != nil {
		t.Fatal(e)
	}
	in, e := host.Resolve(host.Account{UID: os.Getuid(), Username: "tester", Home: home}, paths, "demo", project, mode, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := in.Close(); e != nil {
			t.Error(e)
		}
	})
	return in
}

func TestSessionToolFailureNeverPreparesMountpoint(t *testing.T) {
	for _, mode := range []host.Mode{host.Run, host.Shell, host.Work} {
		in := inputs(t, mode)
		plan, e := buildPlan(in, []string{"HOME=/host", "LD_PRELOAD=bad"})
		if e != nil {
			t.Fatal(e)
		}
		marker := filepath.Join(in.Home(), ".agents")
		if _, e = os.Lstat(marker); !errors.Is(e, os.ErrNotExist) {
			t.Fatal("resolution mutated profile")
		}
		want := errors.New("tool unavailable")
		status, e := executeSession(
			context.Background(), in, plan, bwrap.Stdio{Out: io.Discard, Err: io.Discard},
			func(context.Context) (*bwrap.Tool, error) { return nil, want },
			bwrap.Execute,
		)
		if status != 1 || !errors.Is(e, want) {
			t.Fatal(status, e)
		}
		if _, e = os.Lstat(marker); !errors.Is(e, os.ErrNotExist) {
			t.Fatal("failed preflight mutated profile")
		}
	}
}

func TestUnknownCommandExits2(t *testing.T) {
	var out, stderr bytes.Buffer
	if status := Run(context.Background(), []string{"nosuch"}, bwrap.Stdio{Out: &out, Err: &stderr}); status != 2 {
		t.Fatalf("unknown command = status %d", status)
	}
}

func TestNoVerificationPassedOnFailedRuns(t *testing.T) {
	// Where the invoking account resolves, the refusal must name the missing
	// profile: in a container without the passwd home the same cases pass
	// through the earlier account refusal, and only the status is asserted.
	_, accountErr := host.ResolveAccount()
	for _, args := range [][]string{{"verify", "nosuchprofile"}, {"run", "nosuchprofile"}} {
		var out, stderr bytes.Buffer
		status := Run(context.Background(), args, bwrap.Stdio{Out: &out, Err: &stderr})
		if status != 1 || strings.Contains(out.String(), "Verification passed") {
			t.Fatalf("%q = status %d, stdout %q", args, status, out.String())
		}
		if accountErr == nil && !strings.Contains(stderr.String(), "profile does not exist") {
			t.Fatalf("%q refused without naming the profile: %q", args, stderr.String())
		}
	}
}

// The mutation that drops the inspection write error used to survive the
// sweep, although runSessionForAccount already takes stdio and brokenWriter
// already exists: no new seam is needed to pin it.
func TestInspectOutputFailureIsReported(t *testing.T) {
	base := t.TempDir()
	t.Chdir(t.TempDir())
	home := filepath.Join(base, "realhome")
	account := host.Account{UID: os.Getuid(), Username: "tester", Home: home}
	data := filepath.Join(base, "data")
	paths, err := host.ResolvePaths(account, data)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(paths.Profiles, "demo", ".local", "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "demo"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	status, err := runSessionForAccount(
		context.Background(),
		cli.Command{Kind: cli.Inspect, Profile: "demo"},
		[]string{"XDG_DATA_HOME=" + data},
		bwrap.Stdio{Out: brokenWriter{}, Err: io.Discard},
		account,
	)
	if status != 1 || err == nil || !strings.Contains(err.Error(), "write inspection") {
		t.Fatalf("inspect output failure = status %d, %v", status, err)
	}
}

func TestProbeInvocationRequiresPayload(t *testing.T) {
	if os.Getuid() == 0 || os.Geteuid() == 0 || os.Geteuid() != os.Getuid() {
		t.Skip("public CLI requires a non-root user")
	}
	for _, tc := range []struct {
		name   string
		args   []string
		status int
	}{
		{"missing payload", []string{"__probe"}, 2},
		{"extra arguments", []string{"__probe", "{}", "extra"}, 2},
		{"invalid payload", []string{"__probe", "{}"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, stderr bytes.Buffer
			if status := Run(context.Background(), tc.args, bwrap.Stdio{Out: &out, Err: &stderr}); status != tc.status || out.Len() != 0 || stderr.Len() == 0 {
				t.Fatalf("probe invocation = status %d, stdout %q, stderr %q", status, out.String(), stderr.String())
			}
		})
	}
}

func TestEffectiveIDValidation(t *testing.T) {
	if err := validateEffectiveUID(1000, 1000); err != nil {
		t.Fatal(err)
	}
	if err := validateEffectiveUID(0, 1000); err == nil {
		t.Error("setuid root accepted")
	}
	if err := validateEffectiveUID(0, 0); err == nil {
		t.Error("root accepted")
	}
	if err := validateEffectiveGID(100, 100); err != nil {
		t.Fatal(err)
	}
	if err := validateEffectiveGID(100, 0); err == nil {
		t.Error("setgid accepted")
	}
}

func TestRootRejectedBeforeHelpAndErrorsEscaped(t *testing.T) {
	if os.Getuid() != 0 && os.Geteuid() != 0 {
		t.Skip("root ordering regression requires root fixture")
	}
	for _, args := range [][]string{{"help"}, {"__probe", "{}"}, {"verify", "demo"}} {
		var out, err bytes.Buffer
		if c := Run(context.Background(), args, bwrap.Stdio{Out: &out, Err: &err}); c != 1 || out.Len() != 0 {
			t.Fatalf("root invocation accepted: %q", args)
		}
	}
}

func TestSessionWithoutXDGReachesCommandValidation(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("requires a non-root passwd account")
	}
	status, err := runSessionForAccount(
		context.Background(),
		cli.Command{Kind: cli.ProfileShow, Profile: "../invalid"},
		nil,
		bwrap.Stdio{Out: io.Discard, Err: io.Discard},
		host.Account{UID: os.Getuid(), Username: "tester", Home: t.TempDir()},
	)
	if status != 1 || err == nil || !strings.Contains(err.Error(), "invalid profile name") {
		t.Fatalf("session without XDG did not reach name validation: %d, %v", status, err)
	}
}

func TestValidateEffectiveUID(t *testing.T) {
	for _, tc := range []struct {
		name              string
		real, effective   int
		wantErrorContains string
	}{
		{"root", 1000, 0, "must not run as root"},
		{"different effective uid", 1000, 1001, "effective privileges"},
		{"normal user", 1000, 1000, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateEffectiveUID(tc.real, tc.effective)
			if tc.wantErrorContains == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErrorContains) {
				t.Fatalf("error = %v, want %q", err, tc.wantErrorContains)
			}
		})
	}
}

func TestValidateEffectiveGID(t *testing.T) {
	for _, tc := range []struct {
		name              string
		real, effective   int
		wantErrorContains string
	}{
		{"different effective gid", 1000, 1001, "effective group privileges"},
		{"normal group", 1000, 1000, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateEffectiveGID(tc.real, tc.effective)
			if tc.wantErrorContains == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErrorContains) {
				t.Fatalf("error = %v, want %q", err, tc.wantErrorContains)
			}
		})
	}
}

func TestUnknownCommandKindFailsClosed(t *testing.T) {
	status, err := runSessionForAccount(
		context.Background(),
		cli.Command{Kind: cli.Kind("future-command")},
		nil,
		bwrap.Stdio{},
		host.Account{},
	)
	if status != 1 || err == nil || !strings.Contains(err.Error(), "unsupported command") {
		t.Fatalf("unknown command = status %d, error %v", status, err)
	}
}

func TestVersionCommand(t *testing.T) {
	if os.Getuid() == 0 || os.Geteuid() == 0 || os.Geteuid() != os.Getuid() {
		t.Skip("public CLI requires a non-root user")
	}
	var out, stderr bytes.Buffer
	status := Run(context.Background(), []string{"version"}, bwrap.Stdio{Out: &out, Err: &stderr})
	if status != 0 || out.String() != "abx "+Version+"\n" || stderr.Len() != 0 {
		t.Fatalf("version: status=%d stdout=%q stderr=%q", status, out.String(), stderr.String())
	}
}

// Help text is the whole documentation that exists before any command runs; it
// must reach empty arguments and "help" alike, and name every command the
// parser accepts.
func TestHelpDescribesEveryParsedCommand(t *testing.T) {
	if os.Getuid() == 0 || os.Geteuid() == 0 || os.Geteuid() != os.Getuid() {
		t.Skip("public CLI requires a non-root user")
	}
	for _, args := range [][]string{nil, {"help"}} {
		var out, stderr bytes.Buffer
		status := Run(context.Background(), args, bwrap.Stdio{Out: &out, Err: &stderr})
		if status != 0 || stderr.Len() != 0 {
			t.Fatalf("help args %q: status=%d stderr=%q", args, status, stderr.String())
		}
		if out.Len() == 0 {
			t.Fatalf("help args %q: no output", args)
		}
		for _, kind := range []cli.Kind{cli.ProfileCreate, cli.ProfileShow, cli.ProfileList, cli.Run, cli.Shell, cli.Work, cli.Inspect, cli.Verify, cli.Help, cli.Version} {
			if !strings.Contains(out.String(), string(kind)) {
				t.Errorf("help args %q: does not mention %q", args, kind)
			}
		}
	}
}

func TestExecuteSessionPreparesRetainedInputsBeforeExecution(t *testing.T) {
	in := inputs(t, host.Run)
	plan, err := buildPlan(in, nil)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(in.Home(), ".agents")
	called := 0
	status, err := executeSession(
		context.Background(), in, plan, bwrap.Stdio{Out: io.Discard, Err: io.Discard},
		func(context.Context) (*bwrap.Tool, error) { return nil, nil },
		func(_ context.Context, _ *bwrap.Tool, gotPlan sandbox.Plan, source func(string) (*os.File, error), _ bwrap.Stdio) (int, error) {
			called++
			if gotPlan.WorkingDirectory() != plan.WorkingDirectory() || strings.Join(gotPlan.Command(), "\x00") != strings.Join(plan.Command(), "\x00") {
				t.Fatal("execution did not receive the resolved plan snapshot")
			}
			info, err := os.Stat(marker)
			if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
				t.Fatalf("skills parent was not prepared before execution: %v, mode=%v", err, info)
			}
			f, err := source(in.Home())
			if err != nil {
				t.Fatal(err)
			}
			opened, err := f.Stat()
			if err != nil {
				t.Fatal(err)
			}
			current, err := os.Lstat(in.Home())
			if err != nil || !os.SameFile(opened, current) {
				t.Fatal("execution did not receive the retained home object")
			}
			return 23, nil
		},
	)
	if status != 23 || err != nil || called != 1 {
		t.Fatalf("execution = status %d, err %v, calls %d", status, err, called)
	}
}

func TestExecuteSessionChangedInputNeverExecutes(t *testing.T) {
	in := inputs(t, host.Run)
	plan, err := buildPlan(in, nil)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	status, err := executeSession(
		context.Background(), in, plan, bwrap.Stdio{Out: io.Discard, Err: io.Discard},
		func(context.Context) (*bwrap.Tool, error) {
			if err := os.Chmod(in.Home(), 0o755); err != nil {
				t.Fatal(err)
			}
			return nil, nil
		},
		func(context.Context, *bwrap.Tool, sandbox.Plan, func(string) (*os.File, error), bwrap.Stdio) (int, error) {
			called = true
			return 0, nil
		},
	)
	if status != 1 || err == nil || !strings.Contains(err.Error(), "exact mode 0700") {
		t.Fatalf("changed input = status %d, err %v", status, err)
	}
	if called {
		t.Fatal("executed after retained input changed")
	}
}

func TestExecuteSessionLateSkillsSymlinkNeverExecutes(t *testing.T) {
	in := inputs(t, host.Run)
	plan, err := buildPlan(in, nil)
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	marker := filepath.Join(in.Home(), ".agents")
	called := false
	status, err := executeSession(
		context.Background(), in, plan, bwrap.Stdio{Out: io.Discard, Err: io.Discard},
		func(context.Context) (*bwrap.Tool, error) {
			if err := os.Symlink(outside, marker); err != nil {
				t.Fatal(err)
			}
			return nil, nil
		},
		func(context.Context, *bwrap.Tool, sandbox.Plan, func(string) (*os.File, error), bwrap.Stdio) (int, error) {
			called = true
			return 0, nil
		},
	)
	if status != 1 || err == nil || !strings.Contains(err.Error(), "unsafe skills target") {
		t.Fatalf("late skills symlink = status %d, err %v", status, err)
	}
	if called {
		t.Fatal("executed after skills target was replaced with a symlink")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unsafe skills target was modified: %v, entries=%d", err, len(entries))
	}
}

func TestExecuteSessionPropagatesExecutorFailure(t *testing.T) {
	in := inputs(t, host.Run)
	plan, err := buildPlan(in, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("sandbox execution failed")
	status, err := executeSession(
		context.Background(), in, plan, bwrap.Stdio{Out: io.Discard, Err: io.Discard},
		func(context.Context) (*bwrap.Tool, error) { return nil, nil },
		func(context.Context, *bwrap.Tool, sandbox.Plan, func(string) (*os.File, error), bwrap.Stdio) (int, error) {
			return 1, want
		},
	)
	if status != 1 || !errors.Is(err, want) {
		t.Fatalf("executor failure = status %d, err %v", status, err)
	}
}

func TestFinishClosePreservesOperationAndCleanupFailures(t *testing.T) {
	opErr := errors.New("operation failed")
	closeErr := errors.New("close failed")
	for _, tc := range []struct {
		name       string
		status     int
		result     error
		closeErr   error
		wantStatus int
		wantOp     bool
		wantClose  bool
	}{
		{"success", 0, nil, nil, 0, false, false},
		{"close failure preserves operation status", 23, nil, closeErr, 23, false, true},
		{"close failure on success becomes status 1", 0, nil, closeErr, 1, false, true},
		{"operation failure preserved", 1, opErr, nil, 1, true, false},
		{"operation and close failures joined", 1, opErr, closeErr, 1, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, result := tc.status, tc.result
			finishClose(&status, &result, func() error { return tc.closeErr })
			if status != tc.wantStatus || errors.Is(result, opErr) != tc.wantOp || errors.Is(result, closeErr) != tc.wantClose {
				t.Fatalf("finishClose = status %d, err %v", status, result)
			}
		})
	}
}

func TestFinishCloseJoinsChildStatusWithCleanupError(t *testing.T) {
	closeErr := errors.New("close failed")
	status, result := 23, error(nil)
	finishClose(&status, &result, func() error { return closeErr })
	if status != 23 || result == nil || !strings.Contains(result.Error(), "23") {
		t.Fatalf("finishClose = status %d, err %v", status, result)
	}
	// A child that exited zero contributes no status text of its own.
	status, result = 0, nil
	finishClose(&status, &result, func() error { return closeErr })
	if status != 1 || result == nil || strings.Contains(result.Error(), "status 0") {
		t.Fatalf("finishClose = status %d, err %v", status, result)
	}
	// A launcher-side failure carries an error with its nonzero status, so it
	// must not be named as a child exit: the child never started.
	opErr := errors.New("plan failed")
	status, result = 1, opErr
	finishClose(&status, &result, func() error { return closeErr })
	if status != 1 || !errors.Is(result, opErr) || !errors.Is(result, closeErr) ||
		strings.Contains(result.Error(), "child exited") {
		t.Fatalf("finishClose = status %d, err %v", status, result)
	}
}

// Run used to report every session error as status 1, which erased a kept
// child status: a cleanup failure after a nonzero child exit joins both
// errors, and the joined error arrives here as a plain error. The report must
// keep the status the session returned.
func TestFailKeepsTheSessionStatus(t *testing.T) {
	var errOut bytes.Buffer
	joined := errors.Join(fmt.Errorf("child ended with status %d", 23), errors.New("close failed"))
	if status := fail(23, joined, bwrap.Stdio{Err: &errOut}); status != 23 {
		t.Fatalf("status = %d, want 23", status)
	}
	for _, want := range []string{"child ended with status 23", "close failed"} {
		if !strings.Contains(errOut.String(), want) {
			t.Fatalf("stderr %q does not mention %q", errOut.String(), want)
		}
	}
	// A failure without a status of its own reports 1.
	if status := fail(0, errors.New("failed before any child"), bwrap.Stdio{Err: &errOut}); status != 1 {
		t.Fatalf("status without one of its own = %d, want 1", status)
	}
}

func TestInspectDoesNotPreparePersistentRuntimeState(t *testing.T) {
	account := host.Account{UID: os.Getuid(), Username: "tester", Home: t.TempDir()}
	data := filepath.Join(t.TempDir(), "data")
	paths, err := host.ResolvePaths(account, data)
	if err != nil {
		t.Fatal(err)
	}
	home, err := host.CreateProfile(account, paths, "demo")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(home, ".local/bin")
	if err = os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(bin, "demo"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	// Configured skills would make Prepare create persistent mountpoints.
	if err = os.MkdirAll(paths.SharedSkills, 0o700); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	t.Chdir(project)
	marker := filepath.Join(home, ".agents")
	if _, err = os.Lstat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("profile unexpectedly prepared before inspect")
	}
	var out bytes.Buffer
	status, err := runSessionForAccount(
		context.Background(),
		cli.Command{Kind: cli.Inspect, Profile: "demo"},
		[]string{"XDG_DATA_HOME=" + data},
		bwrap.Stdio{Out: &out, Err: io.Discard},
		account,
	)
	if status != 0 || err != nil {
		t.Fatalf("inspect = status %d, err %v", status, err)
	}
	if !strings.Contains(out.String(), `cwd: "/workspace"`) || !strings.Contains(out.String(), `mount: read-write`) {
		t.Fatalf("unexpected inspect output: %q", out.String())
	}
	if !strings.Contains(out.String(), ` -> "/home/agent/.agents/skills"`) {
		t.Fatalf("inspect omitted configured skills: %q", out.String())
	}
	if _, err = os.Lstat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("inspect prepared persistent runtime state")
	}
}

func TestValueUsesLastEnvironmentAssignment(t *testing.T) {
	env := []string{"A=first", "B=value", "A=last", "AUX=ignored"}
	if got := value(env, "A"); got != "last" {
		t.Fatalf("value(A) = %q", got)
	}
	if got := value(env, "missing"); got != "" {
		t.Fatalf("value(missing) = %q", got)
	}
}

func TestWorkPlanUsesShellInProject(t *testing.T) {
	in := inputs(t, host.Work)
	plan, err := buildPlan(in, []string{"PWD=/host", "HOME=/host", "LD_PRELOAD=bad"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.WorkingDirectory() != "/workspace" || !slices.Equal(plan.Command(), []string{in.Shell(), "-l"}) {
		t.Fatalf("cwd=%q command=%q", plan.WorkingDirectory(), plan.Command())
	}
	for _, e := range plan.Environment() {
		if e.Name == "PWD" && e.Value != "/workspace" || e.Name == "HOME" && e.Value != "/home/agent" || e.Name == "LD_PRELOAD" {
			t.Fatalf("incorrect work environment: %+v", e)
		}
	}
	called := false
	status, err := executeSession(context.Background(), in, plan, bwrap.Stdio{},
		func(context.Context) (*bwrap.Tool, error) { return nil, nil },
		func(_ context.Context, _ *bwrap.Tool, p sandbox.Plan, source func(string) (*os.File, error), _ bwrap.Stdio) (int, error) {
			called = true
			for _, m := range p.Mounts() {
				if m.Target == "/workspace" {
					retained, err := in.SourceFile(in.Project())
					if err != nil {
						t.Fatal(err)
					}
					got, err := source(m.Source)
					if err != nil || got != retained || m.Kind != sandbox.Writable {
						t.Fatalf("workspace source: %v", err)
					}
					return 23, nil
				}
			}
			t.Fatal("workspace missing")
			return 1, nil
		})
	if !called || status != 23 || err != nil {
		t.Fatalf("execution: %v %d %v", called, status, err)
	}
}

func TestWorkProjectReplacementNeverExecutes(t *testing.T) {
	in := inputs(t, host.Work)
	plan, err := buildPlan(in, nil)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	status, err := executeSession(context.Background(), in, plan, bwrap.Stdio{},
		func(context.Context) (*bwrap.Tool, error) {
			if err := os.Rename(in.Project(), in.Project()+"-old"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(in.Project(), 0o700); err != nil {
				t.Fatal(err)
			}
			return nil, nil
		},
		func(context.Context, *bwrap.Tool, sandbox.Plan, func(string) (*os.File, error), bwrap.Stdio) (int, error) {
			called = true
			return 0, nil
		})
	if called || status != 1 || err == nil || !strings.Contains(err.Error(), "selected source changed") {
		t.Fatalf("replaced project: %v %d %v", called, status, err)
	}
}
