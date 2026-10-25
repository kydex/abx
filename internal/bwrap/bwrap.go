// Package bwrap translates a plan and owns one child process.
package bwrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"syscall"
	"time"

	"github.com/kydex/abx/internal/sandbox"
	"golang.org/x/sys/unix"
)

const (
	Path           = "/usr/bin/bwrap"
	MinimumVersion = "0.11.1"
)

// bwrapProbeTimeout bounds the retained binary's version probe. The diagnostic
// text is built from it, and the timeout test shortens it: waiting out the
// real deadline only makes the suite slower, not more convincing.
var bwrapProbeTimeout = 5 * time.Second

type Stdio struct {
	In       io.Reader
	Out, Err io.Writer
}

// Tool is a checked fixed system executable. It owns a retained descriptor.
type Tool struct {
	file *os.File
	info os.FileInfo
	path string
}

func (t *Tool) Close() error {
	if t == nil || t.file == nil {
		return nil
	}
	f := t.file
	t.file = nil
	return f.Close()
}

func (t *Tool) Revalidate() error {
	if t == nil || t.file == nil {
		return errors.New("tool closed")
	}
	info, err := os.Stat(t.path)
	if err != nil {
		return err
	}
	if !os.SameFile(t.info, info) {
		return fmt.Errorf("bubblewrap changed: %q", t.path)
	}
	if reason := unsafeModeReason(info.Mode()); reason != "" {
		return fmt.Errorf("bubblewrap changed: %q (%s)", t.path, reason)
	}
	return nil
}

// unsafeModeReason reports why mode bits fail the executable policy shared
// by open and Revalidate. open additionally requires a regular file,
// metadata, root ownership for system paths, and access; Revalidate
// additionally requires identity via os.SameFile. Neither subsumes the
// other by design: rechecking ownership would add nothing (changing an
// inode owner requires privileges), while entry must verify it once.
//
// This duplicates the trusted-object mode rule that internal/host applies to
// /etc/passwd and the login shell. The duplication is deliberate: bwrap
// executes and must not depend on host, and the two rules are not the same --
// this one adds the execute bit, because its objects are executed. Each package
// pins only its own copy with its own table; the tables are independent, and
// nothing fails here when the host copy changes, so a change to the shared half
// belongs in both copies and both tables in the same commit.
func unsafeModeReason(mode os.FileMode) string {
	switch {
	case mode.Perm()&0o111 == 0:
		return "not executable"
	case mode.Perm()&0o022 != 0:
		return "writable by group or other"
	case mode&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0:
		return "has setuid, setgid, or sticky bit"
	default:
		return ""
	}
}

