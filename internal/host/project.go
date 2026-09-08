// Project policy adapted from abx 0.7.9; see docs/development.md.
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
}

var sensitiveHomeTrees = []string{
	".config", ".ssh", ".gnupg", "dotfiles",
}

// Current resolves os.Getwd through the canonical validation path.
func Current(realHome, dataRoot string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get current directory: %w", err)
	}
	return ResolveProject(cwd, realHome, dataRoot)
}

// Resolve canonicalizes and validates a candidate project directory.
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
	if project == string(filepath.Separator) {
		return "", fmt.Errorf("project %q is the filesystem root", project)
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
		return "", fmt.Errorf("canonicalize protected tree %q: %w", path, err)
	}
	return filepath.Clean(resolved), nil
}
