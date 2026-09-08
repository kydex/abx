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
	"strconv"
	"syscall"
	"time"

	"github.com/kydex/abx/internal/sandbox"
	"golang.org/x/sys/unix"
)

const Path = "/usr/bin/bwrap"
const MinimumVersion = "0.12.0"

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
	if !os.SameFile(t.info, info) || info.Mode().Perm()&0022 != 0 || info.Mode().Perm()&0111 == 0 {
		return errors.New("Bubblewrap changed")
	}
	return nil
}
func Open(ctx context.Context) (*Tool, error) { return open(ctx, Path, true) }
func open(ctx context.Context, path string, system bool) (tool *Tool, err error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Open(resolved, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), resolved)
	defer func() {
		if err != nil {
			err = errors.Join(err, f.Close())
		}
	}()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Mode().Perm()&0022 != 0 || !ok || system && st.Uid != 0 {
		return nil, errors.New("unsafe Bubblewrap executable")
	}
	if err = unix.Access(resolved, unix.X_OK); err != nil {
		return nil, err
	}
	t := &Tool{f, info, resolved}
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// Execute the retained object, not a second lookup after validation.
	cmd := exec.CommandContext(check, "/proc/self/fd/3", "--version")
	cmd.ExtraFiles = []*os.File{f}
	cmd.Env = []string{"LC_ALL=C"}
	cmd.Dir = "/"
	cmd.WaitDelay = time.Second
	output := &limitedBuffer{limit: 4096}
	cmd.Stdout = output
	cmd.Stderr = output
	if err = cmd.Run(); err != nil {
		return nil, fmt.Errorf("Bubblewrap version: %w", err)
	}
	if !versionOK(output.text) {
		return nil, fmt.Errorf("Bubblewrap requires >= %s", MinimumVersion)
	}
	return t, nil
}
func versionOK(s string) bool {
	m := regexp.MustCompile(`^bubblewrap ([0-9]+)\.([0-9]+)\.([0-9]+)\s*$`).FindStringSubmatch(s)
	if m == nil {
		return false
	}
	nums := [3]uint64{}
	for i := range nums {
		n, e := strconv.ParseUint(m[i+1], 10, 64)
		if e != nil {
			return false
		}
		nums[i] = n
	}
	return nums[0] > 0 || nums[1] >= 12
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

// Translate lends each retained bind source in exact mount order. Child fd numbers are local here.
func Translate(plan sandbox.Plan, source func(string) (*os.File, error)) ([]string, []*os.File, error) {
	if err := plan.Validate(); err != nil {
		return nil, nil, fmt.Errorf("invalid plan: %w", err)
	}
	args := []string{"--unshare-user", "--disable-userns", "--unshare-pid", "--unshare-ipc", "--unshare-uts", "--hostname", "abx", "--die-with-parent", "--clearenv"}
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
				return nil, nil, err
			}
			if f == nil {
				return nil, nil, errors.New("nil source file")
			}
			flag := "--ro-bind-fd"
			if m.Kind == sandbox.Writable {
				flag = "--bind-fd"
			}
			args = append(args, flag, strconv.Itoa(3+len(files)), m.Target)
			files = append(files, f)
		case sandbox.Symlink:
			args = append(args, "--symlink", m.Source, m.Target)
		case sandbox.Directory:
			args = append(args, "--dir", m.Target)
		case sandbox.Tmpfs:
			args = append(args, "--tmpfs", m.Target)
		case sandbox.Proc:
			args = append(args, "--proc", m.Target)
		case sandbox.Dev:
			args = append(args, "--dev", m.Target)
		default:
			return nil, nil, errors.New("unsupported mount")
		}
	}
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
	path := "/proc/self/fd/" + strconv.Itoa(3+len(files))
	// #nosec G204 -- this descriptor holds validated /usr/bin/bwrap; argv is not shell-parsed.
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Args[0] = Path
	cmd.ExtraFiles = append(files, t.file)
	return execute(ctx, cmd, stdio)
}
func execute(ctx context.Context, cmd *exec.Cmd, stdio Stdio) (int, error) {
	if err := ctx.Err(); err != nil {
		return 1, err
	}
	cmd.Env = []string{}
	cmd.Dir = "/"
	cmd.Stdin = stdio.In
	cmd.Stdout = stdio.Out
	cmd.Stderr = stdio.Err
	// Bound waits if a descendant retains a pipe used by a test or redirected writer.
	cmd.WaitDelay = time.Second
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
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
	err := cmd.Wait()
	close(done)
	forwardErr := <-forwarded
	if forwardErr != nil {
		return 1, errors.Join(err, forwardErr)
	}
	if err == nil {
		return 0, nil
	}
	if ctx.Err() != nil {
		return 1, fmt.Errorf("execution cancelled: %w", ctx.Err())
	}
	var exited *exec.ExitError
	if !errors.As(err, &exited) {
		return 1, err
	}
	status, ok := exited.Sys().(syscall.WaitStatus)
	if !ok {
		return 1, errors.New("unknown child status")
	}
	if status.Signaled() {
		return 128 + int(status.Signal()), nil
	}
	return status.ExitStatus(), nil
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
