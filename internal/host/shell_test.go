// Login-shell regressions.
package host

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// requireTrustedPasswd skips a test that reads the real account database.
//
// Those tests go through ResolveShell, and ABX now refuses to start when
// /etc/passwd is not owned by root. On a real host it is, and the tests run. In
// a container that remaps user namespaces the database shows up owned by an
// unprivileged uid, so there is nothing to test: the skip says which host
// property is missing instead of reporting a failure that looks like a bug.
func requireTrustedPasswd(t *testing.T) {
	t.Helper()
	if err := checkPasswdOwner("/etc/passwd"); err != nil {
		t.Skipf("host account database is not trusted here: %v", err)
	}
}

// fixtureShellResolver returns a resolver backed by a throwaway account
// database naming the current uid and /bin/sh. Nothing else changes: the same
// shellFromPasswd parses it and the same validateExecutable accepts /bin/sh,
// which is a regular executable under /bin and therefore visible in the
// sandbox. Only the file being read is the test's own.
//
// It exists so the guards in resolveWithShell are exercised where the host
// database is unusable -- inside a user namespace /etc/passwd belongs to an
// unmapped uid, and ABX refuses to start, so a test reading it has nothing to
// exercise.
func fixtureShellResolver(t *testing.T) func(int) Selection {
	t.Helper()
	uid := os.Getuid()
	path := filepath.Join(t.TempDir(), "passwd")
	line := fmt.Sprintf("tester:x:%d:%d::/home/tester:/bin/sh\n", uid, uid)
	if e := os.WriteFile(path, []byte(line), 0o600); e != nil {
		t.Fatal(e)
	}
	return func(got int) Selection { return resolveShellFrom(got, path) }
}

func TestShellFromPasswd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "passwd")
	data := "root:x:0:0:root:/root:/bin/bash\ntester:x:1000:1000::/home/tester:/usr/bin/fish\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := shellFromPasswd(1000, path)
	if err != nil || got != "/usr/bin/fish" {
		t.Fatalf("shellFromPasswd = %q, %v", got, err)
	}
	if _, err := shellFromPasswd(999, path); err == nil {
		t.Fatal("found absent uid")
	}
}

func TestResolveFromRejectsUnavailableLoginShell(t *testing.T) {
	path := filepath.Join(t.TempDir(), "passwd")
	if err := os.WriteFile(path, []byte("tester:x:1000:1000::/home/tester:/missing/shell\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := resolveShellFrom(1000, path)
	if got.Path != "" || len(got.Argv) != 0 || got.Reason == "" {
		t.Fatalf("selection = %#v", got)
	}
}

func TestResolveFromUsesLoginArgv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "passwd")
	if err := os.WriteFile(path, []byte("tester:x:1000:1000::/home/tester:/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := validateExecutable("/bin/sh")
	if err != nil {
		t.Skipf("host /bin/sh is unavailable: %v", err)
	}
	got := resolveShellFrom(1000, path)
	if got.Path != resolved || !reflect.DeepEqual(got.Argv, []string{resolved, "-l"}) || got.Reason != "" {
		t.Fatalf("selection = %#v", got)
	}
}

func TestValidateExecutableRejectsWritableAndSpecialModes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shell")
	if err := os.WriteFile(path, []byte("x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o775); err != nil {
		t.Fatal(err)
	}
	if _, err := validateExecutable(path); err == nil {
		t.Fatal("accepted group-writable shell")
	}
	if err := os.Chmod(path, 0o700|os.ModeSetuid); err != nil {
		t.Fatal(err)
	}
	if _, err := validateExecutable(path); err == nil {
		t.Fatal("accepted setuid shell")
	}
}

func TestShellFromPasswdRejectsUnsafeDatabaseFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "passwd")
	// WriteFile is umask-filtered: set the mode explicitly afterwards.
	if err := os.WriteFile(path, []byte("tester:x:1000:1000::/home/tester:/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o664); err != nil {
		t.Fatal(err)
	}
	if _, err := shellFromPasswd(1000, path); err == nil {
		t.Fatal("accepted group-writable passwd database")
	}
}

func TestVisibleInSandboxBoundaries(t *testing.T) {
	for _, path := range []string{"/bin/fish", "/usr/bin/fish", "/lib64/ld-linux.so", "/etc/alternatives/sh"} {
		if !visibleInSandbox(path) {
			t.Errorf("%s not visible", path)
		}
	}
	for _, path := range []string{"/opt/fish", "/usrx/bin/fish", "bin/fish"} {
		if visibleInSandbox(path) {
			t.Errorf("%s visible", path)
		}
	}
}

func TestResolveUsesHostPasswd(t *testing.T) {
	requireTrustedPasswd(t)
	selection := ResolveShell(os.Getuid())
	if selection.Path == "" {
		t.Fatalf("Resolve = %#v", selection)
	}
}

func TestShellFromPasswdMalformedAndRelative(t *testing.T) {
	path := filepath.Join(t.TempDir(), "passwd")
	data := "malformed\ntester:x:1000:1000::/home/tester:relative\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := shellFromPasswd(1000, path); err == nil {
		t.Fatal("accepted relative shell")
	}
	if _, err := shellFromPasswd(1000, filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("opened missing passwd")
	}
}

