// Login-shell regressions adapted from abx 0.7.9.
package host

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

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
	got := resolveShellFrom(1000, path)
	if got.Path != "/bin/sh" || !reflect.DeepEqual(got.Argv, []string{"/bin/sh", "-l"}) || got.Reason != "" {
		t.Fatalf("selection = %#v", got)
	}
}

func TestVisibleInSandboxBoundaries(t *testing.T) {
	for _, path := range []string{"/bin/fish", "/usr/bin/fish", "/lib64/ld-linux.so"} {
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
	if selection.Path != link || !reflect.DeepEqual(selection.Argv, []string{link, "-l"}) || selection.Reason != "" {
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
