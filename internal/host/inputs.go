// Package host resolves and owns the host objects used by one invocation.
package host

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/kydex/abx/internal/profile"
	"golang.org/x/sys/unix"
)

// Source is an observed system bind or compatibility symlink.
type Source struct{ Path, Target, Link string }
type object struct {
	file *os.File
	info os.FileInfo
}

// Mode selects host resources and the session command.
type Mode uint8

const (
	Run Mode = iota + 1
	Shell
	Work
)

// Inputs owns all retained source descriptors. Close releases them once.
// Values are private; no second command-specific ownership hierarchy exists.
type Inputs struct {
	mode                                     Mode
	account                                  Account
	paths                                    Paths
	home, project, skills, executable, shell string
	command                                  []string
	system                                   []Source
	objects                                  map[string]object
	links                                    map[string]string
}

func (in *Inputs) Mode() Mode        { return in.mode }
func (in *Inputs) Home() string      { return in.home }
func (in *Inputs) Project() string   { return in.project }
func (in *Inputs) Skills() string    { return in.skills }
func (in *Inputs) Shell() string     { return in.shell }
func (in *Inputs) Username() string  { return in.account.Username }
func (in *Inputs) Command() []string { return append([]string(nil), in.command...) }
func (in *Inputs) System() []Source  { return append([]Source(nil), in.system...) }

// SourceFile lends a descriptor until Inputs.Close. The borrower must not close it.
func (in *Inputs) SourceFile(path string) (*os.File, error) {
	o, ok := in.objects[path]
	if !ok || o.file == nil {
		return nil, fmt.Errorf("source is not retained: %q", path)
	}
	return o.file, nil
}
func (in *Inputs) Close() error {
	var result error
	for path, o := range in.objects {
		result = errors.Join(result, o.file.Close())
		delete(in.objects, path)
	}
	return result
}
func (in *Inputs) retain(path string) error {
	if _, ok := in.objects[path]; ok {
		return nil
	}
	fd, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open source %q: %w", path, err)
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil {
		return errors.Join(err, f.Close())
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return errors.Join(fmt.Errorf("unsupported source %q", path), f.Close())
	}
	in.objects[path] = object{f, info}
	return nil
}
func (in *Inputs) private(path string) error {
	if err := in.retain(path); err != nil {
		return err
	}
	return privateInfo(path, in.objects[path].info, in.account.UID)
}
func privateInfo(path string, info os.FileInfo, uid int) error {
	if !info.IsDir() || info.Mode().Perm() != 0700 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return fmt.Errorf("private directory %q must have exact mode 0700", path)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int64(st.Uid) != int64(uid) {
		return fmt.Errorf("private directory %q has wrong owner", path)
	}
	return nil
}

// Resolve selects session inputs. Only run requires an agent; only shell omits the project.
func Resolve(account Account, paths Paths, name, project string, mode Mode, args []string) (in *Inputs, err error) {
	return resolve(account, paths, name, project, mode, true, args)
}

// ResolveVerification uses the run inputs without requiring an installed agent.
func ResolveVerification(account Account, paths Paths, name, project string) (*Inputs, error) {
	return resolve(account, paths, name, project, Run, false, nil)
}

func resolve(account Account, paths Paths, name, project string, mode Mode, needAgent bool, args []string) (in *Inputs, err error) {
	if mode != Run && mode != Shell && mode != Work {
		return nil, errors.New("invalid session mode")
	}
	if mode != Run && len(args) != 0 {
		return nil, errors.New("only run sessions accept arguments")
	}
	if err = profile.ValidateName(name); err != nil {
		return nil, err
	}
	in = profileInputs(account, paths, name)
	in.mode = mode
	owner := in
	defer func() {
		if err != nil {
			err = errors.Join(err, owner.Close())
			in = nil
		}
	}()
	if err = in.readProfile(); err != nil {
		return nil, err
	}
	if err = in.discoverSystem("/"); err != nil {
		return nil, err
	}
	selection := ResolveShell(account.UID)
	if selection.Path == "" {
		return nil, errors.New(selection.Reason)
	}
	in.shell = selection.Path
	if mode != Run {
		in.command = selection.Argv
	}
	if mode != Shell {
		in.project, err = ResolveProject(project, account.Home, paths.Root)
		if err != nil {
			return nil, err
		}
		if err = in.retain(in.project); err != nil {
			return nil, err
		}
		if mode == Run && needAgent {
			in.executable, err = resolveAgent(in.home, name)
			if err != nil {
				return nil, err
			}
			in.command = append([]string{in.executable}, args...)
		}
	}
	if err = in.observeSkills(); err != nil {
		return nil, err
	}
	return in, nil
}

func resolveAgent(home, name string) (string, error) {
	var invalid error
	for _, dir := range []string{".local/bin", ".bun/bin", "bin"} {
		p := filepath.Join(home, dir, name)
		info, err := os.Stat(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			if invalid == nil {
				invalid = fmt.Errorf("inspect profile executable %q: %w", p, err)
			}
			continue
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || unix.Access(p, unix.X_OK) != nil {
			if invalid == nil {
				invalid = fmt.Errorf("unusable profile executable %q", p)
			}
			continue
		}
		return filepath.Join("/home/agent", dir, name), nil
	}
	if invalid != nil {
		return "", invalid
	}
	return "", fmt.Errorf("profile %q has no executable", name)
}

// Revalidate detects replacement of selected source objects; it never rebuilds inputs.
func (in *Inputs) Revalidate() error {
	if len(in.objects) == 0 {
		return errors.New("inputs are closed")
	}
	for path, o := range in.objects {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !os.SameFile(info, o.info) {
			return fmt.Errorf("selected source changed: %q", path)
		}
		if path == in.home || path == in.paths.Root || path == in.paths.Profiles || path == in.paths.Shared {
			if err = privateInfo(path, info, in.account.UID); err != nil {
				return err
			}
		}
	}
	for path, want := range in.links {
		got, err := os.Readlink(path)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("system link changed: %q", path)
		}
	}
	return nil
}