func Open(ctx context.Context) (*Tool, error) { return open(ctx, Path, true) }
func open(ctx context.Context, path string, system bool) (tool *Tool, err error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, fmt.Errorf("resolve Bubblewrap executable %q: %w", path, err)
	}
	fd, err := unix.Open(resolved, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open Bubblewrap executable %q: %w", resolved, err)
	}
	f := os.NewFile(uintptr(fd), resolved)
	defer func() {
		if err != nil {
			err = errors.Join(err, f.Close())
		}
	}()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat Bubblewrap executable %q: %w", resolved, err)
	}
	st := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("unsafe Bubblewrap executable %q: not a regular file", resolved)
	}
	if reason := unsafeModeReason(info.Mode()); reason != "" {
		return nil, fmt.Errorf("unsafe Bubblewrap executable %q: %s", resolved, reason)
	}
	if system && st.Uid != 0 {
		return nil, fmt.Errorf("unsafe Bubblewrap executable %q: not owned by root (uid %d)", resolved, st.Uid)
	}
	if err = unix.Access(resolved, unix.X_OK); err != nil {
		return nil, fmt.Errorf("access Bubblewrap executable %q: %w", resolved, err)
	}
	t := &Tool{f, info, resolved}
	check, cancel := context.WithTimeout(ctx, bwrapProbeTimeout)
	defer cancel()
	// Execute the retained object, not a second lookup after validation.
	// It is the only extra file, so it starts at firstChildFD.
	// #nosec G204 -- the path is the firstChildFD constant, not external input.
	cmd := exec.CommandContext(check, "/proc/self/fd/"+strconv.Itoa(firstChildFD), "--version")
	cmd.ExtraFiles = []*os.File{f}
	cmd.Env = []string{"LC_ALL=C"}
	cmd.Dir = "/"
	// The probe is the one child nothing else ties to the launcher's life:
	// give it a parent-death signal so a launcher killed mid-probe cannot
	// leave a wedged Bubblewrap behind. The session child gets its
	// containment from Bubblewrap's --die-with-parent after sandbox setup.
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	cmd.WaitDelay = time.Second
	output := &limitedBuffer{limit: 4096}
	cmd.Stdout = output
	cmd.Stderr = output
	restoreFDs, err := markInheritedFDsCloseOnExec()
	if err != nil {
		return nil, err
	}
	defer restoreFDs()
	if err = cmd.Run(); err != nil {
		// check is the probe deadline; ctx is the caller's context. The
		// probe deadline is what kills a wedged binary, so report it as such
		// instead of the misleading "signal: killed", and keep caller
		// cancellation distinct from the probe budget. Everything else is the
		// run error itself.
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return nil, fmt.Errorf("bubblewrap version probe stopped by the caller deadline: %w", ctx.Err())
		case ctx.Err() != nil:
			return nil, fmt.Errorf("bubblewrap version probe cancelled: %w", ctx.Err())
		case errors.Is(check.Err(), context.DeadlineExceeded):
			return nil, fmt.Errorf("bubblewrap version probe timed out after %s", bwrapProbeTimeout)
		default:
			return nil, fmt.Errorf("bubblewrap version: %w", err)
		}
	}
	if err := validateVersion(output.text); err != nil {
		return nil, err
	}
	return t, nil
}

// markInheritedFDsCloseOnExec marks every inherited descriptor above the
// standard streams close-on-exec and returns a restore. ExtraFiles entries get
// the same treatment: os/exec reinjects them into the child with the flag
// cleared, so only the parent's copy closes at exec, while leaving them
// inheritable made the child see each entry twice -- under its inherited
// number and under the injected one.
func markInheritedFDsCloseOnExec() (func(), error) {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return nil, fmt.Errorf("read inherited file descriptors: %w", err)
	}
	// Restore by descriptor number and identity: after cmd.Wait the numbers
	// may have been reused, so a plain F_SETFD would clear CLOEXEC on a
	// foreign descriptor.
	type savedFlag struct {
		fd   int
		info os.FileInfo
		flag int
	}
	var saved []savedFlag
	// restore runs either from the caller's defer on success or from an error
	// branch below before returning -- never from both for one call, because
	// the error branches are the paths that skip the caller's defer.
	restore := func() {
		for _, s := range saved {
			if cur, err := os.Stat(fmt.Sprintf("/proc/self/fd/%d", s.fd)); err != nil || !os.SameFile(cur, s.info) {
				continue
			}
			_, _ = unix.FcntlInt(uintptr(s.fd), unix.F_SETFD, s.flag)
		}
	}
	for _, entry := range entries {
		fd, parseErr := strconv.Atoi(entry.Name())
		if parseErr != nil || fd < 3 {
			continue
		}
		flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
		if err != nil {
			if errors.Is(err, unix.EBADF) {
				continue
			}
			restore()
			return nil, fmt.Errorf("read file descriptor %d flags: %w", fd, err)
		}
		if flags&unix.FD_CLOEXEC != 0 {
			continue
		}
		info, err := os.Stat(fmt.Sprintf("/proc/self/fd/%d", fd))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			restore()
			return nil, fmt.Errorf("stat file descriptor %d: %w", fd, err)
		}
		if _, err = unix.FcntlInt(uintptr(fd), unix.F_SETFD, unix.FD_CLOEXEC); err != nil {
			if errors.Is(err, unix.EBADF) {
				continue
			}
			restore()
			return nil, fmt.Errorf("mark file descriptor %d close-on-exec: %w", fd, err)
		}
		saved = append(saved, savedFlag{fd: fd, info: info, flag: flags})
	}
	return restore, nil
}

var versionPattern = regexp.MustCompile(`^bubblewrap ([0-9]+)\.([0-9]+)\.([0-9]+)\s*$`)

// minimumVersion matches MinimumVersion above; kept numeric for comparison.
var minimumVersion = [3]uint64{0, 11, 1}

