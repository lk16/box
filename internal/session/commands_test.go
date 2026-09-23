package session_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/project"
	"github.com/lk16/box/internal/session"
)

func TestCreateCommandIncludesMountsAndKit(t *testing.T) {
	command := session.CreateCommand(fullConfig(), projectAt("/work/demo"), "demo-1")
	want := []string{
		"sbx", "create", "claude", ".", "/cache:ro", "--clone", "--name", "demo-1",
		"--no-share-skills", "--memory", "8g", "--cpus", "2", "--kit", "registry/kit",
		"--template", "frlg-sandbox:1", "--static-mcp", "postgres,kubernetes",
	}
	if !slices.Equal(command, want) {
		t.Fatalf("assembled %v", command)
	}
}

func TestCreateCommandOmitsAnEmptyKitAnUnsetTemplateAndAnUnsetMCP(t *testing.T) {
	command := session.CreateCommand(bareConfig(t), projectAt("/work/demo"), "demo-1")
	for _, flag := range []string{"--kit", "--template", "--static-mcp"} {
		if slices.Contains(command, flag) {
			t.Errorf("%s was passed for a setting nothing was given for", flag)
		}
	}
}

func TestCreateCommandKeepsTheSkillsDirectoryOutOfTheSandbox(t *testing.T) {
	for _, settings := range []config.Config{fullConfig(), bareConfig(t)} {
		command := session.CreateCommand(settings, projectAt("/work/demo"), "demo-1")
		if !slices.Contains(command, "--no-share-skills") {
			t.Errorf("assembled %v", command)
		}
	}
}

func TestCreateCommandTakesTheTemplateFromTheConfig(t *testing.T) {
	settings := bareConfig(t)
	settings.Template = "frlg-sandbox:1"
	command := session.CreateCommand(settings, projectAt("/work/demo"), "demo-1")
	if !slices.Equal(command[len(command)-2:], []string{"--template", "frlg-sandbox:1"}) {
		t.Fatalf("assembled %v", command)
	}
}

func TestCreateCommandNamesTheMCPServersSbxShouldStart(t *testing.T) {
	settings := bareConfig(t)
	settings.MCP = []string{"postgres", "kubernetes"}
	command := session.CreateCommand(settings, projectAt("/work/demo"), "demo-1")
	if !slices.Equal(command[len(command)-2:], []string{"--static-mcp", "postgres,kubernetes"}) {
		t.Fatalf("assembled %v", command)
	}
}

func TestCreateCommandClonesTheRootFromASubfolder(t *testing.T) {
	proj := project.Project{WorkingDirectory: "/work/boxes/billing", Root: "/work/boxes", StartedIn: "billing"}
	command := session.CreateCommand(fullConfig(), proj, "demo-1")
	if !slices.Equal(command[:4], []string{"sbx", "create", "claude", "/work/boxes"}) {
		t.Fatalf("assembled %v", command)
	}
}

func TestAgentArgsIncludesPromptAndModel(t *testing.T) {
	want := []string{"--append-system-prompt", "be careful", "--model", "claude-opus-5"}
	if got := session.AgentArgs(fullConfig(), "be careful"); !slices.Equal(got, want) {
		t.Fatalf("assembled %v", got)
	}
}

func TestAgentArgsIsEmptyWithoutSettings(t *testing.T) {
	if got := session.AgentArgs(bareConfig(t), ""); len(got) != 0 {
		t.Fatalf("assembled %v", got)
	}
}

func TestRunCommandOmitsSeparatorWithoutArgs(t *testing.T) {
	want := []string{"sbx", "run", "claude", "--name", "demo-1"}
	if got := session.RunCommand("demo-1", nil); !slices.Equal(got, want) {
		t.Fatalf("assembled %v", got)
	}
}

func TestRunCommandPassesAgentArgsAfterSeparator(t *testing.T) {
	command := session.RunCommand("demo-1", []string{"--model", "claude-opus-5"})
	if !slices.Equal(command[len(command)-3:], []string{"--", "--model", "claude-opus-5"}) {
		t.Fatalf("assembled %v", command)
	}
}

func TestEnvironmentSetsDiskLimits(t *testing.T) {
	environment := session.Environment(fullConfig())
	if got := valueOf(environment, config.RootSizeEnv); got != "20g" {
		t.Fatalf("the root size is %q", got)
	}
	if got := valueOf(environment, config.DockerSizeEnv); got != "30g" {
		t.Fatalf("the docker size is %q", got)
	}
}

func TestEnvironmentKeepsTheEnvironmentBoxWasRunWith(t *testing.T) {
	t.Setenv("BOX_TEST_MARKER", "kept")
	if got := valueOf(session.Environment(fullConfig()), "BOX_TEST_MARKER"); got != "kept" {
		t.Fatalf("the marker is %q", got)
	}
}

func TestEnvironmentOverridesALimitTheUserAlreadyExported(t *testing.T) {
	t.Setenv(config.RootSizeEnv, "1g")
	environment := session.Environment(fullConfig())
	if got := valueOf(environment, config.RootSizeEnv); got != "20g" {
		t.Fatalf("the root size is %q", got)
	}
	if count := countOf(environment, config.RootSizeEnv); count != 1 {
		t.Fatalf("the root size is set %d times", count)
	}
}

func TestTheStatusCheckReadsTheCloneAtTheRoot(t *testing.T) {
	want := []string{"sbx", "exec", "demo-1", "git", "-C", "/work/boxes", "status", "--porcelain"}
	if got := session.StatusCommand("/work/boxes", "demo-1"); !slices.Equal(got, want) {
		t.Fatalf("assembled %v", got)
	}
}

// valueOf reads one variable out of an environment, the way a process would.
func valueOf(environment []string, name string) string {
	value := ""
	for _, entry := range environment {
		if key, found, _ := strings.Cut(entry, "="); key == name {
			value = found
		}
	}
	return value
}

// countOf says how many times a variable appears in an environment.
func countOf(environment []string, name string) int {
	count := 0
	for _, entry := range environment {
		if key, _, _ := strings.Cut(entry, "="); key == name {
			count++
		}
	}
	return count
}
