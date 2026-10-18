// Package host resolves and owns the host objects used by one invocation:
// the account, the storage layout, the project, the login shell, and the
// system sources mounted into the sandbox. See docs/architecture.md.
package host

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/kydex/abx/internal/profile"
	"golang.org/x/sys/unix"
)

// Source is one observed system resource: a bind mount of Path, or a
// compatibility symlink whose text is Link. Exactly one of Path and Link is
// set -- Revalidate refuses a Source that names both or neither -- and Target
// is the path the sandbox exposes it at.
type (
	Source struct{ Path, Target, Link string }
	object struct {
		file *os.File
		info os.FileInfo
	}
)

// Mode selects host resources and the session command. It mirrors
// sandbox.Mode one to one; app.sessionInput maps between them, and a new mode
// needs all three places at once.
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

// retainProject opens the project and then requires the retained object itself
// to be a directory. ResolveProject judged the same path by name; between that
// check and this open the name can be swapped for a regular file, which retain
// accepts for the /etc sources and would otherwise mount at /workspace.
func (in *Inputs) retainProject() error {
	if err := in.retain(in.project); err != nil {
		return err
	}
	if !in.objects[in.project].info.IsDir() {
		return fmt.Errorf("project %q is not a directory", in.project)
	}
	return nil
}

func (in *Inputs) private(path string) error {
	if err := in.retain(path); err != nil {
		return err
	}
	return privateInfo(path, in.objects[path].info, in.account.UID)
}

