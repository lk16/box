package config

import (
	"path/filepath"
	"strings"
)

// Under joins a path to a directory, except that an absolute path names itself and wins outright.
func Under(directory, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(directory, path)
}

// Resolve makes a path absolute with its symlinks followed, as far as the path exists.
func Resolve(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if real, err := filepath.EvalSymlinks(absolute); err == nil {
		return real
	}
	// A path that is not there yet still has a real parent, which is what a message should name.
	parent, base := filepath.Split(absolute)
	if real, err := filepath.EvalSymlinks(filepath.Clean(parent)); err == nil {
		return filepath.Join(real, base)
	}
	return absolute
}

// IsInside says whether a path lies in a directory, symlinks resolved, the directory itself included.
func IsInside(path, directory string) bool {
	relative, err := filepath.Rel(Resolve(directory), Resolve(path))
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// PathBelow names a directory relative to a root, which is nothing when it is the root itself.
func PathBelow(root, directory string) string {
	relative, err := filepath.Rel(Resolve(root), Resolve(directory))
	if err != nil || relative == "." {
		return ""
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ""
	}
	return relative
}
