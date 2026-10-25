package bwrap

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"slices"
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
		for _, s := range []string{"--ro-bind-fd 3 /usr", "--bind-fd 4 /home/agent", "--ro-bind-fd 5 /home/agent/.agents/skills", "--perms 0700 --dir /run/user", "--size 4294967296 --tmpfs /tmp", "--size 4294967296 --tmpfs /var/tmp", "--size 4294967296 --tmpfs /run", "--proc /proc", "--dev /dev"} {
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
		if args[len(args)-len(p.Command())-1] != "--" {
			t.Fatal("command is not separated from options by --")
		}
	}
}

// An option-like command must stay the command: only "--" separates it from
// the options Translate builds.
func TestOptionLikeCommandStaysAfterDoubleDash(t *testing.T) {
	p, err := sandbox.Build(sandbox.Input{
		Home:        "/profile",
		Mode:        sandbox.Shell,
		System:      []sandbox.Mount{{Kind: sandbox.ReadOnly, Source: "/usr", Target: "/usr"}},
		Environment: []sandbox.Entry{{Name: "PWD", Value: "/home/agent"}},
		Command:     []string{"/bin/sh", "--unshare-all", "-l"},
	})
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
	want := []string{"--", "/bin/sh", "--unshare-all", "-l"}
	if got := args[len(args)-len(want):]; !reflect.DeepEqual(got, want) {
		t.Fatalf("tail = %q, want %q", got, want)
	}
}

