package config

import (
	"slices"
	"sort"
	"strings"

	"github.com/lk16/box/internal/fail"
)

// Scoped names every entry by the member it came from, so a message says which one is at fault.
func Scoped(scope string, values Pairs) Pairs {
	scoped := make(Pairs, 0, len(values))
	for _, pair := range values {
		scoped = append(scoped, Pair{Name: scope + ": " + pair.Name, Value: pair.Value})
	}
	return scoped
}

// DescribeMounts lists named mounts with what the project expects to find in each.
func DescribeMounts(required Pairs, names []string) string {
	lines := make([]string, 0, len(names))
	for _, name := range names {
		lines = append(lines, "  "+name+": "+required.Get(name))
	}
	return strings.Join(lines, "\n")
}

// UnfilledMounts names the declared mounts that have no path on this machine yet.
func UnfilledMounts(required, provided Pairs) []string {
	var names []string
	for _, pair := range required {
		path := provided.Get(pair.Name)
		if path == "" || path == MountPlaceholder {
			names = append(names, pair.Name)
		}
	}
	return names
}

// RequireNamedMounts rejects a mounts file that does not answer the project's declaration exactly.
func RequireNamedMounts(required, provided Pairs) error {
	var unknown []string
	for _, pair := range provided {
		if !required.Has(pair.Name) {
			unknown = append(unknown, pair.Name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fail.Errorf("%s names mounts %s does not declare: %s",
			MountsFile, ConfigFile, strings.Join(unknown, ", "))
	}
	unfilled := UnfilledMounts(required, provided)
	if len(unfilled) == 0 {
		return nil
	}
	return fail.Errorf("%s has no path on this machine for:\n%s\nRun box gen to add every declared name, then replace %s.",
		MountsFile, DescribeMounts(required, unfilled), MountPlaceholder)
}

// OrderMounts returns the declared mounts' paths in declaration order, so the sbx args never shuffle.
func OrderMounts(required, provided Pairs) ([]string, error) {
	if err := RequireNamedMounts(required, provided); err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(required))
	for _, pair := range required {
		paths = append(paths, provided.Get(pair.Name))
	}
	return paths, nil
}

// MountPath returns the path a mount names, rejecting an empty one and a suffix box does not know.
func MountPath(path string) (string, error) {
	if path == "" {
		return "", fail.Errorf("a mount must name a path")
	}
	if strings.Contains(path, ":") {
		return "", fail.Errorf("mount %s has an unknown suffix; mounts are read-only unless you add :rw", path)
	}
	return path, nil
}

// ToWorkspace turns a configured mount into an sbx workspace spec, read-only unless :rw was asked for.
func ToWorkspace(mount string) (string, error) {
	writable := strings.HasSuffix(mount, ReadWriteSuffix)
	path, err := MountPath(strings.TrimSuffix(mount, ReadWriteSuffix))
	if err != nil {
		return "", err
	}
	if writable {
		return ResolvePath(path), nil
	}
	return ResolvePath(path) + ReadOnlySuffix, nil
}

// ToWorkspaces turns the configured mounts into sbx workspace specs, passing a repeated one once.
func ToWorkspaces(mounts []string) ([]string, error) {
	workspaces := []string{}
	for _, mount := range mounts {
		workspace, err := ToWorkspace(mount)
		if err != nil {
			return nil, err
		}
		if slices.Contains(workspaces, workspace) {
			continue
		}
		if err := requireOneAccess(workspaces, workspace); err != nil {
			return nil, err
		}
		workspaces = append(workspaces, workspace)
	}
	return workspaces, nil
}

// requireOneAccess refuses a path one place mounts read-only and another writable, since only one holds.
func requireOneAccess(workspaces []string, workspace string) error {
	for _, other := range workspaces {
		if MountTarget(other) == MountTarget(workspace) {
			return fail.Errorf("mount %s is asked for as both %s and %s", MountTarget(workspace), other, workspace)
		}
	}
	return nil
}

// MountTarget is the host path one sbx workspace spec names, writable or not.
func MountTarget(workspace string) string {
	return strings.TrimSuffix(workspace, ReadOnlySuffix)
}
