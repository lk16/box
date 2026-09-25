package cli_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lk16/box/internal/boxtest"
	"github.com/lk16/box/internal/cli"
	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/system"
	"github.com/lk16/box/internal/update"
)

// fixture is one box invocation's fakes, and the CLI that reaches the world only through them.
type fixture struct {
	runner   *boxtest.Runner
	console  *boxtest.Console
	download *boxtest.Downloader
	cli      cli.CLI
}

// newFixture builds a box whose sbx calls answer silently and whose update check finds nothing.
func newFixture(answer func([]string) system.Result) *fixture {
	runner := &boxtest.Runner{Answer: answer}
	console := &boxtest.Console{}
	download := &boxtest.Downloader{Body: []byte(`{"Version": "v0.1.0"}`)}
	return &fixture{
		runner:   runner,
		console:  console,
		download: download,
		cli: cli.CLI{
			Version: "",
			Deps: system.Deps{
				Run:      runner,
				Console:  console.Handle(false),
				Ask:      &boxtest.Prompter{},
				Download: download,
				Now:      func() time.Time { return time.Unix(1000, 0) },
			},
		},
	}
}

// withRealGit answers sbx itself and lets git run for real, which is what the checks need.
func withRealGit(arguments []string) system.Result {
	if arguments[0] == "sbx" {
		return system.Result{}
	}
	return system.Commands{}.Capture(arguments, nil)
}

// runnableProject builds a project that passes every check box makes before it starts a sandbox.
func runnableProject(t *testing.T, directory string) string {
	t.Helper()
	boxtest.Isolate(t)
	boxtest.StubBinaries(t)
	boxtest.Unset(t, config.SecretsEnv)
	boxtest.MakeRepository(t, directory)
	boxtest.WriteFile(t, filepath.Join(directory, config.GitignoreFile), config.MountsFile+"\n")
	boxtest.WriteFile(t, filepath.Join(directory, config.ConfigFile),
		`{"kit": "registry/kit", "model": "claude-opus-5"}`)
	boxtest.WriteFile(t, filepath.Join(directory+"-secrets", "token"), "sk-ant-secret\n")
	t.Setenv(config.TokenFileEnv, filepath.Join(directory+"-secrets", "token"))
	return directory
}

func TestMainWritesAStarterProjectWithGen(t *testing.T) {
	boxtest.StubBinaries(t)
	directory := t.TempDir()
	run := newFixture(nil)
	if code := run.cli.Main([]string{"gen"}, directory); code != 0 {
		t.Fatalf("exited %d: %s", code, run.console.Warned())
	}
	if _, err := os.ReadFile(filepath.Join(directory, config.ConfigFile)); err != nil {
		t.Fatal(err)
	}
}

func TestMainPrintsTheConfigWithoutCreatingAnything(t *testing.T) {
	directory := t.TempDir()
	runnableProject(t, directory)
	run := newFixture(withRealGit)
	if code := run.cli.Main([]string{"config"}, directory); code != 0 {
		t.Fatalf("exited %d: %s", code, run.console.Warned())
	}
	if !strings.Contains(run.console.Printed(), "claude-opus-5") {
		t.Fatalf("printed:\n%s", run.console.Printed())
	}
	for _, command := range run.runner.Commands {
		if slices.Contains(command, "create") {
			t.Fatalf("config created something: %v", command)
		}
	}
}

func TestMainTurnsARejectedProjectIntoOneLine(t *testing.T) {
	boxtest.StubBinaries(t)
	directory := t.TempDir()
	boxtest.WriteFile(t, filepath.Join(directory, config.ConfigFile), "{}")
	run := newFixture(withRealGit)
	if code := run.cli.Main([]string{"run"}, directory); code != 1 {
		t.Fatalf("exited %d", code)
	}
	if !strings.HasPrefix(run.console.Warned(), "box: kit is not set") {
		t.Fatalf("warned:\n%s", run.console.Warned())
	}
}

