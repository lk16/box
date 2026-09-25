package session

import (
	"fmt"
	"path/filepath"
	"slices"

	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/fail"
	"github.com/lk16/box/internal/project"
)

// Bundle is one member's committed history on its way into the sandbox, and where its clone belongs.
type Bundle struct {
	Member     config.Member
	Path       string
	BundleFile string
}

// hasBase says whether the branch a member's clone starts from is one origin has.
func (s Session) hasBase(path, base string) bool {
	return s.succeeds(project.Git(path, "rev-parse", "--verify", "refs/remotes/origin/"+base))
}

// bundleMember packs the history one member's clone is made from into the directory.
func (s Session) bundleMember(member config.Member, path string, number int, directory string) (Bundle, error) {
	if !s.hasBase(path, member.Branch) {
		return Bundle{}, fail.Errorf("%s at %s has no branch %s on origin", member.Name, path, member.Branch)
	}
	bundleFile := filepath.Join(directory, fmt.Sprintf("%d-%s.bundle", number, filepath.Base(path)))
	if !s.succeeds(project.Git(path, "bundle", "create", bundleFile, "--remotes=origin")) {
		return Bundle{}, fail.Errorf("git could not bundle %s at %s", member.Name, path)
	}
	return Bundle{Member: member, Path: path, BundleFile: bundleFile}, nil
}

// BundleMembers fetches every member at once, then packs what each clone is made from.
func (s Session) BundleMembers(settings config.Config, proj project.Project, directory string) ([]Bundle, error) {
	var paths []string
	for _, checkout := range project.MemberCheckouts(settings, proj) {
		paths = append(paths, checkout.Path)
	}
	if err := s.fetchMembers(paths); err != nil {
		return nil, err
	}
	var bundles []Bundle
	for number, member := range settings.Repos {
		bundle, err := s.bundleMember(member, paths[number], number+1, directory)
		if err != nil {
			return nil, err
		}
		bundles = append(bundles, bundle)
	}
	return bundles, nil
}

// CloneCommands assemble what turns a copied bundle into a clone on the member's own host path.
func CloneCommands(bundle Bundle, sandboxName string) [][]string {
	inside := config.SandboxTemp + "/" + filepath.Base(bundle.BundleFile)
	path := bundle.Path
	base := bundle.Member.Branch
	root := []string{"sbx", "exec", "-u", "root", sandboxName}
	user := []string{"sbx", "exec", sandboxName}
	owner := config.SandboxUser + ":" + config.SandboxUser
	return [][]string{
		{"sbx", "cp", bundle.BundleFile, sandboxName + ":" + inside},
		// A member's parent need not belong to the default user, and git init would fail there.
		with(root, "mkdir", "-p", path),
		with(root, "chown", owner, path),
		with(user, "git", "init", "-q", path),
		// The declared origin names the project alike for everyone, and holds no host credential.
		with(user, "git", "-C", path, "remote", "add", "origin", bundle.Member.GitOrigin),
		with(user, "git", "-C", path, "fetch", "-q", inside, "refs/remotes/origin/*:refs/remotes/origin/*"),
		with(user, "git", "-C", path, "switch", "-q", "-c", base, "--track", "origin/"+base),
	}
}

// with appends arguments to a prefix in a new slice, so no two commands share the prefix's storage.
func with(prefix []string, arguments ...string) []string {
	return slices.Concat(prefix, arguments)
}

// CloneMember puts one member's clone in the sandbox, saying whether every step of it worked.
func (s Session) CloneMember(bundle Bundle, sandboxName string) bool {
	for _, command := range CloneCommands(bundle, sandboxName) {
		if !s.succeeds(command) {
			return false
		}
	}
	return true
}

// FetchMemberWork bundles one member's branches inside the sandbox and fetches them into the member here.
func (s Session) FetchMemberWork(checkout project.Checkout, sandboxName, directory string) bool {
	name := sandboxName + "-" + filepath.Base(checkout.Path) + ".bundle"
	inside := config.SandboxTemp + "/" + name
	bundle := project.Git(checkout.Path, "bundle", "create", inside, "--branches")
	if !s.succeeds(append([]string{"sbx", "exec", sandboxName}, bundle...)) {
		return false
	}
	here := filepath.Join(directory, name)
	if !s.succeeds([]string{"sbx", "cp", sandboxName + ":" + inside, here}) {
		return false
	}
	refspec := fmt.Sprintf("+refs/heads/*:%s/%s/*", config.SandboxRefs, sandboxName)
	return s.succeeds(project.Git(checkout.Path, "fetch", here, refspec))
}

// fetchCommittedWork brings every clone's commits back, or names the repository whose work stayed.
func (s Session) fetchCommittedWork(members []project.Checkout, sandboxName, directory string) (project.Checkout, bool) {
	// A member is a remote of nothing, so its commits come back the way they went in: as a bundle.
	for _, checkout := range members {
		if !s.FetchMemberWork(checkout, sandboxName, directory) {
			return checkout, false
		}
	}
	return project.Checkout{}, true
}
