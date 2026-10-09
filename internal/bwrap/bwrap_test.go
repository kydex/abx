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
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kydex/abx/internal/sandbox"
	"golang.org/x/sys/unix"
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
	cwd := "/workspace"
	if mode == sandbox.Shell {
		cwd = "/home/agent"
	}
	p, e := sandbox.Build(sandbox.Input{Home: "/profile", Project: project, Mode: mode, Skills: "/skills", System: []sandbox.Mount{{Kind: sandbox.ReadOnly, Source: "/usr", Target: "/usr"}}, Environment: []sandbox.Entry{{Name: "PWD", Value: cwd}}, Command: cmd})
	if e != nil {
		t.Fatal(e)
	}
	return p
}

func TestSessionModesUseSameNamespaceAndFDPolicy(t *testing.T) {
	byPath := make(map[string]*os.File)
	for _, path := range []string{"/usr", "/profile", "/skills", "/project"} {
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
		want := []string{"/usr", "/profile", "/skills"}
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
		for _, s := range []string{"--ro-bind-fd 3 /usr", "--bind-fd 4 /home/agent", "--ro-bind-fd 5 /home/agent/.agents/skills", "--perms 0700 --dir /run/user", "--size 4294967296 --tmpfs /tmp", "--size 4294967296 --tmpfs /var/tmp", "--size 4294967296 --tmpfs /run"} {
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

func countOpenFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skipf("no /proc/self/fd: %v", err)
	}
	return len(entries)
}

func TestToolOpenCloseDoesNotLeakFDs(t *testing.T) {
	before := countOpenFDs(t)
	path := writeScript(t, `if [ "$1" = "--version" ]; then
  printf 'bubblewrap 0.12.0\n'
fi`)
	tool, err := open(context.Background(), path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := tool.Close(); err != nil {
		t.Fatal(err)
	}
	if got := countOpenFDs(t); got != before {
		t.Fatalf("open/close leaked %d descriptors", got-before)
	}
	if _, err := open(context.Background(), filepath.Join(t.TempDir(), "missing"), false); err == nil {
		t.Fatal("opened missing tool")
	}
	if got := countOpenFDs(t); got != before {
		t.Fatalf("failed open leaked %d descriptors", got-before)
	}
}

func TestEnvironmentValuesStaySingleArgvElements(t *testing.T) {
	env, err := sandbox.Session("tester", "/bin/sh", "/home/agent", []string{
		"TERM=xterm", "LC_SPACED=a b", "LC_DOLLAR=$(x)", "LC_QUOTE=a'b\"c", "LC_EMPTY=",
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := sandbox.Build(sandbox.Input{Home: "/profile", Project: "", Mode: sandbox.Shell, Skills: "/skills", System: []sandbox.Mount{{Kind: sandbox.ReadOnly, Source: "/usr", Target: "/usr"}}, Environment: env, Command: []string{"/bin/sh", "-l"}})
	if err != nil {
		t.Fatal(err)
	}
	devnull, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	defer devnull.Close()
	args, _, err := Translate(p, func(string) (*os.File, error) { return devnull, nil })
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]string{}
	for i := 0; i+2 < len(args); i++ {
		if args[i] == "--setenv" {
			found[args[i+1]] = args[i+2]
		}
	}
	for k, v := range map[string]string{"LC_SPACED": "a b", "LC_DOLLAR": "$(x)", "LC_QUOTE": `a'b"c`, "LC_EMPTY": ""} {
		if found[k] != v {
			t.Errorf("--setenv %s = %q, want single element %q", k, found[k], v)
		}
	}
}

func TestExecuteRunsBinaryAtRetainedFD(t *testing.T) {
	path := writeScript(t, `if [ "$1" = "--version" ]; then
  printf 'bubblewrap 0.12.0\n'
  exit 0
fi
printf 'SELF=%s\n' "$0"`)
	tool, err := open(context.Background(), path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer tool.Close()
	// Shell fixture lends three bind sources (/system, /profile, /skills),
	// so the retained binary must be reachable at fd 3+3.
	devnull, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	defer devnull.Close()
	var out bytes.Buffer
	status, err := Execute(context.Background(), tool, plan(t, sandbox.Shell), func(string) (*os.File, error) { return devnull, nil }, Stdio{Out: &out, Err: io.Discard})
	if err != nil || status != 0 {
		t.Fatalf("Execute = status %d, err %v", status, err)
	}
	if !strings.Contains(out.String(), "SELF=/proc/self/fd/6") {
		t.Fatalf("binary did not run from retained fd: %q", out.String())
	}
}

// versionOK is a test-only shorthand for the production version check.
func versionOK(s string) bool { return validateVersion(s) == nil }

func TestBadSourcesAndVersion(t *testing.T) {
	if _, _, e := Translate(plan(t, sandbox.Shell), func(string) (*os.File, error) { return nil, errors.New("closed") }); e == nil {
		t.Fatal("accepted bad source")
	}
	for _, v := range []string{"bubblewrap 0.12.0\n", "bubblewrap 0.13.1", "bubblewrap 1.0.0"} {
		if !versionOK(v) {
			t.Fatal(v)
		}
	}
	for _, v := range []string{"bubblewrap 0.0.99", "bubblewrap 0.9.0", "noise 0.12.0", "bubblewrap 0.12", "bubblewrap 9999999999999999999999999.0.0"} {
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

func TestWaitDelayReportsSuccessfulExit(t *testing.T) {
	// The child exits at once while an orphaned descendant holds the output
	// pipe past WaitDelay. Wait then reports ErrWaitDelay instead of nil for
	// an otherwise successful exit; that is the child's status, not a
	// launcher failure. Unreachable through the abx binary itself, which
	// always passes *os.File streams without copying goroutines.
	var out bytes.Buffer
	cmd := exec.Command("/bin/sh", "-c", "sleep 5 >&1 & exit 0")
	status, err := execute(context.Background(), cmd, Stdio{Out: &out, Err: io.Discard})
	if err != nil || status != 0 {
		t.Fatalf("WaitDelay success = status %d, err %v", status, err)
	}
}

func TestWaitDelayPreservesFailedExit(t *testing.T) {
	// Same orphaned pipe as above, but the child itself fails: the failure
	// must survive WaitDelay expiry instead of turning into success.
	var out bytes.Buffer
	cmd := exec.Command("/bin/sh", "-c", "sleep 5 >&1 & exit 17")
	status, err := execute(context.Background(), cmd, Stdio{Out: &out, Err: io.Discard})
	if err != nil || status != 17 {
		t.Fatalf("WaitDelay failure = status %d, err %v", status, err)
	}
}

func TestMarkInheritedFDsKeepsExplicitFiles(t *testing.T) {
	keep, err := os.CreateTemp(t.TempDir(), "keep")
	if err != nil {
		t.Fatal(err)
	}
	defer keep.Close()
	unknown, err := os.CreateTemp(t.TempDir(), "unknown")
	if err != nil {
		t.Fatal(err)
	}
	defer unknown.Close()
	for _, file := range []*os.File{keep, unknown} {
		if _, err := unix.FcntlInt(file.Fd(), unix.F_SETFD, 0); err != nil {
			t.Fatal(err)
		}
	}
	restore, err := markInheritedFDsCloseOnExec([]*os.File{keep})
	if err != nil {
		t.Fatal(err)
	}
	keepFlags, err := unix.FcntlInt(keep.Fd(), unix.F_GETFD, 0)
	if err != nil {
		t.Fatal(err)
	}
	unknownFlags, err := unix.FcntlInt(unknown.Fd(), unix.F_GETFD, 0)
	if err != nil {
		t.Fatal(err)
	}
	if keepFlags&unix.FD_CLOEXEC != 0 {
		t.Fatal("explicit ExtraFiles descriptor was marked close-on-exec")
	}
	if unknownFlags&unix.FD_CLOEXEC == 0 {
		t.Fatal("inherited descriptor remained open across exec")
	}
	restore()
	unknownFlags, err = unix.FcntlInt(unknown.Fd(), unix.F_GETFD, 0)
	if err != nil {
		t.Fatal(err)
	}
	if unknownFlags&unix.FD_CLOEXEC != 0 {
		t.Fatal("inherited descriptor flags were not restored")
	}
	// A restore must never touch a descriptor whose number was reused:
	// close it, let another file take the slot, then restore.
	slot := int(unknown.Fd())
	if err := unknown.Close(); err != nil {
		t.Fatal(err)
	}
	replacement, err := os.CreateTemp(t.TempDir(), "replacement")
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	if int(replacement.Fd()) != slot {
		t.Skipf("descriptor slot %d was not reused (got %d)", slot, replacement.Fd())
	}
	if _, err := unix.FcntlInt(replacement.Fd(), unix.F_SETFD, unix.FD_CLOEXEC); err != nil {
		t.Fatal(err)
	}
	restore()
	replacementFlags, err := unix.FcntlInt(replacement.Fd(), unix.F_GETFD, 0)
	if err != nil {
		t.Fatal(err)
	}
	if replacementFlags&unix.FD_CLOEXEC == 0 {
		t.Fatal("restore cleared CLOEXEC on a reused descriptor")
	}
}

func TestExecuteDropsInheritedFileDescriptors(t *testing.T) {
	secret, err := os.CreateTemp(t.TempDir(), "secret")
	if err != nil {
		t.Fatal(err)
	}
	defer secret.Close()
	fd := int(secret.Fd())
	if _, err := unix.FcntlInt(secret.Fd(), unix.F_SETFD, 0); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", "test ! -e /proc/self/fd/"+strconv.Itoa(fd))
	status, err := execute(context.Background(), cmd, Stdio{})
	if err != nil || status != 0 {
		t.Fatalf("inherited descriptor reached child: status=%d error=%v", status, err)
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

// Signal regression test; these are real helper processes.
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

func TestToolSystemRequiresRootOwnership(t *testing.T) {
	path := writeScript(t, `if [ "$1" = "--version" ]; then
  printf 'bubblewrap 0.12.0\n'
  exit 0
fi
exit 23`)
	if os.Getuid() == 0 {
		if err := os.Chown(path, 65534, 65534); err != nil {
			t.Skipf("cannot stage non-root tool fixture: %v", err)
		}
	}
	tool, err := open(context.Background(), path, false)
	if err != nil {
		t.Fatalf("unprivileged open rejected fixture: %v", err)
	}
	_ = tool.Close()
	if _, err := open(context.Background(), path, true); err == nil || !strings.Contains(err.Error(), "unsafe Bubblewrap executable") {
		t.Fatalf("non-root tool accepted with system=true: %v", err)
	}
}

func TestRevalidateAcceptsOwnerChange(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("chown requires root")
	}
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
	// Ownership rechecking would add nothing (changing an inode owner
	// requires privileges): Revalidate pins identity and mode bits only.
	if err := os.Chown(path, 65534, 65534); err != nil {
		t.Fatal(err)
	}
	if err := tool.Revalidate(); err != nil {
		t.Fatalf("owner change rejected by revalidate: %v", err)
	}
}

func TestToolRejectsSpecialModeBits(t *testing.T) {
	path := writeScript(t, `if [ "$1" = "--version" ]; then
  printf 'bubblewrap 0.12.0\n'
  exit 0
fi
exit 23`)
	for _, mode := range []os.FileMode{0o755 | os.ModeSetuid, 0o755 | os.ModeSetgid, 0o755 | os.ModeSticky} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := open(context.Background(), path, false); err == nil || !strings.Contains(err.Error(), "unsafe Bubblewrap executable") {
			t.Fatalf("special mode %v accepted by open: %v", mode, err)
		}
	}
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	tool, err := open(context.Background(), path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer tool.Close()
	if err := os.Chmod(path, 0o755|os.ModeSetuid); err != nil {
		t.Fatal(err)
	}
	if err := tool.Revalidate(); err == nil || !strings.Contains(err.Error(), "bubblewrap changed") {
		t.Fatalf("special mode accepted by revalidate: %v", err)
	}
}

func TestToolXOKAsInvokingUser(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("requires non-root permission semantics")
	}
	path := filepath.Join(t.TempDir(), "bwrap")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'bubblewrap 0.12.0\\n'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o001); err != nil {
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
	if err = os.Chmod(path, 0o777); err != nil {
		t.Fatal(err)
	}
	if err = tool.Revalidate(); err == nil || !strings.Contains(err.Error(), "bubblewrap changed") {
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
	if err = os.Chmod(path, 0o777); err != nil {
		t.Fatal(err)
	}
	called := false
	status, err := Execute(context.Background(), tool, plan(t, sandbox.Shell), func(string) (*os.File, error) {
		called = true
		return nil, errors.New("source should not be requested")
	}, Stdio{Out: io.Discard, Err: io.Discard})
	if status != 1 || err == nil || !strings.Contains(err.Error(), "bubblewrap changed") {
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
			if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 42\n"), 0o700); err != nil {
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
				if path == "/usr" {
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
			if !reflect.DeepEqual(requested, []string{"/usr", "/profile"}) {
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