func TestMainSendsAProjectWithNoBoxDirectoryToGen(t *testing.T) {
	boxtest.StubBinaries(t)
	run := newFixture(withRealGit)
	if code := run.cli.Main([]string{"run"}, t.TempDir()); code != 1 {
		t.Fatalf("exited %d", code)
	}
	if !strings.Contains(run.console.Warned(), "Run box gen") {
		t.Fatalf("warned:\n%s", run.console.Warned())
	}
}

func TestMainRejectsAFlagOnASetupCommand(t *testing.T) {
	boxtest.StubBinaries(t)
	directory := t.TempDir()
	run := newFixture(nil)
	if code := run.cli.Main([]string{"--memory", "8g", "gen"}, directory); code != 1 {
		t.Fatalf("exited %d", code)
	}
	if !strings.Contains(run.console.Warned(), "gen takes no flags") {
		t.Fatalf("warned:\n%s", run.console.Warned())
	}
	if _, err := os.ReadFile(filepath.Join(directory, config.ConfigFile)); err == nil {
		t.Fatal("gen wrote a config after refusing the flag")
	}
}

func TestMainHandsAReadyProjectToASandboxRun(t *testing.T) {
	directory := t.TempDir()
	runnableProject(t, directory)
	run := newFixture(withRealGit)
	if code := run.cli.Main([]string{"run"}, directory); code != 0 {
		t.Fatalf("exited %d: %s", code, run.console.Warned())
	}
	created := firstMatching(run.runner.Commands, "create")
	if created == nil {
		t.Fatalf("nothing was created; the commands were %v", run.runner.Commands)
	}
	if !slices.Contains(created, "--kit") || !slices.Contains(created, "registry/kit") {
		t.Fatalf("created as %v", created)
	}
	ran := firstMatching(run.runner.Commands, "run")
	if !slices.Contains(ran, "--model") || !slices.Contains(ran, "claude-opus-5") {
		t.Fatalf("ran as %v", ran)
	}
}

func TestMainReturnsTheAgentsOwnExitCode(t *testing.T) {
	directory := t.TempDir()
	runnableProject(t, directory)
	run := newFixture(func(arguments []string) system.Result {
		if arguments[0] == "sbx" && arguments[1] == "run" {
			return system.Result{Code: 7}
		}
		return withRealGit(arguments)
	})
	if code := run.cli.Main([]string{"run"}, directory); code != 7 {
		t.Fatalf("exited %d: %s", code, run.console.Warned())
	}
}

func TestMainChecksTheSbxReleaseBeforeAnyCommand(t *testing.T) {
	boxtest.StubBinaries(t)
	directory := t.TempDir()
	run := newFixture(func(arguments []string) system.Result {
		if slices.Contains(arguments, "version") {
			return system.Result{Stdout: "sbx version: v0.37.0 c022b146\n"}
		}
		return system.Result{}
	})
	if code := run.cli.Main([]string{"gen"}, directory); code != 1 {
		t.Fatalf("exited %d", code)
	}
	if !strings.Contains(run.console.Warned(), "v0.38.0 or newer") {
		t.Fatalf("warned:\n%s", run.console.Warned())
	}
	// gen needs no settings at all, so it shows the check guards every command.
	if _, err := os.ReadFile(filepath.Join(directory, config.ConfigFile)); err == nil {
		t.Fatal("gen wrote a config on an sbx box refuses")
	}
}

func TestMainChecksTheSbxReleaseBeforeTheDaemonItTalksTo(t *testing.T) {
	boxtest.StubBinaries(t)
	run := newFixture(func(arguments []string) system.Result {
		if slices.Contains(arguments, "diagnose") {
			t.Fatal("an sbx box refuses must not be diagnosed")
		}
		return system.Result{Stdout: "sbx version: v0.37.0 c022b146\n"}
	})
	if code := run.cli.Main([]string{"gen"}, t.TempDir()); code != 1 {
		t.Fatalf("exited %d", code)
	}
}

