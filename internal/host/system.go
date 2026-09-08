package host

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (in *Inputs) discoverSystem(root string) error {
	physical := func(p string) string { return filepath.Join(root, strings.TrimPrefix(p, "/")) }
	add := func(path, target string) error {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		if overlaps(resolved, in.account.Home) || overlaps(resolved, in.paths.Root) {
			return fmt.Errorf("protected data overlaps system source %q", resolved)
		}
		if err = in.retain(resolved); err != nil {
			return err
		}
		in.system = append(in.system, Source{Path: resolved, Target: target})
		return nil
	}
	if err := add(physical("/usr"), "/usr"); err != nil {
		return err
	}
	if !in.objects[in.system[0].Path].info.IsDir() {
		return fmt.Errorf("/usr is not a directory")
	}
	for _, p := range []string{"/bin", "/sbin", "/lib", "/lib64"} {
		path := physical(p)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, e := os.Readlink(path)
			if e != nil {
				return e
			}
			in.links[path] = link
			in.system = append(in.system, Source{Target: p, Link: link})
		} else if err = add(path, p); err != nil {
			return err
		}
	}
	for _, name := range []string{"hosts", "resolv.conf", "nsswitch.conf", "gai.conf", "passwd", "group", "ssl", "ca-certificates", "pki", "ld.so.cache", "ld.so.conf", "ld.so.conf.d", "localtime", "timezone", "os-release", "shells", "alternatives"} {
		p := "/etc/" + name
		path := physical(p)
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if err := add(path, p); err != nil {
			return err
		}
	}
	return nil
}
