package session_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/session"
	"github.com/lk16/box/internal/system"
)

// sandbox answers the commands Cleanup runs, from the few things that can go wrong.
type sandbox struct {
	dirty       string
	fetchFails  bool
	statusFails bool
	bundleFails bool
}

// answer stands in for everything Cleanup runs against git, sbx and the refs.
func (s sandbox) answer(arguments []string) system.Result {
	joined := strings.Join(arguments, " ")
	switch {
	case strings.HasPrefix(joined, "git fetch sandbox-"):
		return failWhen(s.fetchFails)
	case strings.Contains(joined, "bundle create"):
		return failWhen(s.bundleFails)
	case strings.Contains(joined, "status --porcelain"):
		if s.statusFails {
			return system.Result{Code: 1, Stderr: "no sandbox"}
		}
		return system.Result{Stdout: s.dirty}
	case arguments[0] == "claude":
		return system.Result{Stdout: "add-retry-logic\n"}
	case strings.Contains(joined, "rev-list --count"):
		return system.Result{Stdout: "0\n"}
	}
	return system.Result{}
}

// failWhen turns a test's "this step goes wrong" into the failed command it stands for.
func failWhen(fails bool) system.Result {
	if fails {
		return system.Result{Code: 1}
	}
	return system.Result{}
}

// cleaning runs Cleanup against a sandbox answering with the given replies.
func cleaning(t *testing.T, answers sandbox, settings config.Config, launch session.Launch) *fixture {
	t.Helper()
	run := newFixture(answers.answer)
	run.session.Cleanup(settings, launch)
	return run
}

func TestCleanupFetchesFromTheSandboxRemoteAndRemovesIt(t *testing.T) {
	run := cleaning(t, sandbox{}, fullConfig(), launchAt("/work/demo"))
	wantCommand(t, run.runner, "git", "fetch", "sandbox-demo-1")
	wantCommand(t, run.runner, "sbx", "rm", "--force", "demo-1")
}

func TestCleanupSettlesTheRefsBeforeRemovingTheSandbox(t *testing.T) {
	run := cleaning(t, sandbox{}, fullConfig(), launchAt("/work/demo"))
	settled := run.runner.Index("git", "-C", "/work/demo", "for-each-ref",
		"--format=%(refname) %(objectname)", config.SandboxRefs+"/demo-1")
	removed := run.runner.Index("sbx", "rm", "--force", "demo-1")
	if settled < 0 || settled > removed {
		t.Fatalf("the refs were settled at %d and the sandbox removed at %d", settled, removed)
	}
}

func TestCleanupKeepsTheRefsAndTheSandboxWhenTheTreeIsDirty(t *testing.T) {
	run := cleaning(t, sandbox{dirty: " M main.go\n"}, fullConfig(), launchAt("/work/demo"))
	wantNoCommand(t, run.runner, "sbx", "rm", "--force", "demo-1")
	wantWarning(t, run.console, "uncommitted changes")
	wantWarning(t, run.console, " M main.go")
}

func TestCleanupKeepsTheSandboxWhenTheFetchFails(t *testing.T) {
	run := cleaning(t, sandbox{fetchFails: true}, fullConfig(), launchAt("/work/demo"))
	wantNoCommand(t, run.runner, "sbx", "rm", "--force", "demo-1")
	wantWarning(t, run.console, "git fetch sandbox-demo-1 failed")
}

func TestCleanupKeepsTheSandboxWhenTheDirtyCheckFails(t *testing.T) {
	run := cleaning(t, sandbox{statusFails: true}, fullConfig(), launchAt("/work/demo"))
	wantNoCommand(t, run.runner, "sbx", "rm", "--force", "demo-1")
	wantWarning(t, run.console, "could not read the sandbox's git status")
}

func TestCleanupSaysHowToRecoverFromASandboxItKept(t *testing.T) {
	run := cleaning(t, sandbox{fetchFails: true}, fullConfig(), launchAt("/work/demo"))
	for _, part := range []string{"sbx exec demo-1", "sbx cp demo-1:", "sbx rm --force demo-1"} {
		wantWarning(t, run.console, part)
	}
}

func TestWarnDirtySaysHowToInspectRecoverAndRemove(t *testing.T) {
	run := cleaning(t, sandbox{dirty: " M box.go\n"}, fullConfig(), launchAt("/work/demo"))
	for _, part := range []string{
		" M box.go",
		"Inspect:  sbx exec demo-1 git -C /work/demo diff",
		"Recover:  sbx cp demo-1:/work/demo/<file> .",
		"sbx rm --force demo-1",
	} {
		wantWarning(t, run.console, part)
	}
}

// groupLaunch builds the launch of a group session, whose one member sits beside the repository.
func groupLaunch(directory string) session.Launch {
	launch := launchAt(filepath.Join(directory, "boxes"))
	launch.AgentArgs = nil
	return launch
}

func TestCleanupBringsEveryMembersWorkBackBeforeAnythingIsRemoved(t *testing.T) {
	directory := t.TempDir()
	boxes := filepath.Join(directory, "boxes")
	run := cleaning(t, sandbox{}, groupConfig(t, boxes, member), groupLaunch(directory))
	bundled := firstMatching(run.runner.Commands, "bundle")
	if bundled == nil || !slices.Equal(bundled[:3], []string{"sbx", "exec", "demo-1"}) {
		t.Fatalf("the member was bundled as %v", bundled)
	}
	if !slices.Contains(bundled, memberPathUnder(directory)) {
		t.Fatalf("the member was bundled as %v", bundled)
	}
	settled := run.runner.Index("git", "-C", memberPathUnder(directory), "for-each-ref",
		"--format=%(refname) %(objectname)", config.SandboxRefs+"/demo-1")
	if settled < 0 {
		t.Fatal("the member's refs were never settled")
	}
	last := run.runner.Commands[len(run.runner.Commands)-1]
	if !slices.Equal(last, []string{"sbx", "rm", "--force", "demo-1"}) {
		t.Fatalf("the last command was %v", last)
	}
}

func TestCleanupKeepsTheSandboxWhenAMembersWorkCannotComeBack(t *testing.T) {
	directory := t.TempDir()
	boxes := filepath.Join(directory, "boxes")
	answers := sandbox{bundleFails: true}
	run := cleaning(t, answers, groupConfig(t, boxes, member), groupLaunch(directory))
	wantNoCommand(t, run.runner, "sbx", "rm", "--force", "demo-1")
	wantWarning(t, run.console, memberPathUnder(directory))
}

func TestCleanupChecksEveryCloneForUncommittedWork(t *testing.T) {
	directory := t.TempDir()
	boxes := filepath.Join(directory, "boxes")
	run := cleaning(t, sandbox{dirty: " M main.go\n"}, groupConfig(t, boxes, member), groupLaunch(directory))
	wantNoCommand(t, run.runner, "sbx", "rm", "--force", "demo-1")
	wantWarning(t, run.console, boxes)
}

// firstMatching is the first command holding a word, which is how a step is found among many.
func firstMatching(commands [][]string, word string) []string {
	for _, command := range commands {
		if slices.Contains(command, word) {
			return command
		}
	}
	return nil
}
