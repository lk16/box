package session_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/session"
	"github.com/lk16/box/internal/system"
)

// gitlab is the one declared secret these tests use, and the host its value may go to.
var gitlab = config.Secret{Name: "GITLAB_TOKEN", Host: "gitlab.com"}

func TestStoreSecretNeverPutsTheTokenOnTheCommandLine(t *testing.T) {
	run := newFixture(nil)
	token := config.SecretValue{Secret: config.OAuthSecret, Value: "sk-ant-secret"}
	if err := run.session.StoreSecret("demo-1", token); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(run.runner.Stdin, []string{"sk-ant-secret"}) {
		t.Fatalf("stdin was %v", run.runner.Stdin)
	}
	if strings.Contains(strings.Join(run.runner.Commands[0], " "), "sk-ant-secret") {
		t.Fatal("the token reached the command line")
	}
}

func TestStoreSecretNamesTheSandboxTheHostAndTheVariable(t *testing.T) {
	run := newFixture(nil)
	token := config.SecretValue{Secret: config.OAuthSecret, Value: "sk-ant-secret"}
	if err := run.session.StoreSecret("demo-1", token); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"sbx", "secret", "set-custom", "--sandbox", "demo-1",
		"--host", config.SecretHost, "--env", config.SecretEnv,
	}
	if !slices.Equal(run.runner.Commands[0], want) {
		t.Fatalf("ran %v", run.runner.Commands[0])
	}
}

func TestStoreSecretNamesTheSandboxTheDeclaredHostAndTheVariable(t *testing.T) {
	run := newFixture(nil)
	if err := run.session.StoreSecret("demo-1", config.SecretValue{Secret: gitlab, Value: "glpat-abc"}); err != nil {
		t.Fatal(err)
	}
	command := run.runner.Commands[0]
	if !slices.Equal(command[len(command)-4:], []string{"--host", "gitlab.com", "--env", "GITLAB_TOKEN"}) {
		t.Fatalf("ran %v", command)
	}
	if !slices.Equal(run.runner.Stdin, []string{"glpat-abc"}) {
		t.Fatalf("stdin was %v", run.runner.Stdin)
	}
}

func TestStoreSecretReportsAnSbxThatRefused(t *testing.T) {
	run := newFixture(func([]string) system.Result { return system.Result{Code: 1} })
	err := run.session.StoreSecret("demo-1", config.SecretValue{Secret: config.OAuthSecret, Value: "x"})
	if err == nil || !strings.Contains(err.Error(), "would not store CLAUDE_CODE_OAUTH_TOKEN for demo-1") {
		t.Fatalf("the refusal was %v", err)
	}
}

func TestDropSecretsRemovesTheSecretForOneSandbox(t *testing.T) {
	run := newFixture(nil)
	run.session.DropSecrets(nil, "demo-1")
	want := []string{"sbx", "secret", "rm", "--sandbox", "demo-1", "--host", config.SecretHost, "-f"}
	if len(run.runner.Commands) != 1 || !slices.Equal(run.runner.Commands[0], want) {
		t.Fatalf("ran %v", run.runner.Commands)
	}
}

func TestDropSecretsRemovesEveryHostThisSandboxHasASecretFor(t *testing.T) {
	run := newFixture(nil)
	secrets := []config.Secret{gitlab, {Name: "SIGNOZ_API_KEY", Host: "gitlab.com"}}
	run.session.DropSecrets(secrets, "demo-1")
	var hosts []string
	for _, command := range run.runner.Commands {
		hosts = append(hosts, command[slices.Index(command, "--host")+1])
	}
	if !slices.Equal(hosts, []string{config.SecretHost, "gitlab.com"}) {
		t.Fatalf("dropped %v", hosts)
	}
}

func TestDistinctHostsPutsBoxOwnTokenHostFirst(t *testing.T) {
	if got := session.DistinctHosts([]config.Secret{gitlab}); !slices.Equal(got, []string{config.SecretHost, "gitlab.com"}) {
		t.Fatalf("listed %v", got)
	}
}
