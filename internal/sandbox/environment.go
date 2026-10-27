package sandbox

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Entry is one deterministic environment assignment.
type Entry struct {
	Name  string
	Value string
}

// maxEnvironmentValueBytes bounds one allowlisted host value. The kernel
// rejects a single execve argument past MAX_ARG_STRLEN (128 KiB), and the
// verify probe carries the same environment in a payload with a 64 KiB limit
// of its own; a value past this bound is refused by name instead of surfacing
// as a raw "argument list too long".
const maxEnvironmentValueBytes = 64 * 1024

// Session builds the clean environment for run, shell, work, and self-test.
func Session(username, shell, workingDirectory string, host []string) ([]Entry, error) {
	if username == "" {
		return nil, errors.New("sandbox username is empty")
	}
	if strings.ContainsRune(username, 0) {
		return nil, fmt.Errorf("sandbox username %q contains NUL", username)
	}
	if strings.ContainsRune(username, '\n') {
		return nil, fmt.Errorf("sandbox username %q contains newline", username)
	}
	if !CleanPath(shell) {
		return nil, fmt.Errorf("sandbox shell %q is not a clean absolute path", shell)
	}
	if strings.ContainsRune(shell, '\n') {
		return nil, fmt.Errorf("sandbox shell %q contains newline", shell)
	}
	if workingDirectory != "/workspace" && workingDirectory != "/home/agent" {
		return nil, fmt.Errorf("unsupported sandbox working directory %q", workingDirectory)
	}
	values := allowlistedHost(host)
	fixed := map[string]string{
		"HOME": "/home/agent", "USER": username, "LOGNAME": username,
		"SHELL": shell, "PWD": workingDirectory, "TMPDIR": "/tmp",
		"XDG_CONFIG_HOME":   "/home/agent/.config",
		"XDG_DATA_HOME":     "/home/agent/.local/share",
		"XDG_CACHE_HOME":    "/home/agent/.cache",
		"XDG_STATE_HOME":    "/home/agent/.local/state",
		"XDG_RUNTIME_DIR":   "/run/user",
		"BUN_INSTALL":       "/home/agent/.bun",
		"NPM_CONFIG_PREFIX": "/home/agent/.local",
		"PATH":              "/home/agent/.local/bin:/home/agent/.local/share/mise/shims:/home/agent/.bun/bin:/home/agent/bin:/usr/local/bin:/usr/bin:/bin",
	}
	for key, value := range fixed {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if len(values[key]) > maxEnvironmentValueBytes {
			return nil, fmt.Errorf("environment variable %q value is %d bytes, above the %d byte limit", key, len(values[key]), maxEnvironmentValueBytes)
		}
	}
	out := make([]Entry, 0, len(keys))
	for _, key := range keys {
		out = append(out, Entry{Name: key, Value: values[key]})
	}
	return out, nil
}

func allowlistedHost(host []string) map[string]string {
	out := map[string]string{}
	for _, assignment := range host {
		key, value, ok := strings.Cut(assignment, "=")
		if !ok || !allowed(key) {
			continue
		}
		if strings.ContainsRune(value, 0) {
			continue
		}
		out[key] = value
	}
	return out
}

// fixedSessionName reports names Session always sets itself. Validate reuses
// it so the environment policy and the structural check cannot drift apart;
// environment_test pins that Session output stays within this set.
func fixedSessionName(name string) bool {
	switch name {
	case "HOME", "USER", "LOGNAME", "SHELL", "PWD", "TMPDIR",
		"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME",
		"XDG_STATE_HOME", "XDG_RUNTIME_DIR", "BUN_INSTALL",
		"NPM_CONFIG_PREFIX", "PATH":
		return true
	default:
		return false
	}
}

func allowed(key string) bool {
	switch key {
	case "TERM", "COLORTERM", "LANG", "LANGUAGE", "TZ", "NO_COLOR", "FORCE_COLOR":
		return true
	default:
		return len(key) > 3 && strings.HasPrefix(key, "LC_") && validName(key)
	}
}

func validName(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		if !((r >= 'A' && r <= 'Z') || r == '_' || (i > 0 && r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}
