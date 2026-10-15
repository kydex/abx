package sandbox

import (
	"reflect"
	"sort"
	"strings"
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
	if m["PATH"] != "/home/agent/.local/bin:/home/agent/.local/share/mise/shims:/home/agent/.bun/bin:/home/agent/bin:/usr/local/bin:/usr/bin:/bin" || m["BUN_INSTALL"] != "/home/agent/.bun" || m["NPM_CONFIG_PREFIX"] != "/home/agent/.local" {
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
		// An empty shell is what host.resolve would carry if it stopped acting on
		// an unresolved login shell. The refusal lives here, one layer down, and
		// it is this table that has to hold it.
		{"tester", "", "/workspace"},
		{"tester", "bash", "/workspace"},
		{"tester", "/bin//bash", "/workspace"},
		{"tester", "/bin/bash", "/tmp"},
		{"test\ner", "/bin/bash", "/workspace"},
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

func TestSessionNamesMatchValidatorPolicy(t *testing.T) {
	entries, err := Session("tester", "/bin/sh", "/workspace", []string{"TERM=xterm", "LC_X=y", "LD_PRELOAD=nope"})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !allowed(e.Name) && !fixedSessionName(e.Name) {
			t.Fatalf("session variable outside validator policy: %#v", e)
		}
	}
}

func FuzzSession(f *testing.F) {
	f.Add("tester", "/usr/bin/bash", "/workspace", "TERM=xterm")
	f.Add("", "", "", "")
	f.Add("u", "/bin/sh", "/home/agent", "LC_ALL=C.UTF-8")
	fixed := map[string]bool{"HOME": true, "USER": true, "LOGNAME": true, "SHELL": true, "PWD": true, "TMPDIR": true, "XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true, "XDG_CACHE_HOME": true, "XDG_STATE_HOME": true, "XDG_RUNTIME_DIR": true, "BUN_INSTALL": true, "NPM_CONFIG_PREFIX": true, "PATH": true}
	f.Fuzz(func(t *testing.T, username, shell, cwd, host string) {
		entries, err := Session(username, shell, cwd, []string{host})
		if err != nil {
			return
		}
		if len(entries) == 0 {
			t.Fatal("accepted session built no environment")
		}
		seen := map[string]bool{}
		prev := ""
		for _, e := range entries {
			if !validName(e.Name) || seen[e.Name] {
				t.Fatalf("invalid session environment: %#v", e)
			}
			seen[e.Name] = true
			if e.Name < prev {
				t.Fatalf("unsorted session environment: %#v", entries)
			}
			prev = e.Name
			if strings.ContainsRune(e.Value, 0) {
				t.Fatalf("NUL in session value: %#v", e)
			}
			if !allowed(e.Name) && !fixed[e.Name] {
				t.Fatalf("non-allowlisted session variable: %#v", e)
			}
		}
	})
}

func TestSessionRejectsNulUsernameAndHostValues(t *testing.T) {
	if _, err := Session("a\x00b", "/usr/bin/bash", "/workspace", nil); err == nil {
		t.Fatal("accepted NUL username")
	}
	entries, err := Session("tester", "/usr/bin/bash", "/workspace", []string{"TERM=a\x00b", "LANG=C.UTF-8"})
	if err != nil {
		t.Fatal(err)
	}
	m := toMap(entries)
	if _, ok := m["TERM"]; ok {
		t.Fatal("forwarded NUL host value")
	}
	if m["LANG"] != "C.UTF-8" {
		t.Fatalf("dropped clean host value: %#v", m)
	}
}

func TestSessionBoundsHostValueLength(t *testing.T) {
	// Independent literals: 64 KiB is a documented bound, so changing the
	// constant must also touch the guides and this test.
	atLimit := strings.Repeat("x", 65536)
	entries, err := Session("tester", "/usr/bin/bash", "/workspace", []string{"LANG=" + atLimit})
	if err != nil {
		t.Fatalf("refused a value at the limit: %v", err)
	}
	if toMap(entries)["LANG"] != atLimit {
		t.Fatal("value at the limit was not forwarded")
	}
	over := strings.Repeat("x", 65536+1)
	_, err = Session("tester", "/usr/bin/bash", "/workspace", []string{"LC_CTYPE=" + over})
	if err == nil || !strings.Contains(err.Error(), "LC_CTYPE") || !strings.Contains(err.Error(), "65536") {
		t.Fatalf("oversized value not refused by name and bound: %v", err)
	}
	// A value outside the allowlist is dropped before its size matters.
	if _, err := Session("tester", "/usr/bin/bash", "/workspace", []string{"SECRET=" + over}); err != nil {
		t.Fatalf("non-allowlisted oversized value refused: %v", err)
	}
}
