package app

import (
	"bytes"
	"context"
	"errors"
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
	paths, e := host.ResolvePaths(host.Account{Home: home}, filepath.Join(base, "data"))
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{project, filepath.Join(paths.Profiles, "demo/.local/bin"), paths.SharedSkills} {
		if e = os.MkdirAll(p, 0700); e != nil {
			t.Fatal(e)
		}
	}
	if e = os.WriteFile(filepath.Join(paths.Profiles, "demo/.local/bin/demo"), []byte("#!/bin/sh\nexit 0\n"), 0700); e != nil {
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
func TestRootRejectedBeforeHelpAndErrorsEscaped(t *testing.T) {
	if os.Getuid() != 0 {
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
	// An invalid name stops before opening storage, so this test never uses profiles in the real home.
	status, err := runSession(context.Background(), cli.Command{Kind: cli.ProfileShow, Profile: "../invalid"}, nil, bwrap.Stdio{Out: io.Discard, Err: io.Discard})
	if status != 1 || err == nil || !strings.Contains(err.Error(), "invalid profile name") {
		t.Fatalf("session without XDG did not reach name validation: %d, %v", status, err)
	}
}

func TestVersionCommand(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("public CLI requires a non-root user")
	}
	var out, stderr bytes.Buffer
	status := Run(context.Background(), []string{"version"}, bwrap.Stdio{Out: &out, Err: &stderr})
	if status != 0 || out.String() != "abx "+Version+"\n" || stderr.Len() != 0 {
		t.Fatalf("version: status=%d stdout=%q stderr=%q", status, out.String(), stderr.String())
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
			if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
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
			if err := os.Chmod(in.Home(), 0755); err != nil {
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
		{"close failure overrides successful status", 23, nil, closeErr, 1, false, true},
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
	if err = os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(bin, "demo"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	// Configured skills would make Prepare create persistent mountpoints.
	if err = os.MkdirAll(paths.SharedSkills, 0700); err != nil {
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
			if err := os.Mkdir(in.Project(), 0700); err != nil {
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
