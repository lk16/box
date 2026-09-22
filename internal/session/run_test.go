package session_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lk16/box/internal/boxtest"
	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/session"
	"github.com/lk16/box/internal/system"
)

// steps names what a run did, in the order it did it, so an order can be asserted on.
func steps(runner *boxtest.Runner) []string {
	var named []string
	for _, command := range runner.Commands {
		named = append(named, name(command))
	}
	return named
}

// name is the short name of one step: the secret it stored, or the sbx subcommand it ran.
func name(command []string) string {
	if command[0] != "sbx" {
		return strings.Join(command, " ")
	}
	if command[1] == "secret" && command[2] == "rm" {
		return "drop-secret"
	}
	if command[1] == "secret" {
		return "store-secret"
	}
	return "sbx " + command[1]
}

// sandboxRun answers the sbx calls a run makes, failing create or the agent when asked to.
func sandboxRun(createFails bool, agentCode int) func([]string) system.Result {
	return func(arguments []string) system.Result {
		if arguments[1] == "create" && createFails {
			return system.Result{Code: 1}
		}
		if arguments[1] == "run" {
			return system.Result{Code: agentCode}
		}
		return system.Result{}
	}
}

// runSession runs one session whose sbx calls answer as told, and hands back what it did.
func runSession(t *testing.T, settings config.Config, launch session.Launch, answer func([]string) system.Result) (*fixture, int) {
	t.Helper()
	run := newFixture(answer)
	return run, run.session.Run(settings, launch)
}

func TestRunStoresTheSecretBeforeCreatingTheSandbox(t *testing.T) {
	run, _ := runSession(t, fullConfig(), launchAt("/work/demo"), sandboxRun(false, 0))
	order := steps(run.runner)
	if slices.Index(order, "store-secret") > slices.Index(order, "sbx create") {
		t.Fatalf("the steps ran as %v", order)
	}
}

func TestRunCreatesRunsAndCleansUpInThatOrder(t *testing.T) {
	run, code := runSession(t, fullConfig(), launchAt("/work/demo"), sandboxRun(false, 0))
	if code != 0 {
		t.Fatalf("exited %d", code)
	}
	order := steps(run.runner)
	want := []string{"drop-secret", "store-secret", "sbx create", "sbx run"}
	if !slices.Equal(order[:4], want) {
		t.Fatalf("the steps ran as %v", order)
	}
	if !slices.Contains(order, "git fetch sandbox-demo-1") {
		t.Fatalf("cleanup never ran; the steps were %v", order)
	}
}

func TestRunStoresEverySecretTheSandboxGets(t *testing.T) {
	launch := launchAt("/work/demo")
	launch.Secrets = append(launch.Secrets, config.SecretValue{Secret: gitlab, Value: "glpat-abc"})
	run, _ := runSession(t, fullConfig(), launch, sandboxRun(false, 0))
	want := []string{"drop-secret", "store-secret", "store-secret", "sbx create"}
	if got := steps(run.runner); !slices.Equal(got[:4], want) {
		t.Fatalf("the steps ran as %v", got)
	}
}

func TestRunReturnsTheAgentsOwnExitCode(t *testing.T) {
	if _, code := runSession(t, fullConfig(), launchAt("/work/demo"), sandboxRun(false, 3)); code != 3 {
		t.Fatalf("exited %d", code)
	}
}

func TestRunCleansUpAfterAnAgentThatFailed(t *testing.T) {
	run, _ := runSession(t, fullConfig(), launchAt("/work/demo"), sandboxRun(false, 1))
	wantCommand(t, run.runner, "git", "fetch", "sandbox-demo-1")
}

