// Package project answers what is true of the repository box was run in, before anything is created.
package project

import (
	"fmt"
	"strings"

	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/system"
)

// Project is where box was run, the repository root sbx clones, and the path from one to the other.
type Project struct {
	WorkingDirectory string
	Root             string
	StartedIn        string
}

// Checkout is a repository a sandbox's work comes back to, and what it already holds.
type Checkout struct {
	Path         string
	KnownCommits string
}

// RepositoryRoot finds the root of the repository box runs in, which is what sbx clones.
func RepositoryRoot(runner system.Runner, workingDirectory string) string {
	root := strings.TrimSpace(system.Capture(runner, git(workingDirectory, "rev-parse", "--show-toplevel")))
	if root == "" {
		return workingDirectory
	}
	return root
}

// Build locates the repository sbx clones, and where inside it this session was started.
func Build(runner system.Runner, workingDirectory string) Project {
	root := RepositoryRoot(runner, workingDirectory)
	return Project{
		WorkingDirectory: workingDirectory,
		Root:             root,
		StartedIn:        config.PathBelow(root, workingDirectory),
	}
}

// ClonePath is what sbx clones: the working directory, unless box was run below the root.
func (p Project) ClonePath() string {
	if p.StartedIn == "" {
		return "."
	}
	return p.Root
}

// MainCheckout is the repository box runs in, whose work is what its own checkout does not hold.
func (p Project) MainCheckout() Checkout {
	return Checkout{Path: p.Root, KnownCommits: config.MainKnown}
}

// MemberCheckouts are the members, whose work is what none of their origin branches hold.
func MemberCheckouts(settings config.Config, project Project) []Checkout {
	checkouts := make([]Checkout, 0, len(settings.Repos))
	for _, member := range settings.Repos {
		path := config.MemberPath(project.WorkingDirectory, member)
		checkouts = append(checkouts, Checkout{Path: path, KnownCommits: config.MemberKnown})
	}
	return checkouts
}

// Checkouts lists every repository a sandbox's work comes back to: the one box runs in, then the members.
func Checkouts(settings config.Config, project Project) []Checkout {
	return append([]Checkout{project.MainCheckout()}, MemberCheckouts(settings, project)...)
}

// StartedInPrompt says which folder the session was started from, which is nothing at the root.
func (p Project) StartedInPrompt() string {
	if p.StartedIn == "" {
		return ""
	}
	return fmt.Sprintf(config.StartedInPrompt, p.StartedIn)
}

// MembersPrompt says where each member's clone is, what it starts on, and how its commits come back.
func MembersPrompt(settings config.Config, project Project) string {
	if len(settings.Repos) == 0 {
		return ""
	}
	var where []string
	for _, member := range settings.Repos {
		where = append(where, "  "+config.MemberPath(project.WorkingDirectory, member)+" on "+member.Branch)
	}
	return fmt.Sprintf(config.MembersPrompt, strings.Join(where, "\n"))
}

// MemberPrompts reads each member's own prompt, headed with the path its clone sits at.
func MemberPrompts(settings config.Config, project Project) ([]string, error) {
	var prompts []string
	for _, member := range settings.Members {
		if member.PromptFile == "" {
			continue
		}
		prompt, err := config.ReadSystemPrompt(member.PromptFile)
		if err != nil {
			return nil, err
		}
		path := config.MemberPath(project.WorkingDirectory, member.Member)
		prompts = append(prompts, path+":\n\n"+prompt)
	}
	return prompts, nil
}

// git assembles a git invocation in one directory, so no call depends on box's own working directory.
func git(directory string, arguments ...string) []string {
	return append([]string{"git", "-C", directory}, arguments...)
}

// ReachablePaths are every host directory the sandbox can read: the clone, its members and the mounts.
func ReachablePaths(settings config.Config, project Project) []string {
	paths := []string{project.Root}
	for _, member := range settings.Repos {
		paths = append(paths, config.MemberPath(project.WorkingDirectory, member))
	}
	for _, workspace := range settings.Mounts {
		paths = append(paths, config.MountTarget(workspace))
	}
	return paths
}