func TestMainChecksTheSbxVersionsBeforeAnyCommand(t *testing.T) {
	boxtest.StubBinaries(t)
	directory := t.TempDir()
	run := newFixture(func(arguments []string) system.Result {
		if slices.Contains(arguments, "diagnose") {
			return system.Result{Stdout: `{"checks": [{"name": "Version match", "status": "fail",
				"message": "client v0.38.0, daemon v0.37.0"}]}`}
		}
		return system.Result{Stdout: "sbx version: v0.38.0 c022b146\n"}
	})
	if code := run.cli.Main([]string{"gen"}, directory); code != 1 {
		t.Fatalf("exited %d", code)
	}
	if !strings.Contains(run.console.Warned(), "different versions") {
		t.Fatalf("warned:\n%s", run.console.Warned())
	}
	if _, err := os.ReadFile(filepath.Join(directory, config.ConfigFile)); err == nil {
		t.Fatal("gen wrote a config on a daemon box refuses")
	}
}

func TestMainChecksForAnUpdateEvenWhenTheCommandFails(t *testing.T) {
	boxtest.StubBinaries(t)
	boxtest.Unset(t, config.UpdateURLEnv)
	t.Setenv(config.CacheHomeEnv, t.TempDir())
	run := newFixture(withRealGit)
	run.cli.Version = "v0.1.0"
	if code := run.cli.Main([]string{"run"}, t.TempDir()); code != 1 {
		t.Fatalf("exited %d", code)
	}
	if len(run.download.URLs) != 1 {
		t.Fatalf("the proxy was asked %d times", len(run.download.URLs))
	}
}

func TestMainPrintsTheUsageForACommandLineItCannotRead(t *testing.T) {
	run := newFixture(nil)
	if code := run.cli.Main([]string{"nope"}, t.TempDir()); code != cli.UsageExit {
		t.Fatalf("exited %d", code)
	}
	if !strings.Contains(run.console.Warned(), "usage: box") {
		t.Fatalf("warned:\n%s", run.console.Warned())
	}
}

func TestMainPrintsTheUsageWhenAskedFor(t *testing.T) {
	run := newFixture(nil)
	if code := run.cli.Main([]string{"--help"}, t.TempDir()); code != 0 {
		t.Fatalf("exited %d", code)
	}
	if !strings.Contains(run.console.Printed(), "usage: box") {
		t.Fatalf("printed:\n%s", run.console.Printed())
	}
}

func TestMainRunsMountPrompt(t *testing.T) {
	boxtest.StubBinaries(t)
	directory := t.TempDir()
	boxtest.WriteFile(t, filepath.Join(directory, config.ConfigFile),
		`{"required_mounts": {"go": "the Go toolchain"}}`)
	run := newFixture(nil)
	if code := run.cli.Main([]string{"mount-prompt"}, directory); code != 0 {
		t.Fatalf("exited %d: %s", code, run.console.Warned())
	}
	if !strings.Contains(run.console.Printed(), "go: the Go toolchain") {
		t.Fatalf("printed:\n%s", run.console.Printed())
	}
}

func TestMainUpdatesTheBoxThatIsRunning(t *testing.T) {
	boxtest.StubBinaries(t)
	boxtest.Unset(t, config.UpdateURLEnv)
	t.Setenv(config.CacheHomeEnv, t.TempDir())
	run := newFixture(nil)
	run.cli.Version = "v0.1.0"
	run.download.Body = []byte(`{"Version": "v0.2.0"}`)
	if code := run.cli.Main([]string{"self-update"}, t.TempDir()); code != 0 {
		t.Fatalf("exited %d: %s", code, run.console.Warned())
	}
	if !run.runner.Ran(update.InstallCommand()...) {
		t.Fatalf("ran %v", run.runner.Commands)
	}
}

// firstMatching is the first sbx command holding a word, which is how a step is found among many.
func firstMatching(commands [][]string, word string) []string {
	for _, command := range commands {
		if command[0] == "sbx" && slices.Contains(command, word) {
			return command
		}
	}
	return nil
}