func TestRunReportsAFailedCreateAndStartsNothing(t *testing.T) {
	run, code := runSession(t, fullConfig(), launchAt("/work/demo"), sandboxRun(true, 0))
	if code != 1 {
		t.Fatalf("exited %d", code)
	}
	want := []string{"drop-secret", "store-secret", "sbx create", "drop-secret"}
	if got := steps(run.runner); !slices.Equal(got, want) {
		t.Fatalf("the steps ran as %v", got)
	}
	wantWarning(t, run.console, "never started")
}

func TestRunClonesEveryMemberIntoTheFreshSandbox(t *testing.T) {
	boxtest.Isolate(t)
	directory := t.TempDir()
	makeClonedMember(t, directory, "billing-api")
	boxes := boxtest.MakeRepository(t, filepath.Join(directory, "boxes"))
	settings := groupConfig(t, boxes, member)
	launch := session.Launch{Project: projectAt(boxes), SandboxName: "demo-1"}
	run := newFixture(sandboxWithRealGit(directory))
	if code := run.session.Run(settings, launch); code != 0 {
		t.Fatalf("exited %d", code)
	}
	wantCommand(t, run.runner, "sbx", "exec", "demo-1", "git", "init", "-q", memberPathUnder(directory))
}

func TestRunSaysWhichMemberThisMachineDoesNotHave(t *testing.T) {
	boxtest.Isolate(t)
	directory := t.TempDir()
	boxes := boxtest.MakeRepository(t, filepath.Join(directory, "boxes"))
	settings := groupConfig(t, boxes, missingMember())
	run, code := runSession(t, settings, session.Launch{Project: projectAt(boxes), SandboxName: "demo-1"}, sandboxRun(false, 0))
	if code != 0 {
		t.Fatalf("exited %d", code)
	}
	wantWarning(t, run.console, "billing-api is not on this machine")
}

func TestRunTakesTheSandboxBackWhenAMemberCannotBeCloned(t *testing.T) {
	boxtest.Isolate(t)
	directory := t.TempDir()
	makeClonedMember(t, directory, "billing-api")
	boxes := boxtest.MakeRepository(t, filepath.Join(directory, "boxes"))
	settings := groupConfig(t, boxes, member)
	launch := session.Launch{Project: projectAt(boxes), SandboxName: "demo-1"}
	answer := sandboxWithRealGit(directory)
	run := newFixture(func(arguments []string) system.Result {
		if slices.Contains(arguments, "mkdir") {
			return system.Result{Code: 1}
		}
		return answer(arguments)
	})
	if code := run.session.Run(settings, launch); code != 1 {
		t.Fatalf("exited %d", code)
	}
	wantNoCommand(t, run.runner, "sbx", "run", "claude", "--name", "demo-1")
	wantCommand(t, run.runner, "sbx", "rm", "--force", "demo-1")
	wantWarning(t, run.console, member.Name)
}

func TestRunStopsWhenAMemberCannotBeBundled(t *testing.T) {
	boxtest.Isolate(t)
	directory := t.TempDir()
	path := makeClonedMember(t, directory, "billing-api")
	boxtest.Git(t, path, "remote", "set-url", "origin", filepath.Join(directory, "gone"))
	boxes := boxtest.MakeRepository(t, filepath.Join(directory, "boxes"))
	settings := groupConfig(t, boxes, member)
	launch := session.Launch{Project: projectAt(boxes), SandboxName: "demo-1"}
	run := newFixture(sandboxWithRealGit(directory))
	if code := run.session.Run(settings, launch); code != 1 {
		t.Fatalf("exited %d", code)
	}
	wantNoCommand(t, run.runner, "sbx", "create", "claude", ".", "--clone", "--name", "demo-1")
	wantWarning(t, run.console, "git fetch origin failed")
}

// sandboxWithRealGit answers sbx itself and lets git run for real, which is what bundling needs.
func sandboxWithRealGit(string) func([]string) system.Result {
	return func(arguments []string) system.Result {
		if arguments[0] == "sbx" {
			return system.Result{}
		}
		return system.Commands{}.Capture(arguments, nil)
	}
}
