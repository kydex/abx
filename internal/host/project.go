// Package host contains project policy; see docs/development.md.
package host

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var systemRoots = []string{
	"/bin", "/boot", "/dev", "/etc", "/lib", "/lib64", "/opt", "/proc",
	"/root", "/run", "/sbin", "/sys", "/usr", "/var",
	"/srv", "/mnt", "/media", "/snap",
}

var sensitiveHomeTrees = []string{
	".config", ".ssh", ".gnupg", "dotfiles",
	".aws", ".docker", ".kube", ".pki", ".cache",
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
	if err := checkProjectOwner(info, project); err != nil {
		return "", err
	}
	project = filepath.Clean(project)
	if project == string(filepath.Separator) {
		return "", fmt.Errorf("project %q is the filesystem root", project)
	}
	for _, exact := range exactProtectedRoots {
		if project == exact {
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
		sensitive, err := canonicalProtectedTree(filepath.Join(realHome, relative))
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
func siblingOutsideHome(realHome, project, homeRoot string) bool {
	if resolved, err := filepath.EvalSymlinks(homeRoot); err == nil {
		homeRoot = resolved
	}
	return within(realHome, homeRoot) && within(project, homeRoot) && !within(project, realHome)
}

func within(path, root string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && !filepath.IsAbs(rel) && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

func canonicalProtectedTree(path string) (string, error) {
	if _, err := os.Lstat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return filepath.Clean(path), nil
		}
		return "", fmt.Errorf("inspect protected tree %q: %w", path, err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("canonicalize protected tree %q: %w (remove the dangling symlink if one points here)", path, err)
	}
	return filepath.Clean(resolved), nil
}
