package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
	"unicode/utf8"

	"github.com/kydex/abx/internal/bwrap"
	"github.com/kydex/abx/internal/cli"
	"github.com/kydex/abx/internal/host"
	"github.com/kydex/abx/internal/probe"
	"github.com/kydex/abx/internal/sandbox"
	"golang.org/x/sys/unix"
)

func runVerify(
	ctx context.Context, in *host.Inputs, home string, env []string, stdio bwrap.Stdio,
	openTool func(context.Context) (*bwrap.Tool, error),
	execute func(context.Context, *bwrap.Tool, sandbox.Plan, func(string) (*os.File, error), bwrap.Stdio) (int, error),
) (status int, result error) {
	input, err := sessionInput(in, env)
	if err != nil {
		return 1, err
	}
	input.Command = []string{sandbox.ProbePath, "__probe", "{}"}
	// The first build validates the shape before host resources exist; the
	// second build below runs after the sentinel and payload do.
	if _, err = sandbox.BuildVerification(input); err != nil {
		return 1, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	phase := "opening the tool"
	tool, err := openTool(ctx)
	if err != nil {
		return 1, cancellationError(ctx, phase, err)
	}
	defer finishClose(&status, &result, tool.Close)
	phase = "host observation"
	// /proc/self/exe denotes this running binary, even after its installed path is replaced.
	// O_PATH keeps verify working when the binary is executable but not readable.
	fd, err := unix.Open("/proc/self/exe", unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return 1, cancellationError(ctx, phase, err)
	}
	self := os.NewFile(uintptr(fd), "/proc/self/exe")
	if self == nil {
		_ = unix.Close(fd)
		return 1, errors.New("open /proc/self/exe: invalid descriptor")
	}
	defer finishClose(&status, &result, self.Close)
	expected, err := observeHost(home, input)
	if err != nil {
		return 1, cancellationError(ctx, phase, err)
	}
	phase = "probe preparation"
	files := &probeFiles{}
	defer finishClose(&status, &result, files.Close)
	expected.Sentinel, err = files.create(home)
	if err != nil {
		return 1, cancellationError(ctx, phase, err)
	}
	raw, err := verifyInput(expected)
	if err != nil {
		return 1, cancellationError(ctx, phase, err)
	}
	phase = "sandbox preparation"
	input.Command[2] = string(raw)
	plan, err := sandbox.BuildVerification(input)
	if err != nil {
		return 1, cancellationError(ctx, phase, err)
	}
	if err = in.Prepare(); err != nil {
		return 1, cancellationError(ctx, phase, err)
	}
	if err = in.Revalidate(); err != nil {
		return 1, cancellationError(ctx, phase, err)
	}
	// The host-side phases above are not interruptible, so the deadline may
	// already have expired when execute is about to start; its own context
	// check would return the raw context error. Report the timeout here with
	// the same text the guide names.
	phase = "starting the sandbox"
	if err := ctx.Err(); err != nil {
		return 1, fmt.Errorf("execution cancelled during %s: %w", phase, err)
	}
	phase = "running the sandbox"
	status, err = execute(ctx, tool, plan, func(path string) (*os.File, error) {
		if path == "/proc/self/exe" {
			return self, nil
		}
		return in.SourceFile(path)
	}, stdio)
	if err != nil {
		return 1, cancellationError(ctx, phase, err)
	}
	if status != 0 {
		// Execute reports a child killed by a signal as 128 plus the signal
		// number, not as an exit code. The note only applies to those
		// statuses; an ordinary exit 1 is the common FAIL result and must not
		// send the reader looking for a signal.
		if status >= 128 {
			return 1, fmt.Errorf("verification process ended with status %d, where a kill by signal counts as 128 plus its signal number", status)
		}
		return 1, fmt.Errorf("verification process ended with status %d", status)
	}
	// Final success is printed only after outer resource and temporary-file cleanup.
	return 0, nil
}

func cancellationError(ctx context.Context, phase string, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
		return fmt.Errorf("execution cancelled during %s: %w", phase, ctxErr)
	}
	return err
}

// validProbePaths keeps JSON encoding from replacing invalid UTF-8 bytes with
// U+FFFD and making the probe inspect a different path than the host did.
func validProbePaths(expected probe.Expected) error {
	paths := []string{expected.Home, expected.Sentinel}
	for p := range expected.Objects {
		paths = append(paths, p)
	}
	paths = append(paths, expected.ReadOnly...)
	for _, p := range paths {
		if !utf8.ValidString(p) {
			return fmt.Errorf("verification path is not valid UTF-8: %q", p)
		}
	}
	return nil
}

