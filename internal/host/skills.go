package host

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func (in *Inputs) observeSkills() error {
	p := in.paths.SharedSkills
	if _, err := os.Lstat(p); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := in.private(in.paths.Shared); err != nil {
		return err
	}
	for _, dir := range []string{filepath.Dir(p), p} {
		if err := in.retain(dir); err != nil {
			return err
		}
		if !in.objects[dir].info.IsDir() {
			return fmt.Errorf("skills source is not a directory: %q", dir)
		}
	}
	in.skills = p
	return in.checkTargets()
}
func (in *Inputs) checkTargets() error {
	for _, relative := range []string{".agents", ".agents/skills"} {
		fd, err := unix.Openat2(int(in.objects[in.home].file.Fd()), relative, &unix.OpenHow{Flags: unix.O_PATH | unix.O_CLOEXEC | unix.O_DIRECTORY, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
		if errors.Is(err, unix.ENOENT) {
			continue
		}
		if err != nil {
			return fmt.Errorf("unsafe skills target %s: %w", relative, err)
		}
		if err = unix.Close(fd); err != nil {
			return err
		}
	}
	return nil
}

// Prepare creates the optional .agents parent before Bubblewrap starts.
// Bubblewrap may also leave its empty skills mountpoint in the writable home.
func (in *Inputs) Prepare() error {
	if err := in.Revalidate(); err != nil {
		return err
	}
	if in.skills == "" {
		return nil
	}
	if err := in.checkTargets(); err != nil {
		return err
	}
	home := int(in.objects[in.home].file.Fd())
	err := unix.Mkdirat(home, ".agents", 0700)
	if err != nil && !errors.Is(err, unix.EEXIST) {
		return fmt.Errorf("create skills parent: %w", err)
	}
	if err == nil {
		fd, e := unix.Openat2(home, ".agents", &unix.OpenHow{Flags: unix.O_PATH | unix.O_CLOEXEC | unix.O_DIRECTORY, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS})
		if e != nil {
			return fmt.Errorf("open created skills parent (partial state remains): %w", e)
		}
		e = unix.Fchmodat(fd, "", 0700, unix.AT_EMPTY_PATH)
		if e = errors.Join(e, unix.Close(fd)); e != nil {
			return fmt.Errorf("set created skills parent mode (partial state remains): %w", e)
		}
	}
	return in.checkTargets()
}
