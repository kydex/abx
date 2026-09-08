// Login-shell selection adapted from abx 0.7.9.
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
)

const executeAccessMode = 1 // X_OK on Linux.

// Selection is the sandbox shell path and login argv.
type Selection struct {
	Path   string
	Argv   []string
	Reason string
}

// Resolve reads /etc/passwd and validates the selected shell.
func ResolveShell(uid int) Selection { return resolveShellFrom(uid, "/etc/passwd") }

// ResolveFrom is the deterministic passwd-file test seam.
func resolveShellFrom(uid int, passwdPath string) Selection {
	return resolveFrom(uid, passwdPath, visibleInSandbox)
}

func resolveFrom(uid int, passwdPath string, visible func(string) bool) Selection {
	path, err := shellFromPasswd(uid, passwdPath)
	if err == nil {
		var resolved string
		if resolved, err = validateExecutable(path); err == nil && visible(path) && visible(resolved) {
			return Selection{Path: path, Argv: []string{path, "-l"}}
		}
		if err == nil && !visible(resolved) {
			err = fmt.Errorf("login shell %q resolves outside read-only system mounts to %q", path, resolved)
		}
	}
	reason := "login shell could not be resolved"
	if err != nil {
		reason = err.Error()
	} else if !visible(path) {
		reason = fmt.Sprintf("login shell %q is outside read-only system mounts", path)
	}
	return Selection{Reason: reason}
}

func shellFromPasswd(uid int, path string) (string, error) {
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
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 || syscall.Access(path, executeAccessMode) != nil {
		return "", fmt.Errorf("login shell %q is not a regular file executable by the invoking user", path)
	}
	return resolved, nil
}

func visibleInSandbox(path string) bool {
	for _, root := range []string{"/usr", "/bin", "/sbin", "/lib", "/lib64"} {
		if path == root || strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}
