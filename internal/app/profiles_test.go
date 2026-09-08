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
	if code, err := invoke(cli.ProfileList, ""); code != 0 || err != nil || !strings.Contains(out.String(), "demo  unavailable  not-configured") {
		t.Fatal(code, err, out.String())
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
	if _, err = os.Stat(filepath.Join(p.Profiles, "demo")); err != nil {
		t.Fatal("lost created profile", err)
	}
}
