// Package host contains account and path resolution.
package host

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
)

// Account is the invoking user's trusted passwd identity.
type Account struct {
	UID      int
	Username string
	Home     string
}

// Paths are the persistent layout paths.
type Paths struct {
	Root         string
	Profiles     string
	Shared       string
	SharedSkills string
}

var (
	// ErrProfileNotFound classifies an absent profile.
	ErrProfileNotFound = errors.New("profile does not exist")
	// ErrProfileExists classifies an explicit create of an existing profile.
	ErrProfileExists = errors.New("profile already exists")
)

// ResolveAccount reads the current non-root account from the passwd database.
func ResolveAccount() (Account, error) {
	return resolveAccount(os.Getuid(), user.LookupId)
}

func resolveAccount(uid int, lookup func(string) (*user.User, error)) (Account, error) {
	if uid == 0 {
		return Account{}, errors.New("abx must not run as root")
	}
	u, err := lookup(strconv.Itoa(uid))
	if err != nil {
		return Account{}, fmt.Errorf("look up passwd entry for uid %d: %w", uid, err)
	}
	if u == nil {
		return Account{}, fmt.Errorf("look up passwd entry for uid %d: empty result", uid)
	}
	passwdUID, err := strconv.Atoi(u.Uid)
	if err != nil {
		return Account{}, fmt.Errorf("parse passwd uid %q: %w", u.Uid, err)
	}
	if passwdUID != uid {
		return Account{}, fmt.Errorf("passwd uid %d does not match invoking uid %d", passwdUID, uid)
	}
	return accountFromPasswd(passwdUID, u.Username, u.HomeDir)
}

func accountFromPasswd(uid int, username, home string) (Account, error) {
	if uid <= 0 {
		return Account{}, fmt.Errorf("invalid non-root uid %d", uid)
	}
	if username == "" {
		return Account{}, errors.New("passwd entry has empty username")
	}
	if !filepath.IsAbs(home) {
		return Account{}, fmt.Errorf("passwd home %q is not absolute", home)
	}
	canonical, err := filepath.EvalSymlinks(home)
	if err != nil {
		return Account{}, fmt.Errorf("canonicalize passwd home %q: %w", home, err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return Account{}, fmt.Errorf("stat passwd home %q: %w", canonical, err)
	}
	if !info.IsDir() {
		return Account{}, fmt.Errorf("passwd home %q is not a directory", canonical)
	}
	return Account{UID: uid, Username: username, Home: canonical}, nil
}

// ResolvePaths derives the data root. Relative XDG_DATA_HOME is ignored.
func ResolvePaths(account Account, xdgDataHome string) (Paths, error) {
	if !filepath.IsAbs(account.Home) {
		return Paths{}, fmt.Errorf("real home %q is not absolute", account.Home)
	}
	base := filepath.Join(account.Home, ".local", "share")
	if filepath.IsAbs(xdgDataHome) {
		base = filepath.Clean(xdgDataHome)
	}
	canonicalBase, err := canonicalizeExisting(base)
	if err != nil {
		return Paths{}, fmt.Errorf("canonicalize data base: %w", err)
	}
	if canonicalBase == string(filepath.Separator) {
		return Paths{}, fmt.Errorf("data base %q is the filesystem root", canonicalBase)
	}
	for _, relative := range sensitiveHomeTrees {
		sensitive, err := canonicalProtectedTree(filepath.Join(filepath.Clean(account.Home), relative))
		if err != nil {
			return Paths{}, err
		}
		if within(canonicalBase, sensitive) {
			return Paths{}, fmt.Errorf("data base %q overlaps sensitive home tree %q", canonicalBase, sensitive)
		}
	}
	if account.UID > 0 {
		if err := checkDataBaseOwner(canonicalBase, account.UID); err != nil {
			return Paths{}, err
		}
	}
	// Keep the launcher-owned final component lexical. Existing ancestor
	// symlinks are resolved above, but an existing data root must still be
	// inspected and rejected as a symlink by the layout validators.
	root := filepath.Join(canonicalBase, "abx")
	return Paths{
		Root:         root,
		Profiles:     filepath.Join(root, "profiles"),
		Shared:       filepath.Join(root, "shared"),
		SharedSkills: filepath.Join(root, "shared", "agents", "skills"),
	}, nil
}

func canonicalizeExisting(path string) (string, error) {
	path = filepath.Clean(path)
	missing := []string{}
	current := path
	for {
		_, err := os.Lstat(current)
		if err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no existing ancestor for %q", path)
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

// checkDataBaseOwner requires the longest existing ancestor of the data base
// to be a directory owned by the invoking user. This rejects roots under
// another user's directory or under root-owned world-writable parents
// without requiring an exact 0700 mode on shared ancestors such as
// ~/.local/share or test TempDirs.
func checkDataBaseOwner(canonicalBase string, uid int) error {
	current := canonicalBase
	for {
		info, err := os.Stat(current)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("data base ancestor %q is not a directory", current)
			}
			st, ok := info.Sys().(*syscall.Stat_t)
			if !ok {
				return fmt.Errorf("data base ancestor %q has unknown owner", current)
			}
			if int(st.Uid) != uid {
				return fmt.Errorf("data base ancestor %q is not owned by the invoking user", current)
			}
			return nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("inspect data base ancestor %q: %w", current, err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return fmt.Errorf("no existing ancestor for data base %q", canonicalBase)
		}
		current = parent
	}
}
