package session

import (
	"os"
	"strings"

	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/project"
)

// printRecovery says how to look inside a kept sandbox, take work out of it, and remove it by hand.
func (s Session) printRecovery(path, sandboxName string) {
	s.Deps.Console.Warn("Inspect:  sbx exec %s git -C %s diff", sandboxName, path)
	s.Deps.Console.Warn("Recover:  sbx cp %s:%s/<file> .", sandboxName, path)
	s.Deps.Console.Warn("Then remove manually once safe: sbx rm --force %s", sandboxName)
}

// warnDirty tells the user how to recover uncommitted work left behind in a sandbox.
func (s Session) warnDirty(path, sandboxName, dirty string) {
	s.Deps.Console.Warn("WARNING: %s in sandbox %s has uncommitted changes -- not removing it.", path, sandboxName)
	// git status ends in a newline, and the blank line it leaves separates it from what follows.
	s.Deps.Console.Warn("%s", dirty)
	s.printRecovery(path, sandboxName)
}

// warnUnchecked tells the user box could not find out whether removing a sandbox would lose work.
func (s Session) warnUnchecked(path, sandboxName, reason string) {
	s.Deps.Console.Warn("WARNING: %s.", reason)
	s.Deps.Console.Warn("box cannot tell whether sandbox %s holds work -- not removing it.", sandboxName)
	s.printRecovery(path, sandboxName)
}

// clonesAreCommitted says whether every clone is committed, warning about the first that is not.
func (s Session) clonesAreCommitted(checkouts []project.Checkout, sandboxName string) bool {
	for _, checkout := range checkouts {
		status := s.Deps.Run.Capture(StatusCommand(checkout.Path, sandboxName))
		if status.Code != 0 {
			s.warnUnchecked(checkout.Path, sandboxName, "sbx exec could not read the sandbox's git status")
			return false
		}
		if strings.TrimSpace(status.Stdout) != "" {
			s.warnDirty(checkout.Path, sandboxName, status.Stdout)
			return false
		}
	}
	return true
}

// Cleanup pulls committed work back, then drops the sandbox unless work would be lost.
func (s Session) Cleanup(settings config.Config, launch Launch) {
	sandboxName := launch.SandboxName
	main := launch.Project.MainCheckout()
	members := project.MemberCheckouts(settings, launch.Project)
	remote := "sandbox-" + sandboxName
	// Removal follows, so "I could not tell" must never be read as "there is nothing to lose".
	if !s.succeeds([]string{"git", "fetch", remote}) {
		s.warnUnchecked(main.Path, sandboxName, "git fetch "+remote+" failed, so its commits are not here")
		return
	}
	directory, err := os.MkdirTemp("", "box")
	if err != nil {
		s.warnUnchecked(main.Path, sandboxName, "box could not make a directory to bring the bundles back in")
		return
	}
	defer func() { _ = os.RemoveAll(directory) }()
	if unfetched, ok := s.fetchCommittedWork(members, sandboxName, directory); !ok {
		s.warnUnchecked(unfetched.Path, sandboxName, "the sandbox's commits in "+unfetched.Path+" are not here")
		return
	}
	checkouts := append([]project.Checkout{main}, members...)
	if !s.clonesAreCommitted(checkouts, sandboxName) {
		return
	}
	for _, checkout := range checkouts {
		s.SettleSandboxRefs(checkout, sandboxName)
	}
	s.DropSecrets(settings.SecretHosts, sandboxName)
	s.Deps.Run.Attach([]string{"sbx", "rm", "--force", sandboxName}, nil)
}