func TestValidateExecutableRejectsDirectoryAndNonExecutable(t *testing.T) {
	dir := t.TempDir()
	if _, err := validateExecutable(dir); err == nil {
		t.Fatal("accepted directory")
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateExecutable(file); err == nil {
		t.Fatal("accepted non-executable")
	}
}

func TestValidateExecutableRequiresInvokingUserExecutePermission(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses ordinary execute permission checks")
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0o001); err != nil {
		t.Fatal(err)
	}
	if _, err := validateExecutable(file); err == nil {
		t.Fatal("accepted other-only execute permission for the owning user")
	}
}

func TestValidateExecutableResolvesSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "shell")
	if err := os.WriteFile(target, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	resolved, err := validateExecutable(link)
	if err != nil || resolved != target {
		t.Fatalf("validateExecutable = %q, %v", resolved, err)
	}
}

func TestValidateExecutableRejectsBrokenAndLoopingSymlinks(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken")
	if err := os.Symlink(filepath.Join(dir, "missing"), broken); err != nil {
		t.Fatal(err)
	}
	if _, err := validateExecutable(broken); err == nil {
		t.Fatal("accepted broken shell symlink")
	}
	loop := filepath.Join(dir, "loop")
	if err := os.Symlink("loop", loop); err != nil {
		t.Fatal(err)
	}
	if _, err := validateExecutable(loop); err == nil {
		t.Fatal("accepted shell symlink loop")
	}
}

func TestResolveFromAcceptsVisibleResolvedShell(t *testing.T) {
	resolved, err := validateExecutable("/bin/sh")
	if err != nil {
		t.Skipf("host /bin/sh is unavailable: %v", err)
	}
	if !visibleInSandbox("/bin/sh") || !visibleInSandbox(resolved) {
		t.Fatalf("/bin/sh resolved outside sandbox mounts to %q", resolved)
	}
}

func TestResolveFromChecksResolvedShellVisibility(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "shell")
	passwd := filepath.Join(dir, "passwd")
	if err := os.WriteFile(target, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	data := "tester:x:1000:1000::/home/tester:" + link + "\n"
	if err := os.WriteFile(passwd, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	selection := resolveFrom(1000, passwd, func(path string) bool { return path == link })
	if selection.Path != "" || len(selection.Argv) != 0 || !strings.Contains(selection.Reason, "resolves outside read-only system mounts") {
		t.Fatalf("selection = %#v", selection)
	}

	selection = resolveFrom(1000, passwd, func(path string) bool { return path == link || path == target })
	if selection.Path != target || !reflect.DeepEqual(selection.Argv, []string{target, "-l"}) || selection.Reason != "" {
		t.Fatalf("selection = %#v", selection)
	}
}

func TestResolveFromRejectsShellPathOutsideSystemMounts(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target-shell")
	path := filepath.Join(dir, "login-shell")
	if err := os.WriteFile(target, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	passwd := filepath.Join(dir, "passwd")
	if err := os.WriteFile(passwd, []byte("tester:x:1000:1000::/home/tester:"+path+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	selection := resolveFrom(1000, passwd, func(candidate string) bool { return candidate == target })
	if selection.Path != "" || !strings.Contains(selection.Reason, "outside read-only system mounts") {
		t.Fatalf("selection = %#v", selection)
	}
}