// Compatibility links reach Bubblewrap as symlink creation, not as borrowed
// descriptors: the pair must keep the link text and target, must not consume a
// child FD, and must survive in the production order, where the /usr bind
// comes first. Bubblewrap resolves the link text on access, not when it
// creates the symlink, so neither order is an invariant.
func TestTranslateLendsCompatibilityLinksAsSymlinkArguments(t *testing.T) {
	p, err := sandbox.Build(sandbox.Input{
		Home:    "/profile",
		Project: "/project",
		Mode:    sandbox.Run,
		System: []sandbox.Mount{
			{Kind: sandbox.ReadOnly, Source: "/usr", Target: "/usr"},
			{Kind: sandbox.Symlink, Source: "usr/bin", Target: "/bin"},
		},
		Environment: []sandbox.Entry{{Name: "PWD", Value: "/workspace"}},
		Command:     []string{"/home/agent/bin/agent"},
	})
	if err != nil {
		t.Fatal(err)
	}
	devnull, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	defer devnull.Close()
	var borrowed []string
	args, files, err := Translate(p, func(path string) (*os.File, error) {
		borrowed = append(borrowed, path)
		return devnull, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(borrowed, []string{"/usr", "/profile", "/project"}) || len(files) != 3 {
		t.Fatalf("borrowed = %v, files = %d", borrowed, len(files))
	}
	link := slices.Index(args, "--symlink")
	if link < 0 || link+2 >= len(args) || args[link+1] != "usr/bin" || args[link+2] != "/bin" {
		t.Fatalf("symlink arguments = %q", args)
	}
	if bind := slices.Index(args, "--ro-bind-fd"); bind < 0 {
		t.Fatalf("bind arguments = %q", args)
	}
}

// Two host names can resolve to one path (for example /etc/pki and /etc/ssl).
// Retain deduplicates the descriptor, so Translate lends the same *os.File
// under two child numbers; that must keep working instead of surfacing as a
// vague "Invalid fd" from Bubblewrap on such a host.
func TestTranslateKeepsDuplicateSourceUnderTwoChildFDs(t *testing.T) {
	p, err := sandbox.Build(sandbox.Input{
		Home:    "/profile",
		Project: "/project",
		Mode:    sandbox.Run,
		System: []sandbox.Mount{
			{Kind: sandbox.ReadOnly, Source: "/usr", Target: "/usr"},
			{Kind: sandbox.ReadOnly, Source: "/usr", Target: "/lib64"},
		},
		Environment: []sandbox.Entry{{Name: "PWD", Value: "/workspace"}},
		Command:     []string{"/home/agent/bin/agent"},
	})
	if err != nil {
		t.Fatal(err)
	}
	shared, err := os.CreateTemp(t.TempDir(), "shared")
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Close()
	other, err := os.CreateTemp(t.TempDir(), "other")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	byPath := map[string]*os.File{"/usr": shared, "/profile": other, "/project": other}
	args, files, err := Translate(p, func(path string) (*os.File, error) { return byPath[path], nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 4 || files[0] != shared || files[1] != shared {
		t.Fatalf("duplicate source not lent twice: %d files", len(files))
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"--ro-bind-fd 3 /usr", "--ro-bind-fd 4 /lib64"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %q", want, args)
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
  printf 'bubblewrap 0.11.1\n'
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

// The version probe's own failures belong to the run error: the deadline and
// cancellation branches do not fire, so reporting the value that made the
// probe fail must not report the deadline context, whose Err() is nil there.
func TestOpenReportsVersionProbeFailure(t *testing.T) {
	path := writeScript(t, `exit 3`)
	_, err := open(context.Background(), path, false)
	if err == nil || !strings.Contains(err.Error(), "bubblewrap version: exit status 3") {
		t.Fatalf("probe failure not reported: %v", err)
	}
}

func TestEnvironmentValuesStaySingleArgvElements(t *testing.T) {
	env, err := sandbox.Session("tester", "/bin/sh", "/home/agent", []string{
		"TERM=xterm", "LC_SPACED=a b", "LC_DOLLAR=$(x)", "LC_QUOTE=a'b\"c", "LC_EMPTY=",
		"LC_NEWLINE=line1\nline2", "LC_DASH=--ro-bind / /",
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
	for k, v := range map[string]string{"LC_SPACED": "a b", "LC_DOLLAR": "$(x)", "LC_QUOTE": `a'b"c`, "LC_EMPTY": "", "LC_NEWLINE": "line1\nline2", "LC_DASH": "--ro-bind / /"} {
		if found[k] != v {
			t.Errorf("--setenv %s = %q, want single element %q", k, found[k], v)
		}
	}
}

func TestExecuteRunsBinaryAtRetainedFD(t *testing.T) {
	path := writeScript(t, `if [ "$1" = "--version" ]; then
  printf 'bubblewrap 0.11.1\n'
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

// A refused plan must be reported with its own reason: starting Bubblewrap
// with no arguments replaces "invalid plan"/"bind source" with its help text.
func TestExecuteReportsTranslationRefusal(t *testing.T) {
	tool, err := open(context.Background(), writeScript(t, `printf 'bubblewrap 0.11.1\n'`), false)
	if err != nil {
		t.Fatal(err)
	}
	defer tool.Close()
	status, err := Execute(context.Background(), tool, sandbox.Plan{}, func(string) (*os.File, error) { return nil, errors.New("unused") }, Stdio{Out: io.Discard, Err: io.Discard})
	if status != 1 || err == nil || !strings.Contains(err.Error(), "invalid plan") {
		t.Fatalf("invalid plan = status %d, err %v", status, err)
	}
	status, err = Execute(context.Background(), tool, plan(t, sandbox.Shell), func(string) (*os.File, error) { return nil, errors.New("gone") }, Stdio{Out: io.Discard, Err: io.Discard})
	if status != 1 || err == nil || !strings.Contains(err.Error(), `bind source for "/usr": gone`) {
		t.Fatalf("bind source = status %d, err %v", status, err)
	}
}

// A context cancelled before start must not run the command at all: the
// caller cancelled it, and verify bounds the session with a 30s context.
func TestExecuteDoesNotStartCancelledCommand(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "started")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	status, err := execute(ctx, exec.Command(writeScript(t, "touch "+marker)), Stdio{Out: io.Discard, Err: io.Discard})
	if status != 1 || !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "execution cancelled") {
		t.Fatalf("cancelled start = status %d, err %v", status, err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("command ran despite the cancelled context")
	}
}

// versionOK is a test-only shorthand for the production version check.
func versionOK(s string) bool { return validateVersion(s) == nil }

func TestBadSourcesAndVersion(t *testing.T) {
	if _, _, e := Translate(plan(t, sandbox.Shell), func(string) (*os.File, error) { return nil, errors.New("closed") }); e == nil {
		t.Fatal("accepted bad source")
	}
	for _, v := range []string{"bubblewrap 0.11.1\n", "bubblewrap 0.12.0\n", "bubblewrap 0.13.1", "bubblewrap 1.0.0"} {
		if !versionOK(v) {
			t.Fatal(v)
		}
	}
	for _, v := range []string{"bubblewrap 0.0.99", "bubblewrap 0.9.0", "bubblewrap 0.11.0", "noise 0.12.0", "bubblewrap 0.12", "bubblewrap 9999999999999999999999999.0.0"} {
		if versionOK(v) {
			t.Fatal(v)
		}
	}
	b := limitedBuffer{limit: 2}
	if _, e := b.Write([]byte("abc")); e == nil {
		t.Fatal("unbounded output")
	}
}

func TestMinimumVersionStringMatchesNumeric(t *testing.T) {
	// The message string and the comparison array describe one gate;
	// raising only one of them must fail here.
	m := versionPattern.FindStringSubmatch("bubblewrap " + MinimumVersion)
	if m == nil {
		t.Fatalf("MinimumVersion %q does not parse", MinimumVersion)
	}
	for i, want := range minimumVersion {
		got, err := strconv.ParseUint(m[i+1], 10, 64)
		if err != nil || got != want {
			t.Fatalf("MinimumVersion %q parses to %q, want %v", MinimumVersion, m[1:], minimumVersion)
		}
	}
}

func TestOpenUsesFixedBubblewrapPath(t *testing.T) {
	// Open uses the production path. A missing or unsafe binary must fail
	// naming that path. A too-old binary is a different, valid host state:
	// the version probe names the version instead. Only lookup/validation
	// errors carry the path.
	if _, err := Open(context.Background()); err != nil &&
		strings.Contains(err.Error(), "Bubblewrap executable") &&
		!strings.Contains(err.Error(), Path) {
		t.Fatalf("Open error does not name %q: %v", Path, err)
	}
	// A dangling link must name the configured path too: the raw EvalSymlinks
	// error named only the link target.
	dangling := filepath.Join(t.TempDir(), "bwrap")
	if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), dangling); err != nil {
		t.Fatal(err)
	}
	if _, err := open(context.Background(), dangling, false); err == nil || !strings.Contains(err.Error(), dangling) {
		t.Fatalf("dangling Open error does not name %q: %v", dangling, err)
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

// waitResult decides the whole status table in one place: a child that exited
// on its own keeps its status even when a cancellation raced with its exit,
// while a signalled child under an active cancellation is still reported as
// cancelled.
func TestWaitResultKeepsChildExitOverRacingCancellation(t *testing.T) {
	exitErr := func(t *testing.T, script string) error {
		t.Helper()
		err := exec.Command("/bin/sh", "-c", script).Run()
		if _, ok := err.(*exec.ExitError); !ok {
			t.Fatalf("%q = %v", script, err)
		}
		return err
	}
	seven := exitErr(t, "exit 7")
	killed := exitErr(t, "kill -KILL $$")
	waitFailure := errors.New("wait failed")
	cancelled := "execution cancelled: context deadline exceeded"
	for _, test := range []struct {
		name       string
		waitErr    error
		ctxErr     error
		wantStatus int
		wantErr    string
	}{
		{"success", nil, nil, 0, ""},
		{"success over cancellation", nil, context.DeadlineExceeded, 0, ""},
		{"own exit", seven, nil, 7, ""},
		{"own exit racing cancellation", seven, context.DeadlineExceeded, 7, ""},
		{"signal", killed, nil, 128 + int(syscall.SIGKILL), ""},
		{"signal under cancellation", killed, context.DeadlineExceeded, 1, cancelled},
		{"wait delay", exec.ErrWaitDelay, nil, 0, ""},
		{"wait delay under cancellation", exec.ErrWaitDelay, context.DeadlineExceeded, 1, cancelled},
		{"wait error", waitFailure, nil, 1, "wait failed"},
		{"wait error under cancellation", waitFailure, context.DeadlineExceeded, 1, cancelled},
	} {
		t.Run(test.name, func(t *testing.T) {
			status, err := waitResult(test.waitErr, test.ctxErr)
			if status != test.wantStatus {
				t.Fatalf("status = %d, want %d", status, test.wantStatus)
			}
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if err == nil || err.Error() != test.wantErr {
				t.Fatalf("error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestMarkInheritedFDsCloseOnExec(t *testing.T) {
	explicit, err := os.CreateTemp(t.TempDir(), "explicit")
	if err != nil {
		t.Fatal(err)
	}
	defer explicit.Close()
	unknown, err := os.CreateTemp(t.TempDir(), "unknown")
	if err != nil {
		t.Fatal(err)
	}
	defer unknown.Close()
	for _, file := range []*os.File{explicit, unknown} {
		if _, err := unix.FcntlInt(file.Fd(), unix.F_SETFD, 0); err != nil {
			t.Fatal(err)
		}
	}
	restore, err := markInheritedFDsCloseOnExec()
	if err != nil {
		t.Fatal(err)
	}
	// ExtraFiles entries are marked like any other descriptor: the child
	// receives them through dup2, which clears the flag on its copy, and an
	// unmarked parent copy would reach the child a second time.
	explicitFlags, err := unix.FcntlInt(explicit.Fd(), unix.F_GETFD, 0)
	if err != nil {
		t.Fatal(err)
	}
	if explicitFlags&unix.FD_CLOEXEC == 0 {
		t.Fatal("extra-files descriptor stayed inheritable across exec")
	}
	unknownFlags, err := unix.FcntlInt(unknown.Fd(), unix.F_GETFD, 0)
	if err != nil {
		t.Fatal(err)
	}
	if unknownFlags&unix.FD_CLOEXEC == 0 {
		t.Fatal("inherited descriptor remained open across exec")
	}
	restore()
	for _, file := range []*os.File{explicit, unknown} {
		flags, err := unix.FcntlInt(file.Fd(), unix.F_GETFD, 0)
		if err != nil {
			t.Fatal(err)
		}
		if flags&unix.FD_CLOEXEC != 0 {
			t.Fatalf("descriptor %d flags were not restored", file.Fd())
		}
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
		// Ctrl+\ at a terminal. Forwarded like the rest: unhandled it dumped
		// the launcher's goroutines and exited 2 instead of interrupting.
		{"quit", syscall.SIGQUIT},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			result := filepath.Join(dir, "result")
			ready := filepath.Join(dir, "ready")
			script := writeScript(t, `trap 'printf signal > "$1"; exit 0' INT TERM HUP QUIT
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

// SIGHUP and SIGINT are the signals Go preserves an inherited SIG_IGN for.
// The helper process ignores one before execute, so the parent test process
// never changes its own signal disposition; otherwise a later TestForwardsSignals
// run inherits SIG_IGN and fails under -count=2 or -shuffle.
func TestInheritedIgnoredSignalsStayIgnored(t *testing.T) {
	if os.Getenv("ABX_IGNORED_SIGNAL_HELPER") == "1" {
		var sig syscall.Signal
		switch os.Getenv("ABX_IGNORED_SIGNAL") {
		case "HUP":
			sig = syscall.SIGHUP
		case "INT":
			sig = syscall.SIGINT
		default:
			t.Fatalf("unknown ignored signal %q", os.Getenv("ABX_IGNORED_SIGNAL"))
		}
		signal.Ignore(sig)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		status, err := execute(ctx, exec.CommandContext(ctx, os.Getenv("ABX_SIGNAL_SCRIPT"), os.Getenv("ABX_SIGNAL_READY"), os.Getenv("ABX_SIGNAL_RESULT")), Stdio{})
		if err != nil || status != 0 {
			t.Fatalf("ignored signal helper = %d, %v", status, err)
		}
		return
	}

	for _, tt := range []struct {
		name string
		env  string
		sig  syscall.Signal
	}{
		{"hangup", "HUP", syscall.SIGHUP},
		{"interrupt", "INT", syscall.SIGINT},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			result := filepath.Join(dir, "result")
			ready := filepath.Join(dir, "ready")
			script := writeScript(t, `printf ready > "$1"
sleep 0.5
printf alive > "$2"`)
			helper := exec.Command(os.Args[0], "-test.run=^TestInheritedIgnoredSignalsStayIgnored$")
			helper.Env = append(os.Environ(),
				"ABX_IGNORED_SIGNAL_HELPER=1",
				"ABX_IGNORED_SIGNAL="+tt.env,
				"ABX_SIGNAL_SCRIPT="+script,
				"ABX_SIGNAL_READY="+ready,
				"ABX_SIGNAL_RESULT="+result,
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
					t.Fatal("ignored-signal helper did not become ready")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err := helper.Process.Signal(tt.sig); err != nil {
				_ = helper.Process.Kill()
				_ = helper.Wait()
				t.Fatal(err)
			}
			if err := helper.Wait(); err != nil {
				t.Fatalf("ignored-signal helper: %v", err)
			}
			data, readErr := os.ReadFile(result)
			if readErr != nil || string(data) != "alive" {
				t.Fatalf("ignored signal result = %q, %v", data, readErr)
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
  printf 'bubblewrap 0.11.1\n'
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
  printf 'bubblewrap 0.11.1\n'
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
  printf 'bubblewrap 0.11.1\n'
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
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'bubblewrap 0.11.1\\n'\n"), 0o600); err != nil {
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
  printf 'bubblewrap 0.11.1\n'
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

// A retained binary that disappears between checks must surface the stat
// failure itself, not a comparison against a missing object.
func TestRevalidateReportsVanishedBinary(t *testing.T) {
	path := writeScript(t, `if [ "$1" = "--version" ]; then
  printf 'bubblewrap 0.11.1\n'
  exit 0
fi
exit 23`)
	tool, err := open(context.Background(), path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer tool.Close()
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = tool.Revalidate(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("vanished binary = %v", err)
	}
}

func TestExecuteRevalidatesToolBeforeLendingSources(t *testing.T) {
	path := writeScript(t, `if [ "$1" = "--version" ]; then
  printf 'bubblewrap 0.11.1\n'
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
  printf 'bubblewrap 0.11.1\n'
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
		{"bubblewrap 0.11.0\n", "too old; requires >= 0.11.1"},
		{"", "could not parse Bubblewrap version"},
		{"unexpected output\n", "could not parse Bubblewrap version"},
		{"bubblewrap 9999999999999999999999999.0.0", "could not parse Bubblewrap version"},
	} {
		if err := validateVersion(tc.input); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("version %q: %v", tc.input, err)
		}
	}
}
