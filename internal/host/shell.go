// Package host contains login-shell selection.
package host

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// Selection is the sandbox shell path and login argv.
type Selection struct {
	Path   string
	Argv   []string
	Reason string
}

// passwdPath is the account database ABX trusts. The owner rule and the parser
// must name the same file: two literals here would let the check drift onto one
// file while the other is read.
const passwdPath = "/etc/passwd"

// ResolveShell reads /etc/passwd and validates the selected shell.
func ResolveShell(uid int) Selection {
	if err := checkPasswdOwner(passwdPath); err != nil {
		return Selection{Reason: err.Error()}
	}
	return resolveShellFrom(uid, passwdPath)
}

// resolveShellFrom is the deterministic passwd-file test seam.
func resolveShellFrom(uid int, passwdPath string) Selection {
	return resolveFrom(uid, passwdPath, visibleInSandbox)
}

func resolveFrom(uid int, passwdPath string, visible func(string) bool) Selection {
	path, err := shellFromPasswd(uid, passwdPath)
	if err == nil {
		var resolved string
		if resolved, err = validateExecutable(path); err == nil && visible(path) && visible(resolved) {
			return Selection{Path: resolved, Argv: []string{resolved, "-l"}}
		}
		if err == nil && visible(path) && !visible(resolved) {
			err = fmt.Errorf("login shell %q resolves outside read-only system mounts to %q", path, resolved)
		}
	}
	if err != nil {
		return Selection{Reason: err.Error()}
	}
	// With err == nil the resolved shell is visible (otherwise err would be
	// set above) while path is not (otherwise we would have returned early).
	return Selection{Reason: fmt.Sprintf("login shell %q is outside read-only system mounts", path)}
}

// checkPasswdFile pins the trust assumptions on the account database file:
// a regular file without privilege-escalation bits that group and other
// cannot rewrite. The account lookup path (resolveAccount) goes through NSS
// and names no file, so only the direct reader is checked here.
func checkPasswdFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat passwd database %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("passwd database %q is not a regular file", path)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("passwd database %q is writable by group or other", path)
	}
	if info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return fmt.Errorf("passwd database %q has setuid, setgid, or sticky bit", path)
	}
	return nil
}

// checkPasswdOwner pins the trust anchor itself. It is deliberately separate
// from checkPasswdFile and is reached only for the database ABX actually uses:
// the account entry it yields names the home every session runs against, so a
// passwd file owned by anyone but root is one somebody could replace. On a real
// host /etc/passwd is always root-owned; a container that hands over a
// different one is out of scope, the same way a root-owned home now is.
func checkPasswdOwner(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat passwd database %q: %w", path, err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("passwd database %q has unknown owner", path)
	}
	if st.Uid != 0 {
		return fmt.Errorf("passwd database %q is owned by uid %d, not by root", path, st.Uid)
	}
	return nil
}

func shellFromPasswd(uid int, path string) (string, error) {
	if err := checkPasswdFile(path); err != nil {
		return "", err
	}
	// #nosec G304 -- production passes fixed /etc/passwd; the parameter is a deterministic test seam.
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("open passwd database: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) != 7 {
			continue
		}
		entryUID, parseErr := strconv.Atoi(fields[2])
		if parseErr == nil && entryUID == uid {
			if !filepath.IsAbs(fields[6]) {
				return "", fmt.Errorf("passwd login shell %q is not absolute", fields[6])
			}
			return filepath.Clean(fields[6]), nil
		}
	}
	return "", fmt.Errorf("passwd entry for uid %d has no login shell", uid)
}

func validateExecutable(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("login shell %q does not exist", path)
		}
		return "", fmt.Errorf("resolve login shell %q: %w", path, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("login shell %q does not exist", path)
		}
		return "", fmt.Errorf("stat login shell %q: %w", path, err)
	}
	// Ownership is deliberately not required: user-managed shells live under
	// system paths too (for example /usr/local/bin), and visibility already
	// constrains shells to mounted system trees.
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 || unix.Access(path, unix.X_OK) != nil {
		return "", fmt.Errorf("login shell %q is not a regular file executable by the invoking user", path)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return "", fmt.Errorf("login shell %q is writable by group or other", path)
	}
	if info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return "", fmt.Errorf("login shell %q has setuid, setgid, or sticky bit", path)
	}
	return resolved, nil
}

func visibleInSandbox(path string) bool {
	for _, root := range []string{"/usr", "/bin", "/sbin", "/lib", "/lib64", "/etc/alternatives"} {
		if path == root || strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}
