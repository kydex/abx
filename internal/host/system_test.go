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
		if err := os.MkdirAll(filepath.Join(root, p), 0700); err != nil {
			t.Fatal(err)
		}
	}
	bundle := filepath.Join(root, "etc/ca-certificates/extracted/tls-ca-bundle.pem")
	if err := os.WriteFile(bundle, []byte("fixture"), 0600); err != nil {
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
				if err := os.Mkdir(filepath.Join(root, p), 0700); err != nil {
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
			if err := os.Mkdir(path, 0700); err != nil {
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
