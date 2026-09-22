package session_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lk16/box/internal/boxtest"
	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/project"
	"github.com/lk16/box/internal/session"
	"github.com/lk16/box/internal/system"
)

// writeToken writes a token file beside the repository, which is the only place box accepts one.
func writeToken(t *testing.T, directory string) string {
	t.Helper()
	path := filepath.Join(directory+"-secrets", "token")
	boxtest.WriteFile(t, path, "sk-ant-secret\n")
	return path
}

// runnableProject builds a project that passes every check box makes before it starts a sandbox.
func runnableProject(t *testing.T, directory string) string {
	t.Helper()
	boxtest.Isolate(t)
	boxtest.StubBinaries(t)
	boxtest.MakeRepository(t, directory)
	boxtest.WriteFile(t, filepath.Join(directory, config.GitignoreFile), config.MountsFile+"\n")
	boxtest.WriteFile(t, filepath.Join(directory, config.ConfigFile), "{}")
	boxtest.WriteFile(t, filepath.Join(directory, config.MountsFile), `{"cache": "/cache"}`)
	return directory
}

// readySettings builds the settings a run needs before box will start a sandbox.
func readySettings(t *testing.T, directory string, extra map[string]string) config.Config {
	t.Helper()
	given := config.NewValues()
	given.Text["kit"] = "registry/kit"
	given.Text["model"] = "claude-opus-5"
	for key, value := range extra {
		given.Text[key] = value
	}
	built, err := config.Build(given, nil, nil, directory)
	if err != nil {
		t.Fatal(err)
	}
	return built
}

// preparing resolves a launch with no sandbox names taken, and hands back what it resolved.
func preparing(t *testing.T, settings config.Config, proj project.Project, tokenFile string, taken string) (session.Launch, error) {
	t.Helper()
	run := newFixture(func(arguments []string) system.Result {
		if arguments[0] == "sbx" && arguments[1] == "ls" {
			return system.Result{Stdout: taken}
		}
		return system.Commands{}.Capture(arguments)
	})
	return run.session.PrepareLaunch(settings, proj, tokenFile)
}

func TestPrepareLaunchRefusesAMachineWithoutSbx(t *testing.T) {
	boxtest.EmptyPath(t)
	directory := t.TempDir()
	_, err := preparing(t, fullConfig(), projectAt(directory), "/secrets/token", "")
	if err == nil || !strings.Contains(err.Error(), "not on PATH") {
		t.Fatalf("the refusal was %v", err)
	}
}

func TestPrepareLaunchResolvesANameATokenAndTheAgentArgs(t *testing.T) {
	directory := t.TempDir()
	runnableProject(t, directory)
	token := writeToken(t, directory)
	prompt := filepath.Join(directory+"-secrets", "agent.md")
	boxtest.WriteFile(t, prompt, "project rules")
	settings := readySettings(t, directory, map[string]string{"name": "demo", "prompt_file": prompt})
	launch, err := preparing(t, settings, project.Build(system.Commands{}, directory), token, "demo-1\n")
	if err != nil {
		t.Fatal(err)
	}
	if launch.SandboxName != "demo-2" {
		t.Fatalf("named %q", launch.SandboxName)
	}
	want := []config.SecretValue{{Secret: config.OAuthSecret, Value: "sk-ant-secret"}}
	if !slices.Equal(launch.Secrets, want) {
		t.Fatalf("the secrets are %v", launch.Secrets)
	}
	if launch.AgentArgs[0] != "--append-system-prompt" {
		t.Fatalf("the agent args are %v", launch.AgentArgs)
	}
	if launch.AgentArgs[1] != config.BasePrompt+"\n\nproject rules" {
		t.Fatalf("the prompt reads %q", launch.AgentArgs[1])
	}
	if !slices.Equal(launch.AgentArgs[2:], []string{"--model", "claude-opus-5"}) {
		t.Fatalf("the agent args are %v", launch.AgentArgs)
	}
}

func TestPrepareLaunchTellsASubfolderSessionWhereItStarted(t *testing.T) {
	directory := t.TempDir()
	runnableProject(t, directory)
	subfolder := filepath.Join(directory, "billing")
	boxtest.WriteFile(t, filepath.Join(subfolder, config.ConfigFile), "{}")
	token := writeToken(t, directory)
	settings := readySettings(t, subfolder, nil)
	launch, err := preparing(t, settings, project.Build(system.Commands{}, subfolder), token, "")
	if err != nil {
		t.Fatal(err)
	}
	if launch.Project.Root != config.Resolve(directory) {
		t.Fatalf("the root is %q", launch.Project.Root)
	}
	if !strings.Contains(launch.AgentArgs[1], "billing") {
		t.Fatalf("the prompt reads %q", launch.AgentArgs[1])
	}
}