func validateVersion(s string) error {
	m := versionPattern.FindStringSubmatch(s)
	if m == nil {
		return fmt.Errorf("could not parse Bubblewrap version from %q", s)
	}
	nums := [3]uint64{}
	for i := range nums {
		n, e := strconv.ParseUint(m[i+1], 10, 64)
		if e != nil {
			return fmt.Errorf("could not parse Bubblewrap version from %q", s)
		}
		nums[i] = n
	}
	for i := range nums {
		if nums[i] > minimumVersion[i] {
			break
		}
		if nums[i] < minimumVersion[i] {
			return fmt.Errorf("bubblewrap %s.%s.%s is too old; requires >= %s", m[1], m[2], m[3], MinimumVersion)
		}
	}
	return nil
}

type limitedBuffer struct {
	text  string
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(b.text)+len(p) > b.limit {
		return 0, errors.New("tool output exceeded limit")
	}
	b.text += string(p)
	return len(p), nil
}

// firstChildFD is where retained bind sources start in the child: 0-2 are
// the standard streams.
const firstChildFD = 3

// Translate lends each retained bind source in exact mount order. Child fd numbers are local here.
func Translate(plan sandbox.Plan, source func(string) (*os.File, error)) ([]string, []*os.File, error) {
	if err := plan.Validate(); err != nil {
		return nil, nil, fmt.Errorf("invalid plan: %w", err)
	}
	args := []string{
		"--unshare-user", "--disable-userns", "--unshare-pid", "--unshare-ipc",
		"--unshare-uts", "--hostname", "abx", "--die-with-parent", "--clearenv",
	}
	for _, e := range plan.Environment() {
		args = append(args, "--setenv", e.Name, e.Value)
	}
	args = append(args, "--chdir", plan.WorkingDirectory())
	var files []*os.File
	for _, m := range plan.Mounts() {
		if m.Mode != 0 {
			args = append(args, "--perms", fmt.Sprintf("%04o", m.Mode))
		}
		switch m.Kind {
		case sandbox.ReadOnly, sandbox.Writable:
			f, err := source(m.Source)
			if err != nil {
				return nil, nil, fmt.Errorf("bind source for %q: %w", m.Target, err)
			}
			if f == nil {
				return nil, nil, fmt.Errorf("nil bind source for %q", m.Target)
			}
			flag := "--ro-bind-fd"
			if m.Kind == sandbox.Writable {
				flag = "--bind-fd"
			}
			args = append(args, flag, strconv.Itoa(firstChildFD+len(files)), m.Target)
			files = append(files, f)
		case sandbox.Symlink:
			args = append(args, "--symlink", m.Source, m.Target)
		case sandbox.Directory:
			args = append(args, "--dir", m.Target)
		case sandbox.Tmpfs:
			args = append(args, "--size", strconv.FormatUint(m.Size, 10), "--tmpfs", m.Target)
		case sandbox.Proc:
			args = append(args, "--proc", m.Target)
		case sandbox.Dev:
			args = append(args, "--dev", m.Target)
		default:
			return nil, nil, fmt.Errorf("unsupported mount kind %d for %q", m.Kind, m.Target)
		}
	}
	// "--" ends option parsing unconditionally: a command that begins with
	// "-" must stay the command even if Bubblewrap's own parsing changes.
	args = append(args, "--")
	args = append(args, plan.Command()...)
	return args, files, nil
}

func Execute(ctx context.Context, t *Tool, plan sandbox.Plan, source func(string) (*os.File, error), stdio Stdio) (int, error) {
	if err := t.Revalidate(); err != nil {
		return 1, err
	}
	args, files, err := Translate(plan, source)
	if err != nil {
		return 1, err
	}
	path := "/proc/self/fd/" + strconv.Itoa(firstChildFD+len(files))
	// #nosec G204 -- this descriptor holds validated /usr/bin/bwrap; argv is not shell-parsed.
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Args[0] = Path
	cmd.ExtraFiles = append(slices.Clone(files), t.file)
	return execute(ctx, cmd, stdio)
}

