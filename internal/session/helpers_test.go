package session_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/lk16/box/internal/boxtest"
	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/project"
	"github.com/lk16/box/internal/session"
	"github.com/lk16/box/internal/system"
)

// member is the one member a group declares in these tests.
var member = config.Member{
	Name:      "billing-api",
	Branch:    "develop",
	GitOrigin: "https://example.com/billing-api.git",
	Path:      "../billing-api",
}

// fixture is one run's fakes, and the session that reaches the system only through them.
type fixture struct {
	runner  *boxtest.Runner
	console *boxtest.Console
	session session.Session
}

// newFixture builds a session whose every command is answered by the given function.
func newFixture(answer func(arguments []string) system.Result) *fixture {
	runner := &boxtest.Runner{Answer: answer}
	console := &boxtest.Console{}
	return &fixture{
		runner:  runner,
		console: console,
		session: session.Session{Deps: system.Deps{Run: runner, Console: console.Handle(false)}},
	}
}

// fullConfig builds a config with every field set to a recognisable value.
func fullConfig() config.Config {
	return config.Config{
		Name: "demo", Memory: "8g", CPUs: "2", RootSize: "20g", DockerSize: "30g",
		Model: "claude-opus-5", PromptFile: "docs/project-prompt.md", Kit: "registry/kit",
		Template: "frlg-sandbox:1", MCP: []string{"postgres", "kubernetes"}, Mounts: []string{"/cache:ro"},
	}
}

// bareConfig builds a config with nothing but the defaults behind it.
func bareConfig(t *testing.T) config.Config {
	t.Helper()
	built, err := config.Build(config.NewValues(), nil, nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return built
}

// projectAt builds the project a session started at the repository root resolves to.
func projectAt(directory string) project.Project {
	return project.Project{WorkingDirectory: directory, Root: directory}
}

// checkoutAt builds the checkout the repository box runs in resolves to.
func checkoutAt(directory string) project.Checkout {
	return project.Checkout{Path: directory, KnownCommits: config.MainKnown}
}

// launchAt builds what PrepareLaunch would have resolved for a run in one directory.
func launchAt(directory string) session.Launch {
	return session.Launch{
		Project:     projectAt(directory),
		SandboxName: "demo-1",
		Secrets:     []config.SecretValue{{Secret: config.OAuthSecret, Value: "sk-ant-secret"}},
		AgentArgs:   []string{"--model", "claude-opus-5"},
	}
}

// groupConfig builds the config of a group working on the given members, none bringing settings.
func groupConfig(t *testing.T, directory string, members ...config.Member) config.Config {
	t.Helper()
	settings := make([]config.MemberSettings, 0, len(members))
	for _, each := range members {
		settings = append(settings, config.MemberSettings{Member: each})
	}
	built, err := config.Build(config.NewValues(), nil, settings, directory)
	if err != nil {
		t.Fatal(err)
	}
	return built
}

// wantCommand fails unless the fake was asked to run exactly this command.
func wantCommand(t *testing.T, runner *boxtest.Runner, arguments ...string) {
	t.Helper()
	if !runner.Ran(arguments...) {
		t.Fatalf("%v was never run; the commands were %v", arguments, runner.Commands)
	}
}

// wantNoCommand fails when the fake was asked to run this command.
func wantNoCommand(t *testing.T, runner *boxtest.Runner, arguments ...string) {
	t.Helper()
	if runner.Ran(arguments...) {
		t.Fatalf("%v was run and should not have been", arguments)
	}
}

// wantWarning fails unless the run said this on stderr.
func wantWarning(t *testing.T, console *boxtest.Console, contains string) {
	t.Helper()
	if !strings.Contains(console.Warned(), contains) {
		t.Fatalf("stderr was %q, wanted it to say %q", console.Warned(), contains)
	}
}

// memberPathUnder is where the one member sits next to a group folder.
func memberPathUnder(directory string) string {
	return config.Resolve(filepath.Join(directory, "billing-api"))
}