// verifyInput serializes the host observations and refuses a payload the probe
// cannot accept before the sandbox starts. The probe reads at most
// probe.MaxInputBytes; a payload past that bound would either be refused by the
// probe itself or, past the kernel's per-argument limit, fail inside execve as
// a raw E2BIG. The per-value environment limit does not bound the sum, and
// JSON escaping can expand values further.
func verifyInput(expected probe.Expected) ([]byte, error) {
	if err := validProbePaths(expected); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(expected)
	if err != nil {
		return nil, err
	}
	if len(raw) > probe.MaxInputBytes {
		return nil, fmt.Errorf("verification input is %d bytes, above the %d byte probe limit (environment included)", len(raw), probe.MaxInputBytes)
	}
	return raw, nil
}

func observeHost(home string, input sandbox.Input) (probe.Expected, error) {
	e := probe.Expected{Home: home, Objects: map[string]probe.Identity{}, Skills: input.Skills != ""}
	for _, p := range []string{home, "/tmp", "/var/tmp", "/run", "/dev", "/proc", "/dev/shm"} {
		id, err := probe.Identify(p)
		if err != nil {
			return e, err
		}
		e.Objects[p] = id
	}
	var err error
	e.Profile, err = probe.Identify(input.Home)
	if err != nil {
		return e, err
	}
	e.Project, err = probe.Identify(input.Project)
	if err != nil {
		return e, err
	}
	e.Namespaces = make(map[string]string)
	for _, name := range probe.NamespaceNames() {
		e.Namespaces[name], err = os.Readlink("/proc/self/ns/" + name)
		if err != nil {
			return e, fmt.Errorf("observe host %s namespace: %w", name, err)
		}
	}
	for _, v := range input.Environment {
		e.Environment = append(e.Environment, v.Name+"="+v.Value)
	}
	for _, m := range input.System {
		if m.Kind == sandbox.ReadOnly {
			e.ReadOnly = append(e.ReadOnly, m.Target)
		}
	}
	if e.Skills {
		e.ReadOnly = append(e.ReadOnly, sandbox.SkillsPath)
	}
	e.ReadOnly = append(e.ReadOnly, sandbox.ProbePath)
	return e, nil
}

type probeFile struct {
	path string
	info os.FileInfo
}
type probeFiles struct{ files []probeFile }

// create is the one session object made by name rather than through a retained
// descriptor: the sentinel lives in the account home, which is deliberately not
// retained. Its identity is captured now and checked again in Close.
func (p *probeFiles) create(home string) (string, error) {
	f, err := os.CreateTemp(home, ".abx-verify-*")
	if err != nil {
		return "", err
	}
	info, err := f.Stat()
	if err != nil {
		// Register only after the identity sample exists: Close must never
		// delete a path it cannot compare against the created file. The
		// possible residue is covered by the best-effort cleanup rule.
		return "", errors.Join(err, f.Close())
	}
	p.files = append(p.files, probeFile{path: f.Name(), info: info})
	if err = f.Close(); err != nil {
		return "", err
	}
	return f.Name(), nil
}

func (p *probeFiles) Close() error {
	var result error
	for i := len(p.files) - 1; i >= 0; i-- {
		f := p.files[i]
		info, err := os.Lstat(f.path)
		if err == nil && f.info != nil && !os.SameFile(info, f.info) {
			err = errors.New("temporary probe object was replaced")
		}
		if err == nil {
			err = os.Remove(f.path)
			// Lstat saw the file, so ENOENT here means it disappeared between
			// the two syscalls. That is the one cleanup race accepted as clean;
			// a missing sentinel before Lstat still fails above.
			if errors.Is(err, os.ErrNotExist) {
				err = nil
			}
		}
		if err != nil {
			result = errors.Join(result, fmt.Errorf("cleanup %q: %w", f.path, err))
		}
	}
	p.files = nil
	return result
}

// Keep success reporting after runSession's descriptor and probe cleanup. A
// failed write must not read as a failed verification: name what actually
// happened before the write error.
func verifySuccess(out io.Writer) error {
	if _, err := io.WriteString(out, "Verification passed; temporary probe files removed.\n"); err != nil {
		return fmt.Errorf("verification passed, but its report could not be written: %w", err)
	}
	return nil
}

// reportVerificationOutcome decides whether the success line belongs to this
// run. It is separate from Run so the decision is testable without a sandbox;
// the mutation that dropped the print used to leave the suite green.
func reportVerificationOutcome(kind cli.Kind, status int, out io.Writer) error {
	if kind != cli.Verify || status != 0 {
		return nil
	}
	return verifySuccess(out)
}
