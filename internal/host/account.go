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

// ResolveAccount reads the current non-root account from the passwd database
// after checking the database file mode.
func ResolveAccount() (Account, error) {
	return resolveAccountFrom(os.Getuid(), user.LookupId, passwdPath)
}

// resolveAccountFrom is the deterministic passwd-file test seam. Every command
// reaches the account through it, so profile commands cannot skip the mode
// check that session commands perform.
func resolveAccountFrom(uid int, lookup func(string) (*user.User, error), passwdFile string) (Account, error) {
	if uid == 0 {
		return Account{}, errors.New("abx must not run as root")
	}
	if err := checkPasswdFile(passwdFile); err != nil {
		return Account{}, err
	}
	return resolveAccount(uid, lookup)
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
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return Account{}, fmt.Errorf("passwd home %q does not exist; it must be an existing directory owned by uid %d (see docs/installation.md): %w", home, uid, err)
		case errors.Is(err, fs.ErrPermission):
			return Account{}, fmt.Errorf("passwd home %q is not accessible; it must be an existing directory owned by uid %d (see docs/installation.md): %w", home, uid, err)
		case errors.Is(err, syscall.ELOOP):
			return Account{}, fmt.Errorf("passwd home %q is a symlink loop; it must be an existing directory owned by uid %d (see docs/installation.md): %w", home, uid, err)
		default:
			return Account{}, fmt.Errorf("canonicalize passwd home %q: %w", home, err)
		}
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return Account{}, fmt.Errorf("stat passwd home %q: %w", canonical, err)
	}
	if !info.IsDir() {
		return Account{}, fmt.Errorf("passwd home %q is not a directory", canonical)
	}
	if err := checkHomeTrust(info, canonical, uid); err != nil {
		return Account{}, err
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
	if canonicalBase == "/" {
		return Paths{}, fmt.Errorf("data base %q is the filesystem root", canonicalBase)
	}
	for _, relative := range sensitiveHomeTrees {
		sensitive, err := canonicalSensitiveTreePaths(filepath.Join(filepath.Clean(account.Home), relative))
		if err != nil {
			return Paths{}, err
		}
		for _, path := range sensitive {
			if within(canonicalBase, path) {
				return Paths{}, fmt.Errorf("data base %q overlaps sensitive home tree %q", canonicalBase, path)
			}
		}
	}
	if err := checkDataBaseOwner(canonicalBase, account.UID); err != nil {
		return Paths{}, err
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
				return "", fmt.Errorf("canonicalize data base ancestor %q: %w", current, err)
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

// checkHomeTrust pins both preconditions for the account home: it must belong
// to the invoking user and must not be world-writable. Group-writable homes are
// accepted under the same team policy as the data-base ancestors; "no local
// user may write it" would promise more than this check enforces. The home is
// named by the passwd database and is the default storage base and the
// location of the verify sentinel; the sandbox home is always the selected
// profile. A home
// owned by somebody else is one somebody else can replace. Containers that
// hand the process a root-owned home are no longer supported: ABX is for real
// hosts, where the home belongs to the invoking user.
func checkHomeTrust(info os.FileInfo, home string, uid int) error {
	if info.Mode().Perm()&0o002 != 0 {
		return fmt.Errorf("passwd home %q is world-writable", home)
	}
	st := info.Sys().(*syscall.Stat_t)
	if int(st.Uid) != uid {
		return fmt.Errorf("passwd home %q is owned by uid %d, not by uid %d", home, st.Uid, uid)
	}
	return nil
}

// checkDataBaseOwner requires the nearest existing ancestor of the data base
// to be a directory owned by the invoking user, without requiring an exact
// 0700 mode on shared ancestors such as ~/.local/share or test TempDirs. The
// walk stops at that ancestor: directories above it are not inspected, so the
// rule permits a data base under a user-owned directory that itself sits under
// a world-writable parent. A world-writable ancestor is rejected unless
// sticky, since any local user could otherwise replace the storage root.
// Group-writable ancestors stay allowed: that is a team policy, not an
// attack, and refusing it breaks umask-002 machines wholesale.
func checkDataBaseOwner(canonicalBase string, uid int) error {
	current := canonicalBase
	for {
		info, err := os.Stat(current)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("data base ancestor %q is not a directory", current)
			}
			st := info.Sys().(*syscall.Stat_t)
			if int(st.Uid) != uid {
				return fmt.Errorf("data base ancestor %q is not owned by the invoking user", current)
			}
			// Only world-writable ancestors are rejected: group-writable is a
			// deliberate team policy (umask 002, shared groups, dotfile
			// managers) and the sticky bit is the container's own answer to it.
			if info.Mode().Perm()&0o002 != 0 && info.Mode()&os.ModeSticky == 0 {
				return fmt.Errorf("data base ancestor %q is world-writable", current)
			}
			// The sticky bit is allowed above, setuid and setgid are not: a
			// setgid directory makes everything created under it inherit a
			// group, so the storage root could end up group-owned by a path
			// this check never inspects. The passwd database is checked the
			// same way.
			if info.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 {
				return fmt.Errorf("data base ancestor %q has a setuid or setgid bit", current)
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
