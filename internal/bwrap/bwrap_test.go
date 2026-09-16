package bwrap

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kydex/abx/internal/sandbox"
)

func plan(t *testing.T, mode sandbox.Mode) sandbox.Plan {
	t.Helper()
	project := "/project"
	cmd := []string{"/home/agent/bin/agent", "a b", "$(literal)"}
	if mode == sandbox.Shell {
		project = ""
	}
	if mode != sandbox.Run {
		cmd = []string{"/bin/sh", "-l"}
	}
	p, e := sandbox.Build(sandbox.Input{Home: "/profile", Project: project, Mode: mode, Skills: "/skills", System: []sandbox.Mount{{Kind: sandbox.ReadOnly, Source: "/system", Target: "/usr"}}, Command: cmd})
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestSessionModesUseSameNamespaceAndFDPolicy(t *testing.T) {
	byPath := make(map[string]*os.File)
	for _, path := range []string{"/system", "/profile", "/skills", "/project"} {
		f, err := os.CreateTemp(t.TempDir(), "source")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := f.Close(); err != nil {
				t.Error(err)
			}
		})
		byPath[path] = f
	}
	for _, mode := range []sandbox.Mode{sandbox.Run, sandbox.Shell, sandbox.Work} {
		var sources []string
		p := plan(t, mode)
		args, files, e := Translate(p, func(path string) (*os.File, error) { sources = append(sources, path); return byPath[path], nil })
		if e != nil {
			t.Fatal(e)
		}
		prefix := []string{"--unshare-user", "--disable-userns", "--unshare-pid", "--unshare-ipc", "--unshare-uts", "--hostname", "abx", "--die-with-parent", "--clearenv"}
		if !reflect.DeepEqual(args[:len(prefix)], prefix) {
			t.Fatal(args)
		}
		want := []string{"/system", "/profile", "/skills"}
		if mode != sandbox.Shell {
			want = append(want, "/project")
		}
		if !reflect.DeepEqual(sources, want) || len(files) != len(want) {
			t.Fatal(sources)
		}
		for i, path := range want {
			if files[i] != byPath[path] {
				t.Fatalf("mode %v: child FD %d does not refer to %s", mode, 3+i, path)
			}
		}
		joined := strings.Join(args, " ")
		for _, s := range []string{"--ro-bind-fd 3 /usr", "--bind-fd 4 /home/agent", "--ro-bind-fd 5 /home/agent/.agents/skills", "--perms 0700 --dir /run/user"} {
			if !strings.Contains(joined, s) {
				t.Fatalf("missing %s", s)
			}
		}
		if mode == sandbox.Shell && strings.Contains(joined, "/workspace") {
			t.Fatal("project leaked into shell")
		}
		if mode != sandbox.Shell && !strings.Contains(joined, "--bind-fd 6 /workspace") {
			t.Fatal("missing project")
		}
		tail := args[len(args)-len(p.Command()):]
		if !reflect.DeepEqual(tail, p.Command()) {
			t.Fatal("changed child argv")
		}
	}
}
func TestBadSourcesAndVersion(t *testing.T) {
	if _, _, e := Translate(plan(t, sandbox.Shell), func(string) (*os.File, error) { return nil, errors.New("closed") }); e == nil {
		t.Fatal("accepted bad source")
	}
	for _, v := range []string{"bubblewrap 0.12.0\n", "bubblewrap 0.13.1", "bubblewrap 1.0.0"} {
		if !versionOK(v) {
			t.Fatal(v)
		}
	}
	for _, v := range []string{"bubblewrap 0.9.0", "noise 0.12.0", "bubblewrap 0.12", "bubblewrap 9999999999999999999999999.0.0"} {
		if versionOK(v) {
			t.Fatal(v)
		}
	}
	b := limitedBuffer{limit: 2}
	if _, e := b.Write([]byte("abc")); e == nil {
		t.Fatal("unbounded output")
	}
}
func TestChildStreamsExitAndCleanHostEnvironment(t *testing.T) {
	t.Setenv("ABX_TEST_HOST_SECRET", "must-not-inherit")
	var out, stderr bytes.Buffer
	// Constant helper script is test code, never constructed from user/project input.
	cmd := exec.CommandContext(context.Background(), "/bin/sh", "-c", `test -z "$ABX_TEST_HOST_SECRET" || exit 99; read -r value; printf '%s' "$value"; printf err >&2; exit 17`)
	status, e := execute(context.Background(), cmd, Stdio{In: strings.NewReader("a b\n"), Out: &out, Err: &stderr})
	if e != nil || status != 17 || out.String() != "a b" || stderr.String() != "err" {
		t.Fatalf("%d %v %q %q", status, e, out.String(), stderr.String())
	}
}
func TestChildCancellationAndSignalExit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	status, e := execute(ctx, exec.CommandContext(ctx, "/bin/sh", "-c", "exec sleep 10"), Stdio{Out: io.Discard, Err: io.Discard})
	if status != 1 || !errors.Is(e, context.DeadlineExceeded) {
		t.Fatalf("%d %v", status, e)
	}
	status, e = execute(context.Background(), exec.Command("/bin/sh", "-c", "kill -TERM $$"), Stdio{})
	if e != nil || status != 128+int(syscall.SIGTERM) {
		t.Fatalf("%d %v", status, e)
	}
}
func TestCancellationAfterSuccessfulWaitDoesNotOverrideSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "/bin/true")
	status, e := execute(ctx, cmd, Stdio{})
	cancel()
	if e != nil || status != 0 {
		t.Fatal(status, e)
	}
}

