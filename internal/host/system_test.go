package host

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSystemSourcesKeepArchTrustAndRejectProtectedOverlap(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"usr", "etc/ssl/certs", "etc/ca-certificates/extracted"} {
		if err := os.MkdirAll(filepath.Join(root, p), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	bundle := filepath.Join(root, "etc/ca-certificates/extracted/tls-ca-bundle.pem")
	if err := os.WriteFile(bundle, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../ca-certificates/extracted/tls-ca-bundle.pem", filepath.Join(root, "etc/ssl/certs/ca-certificates.crt")); err != nil {
		t.Fatal(err)
	}
	in := fixture(t)
	if err := in.discoverSystem(root); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range in.System() {
		if s.Target == "/etc/ca-certificates" {
			found = true
			if s.Path != filepath.Join(root, "etc/ca-certificates") {
				t.Fatal(s)
			}
		}
	}
	if !found {
		t.Fatal("Arch trust source omitted")
	}
	for _, protected := range []string{"home", "data"} {
		other := fixture(t)
		if protected == "home" {
			other.account.Home = filepath.Join(root, "usr/private")
		} else {
			other.paths.Root = filepath.Join(root, "usr/private")
		}
		if err := other.discoverSystem(root); err == nil {
			t.Fatalf("exported %s through /usr", protected)
		}
	}
}

func TestCompatSymlinkRevalidationDetectsSwap(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "usr", "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("usr/bin", filepath.Join(root, "bin")); err != nil {
		t.Fatal(err)
	}
	in := fixture(t)
	if err := in.discoverSystem(root); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "bin")
	want, ok := in.links[link]
	if !ok || want == "" {
		t.Fatalf("compat symlink not recorded: %#v", in.links)
	}
	if err := in.Revalidate(); err != nil {
		t.Fatalf("unchanged compat symlink rejected: %v", err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("usr", link); err != nil {
		t.Fatal(err)
	}
	if err := in.Revalidate(); err == nil {
		t.Fatal("swapped compat symlink accepted")
	}
}

// Missing host resources are skipped silently, and a dangling /etc entry is a
// resource that is absent, not a failure: it resolves nowhere, so there is
// nothing to mount and no reason to abort the session. The compat links keep
// their own stricter rule (TestDiscoverSystemRejectsUnsafeCompatLinks).
func TestDiscoverSystemSkipsDanglingCandidate(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"usr", "etc"} {
		if err := os.MkdirAll(filepath.Join(root, p), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	broken := filepath.Join(root, "etc", "resolv.conf")
	if err := os.Symlink(filepath.Join(root, "missing-target"), broken); err != nil {
		t.Fatal(err)
	}
	in := fixture(t)
	if err := in.discoverSystem(root); err != nil {
		t.Fatalf("dangling candidate aborted discovery: %v", err)
	}
	for _, s := range in.System() {
		if s.Path == broken || s.Target == "/etc/resolv.conf" {
			t.Fatalf("dangling candidate was mounted: %+v", s)
		}
	}
}

func TestSkillsRejectIntermediateSymlinks(t *testing.T) {
	for _, level := range []string{"shared", "agents"} {
		t.Run(level, func(t *testing.T) {
			in := fixture(t)
			path := in.paths.Shared
			if level == "agents" {
				path = filepath.Dir(in.paths.SharedSkills)
			}
			if err := os.Rename(path, path+"-saved"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(path+"-saved", path); err != nil {
				t.Fatal(err)
			}
			if err := in.observeSkills(); err == nil {
				t.Fatal("accepted intermediate link")
			}
		})
	}
}

func TestAlternativesRetainedAndProtectedTargetsRejected(t *testing.T) {
	for _, protected := range []bool{false, true} {
		t.Run(fmt.Sprint(protected), func(t *testing.T) {
			root := t.TempDir()
			for _, p := range []string{"usr", "etc"} {
				if err := os.Mkdir(filepath.Join(root, p), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			in := fixture(t)
			path := filepath.Join(root, "etc/alternatives")
			if protected {
				if err := os.Symlink(in.account.Home, path); err != nil {
					t.Fatal(err)
				}
				if err := in.discoverSystem(root); err == nil {
					t.Fatal("exposed protected home")
				}
				return
			}
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := in.discoverSystem(root); err != nil {
				t.Fatal(err)
			}
			for _, source := range in.System() {
				if source.Target == "/etc/alternatives" {
					f, err := in.SourceFile(source.Path)
					if err != nil || f == nil {
						t.Fatal("source not retained", err)
					}
					return
				}
			}
			t.Fatal("alternatives omitted")
		})
	}
}
