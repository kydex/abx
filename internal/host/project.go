package host

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

var systemRoots = []string{
	"/bin", "/boot", "/dev", "/etc", "/lib", "/lib64", "/opt", "/proc",
	"/root", "/run", "/sbin", "/sys", "/usr", "/var",
	"/srv", "/mnt", "/media", "/snap",
}

var sensitiveHomeTrees = []string{
	".config", ".ssh", ".gnupg", "dotfiles",
	".aws", ".docker", ".kube", ".pki", ".cache",
	".netrc", ".git-credentials", ".password-store", ".npmrc",
	".gem/credentials", ".local/share/keyrings", ".mozilla", ".thunderbird",
}

// exactProtectedRoots rejects exact matches without blocking subdirectories.
// Overlap checks above would reject every TempDir-based test project if /tmp
// were listed in systemRoots, so keep world-writable roots exact-only here.
// /var/tmp and /dev/shm are already covered by /var and /dev in systemRoots,
// so they only change the error text here. Do not remove /var or /dev from
// systemRoots: without them, subdirectories outside these exact paths would pass.
var exactProtectedRoots = []string{
	"/tmp", "/var/tmp", "/dev/shm",
}

// isExactProtectedRoot reports whether project is an exact-protected root
// itself, following a symlink alias the way canonicalProtectedTree does for
// system trees. Without the resolved comparison, a /tmp that is a link to a
// tree outside the protected roots would canonicalize past the literal.
func isExactProtectedRoot(project, exact string) (bool, error) {
	if project == exact {
		return true, nil
	}
	resolved, err := canonicalProtectedTree(exact)
	if err != nil {
		return false, err
	}
	return project == resolved, nil
}

// ResolveProject canonicalizes and validates a candidate project directory.
func ResolveProject(cwd, realHome, dataRoot string) (string, error) {
	return resolveProject(cwd, realHome, dataRoot, systemRoots)
}

func resolveProject(cwd, realHome, dataRoot string, protectedRoots []string) (string, error) {
	if !filepath.IsAbs(cwd) {
		return "", fmt.Errorf("current directory %q is not absolute", cwd)
	}
	project, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return "", fmt.Errorf("canonicalize current directory %q: %w", cwd, err)
	}
	info, err := os.Stat(project)
	if err != nil {
		return "", fmt.Errorf("stat current directory %q: %w", project, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("current path %q is not a directory", project)
	}
	project = filepath.Clean(project)
	if project == "/" {
		return "", fmt.Errorf("project %q is the filesystem root", project)
	}
	for _, exact := range exactProtectedRoots {
		match, err := isExactProtectedRoot(project, exact)
		if err != nil {
			return "", err
		}
		if match {
			return "", fmt.Errorf("project %q is a protected system path", project)
		}
	}
	for _, root := range protectedRoots {
		resolved, err := canonicalProtectedTree(root)
		if err != nil {
			return "", err
		}
		if overlaps(project, root) || overlaps(project, resolved) {
			return "", fmt.Errorf("project %q overlaps protected system tree %q", project, root)
		}
	}
	realHome = filepath.Clean(realHome)
	if project == realHome || within(realHome, project) {
		return "", fmt.Errorf("project %q is the real home or its ancestor", project)
	}
	// Reject another user's home such as /home/bob/proj when realHome is
	// /home/alice. Projects inside the own home remain allowed. Scoped to
	// /home so isolated test directories under /tmp keep working.
	if siblingOutsideHome(realHome, project, "/home") {
		return "", fmt.Errorf("project %q is outside the real home %q", project, realHome)
	}
	for _, relative := range sensitiveHomeTrees {
		sensitive, err := canonicalSensitiveTree(filepath.Join(realHome, relative))
		if err != nil {
			return "", err
		}
		if overlaps(project, sensitive) {
			return "", fmt.Errorf("project %q overlaps sensitive home tree %q", project, sensitive)
		}
	}
	if overlaps(project, filepath.Clean(dataRoot)) {
		return "", fmt.Errorf("project %q overlaps abx data root %q", project, dataRoot)
	}
	return project, nil
}

func overlaps(a, b string) bool { return within(a, b) || within(b, a) }

// siblingOutsideHome reports a project under the shared home root but outside
// the invoking user's home. homeRoot is canonicalized once so a symlinked
// /home does not silently disable the rule.
//
// The invoking user's own home does not have to live under homeRoot. Scoping
// the rule to that arrangement switched it off entirely wherever the home is
// arranged differently -- /var/lib/users/alice, or a container root -- and on
// those hosts /home/bob/proj mounted writable. The condition is now simply that
// the project sits under homeRoot and not under the caller's own home.
func siblingOutsideHome(realHome, project, homeRoot string) bool {
	if resolved, err := filepath.EvalSymlinks(homeRoot); err == nil {
		homeRoot = resolved
	}
	return within(project, homeRoot) && !within(project, realHome)
}

func within(path, root string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && !filepath.IsAbs(rel) && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

// canonicalProtectedTree resolves a protected system tree for the overlap
// rules. A root that cannot resolve must not disable every project command:
// there is nothing to mount at that path, and the project itself is already
// resolved. The resolution follows canonicalSensitiveTree.
func canonicalProtectedTree(path string) (string, error) {
	return canonicalSensitiveTree(path)
}

// canonicalSensitiveTree resolves a sensitive home tree for the two overlap
// rules, and it is deliberately forgiving about a tree that does not resolve:
// a dangling or cyclic link still says where it is headed, and that target is
// what has to be compared against, because a path that does not exist today can
// sit inside the project or the data base tomorrow. A path that cannot exist at
// all -- missing, blocked by a non-directory in the middle, or caught in a
// cycle above the final component -- is compared literally.
//
// The literal link would be safe too — nothing can exist underneath a link that
// resolves nowhere, and both callers compare against an already resolved path —
// but the target is the stronger statement and it costs nothing.
func canonicalSensitiveTree(path string) (string, error) {
	if _, err := os.Lstat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.ENOTDIR) {
			return filepath.Clean(path), nil
		}
		return "", fmt.Errorf("inspect protected tree %q: %w", path, err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return filepath.Clean(resolved), nil
	}
	// The link did not resolve: it is dangling, cyclic, or unreadable. Its
	// own text still says where it is headed, and that literal target is what
	// the overlap rules compare against. Only a path without a link to read
	// is refused.
	target, readErr := os.Readlink(path)
	if readErr != nil {
		return "", fmt.Errorf("canonicalize protected tree %q: %w", path, err)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	return filepath.Clean(target), nil
}