// Signal regression adapted from abx 0.7.9; these are real helper processes.
func TestForwardsSignals(t *testing.T) {
	if os.Getenv("ABX_SIGNAL_HELPER") == "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		code, err := execute(ctx, exec.CommandContext(ctx, os.Getenv("ABX_SIGNAL_SCRIPT"), os.Getenv("ABX_SIGNAL_RESULT"), os.Getenv("ABX_SIGNAL_READY")), Stdio{})
		if err != nil || code != 0 {
			t.Fatalf("helper RunArgv = %d, %v", code, err)
		}
		return
	}

	tests := []struct {
		name   string
		signal syscall.Signal
	}{
		{"interrupt", syscall.SIGINT},
		{"terminate", syscall.SIGTERM},
		{"hangup", syscall.SIGHUP},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			result := filepath.Join(dir, "result")
			ready := filepath.Join(dir, "ready")
			script := writeScript(t, `trap 'printf signal > "$1"; exit 0' INT TERM HUP
printf ready > "$2"
while :; do sleep 1; done`)
			helper := exec.Command(os.Args[0], "-test.run=^TestForwardsSignals$")
			helper.Env = append(os.Environ(),
				"ABX_SIGNAL_HELPER=1",
				"ABX_SIGNAL_SCRIPT="+script,
				"ABX_SIGNAL_RESULT="+result,
				"ABX_SIGNAL_READY="+ready,
			)
			if err := helper.Start(); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(3 * time.Second)
			for {
				if _, err := os.Stat(ready); err == nil {
					break
				} else if !os.IsNotExist(err) {
					t.Fatal(err)
				}
				if time.Now().After(deadline) {
					_ = helper.Process.Kill()
					_ = helper.Wait()
					t.Fatal("signal helper did not become ready")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err := helper.Process.Signal(tt.signal); err != nil {
				_ = helper.Process.Kill()
				_ = helper.Wait()
				t.Fatal(err)
			}
			if err := helper.Wait(); err != nil {
				t.Fatalf("signal helper: %v", err)
			}
			data, err := os.ReadFile(result)
			if err != nil || string(data) != "signal" {
				t.Fatalf("forwarding result = %q, %v", data, err)
			}
		})
	}
}

func writeScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestToolXOKAsInvokingUser(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("requires non-root permission semantics")
	}
	path := filepath.Join(t.TempDir(), "bwrap")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'bubblewrap 0.12.0\\n'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0001); err != nil {
		t.Fatal(err)
	}
	tool, err := open(context.Background(), path, false)
	if tool != nil {
		_ = tool.Close()
		t.Fatal("accepted non-executable tool")
	}
	if !errors.Is(err, syscall.EACCES) {
		t.Fatalf("expected invoking-user execute denial before version check: %v", err)
	}
}

func TestForwardSignalFailures(t *testing.T) {
	signalErr, killErr := errors.New("signal denied"), errors.New("kill denied")
	for _, tc := range []struct {
		name                 string
		signal, kill         error
		called               bool
		wantSignal, wantKill bool
	}{
		{"success", nil, nil, false, false, false},
		{"reaped", os.ErrProcessDone, nil, false, false, false},
		{"fallback", signalErr, nil, true, true, false},
		{"both fail", signalErr, killErr, true, true, true},
		{"reaped during fallback", signalErr, os.ErrProcessDone, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			err := forwardSignal(syscall.SIGTERM, func(s os.Signal) error {
				if s != syscall.SIGTERM {
					t.Fatal(s)
				}
				return tc.signal
			}, func() error { called = true; return tc.kill })
			if called != tc.called || errors.Is(err, signalErr) != tc.wantSignal || errors.Is(err, killErr) != tc.wantKill {
				t.Fatalf("called=%v err=%v", called, err)
			}
		})
	}
}

