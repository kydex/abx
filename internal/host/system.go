package host

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// systemLayout is the fixed set of host paths ABX exposes into the sandbox.
// Production always uses hostSystemLayout; a test can supply its own so the
// refusals below can be exercised against objects it controls instead of the
// real /usr and /etc.
type systemLayout struct {
	// usr is required and must be a directory: without it the sandbox has no
	// userspace to run anything in.
	usr string
	// compat are the compatibility directories. Each may be absent or a
	// symlink to its real location.
	compat []string
	// etc are optional file and directory names under /etc.
	etc []string
}

var hostSystemLayout = systemLayout{
	usr:    "/usr",
	compat: []string{"/bin", "/sbin", "/lib", "/lib64"},
	etc: []string{
		"hosts", "resolv.conf", "nsswitch.conf", "gai.conf", "passwd", "group",
		"ssl", "ca-certificates", "pki", "ld.so.cache", "ld.so.conf",
		"ld.so.conf.d", "localtime", "timezone", "os-release", "shells",
		"alternatives",
	},
}

func (in *Inputs) discoverSystem(root string) error {
	return in.discoverLayout(root, hostSystemLayout)
}

func (in *Inputs) discoverLayout(root string, layout systemLayout) error {
	// Discovered targets must stay within sandbox.allowedSystemTarget;
	// anything else fails closed in plan.Build instead of widening the sandbox.
	physical := func(p string) string { return filepath.Join(root, strings.TrimPrefix(p, "/")) }
	add := func(path, target string) (string, error) {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", fmt.Errorf("resolve system source %q: %w", path, err)
		}
		if overlaps(resolved, in.account.Home) || overlaps(resolved, in.paths.Root) {
			return "", fmt.Errorf("protected data overlaps system source %q", resolved)
		}
		if err = in.retain(resolved); err != nil {
			return "", err
		}
		in.system = append(in.system, Source{Path: resolved, Target: target})
		return resolved, nil
	}
	usr, err := add(physical(layout.usr), layout.usr)
	if err != nil {
		return err
	}
	if !in.objects[usr].info.IsDir() {
		return fmt.Errorf("%s is not a directory", layout.usr)
	}
	for _, p := range layout.compat {
		path := physical(p)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect compat system path %q: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, e := os.Readlink(path)
			if e != nil {
				return fmt.Errorf("read compat system link %q: %w", path, e)
			}
			if filepath.IsAbs(link) {
				return fmt.Errorf("compat system link %q must be relative, not %q", path, link)
			}
			resolved, e := filepath.EvalSymlinks(path)
			if e != nil {
				return fmt.Errorf("resolve compat system link %q: %w", path, e)
			}
			if overlaps(resolved, in.account.Home) || overlaps(resolved, in.paths.Root) {
				return fmt.Errorf("protected data overlaps system link %q", resolved)
			}
			if !in.withinSystemSource(resolved) {
				return fmt.Errorf("compat system link %q resolves outside mounted system sources to %q", path, resolved)
			}
			in.links[path] = link
			in.system = append(in.system, Source{Target: p, Link: link})
		} else if _, err = add(path, p); err != nil {
			return err
		}
	}
	// The /etc resources mount as discovered, without a mode or owner check
	// of their own. /etc/passwd is the one that holds a trust anchor, and its
	// strictness -- a regular file group and other cannot rewrite -- is
	// enforced by the ResolveShell branch reading passwdPath in shell.go,
	// which runs after this loop; moving that read without moving
	// checkPasswdFile with it would leave the mounted copy unchecked.
	for _, name := range layout.etc {
		p := "/etc/" + name
		path := physical(p)
		// Stat follows the final link, so a dangling target, a cycle, and
		// a non-directory in the middle all surface here as errno values
		// that optionalEtcUnavailable recognises.
		if _, err := os.Stat(path); optionalEtcUnavailable(err) {
			continue
		} else if err != nil {
			return fmt.Errorf("inspect system path %q: %w", p, err)
		}
		if _, err := add(path, p); err != nil {
			if optionalEtcUnavailable(err) {
				continue
			}
			return err
		}
	}
	return nil
}

// optionalEtcUnavailable reports errors that leave an optional /etc candidate
// with nothing to mount: a missing object, a non-directory in the middle of
// the path, or a symlink cycle. They are skipped just like a missing file.
func optionalEtcUnavailable(err error) bool {
	return errors.Is(err, os.ErrNotExist) ||
		errors.Is(err, syscall.ELOOP) ||
		errors.Is(err, syscall.ENOTDIR)
}