func TestPrepareLaunchReadsAValueForEveryDeclaredSecret(t *testing.T) {
	directory := t.TempDir()
	runnableProject(t, directory)
	token := writeToken(t, directory)
	secrets := filepath.Join(directory+"-secrets", "box.env")
	boxtest.WriteFile(t, secrets, "GITLAB_TOKEN=glpat-abc\n")
	t.Setenv(config.SecretsEnv, secrets)
	settings := readySettings(t, directory, nil)
	settings.SecretHosts = []config.Secret{gitlab}
	launch, err := preparing(t, settings, project.Build(system.Commands{}, directory), token, "")
	if err != nil {
		t.Fatal(err)
	}
	if launch.Secrets[0].Secret != config.OAuthSecret {
		t.Fatalf("the first secret is %v", launch.Secrets[0])
	}
	if launch.Secrets[1] != (config.SecretValue{Secret: gitlab, Value: "glpat-abc"}) {
		t.Fatalf("the second secret is %v", launch.Secrets[1])
	}
}

func TestPrepareLaunchRefusesATokenTheSandboxCouldReadItself(t *testing.T) {
	directory := t.TempDir()
	runnableProject(t, directory)
	inside := filepath.Join(directory, "token")
	boxtest.WriteFile(t, inside, "sk-ant-secret\n")
	settings := readySettings(t, directory, nil)
	_, err := preparing(t, settings, project.Build(system.Commands{}, directory), inside, "")
	if err == nil || !strings.Contains(err.Error(), "can read everything there") {
		t.Fatalf("the refusal was %v", err)
	}
}

func TestPrepareLaunchRequiresTheTokenEnvironmentVariable(t *testing.T) {
	directory := t.TempDir()
	runnableProject(t, directory)
	settings := readySettings(t, directory, nil)
	_, err := preparing(t, settings, project.Build(system.Commands{}, directory), "", "")
	if err == nil || !strings.Contains(err.Error(), config.TokenFileEnv+" is not set") {
		t.Fatalf("the refusal was %v", err)
	}
}

func TestPrepareLaunchRejectsADirectoryThatIsNotARepository(t *testing.T) {
	boxtest.StubBinaries(t)
	directory := t.TempDir()
	boxtest.WriteFile(t, filepath.Join(directory, config.ConfigFile), "{}")
	_, err := preparing(t, fullConfig(), projectAt(directory), "/secrets/token", "")
	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("the refusal was %v", err)
	}
}

func TestPrepareLaunchRejectsARepositoryWithNoCommits(t *testing.T) {
	boxtest.StubBinaries(t)
	directory := boxtest.GitInit(t, t.TempDir())
	boxtest.WriteFile(t, filepath.Join(directory, config.ConfigFile), "{}")
	_, err := preparing(t, fullConfig(), projectAt(directory), "/secrets/token", "")
	if err == nil || !strings.Contains(err.Error(), "no commits") {
		t.Fatalf("the refusal was %v", err)
	}
}

func TestPrepareLaunchRejectsACommittableMountsFile(t *testing.T) {
	boxtest.Isolate(t)
	boxtest.StubBinaries(t)
	directory := boxtest.MakeRepository(t, t.TempDir())
	boxtest.WriteFile(t, filepath.Join(directory, config.GitignoreFile), "")
	boxtest.WriteFile(t, filepath.Join(directory, config.ConfigFile), "{}")
	boxtest.WriteFile(t, filepath.Join(directory, config.MountsFile), `{"cache": "/cache"}`)
	_, err := preparing(t, fullConfig(), projectAt(directory), "/secrets/token", "")
	if err == nil || !strings.Contains(err.Error(), "not ignored by git") {
		t.Fatalf("the refusal was %v", err)
	}
}

func TestPrepareLaunchRequiresAKitAndAModel(t *testing.T) {
	boxtest.StubBinaries(t)
	directory := t.TempDir()
	boxtest.WriteFile(t, filepath.Join(directory, config.ConfigFile), "{}")
	bare, err := config.Build(config.NewValues(), nil, nil, directory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := preparing(t, bare, projectAt(directory), "/secrets/token", ""); err == nil ||
		!strings.Contains(err.Error(), "kit is not set") {
		t.Fatalf("the refusal was %v", err)
	}
	withKit := bare
	withKit.Kit = "registry/kit"
	if _, err := preparing(t, withKit, projectAt(directory), "/secrets/token", ""); err == nil ||
		!strings.Contains(err.Error(), "model is not set") {
		t.Fatalf("the refusal was %v", err)
	}
}
