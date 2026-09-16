package sandbox

import (
	"reflect"
	"sort"
	"testing"
)

func toMap(entries []Entry) map[string]string {
	out := map[string]string{}
	for _, entry := range entries {
		out[entry.Name] = entry.Value
	}
	return out
}

func TestSessionAllowlistAndFixedValues(t *testing.T) {
	host := []string{"TERM=xterm-256color", "LC_ALL=C.UTF-8", "LD_PRELOAD=evil", "SSH_AUTH_SOCK=/sock", "NODE_OPTIONS=--require=x", "HOME=/real", "bad=x"}
	got, err := Session("tester", "/usr/bin/fish", "/workspace", host)
	if err != nil {
		t.Fatal(err)
	}
	m := toMap(got)
	if m["HOME"] != "/home/agent" || m["SHELL"] != "/usr/bin/fish" || m["PWD"] != "/workspace" || m["TERM"] != "xterm-256color" || m["LC_ALL"] != "C.UTF-8" {
		t.Fatalf("environment = %#v", m)
	}
	if m["PATH"] != "/home/agent/.local/bin:/home/agent/.bun/bin:/home/agent/bin:/usr/local/bin:/usr/bin:/bin" || m["BUN_INSTALL"] != "/home/agent/.bun" || m["NPM_CONFIG_PREFIX"] != "/home/agent/.local" {
		t.Fatalf("package-manager environment = %#v", m)
	}
	for _, forbidden := range []string{"LD_PRELOAD", "SSH_AUTH_SOCK", "NODE_OPTIONS"} {
		if _, ok := m[forbidden]; ok {
			t.Errorf("forwarded %s", forbidden)
		}
	}
	names := make([]string, len(got))
	for i := range got {
		names[i] = got[i].Name
	}
	if !sort.StringsAreSorted(names) {
		t.Fatalf("not sorted: %v", names)
	}
	again, _ := Session("tester", "/usr/bin/fish", "/workspace", host)
	if !reflect.DeepEqual(got, again) {
		t.Fatal("output is not deterministic")
	}
}

func TestShellWorkingDirectory(t *testing.T) {
	got, err := Session("tester", "/bin/bash", "/home/agent", nil)
	if err != nil {
		t.Fatal(err)
	}
	if m := toMap(got); m["PWD"] != "/home/agent" {
		t.Fatalf("shell environment = %#v", m)
	}
}

func TestSessionRejectsInvalidStructure(t *testing.T) {
	for _, tc := range []struct{ username, shell, cwd string }{
		{"", "/bin/bash", "/workspace"},
		{"tester", "bash", "/workspace"},
		{"tester", "/bin/bash", "/tmp"},
	} {
		if _, err := Session(tc.username, tc.shell, tc.cwd, nil); err == nil {
			t.Errorf("accepted %#v", tc)
		}
	}
}

func TestAllowed(t *testing.T) {
	for _, key := range []string{"TERM", "COLORTERM", "LANG", "LANGUAGE", "LC_TIME", "TZ", "NO_COLOR", "FORCE_COLOR"} {
		if !allowed(key) {
			t.Errorf("%s not allowed", key)
		}
	}
	for _, key := range []string{"LC_bad", "LC_", "PATH", "HTTP_PROXY", ""} {
		if allowed(key) {
			t.Errorf("%s allowed", key)
		}
	}
}

func TestSessionKeepsStorageAndIdentityInsideSandbox(t *testing.T) {
	want := map[string]string{
		"USER": "tester", "LOGNAME": "tester", "TMPDIR": "/tmp",
		"XDG_CONFIG_HOME": "/home/agent/.config",
		"XDG_DATA_HOME":   "/home/agent/.local/share",
		"XDG_CACHE_HOME":  "/home/agent/.cache",
		"XDG_STATE_HOME":  "/home/agent/.local/state",
		"XDG_RUNTIME_DIR": "/run/user",
	}
	var host []string
	for key := range want {
		host = append(host, key+"=/host/private")
	}
	for _, cwd := range []string{"/workspace", "/home/agent"} {
		t.Run(cwd, func(t *testing.T) {
			entries, err := Session("tester", "/bin/sh", cwd, host)
			if err != nil {
				t.Fatal(err)
			}
			got := toMap(entries)
			for key, value := range want {
				if got[key] != value {
					t.Errorf("%s = %q, want %q", key, got[key], value)
				}
			}
		})
	}
}
