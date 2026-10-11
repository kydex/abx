package probe

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// check forms the entire verdict of abx verify. On a host it is reachable only
// through Run, and Run always dies at gate(), so this is its first execution by
// any ordinary test. No seam is required: check takes an Expected and returns a
// slice. The test pins the FORM — which observations exist, which states are
// legal, and that report refuses to report success when any of them is not PASS.
// It asserts nothing about PASS itself: the sandbox observations cannot pass on
// a host, and that half still needs a live stand.
func TestCheckProducesEveryObservation(t *testing.T) {
	dir := t.TempDir()
	checks := check(Expected{
		Environment: []string{"HOME=/home/agent"},
		Sentinel:    dir + "/sentinel",
		ReadOnly:    []string{"/usr"},
	}, hostObserver())
	got := make([]string, len(checks))
	for i, c := range checks {
		got[i] = c.Name
	}
	want := []string{
		"working directory", "hostname", "exact environment",
		"hidden " + dir + "/sentinel", "hidden /etc/shadow",
		"selected /home/agent", "selected /workspace", "host home hidden",
		"user namespace", "mnt namespace", "pid namespace", "ipc namespace", "uts namespace",
		"private /tmp", "private /var/tmp", "private /run", "private /dev",
		"tmpfs /tmp", "tmpfs /var/tmp", "tmpfs /run",
		"runtime directory mode", "null device", "read-only /usr",
		"shared skills",
		"write and cleanup /home/agent", "write and cleanup /workspace",
	}
	for _, name := range want {
		if !slices.Contains(got, name) {
			t.Errorf("check no longer produces %q", name)
		}
	}
	if len(checks) != len(want) {
		t.Errorf("check produces %d observations, want %d: %v", len(checks), len(want), got)
	}
	for _, c := range checks {
		switch c.State {
		case statePass, stateUnavailable, stateFail, stateNA:
		default:
			t.Errorf("observation %q has unknown state %q", c.Name, c.State)
		}
	}
}

// report is what turns the observation list into a verdict, and it must never
// print PASS overall while anything is not PASS. The only exception is an
// explicitly unconfigured observation.
func TestReportIsFailClosedAndExemptsOnlyUnconfigured(t *testing.T) {
	var out bytes.Buffer
	if err := report(&out, []Check{{Name: "shared skills", State: stateNA, Detail: "not configured"}}); err != nil {
		t.Errorf("unconfigured observation failed the report: %v", err)
	}
	for _, state := range []string{stateFail, stateUnavailable} {
		out.Reset()
		if err := report(&out, []Check{{Name: "null device", State: state, Detail: "d"}}); err == nil {
			t.Errorf("report accepted state %q", state)
		}
		if !strings.Contains(out.String(), "null device") {
			t.Errorf("report did not disclose the failing observation %q", state)
		}
	}
}

// The expected environment reaches the probe through JSON, and the JSON encoder
// rewrites invalid UTF-8. Sanitizing both sides has to reproduce exactly that
// rewrite, otherwise the comparison still cannot converge.
func TestSanitizeMatchesJSONEncoding(t *testing.T) {
	for _, value := range []string{
		"plain=value", "utf8=значение", "invalid=\xff", "pair=\xff\xfe",
		"truncated=\xff\xfe\xfd", "mixed=\xffok", "ok\xff",
	} {
		entry := "VAR=" + value
		encoded, err := json.Marshal(entry)
		if err != nil {
			t.Fatalf("marshal %q: %v", entry, err)
		}
		var round string
		if err := json.Unmarshal(encoded, &round); err != nil {
			t.Fatalf("unmarshal %q: %v", entry, err)
		}
		want := sanitizeEnvironment([]string{entry})[0]
		if got := sanitizeEnvironment([]string{round})[0]; got != want {
			t.Errorf("sanitize is not idempotent under JSON for %q: host side %q, round trip %q", entry, want, got)
		}
	}
}

// The point of the sanitizing: a host value that is not valid UTF-8 has to
// compare equal to its own JSON round trip, and a genuinely different value has
// to keep failing.
func TestCheckEnvironmentIgnoresJSONOnlyDifference(t *testing.T) {
	if c := checkEnvironment([]string{"SHELL=/bin/sh\xff"}, []string{"SHELL=/bin/sh\ufffd"}); c.State != statePass {
		t.Errorf("valid UTF-8 round trip reported as a difference: %q", c.Detail)
	}
	if c := checkEnvironment([]string{"SHELL=/bin/sh"}, []string{"SHELL=/bin/bash"}); c.State != stateFail {
		t.Errorf("real difference accepted: %q", c.State)
	}
}
