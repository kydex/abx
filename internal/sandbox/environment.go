// Environment policy adapted from abx 0.7.9.
package sandbox

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Entry is one deterministic environment assignment.
type Entry struct {
	Name  string
	Value string
}

// Session builds the clean environment for run, shell, work, and self-test.
func Session(username, shell, workingDirectory string, host []string) ([]Entry, error) {
	if username == "" {
		return nil, fmt.Errorf("sandbox username is empty")
	}
	if !filepath.IsAbs(shell) {
		return nil, fmt.Errorf("sandbox shell %q is not absolute", shell)
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
		"PATH":              "/home/agent/.local/bin:/home/agent/.bun/bin:/home/agent/bin:/usr/local/bin:/usr/bin:/bin",
	}
	for key, value := range fixed {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
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
		out[key] = value
	}
	return out
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