func TestToolLifecycleDetectsMutationAndCloseFailure(t *testing.T) {
	path := writeScript(t, `if [ "$1" = "--version" ]; then
  printf 'bubblewrap 0.12.0\n'
  exit 0
fi
exit 23`)
	tool, err := open(context.Background(), path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = tool.Revalidate(); err != nil {
		t.Fatal("unchanged tool rejected", err)
	}
	if err = os.Chmod(path, 0777); err != nil {
		t.Fatal(err)
	}
	if err = tool.Revalidate(); err == nil || !strings.Contains(err.Error(), "Bubblewrap changed") {
		t.Fatal("unsafe permission change accepted", err)
	}
	if err = tool.file.Close(); err != nil {
		t.Fatal(err)
	}
	if err = tool.Close(); err == nil {
		t.Fatal("descriptor close failure was lost")
	}
	if err = tool.Close(); err != nil {
		t.Fatal("second close should be harmless", err)
	}
	if err = tool.Revalidate(); err == nil || !strings.Contains(err.Error(), "tool closed") {
		t.Fatal("closed tool revalidated", err)
	}
}

func TestExecuteRevalidatesToolBeforeLendingSources(t *testing.T) {
	path := writeScript(t, `if [ "$1" = "--version" ]; then
  printf 'bubblewrap 0.12.0\n'
  exit 0
fi
exit 23`)
	tool, err := open(context.Background(), path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer tool.Close()
	if err = os.Chmod(path, 0777); err != nil {
		t.Fatal(err)
	}
	called := false
	status, err := Execute(context.Background(), tool, plan(t, sandbox.Shell), func(string) (*os.File, error) {
		called = true
		return nil, errors.New("source should not be requested")
	}, Stdio{Out: io.Discard, Err: io.Discard})
	if status != 1 || err == nil || !strings.Contains(err.Error(), "Bubblewrap changed") {
		t.Fatalf("Execute = status %d, err %v", status, err)
	}
	if called {
		t.Fatal("bind source was requested before tool revalidation")
	}
}

func TestExecuteUsesRetainedToolObject(t *testing.T) {
	path := writeScript(t, `if [ "$1" = "--version" ]; then
  printf 'bubblewrap 0.12.0\n'
  exit 0
fi
exit 23`)
	tool, err := open(context.Background(), path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer tool.Close()
	source, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	replaced := false
	status, err := Execute(context.Background(), tool, plan(t, sandbox.Shell), func(string) (*os.File, error) {
		// Execute has revalidated the tool before borrowing mount sources.
		// Replace its pathname here, without a timing-dependent goroutine.
		if !replaced {
			if err := os.Rename(path, path+"-original"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 42\n"), 0700); err != nil {
				t.Fatal(err)
			}
			replaced = true
		}
		return source, nil
	}, Stdio{Out: io.Discard, Err: io.Discard})
	if !replaced {
		t.Fatal("tool pathname was not replaced")
	}
	if status != 23 || err != nil {
		t.Fatalf("retained tool execution = status %d, err %v", status, err)
	}
}

func TestTranslateRejectsInvalidPlanBeforeSourceLookup(t *testing.T) {
	called := false
	_, _, err := Translate(sandbox.Plan{}, func(string) (*os.File, error) {
		called = true
		return nil, errors.New("must not be called")
	})
	if err == nil || called {
		t.Fatalf("err=%v sourceCalled=%v", err, called)
	}
}

func TestTranslateSourceFailurePreservesBorrowedFiles(t *testing.T) {
	for _, nilSource := range []bool{false, true} {
		name := "lookup error"
		if nilSource {
			name = "nil source"
		}
		t.Run(name, func(t *testing.T) {
			f, err := os.CreateTemp(t.TempDir(), "source")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := f.Close(); err != nil {
					t.Error(err)
				}
			})
			lookupErr := errors.New("source unavailable")
			var requested []string
			args, files, err := Translate(plan(t, sandbox.Shell), func(path string) (*os.File, error) {
				requested = append(requested, path)
				if path == "/system" {
					return f, nil
				}
				if nilSource {
					return nil, nil
				}
				return nil, lookupErr
			})
			if err == nil || (!nilSource && !errors.Is(err, lookupErr)) {
				t.Fatalf("source failure was lost: %v", err)
			}
			if len(args) != 0 || len(files) != 0 {
				t.Fatal("returned a partial launch after source failure")
			}
			if !reflect.DeepEqual(requested, []string{"/system", "/profile"}) {
				t.Fatalf("continued looking up sources after failure: %v", requested)
			}
			if _, err := f.Stat(); err != nil {
				t.Fatalf("closed a descriptor owned by the caller: %v", err)
			}
		})
	}
}

func TestVersionDiagnosticsDistinguishOldAndMalformed(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"bubblewrap 0.11.0\n", "too old; requires >= 0.12.0"},
		{"", "could not parse Bubblewrap version"},
		{"unexpected output\n", "could not parse Bubblewrap version"},
		{"bubblewrap 9999999999999999999999999.0.0", "could not parse Bubblewrap version"},
	} {
		if err := validateVersion(tc.input); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("version %q: %v", tc.input, err)
		}
	}
}