func privateInfo(path string, info os.FileInfo, uid int) error {
	if !info.IsDir() {
		return fmt.Errorf("private path %q is not a directory", path)
	}
	if info.Mode().Perm() != 0o700 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return fmt.Errorf("private directory %q must have exact mode 0700", path)
	}
	st := info.Sys().(*syscall.Stat_t)
	if int64(st.Uid) != int64(uid) {
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
		if err = in.retainProject(); err != nil {
			return nil, err
		}
		if mode == Run && needAgent {
			in.executable, err = resolveAgent(in.home, name)
			if err != nil {
				return nil, err
			}
			if err = in.checkAgentPlacement(); err != nil {
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

// agentMountPoint is where the profile home appears inside the sandbox.
const agentMountPoint = "/home/agent"

// checkAgentPlacement refuses an agent whose real file will not exist inside
// the sandbox, before anything is started.
//
// resolveAgent accepts any symlink that ends at a regular executable file,
// which is what a package manager needs: the binary lives under /usr or inside
// the profile, and ~/.local/bin/<name> points at it. But a link that ends
// outside the profile and outside every mounted system source yields a path
// that is absent inside the sandbox, and the session then dies at exec time
// blaming the tool rather than the profile. Nothing is mounted there, so this
// reports the real cause instead.
func (in *Inputs) checkAgentPlacement() error {
	// A component boundary, not a string prefix: /home/agentX is outside the
	// mount even though it starts with the same text.
	rel, ok := strings.CutPrefix(in.executable, agentMountPoint)
	if !ok || (rel != "" && !strings.HasPrefix(rel, "/")) {
		return fmt.Errorf("agent path %q is not inside the agent mount", in.executable)
	}
	host := filepath.Join(in.home, rel)
	resolved, err := filepath.EvalSymlinks(host)
	if err != nil {
		return fmt.Errorf("resolve profile executable %q: %w", host, err)
	}
	if within(resolved, in.home) || in.withinSystemSource(resolved) {
		return nil
	}
	return fmt.Errorf(
		"profile executable %q resolves to %q, which the sandbox does not mount; "+
			"put the binary inside the profile or under a mounted system path such as /usr, "+
			"with a relative link target such as ../lib/demo",
		host, resolved)
}

// withinSystemSource reports whether resolved lies on a mounted system bind
// source. Compatibility links are skipped: their text points at a target
// inside the mounted tree, not at a mount of its own.
func (in *Inputs) withinSystemSource(resolved string) bool {
	for _, s := range in.system {
		if s.Link == "" && (resolved == s.Path || within(resolved, s.Path)) {
			return true
		}
	}
	return false
}

func resolveAgent(home, name string) (string, error) {
	var invalid error
	var missing []string
	// Mise shims come second: they are the managed toolchain, while plain
	// installs in .bun/bin or bin stay as further fallbacks.
	for _, dir := range []string{".local/bin", ".local/share/mise/shims", ".bun/bin", "bin"} {
		p := filepath.Join(home, dir, name)
		info, err := os.Stat(p)
		if errors.Is(err, os.ErrNotExist) {
			// Stat follows links, so a dangling symlink lands here too. An
			// object that Lstat sees at the path is exactly that dangling
			// link, and the final error has to name it: silence used to make
			// it indistinguishable from a path that was never there.
			entry := fmt.Sprintf("%q", p)
			if _, lerr := os.Lstat(p); lerr == nil {
				entry += " (dangling link)"
			}
			missing = append(missing, entry)
			continue
		}
		if err != nil {
			if invalid == nil {
				invalid = fmt.Errorf("inspect profile executable %q: %w", p, err)
			}
			continue
		}
		switch {
		case !info.Mode().IsRegular():
			err = fmt.Errorf("profile executable %q is not a regular file", p)
		case info.Mode().Perm()&0o111 == 0:
			err = fmt.Errorf("profile executable %q has no execute permission bits", p)
		default:
			if accessErr := unix.Access(p, unix.X_OK); accessErr != nil {
				err = fmt.Errorf("access profile executable %q: %w", p, accessErr)
			}
		}
		// The agent is executed by the same launcher, in the same trust
		// position, as the login shell, so it gets the shell's remaining two
		// checks as well. It used to stop after the execute test: an agent that
		// the user's group can rewrite, or one carrying setuid, was accepted
		// while an equivalent shell was refused.
		if err == nil {
			if reason := unsafeModeReason(info.Mode()); reason != "" {
				err = fmt.Errorf("profile executable %q %s", p, reason)
			}
		}
		if err != nil {
			if invalid == nil {
				invalid = err
			}
			continue
		}
		return filepath.Join(agentMountPoint, dir, name), nil
	}
	if invalid != nil {
		return "", invalid
	}
	return "", fmt.Errorf("profile %q has no executable; checked %s", name, strings.Join(missing, ", "))
}

// role names a retained object for diagnostics: the same failure covers
// several kinds of source, and the message must say which one changed.
func (in *Inputs) role(path string) string {
	switch {
	case path == in.home:
		return "profile home"
	case path == in.paths.Root:
		return "data root"
	case path == in.paths.Profiles:
		return "profiles directory"
	case path == in.paths.Shared:
		return "shared directory"
	case path == in.project:
		return "project"
	case path == in.skills:
		return "shared skills"
	case path == filepath.Dir(in.paths.SharedSkills):
		return "shared skills directory"
	}
	for _, s := range in.system {
		if path == s.Path {
			return "system source"
		}
	}
	return "source"
}

// Revalidate detects replacement of selected source objects; it never rebuilds inputs.
func (in *Inputs) Revalidate() error {
	if len(in.objects) == 0 {
		return errors.New("inputs are closed")
	}
	// Source carries the union of a bind and a link in two plain fields;
	// downstream reads Link == "" to tell them apart, so a malformed Source
	// must stop here rather than be reinterpreted.
	for _, s := range in.system {
		switch {
		case s.Path == "" && s.Link == "":
			return fmt.Errorf("system source %q has neither a path nor a link", s.Target)
		case s.Path != "" && s.Link != "":
			return fmt.Errorf("system source %q names both a path and a link", s.Target)
		}
	}
	// Sorted keys make the reported object deterministic when several were
	// replaced between selection and preparation.
	paths := make([]string, 0, len(in.objects))
	for path := range in.objects {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		o := in.objects[path]
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !os.SameFile(info, o.info) {
			return fmt.Errorf("selected source changed: %q (%s)", path, in.role(path))
		}
		if path == in.home || path == in.paths.Root ||
			path == in.paths.Profiles || path == in.paths.Shared {
			if err = privateInfo(path, info, in.account.UID); err != nil {
				return err
			}
		}
	}
	links := make([]string, 0, len(in.links))
	for path := range in.links {
		links = append(links, path)
	}
	sort.Strings(links)
	for _, path := range links {
		got, err := os.Readlink(path)
		if err != nil {
			return err
		}
		if got != in.links[path] {
			return fmt.Errorf("system link changed: %q", path)
		}
	}
	return nil
}
