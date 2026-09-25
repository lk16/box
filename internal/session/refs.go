package session

import (
	"fmt"
	"strings"

	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/project"
)

// SandboxRef is one ref a sandbox left behind, and the commit it points at.
type SandboxRef struct {
	RefName string
	Commit  string
}

// ParseSandboxRefs pulls the ref name and commit out of for-each-ref lines.
func ParseSandboxRefs(refsOutput string) []SandboxRef {
	var refs []SandboxRef
	for _, line := range strings.Split(refsOutput, "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 {
			refs = append(refs, SandboxRef{RefName: parts[0], Commit: parts[1]})
		}
	}
	return refs
}

// SandboxRefs reads the refs this sandbox's work was fetched into.
func (s Session) SandboxRefs(checkout project.Checkout, sandboxName string) []SandboxRef {
	command := project.Git(checkout.Path, "for-each-ref", "--format=%(refname) %(objectname)",
		config.SandboxRefs+"/"+sandboxName)
	return ParseSandboxRefs(s.capture(command))
}

// NewCommits names the commits a ref holds that its repository does not, the way git spells a range.
func NewCommits(checkout project.Checkout, commit string) []string {
	return []string{commit, "--not", checkout.KnownCommits}
}

// CountNewCommits counts the sandbox's commits this repository lacks, or nothing when git could not say.
func (s Session) CountNewCommits(checkout project.Checkout, commit string) string {
	command := append(project.Git(checkout.Path, "rev-list", "--count"), NewCommits(checkout, commit)...)
	return strings.TrimSpace(s.capture(command))
}

// NewCommitSubjects reads the subjects of the sandbox's commits, which a branch gets named after.
func (s Session) NewCommitSubjects(checkout project.Checkout, commit string) string {
	return s.capture(append(project.Git(checkout.Path, "log", "--format=%s"), NewCommits(checkout, commit)...))
}

// LocalBranchNames collects the branch names this repository already has.
func (s Session) LocalBranchNames(checkout project.Checkout) map[string]bool {
	command := project.Git(checkout.Path, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	names := map[string]bool{}
	for _, name := range strings.Fields(s.capture(command)) {
		names[name] = true
	}
	return names
}

// PickBranchName returns the suggested name, numbered from two when the repository already has it.
func PickBranchName(branch string, used map[string]bool) string {
	if !used[branch] {
		return branch
	}
	for number := 2; ; number++ {
		numbered := fmt.Sprintf("%s-%d", branch, number)
		if !used[numbered] {
			return numbered
		}
	}
}

// CreateBranch points a new branch at the sandbox's commit, saying whether git accepted it.
func (s Session) CreateBranch(checkout project.Checkout, branch, commit string) bool {
	return s.succeeds(project.Git(checkout.Path, "branch", branch, commit))
}

// DeleteRef drops a ref, which is safe to leave behind when it fails.
func (s Session) DeleteRef(checkout project.Checkout, refName string) {
	s.capture(project.Git(checkout.Path, "update-ref", "-d", refName))
}

// Plural renders a count and what it counts, so a single commit does not read as "1 commits".
func Plural(count, noun string) string {
	if count == "1" {
		return "1 " + noun
	}
	return count + " " + noun + "s"
}

// SettleRef turns one sandbox ref into a branch, drops it when it holds nothing, and keeps it otherwise.
func (s Session) SettleRef(checkout project.Checkout, ref SandboxRef) {
	// Every member's ref carries the same sandbox name, so only the path tells them apart.
	prefix := "box: " + checkout.Path + ":"
	count := s.CountNewCommits(checkout, ref.Commit)
	if count == "" {
		s.Deps.Console.Warn("%s git could not read %s, so it was kept.", prefix, ref.RefName)
		return
	}
	if count == "0" {
		s.DeleteRef(checkout, ref.RefName)
		s.Deps.Console.Warn("%s %s held no commits, so it was dropped.", prefix, ref.RefName)
		return
	}
	suggested := s.SuggestBranchName(s.NewCommitSubjects(checkout, ref.Commit))
	if suggested == "" {
		s.Deps.Console.Warn("%s naming a branch failed, so the work stayed on %s.", prefix, ref.RefName)
		return
	}
	branch := PickBranchName(suggested, s.LocalBranchNames(checkout))
	if !s.CreateBranch(checkout, branch, ref.Commit) {
		s.Deps.Console.Warn("%s git refused branch %s, so the work stayed on %s.", prefix, branch, ref.RefName)
		return
	}
	s.DeleteRef(checkout, ref.RefName)
	s.Deps.Console.Warn("%s branch %s holds %s from %s.", prefix, branch, Plural(count, "commit"), ref.RefName)
}

// SettleSandboxRefs gives the sandbox's committed work a branch, so nothing stays addressable only by ref.
func (s Session) SettleSandboxRefs(checkout project.Checkout, sandboxName string) {
	for _, ref := range s.SandboxRefs(checkout, sandboxName) {
		s.SettleRef(checkout, ref)
	}
}