func execute(ctx context.Context, cmd *exec.Cmd, stdio Stdio) (int, error) {
	// verify checks the deadline before Execute, but revalidation and
	// translation run in between; name the expiry here as the guide promises
	// instead of returning the raw context error.
	if err := ctx.Err(); err != nil {
		return 1, fmt.Errorf("execution cancelled: %w", err)
	}
	cmd.Env = []string{}
	cmd.Dir = "/"
	cmd.Stdin = stdio.In
	cmd.Stdout = stdio.Out
	cmd.Stderr = stdio.Err
	// Bound waits if a descendant retains a pipe used by a test or redirected writer.
	cmd.WaitDelay = time.Second
	signals := make(chan os.Signal, 4)
	// SIGQUIT is included so a terminal Ctrl+\ interrupts the session like
	// Ctrl+C instead of dumping the launcher's goroutines and exiting 2, a
	// status docs/usage.md documents as a CLI syntax error. Unhandled it also
	// leaves the sandbox running: the agent never learns the session is over.
	// Buffer of 4 holds one of each subscribed signal; a burst of repeats
	// while a previous one is still being forwarded can drop a duplicate,
	// since os/signal discards into a full buffer instead of blocking.
	//
	// SIGHUP and SIGINT are the signals for which Go preserves an inherited
	// SIG_IGN. Notify would replace that disposition, so `nohup abx run ... &`
	// (or a shell's `trap '' HUP`) would die from the hangup it asked to
	// ignore. For SIGTERM and SIGQUIT the Go runtime installs its own handler
	// before main, so an inherited SIG_IGN is no longer visible here; those
	// two are always subscribed and forwarded.
	for _, s := range []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT} {
		if !signal.Ignored(s) {
			signal.Notify(signals, s)
		}
	}
	defer signal.Stop(signals)
	restoreFDs, err := markInheritedFDsCloseOnExec()
	if err != nil {
		return 1, err
	}
	defer restoreFDs()
	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("start Bubblewrap: %w", err)
	}
	done := make(chan struct{})
	forwarded := make(chan error, 1)
	go func() {
		for {
			select {
			case <-done:
				forwarded <- nil
				return
			case s := <-signals:
				if err := forwardSignal(s, cmd.Process.Signal, cmd.Process.Kill); err != nil {
					forwarded <- err
					return
				}
			}
		}
	}()
	err = cmd.Wait()
	close(done)
	forwardErr := <-forwarded
	if forwardErr != nil {
		return 1, errors.Join(err, forwardErr)
	}
	return waitResult(err, ctx.Err())
}

// waitResult turns the outcome of cmd.Wait into a process status. A child that
// exited on its own keeps its real status even when a cancellation raced with
// the exit; only a termination attributable to the cancelled context is
// reported as cancelled. The signals a child sees while a cancellation is
// active are the cancellation's own: the launcher forwards what it receives
// (see forwardSignal), and with no new session or separate process group the
// terminal delivers its copy to the payload too. A signalled child is
// therefore reported as cancelled, not as a crash.
func waitResult(waitErr, ctxErr error) (int, error) {
	var exited *exec.ExitError
	if errors.As(waitErr, &exited) {
		if status := exited.Sys().(syscall.WaitStatus); !status.Signaled() {
			// The child finished on its own; the cancellation lost the race
			// and must not overwrite what it actually did.
			return status.ExitStatus(), nil
		}
	}
	if waitErr == nil {
		return 0, nil
	}
	if ctxErr != nil {
		return 1, fmt.Errorf("execution cancelled: %w", ctxErr)
	}
	// A successful child that leaves its I/O pipes to an orphaned descendant
	// reports ErrWaitDelay instead of nil once WaitDelay expires. That is the
	// child's real exit, not a launcher failure.
	if errors.Is(waitErr, exec.ErrWaitDelay) {
		return 0, nil
	}
	if exited == nil {
		return 1, waitErr
	}
	return 128 + int(exited.Sys().(syscall.WaitStatus).Signal()), nil
}

// forwardSignal preserves both failures; an already-reaped child needs no signal.
func forwardSignal(s os.Signal, signal func(os.Signal) error, kill func() error) error {
	err := signal(s)
	if err == nil || errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	result := fmt.Errorf("forward signal: %w", err)
	if killErr := kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
		result = errors.Join(result, fmt.Errorf("kill after signal failure: %w", killErr))
	}
	return result
}
