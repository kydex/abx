package host

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kydex/abx/internal/profile"
	"golang.org/x/sys/unix"
)

// ProfileStatus reports local configuration, not a successful sandbox preflight.
// Agent and optional skills failures are informational; mandatory layout failures
// are returned as errors by ShowProfile and ListProfiles.
type ProfileStatus struct {
	Name, Home  string
	Exists      bool
	Executable  string
	AgentError  error
	Skills      string
	SkillsError error
}

func profileInputs(account Account, paths Paths, name string) *Inputs {
	return &Inputs{account: account, paths: paths, home: filepath.Join(paths.Profiles, name), objects: map[string]object{}, links: map[string]string{}}
}

func (in *Inputs) readProfile() error {
	for _, p := range []string{in.paths.Root, in.paths.Profiles, in.home} {
		if err := in.private(p); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%w: %q", ErrProfileNotFound, in.home)
			}
			return err
		}
	}
	return nil
}

func ShowProfile(account Account, paths Paths, name string) (status ProfileStatus, result error) {
	if err := profile.ValidateName(name); err != nil {
		return status, err
	}
	in := profileInputs(account, paths, name)
	defer func() { result = errors.Join(result, in.Close()) }()
	status.Name, status.Home = name, in.home
	if err := in.readProfile(); errors.Is(err, ErrProfileNotFound) {
		return status, nil
	} else if err != nil {
		return status, err
	}
	status.Exists = true
	status.Executable, status.AgentError = resolveAgent(in.home, name)
	status.SkillsError = in.observeSkills()
	switch {
	case status.SkillsError != nil:
		status.Skills = "invalid"
	case in.skills != "":
		status.Skills = "configured"
	default:
		status.Skills = "not-configured"
	}
	return status, nil
}

func ListProfiles(account Account, paths Paths) (statuses []ProfileStatus, result error) {
	in := profileInputs(account, paths, "")
	defer func() { result = errors.Join(result, in.Close()) }()
	for _, p := range []string{paths.Root, paths.Profiles} {
		if err := in.private(p); errors.Is(err, os.ErrNotExist) {
			return nil, nil
		} else if err != nil {
			return nil, err
		}
	}
	// Read the retained directory, even if its original path was renamed.
	fd, err := unix.Openat(int(in.objects[paths.Profiles].file.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	dir := os.NewFile(uintptr(fd), paths.Profiles)
	entries, err := dir.ReadDir(-1)
	if err = errors.Join(err, dir.Close()); err != nil {
		return nil, err
	}
	// ReadDir on a file descriptor preserves filesystem order; sort explicitly.
	slices.SortFunc(entries, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	for _, entry := range entries {
		name := entry.Name()
		status, err := ShowProfile(account, paths, name)
		if err != nil {
			return nil, fmt.Errorf("profile entry %q: %w", name, err)
		}
		if !status.Exists {
			return nil, fmt.Errorf("profile entry %q disappeared while listing", name)
		}
		statuses = append(statuses, status)
	}
	return statuses, in.Revalidate()
}

// CreateProfile creates only missing directories. It never repairs or removes an
// existing object. Failed creation can leave newly created directories in place.
func CreateProfile(account Account, paths Paths, name string) (home string, result error) {
	if err := profile.ValidateName(name); err != nil {
		return "", err
	}
	if !filepath.IsAbs(paths.Root) || filepath.Clean(paths.Root) != paths.Root || filepath.Base(paths.Root) != "abx" || paths.Profiles != filepath.Join(paths.Root, "profiles") {
		return "", errors.New("invalid profile storage paths")
	}
	parent, err := openDirectoryAt(unix.AT_FDCWD, "/")
	if err != nil {
		return "", err
	}
	created := false
	defer func() {
		result = errors.Join(result, parent.Close())
		if result != nil && created {
			result = fmt.Errorf("profile creation incomplete; created directories may remain: %w", result)
		}
	}()
	components := strings.Split(strings.TrimPrefix(paths.Root, "/"), "/")
	components = append(components, "profiles", name)
	current := "/"
	for i, part := range components {
		current = filepath.Join(current, part)
		private := i >= len(components)-3
		exclusive := i == len(components)-1
		next, made, err := createDirectoryAt(parent, part, account.UID, private, exclusive)
		created = created || made
		if err != nil {
			return "", fmt.Errorf("prepare profile directory %q: %w", current, err)
		}
		closeErr := parent.Close()
		parent = next
		if closeErr != nil {
			return "", closeErr
		}
	}
	return filepath.Join(paths.Profiles, name), nil
}

func openDirectoryAt(parent int, name string) (*os.File, error) {
	fd, err := unix.Openat(parent, name, unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open directory %q: %w", name, err)
	}
	return os.NewFile(uintptr(fd), name), nil
}

func createDirectoryAt(parent *os.File, name string, uid int, private, exclusive bool) (dir *os.File, created bool, result error) {
	var err error
	if !exclusive {
		dir, err = openDirectoryAt(int(parent.Fd()), name)
		if err != nil && !errors.Is(err, unix.ENOENT) {
			return nil, false, err
		}
	}
	if dir == nil {
		err = unix.Mkdirat(int(parent.Fd()), name, 0700)
		switch {
		case err == nil:
			created = true
		case errors.Is(err, unix.EEXIST):
			if exclusive {
				return nil, false, fmt.Errorf("%w: %q", ErrProfileExists, name)
			}
		default:
			return nil, false, fmt.Errorf("create directory %q: %w", name, err)
		}
		dir, err = openDirectoryAt(int(parent.Fd()), name)
		if err != nil {
			return nil, created, err
		}
	}
	defer func() {
		if result != nil {
			result = errors.Join(result, dir.Close())
			dir = nil
		}
	}()
	if created {
		// O_PATH can open even mode 000 under a restrictive umask. Apply mode
		// through this descriptor, never chmod a pre-existing pathname.
		if err = unix.Fchmodat(int(dir.Fd()), "", 0700, unix.AT_EMPTY_PATH); err != nil {
			return dir, created, fmt.Errorf("set mode for new directory %q: %w", name, err)
		}
	}
	if private || created {
		info, err := dir.Stat()
		if err != nil {
			return dir, created, err
		}
		if err = privateInfo(name, info, uid); err != nil {
			return dir, created, err
		}
	}
	return dir, created, nil
}
