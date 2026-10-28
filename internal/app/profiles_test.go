package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kydex/abx/internal/cli"
	"github.com/kydex/abx/internal/host"
)

// runProfiles serves a closed set of profile commands; a future kind must be
// refused rather than fall through to success.
func TestUnknownProfileOperationFailsClosed(t *testing.T) {
	var out bytes.Buffer
	status, err := runProfiles(cli.Command{Kind: cli.Kind("future-profile-op")}, host.Account{}, host.Paths{}, &out)
	if status != 1 || err == nil || !strings.Contains(err.Error(), "unsupported profile operation") {
		t.Fatalf("unknown profile operation = status %d, error %v", status, err)
	}
}

func TestProfileCommandsReportWithoutRuntime(t *testing.T) {
	base := t.TempDir()
	a := host.Account{UID: os.Getuid(), Username: "tester", Home: base}
	p, err := host.ResolvePaths(a, filepath.Join(base, "data\nescaped"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	invoke := func(k cli.Kind, name string) (int, error) {
		out.Reset()
		return runProfiles(cli.Command{Kind: k, Profile: name}, a, p, &out)
	}
	if code, err := invoke(cli.ProfileList, ""); code != 0 || err != nil || out.Len() != 0 {
		t.Fatal(code, err, out.String())
	}
	if code, err := invoke(cli.ProfileShow, "demo"); code != 0 || err != nil || !strings.Contains(out.String(), "State: absent\n") {
		t.Fatal(code, err, out.String())
	}
	if _, err = os.Lstat(p.Root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read created storage")
	}
	if code, err := invoke(cli.ProfileCreate, "demo"); code != 0 || err != nil {
		t.Fatal(code, err)
	}
	if strings.Contains(out.String(), "data\nescaped") || !strings.Contains(out.String(), `data\nescaped`) {
		t.Fatalf("unsafe output: %q", out.String())
	}
	if code, err := invoke(cli.ProfileCreate, "demo"); code != 1 || !errors.Is(err, host.ErrProfileExists) || out.Len() != 0 {
		t.Fatal(code, err, out.String())
	}
	if code, err := invoke(cli.ProfileShow, "demo"); code != 0 || err != nil || !strings.Contains(out.String(), "Command: unavailable") || !strings.Contains(out.String(), "Skills: not-configured") {
		t.Fatal(code, err, out.String())
	}
	if code, err := invoke(cli.ProfileList, ""); code != 0 || err != nil || !strings.Contains(out.String(), "demo     unavailable  not-configured") {
		t.Fatal(code, err, out.String())
	}
	// A present but unusable skills layout gets its own detail line.
	if err := os.MkdirAll(filepath.Dir(p.SharedSkills), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.SharedSkills, []byte("not a directory\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, err := invoke(cli.ProfileShow, "demo"); code != 0 || err != nil ||
		!strings.Contains(out.String(), "Skills: invalid") ||
		!strings.Contains(out.String(), "Skills detail: skills source is not a directory") {
		t.Fatal(code, err, out.String())
	}
}

// ListProfiles fails closed on an unsafe profile layout. The host package
// covers the predicate; this pins the command-level contract: status 1, no
// partial table on stdout, and the profile named in the error.
func TestProfileListUnsafeLayoutFailsWithoutOutput(t *testing.T) {
	base := t.TempDir()
	a := host.Account{UID: os.Getuid(), Username: "tester", Home: base}
	paths, err := host.ResolvePaths(a, filepath.Join(base, "data"))
	if err != nil {
		t.Fatal(err)
	}
	home, err := host.CreateProfile(a, paths, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(home, 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	code, err := runProfiles(cli.Command{Kind: cli.ProfileList}, a, paths, &out)
	if code != 1 || err == nil {
		t.Fatalf("unsafe profile list = status %d, error %v", code, err)
	}
	if out.Len() != 0 {
		t.Fatalf("unsafe profile list wrote a partial table: %q", out.String())
	}
	if !strings.Contains(err.Error(), `profile entry "demo"`) ||
		!strings.Contains(err.Error(), "must have exact mode 0700") {
		t.Fatalf("unsafe profile list error does not name the entry and reason: %v", err)
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("output failed") }

func TestProfileOutputFailureDoesNotDeleteCreatedProfile(t *testing.T) {
	base := t.TempDir()
	a := host.Account{UID: os.Getuid(), Home: base}
	p, err := host.ResolvePaths(a, base)
	if err != nil {
		t.Fatal(err)
	}
	code, err := runProfiles(cli.Command{Kind: cli.ProfileCreate, Profile: "demo"}, a, p, brokenWriter{})
	if code != 1 || err == nil {
		t.Fatal(code, err)
	}
	if !strings.Contains(err.Error(), "profile was created") {
		t.Fatalf("confirmation failure does not say the profile exists: %v", err)
	}
	if _, err = os.Stat(filepath.Join(p.Profiles, "demo")); err != nil {
		t.Fatal("lost created profile", err)
	}
}

func TestProfileListAlignsDifferentNameAndCommandLengths(t *testing.T) {
	base := t.TempDir()
	a := host.Account{UID: os.Getuid(), Home: base}
	paths, err := host.ResolvePaths(a, base)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "long-profile-name"} {
		home, err := host.CreateProfile(a, paths, name)
		if err != nil {
			t.Fatal(err)
		}
		if name == "a" {
			if err := os.MkdirAll(filepath.Join(home, "bin"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home, "bin", name), []byte("#!/bin/sh\n"), 0o700); err != nil {
				t.Fatal(err)
			}
		}
	}
	var out bytes.Buffer
	code, err := runProfiles(cli.Command{Kind: cli.ProfileList}, a, paths, &out)
	if code != 0 || err != nil {
		t.Fatal(code, err)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatal(out.String())
	}
	commandColumn, skillsColumn := strings.Index(lines[0], "COMMAND"), strings.Index(lines[0], "SKILLS")
	for i, command := range []string{"available", "unavailable"} {
		line := lines[i+1]
		if strings.Index(line, command) != commandColumn || strings.Index(line, "not-configured") != skillsColumn || strings.TrimRight(line, " \t") != line {
			t.Fatalf("misaligned row: %q", line)
		}
	}
}

func TestProfileReadCommandsPropagateOutputFailure(t *testing.T) {
	base := t.TempDir()
	a := host.Account{UID: os.Getuid(), Home: base}
	paths, err := host.ResolvePaths(a, base)
	if err != nil {
		t.Fatal(err)
	}
	home, err := host.CreateProfile(a, paths, "demo")
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []cli.Kind{cli.ProfileShow, cli.ProfileList} {
		code, err := runProfiles(cli.Command{Kind: kind, Profile: "demo"}, a, paths, brokenWriter{})
		if code != 1 || err == nil || !strings.Contains(err.Error(), "output failed") {
			t.Fatalf("%s lost output failure: code=%d err=%v", kind, code, err)
		}
		entries, err := os.ReadDir(home)
		if err != nil || len(entries) != 0 {
			t.Fatalf("%s changed profile: %v %v", kind, entries, err)
		}
	}
}

func TestProfileShowReportsFoundAgent(t *testing.T) {
	base := t.TempDir()
	a := host.Account{UID: os.Getuid(), Username: "tester", Home: base}
	p, err := host.ResolvePaths(a, filepath.Join(base, "data"))
	if err != nil {
		t.Fatal(err)
	}
	home, err := host.CreateProfile(a, p, "demo")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "demo"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	code, err := runProfiles(cli.Command{Kind: cli.ProfileShow, Profile: "demo"}, a, p, &out)
	if code != 0 || err != nil {
		t.Fatalf("profile show = status %d, %v", code, err)
	}
	if !strings.Contains(out.String(), "Command: /home/agent/.local/bin/demo\n") {
		t.Fatalf("profile show did not report the found agent: %q", out.String())
	}
}
