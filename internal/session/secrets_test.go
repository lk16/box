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
	token := config.SecretValue{Secret: gitlab, Value: "glpat-abc"}
	if err := run.session.StoreSecret("demo-1", token); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(run.runner.Stdin, []string{"glpat-abc"}) {
		t.Fatalf("stdin was %v", run.runner.Stdin)
	}
	if strings.Contains(strings.Join(run.runner.Commands[0], " "), "glpat-abc") {
		t.Fatal("the token reached the command line")
	}
}

func TestStoreSecretNamesTheSandboxTheHostAndTheVariable(t *testing.T) {
	run := newFixture(nil)
	if err := run.session.StoreSecret("demo-1", config.SecretValue{Secret: gitlab, Value: "glpat-abc"}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"sbx", "secret", "set-custom", "--sandbox", "demo-1",
		"--host", "gitlab.com", "--env", "GITLAB_TOKEN",
	}
	if !slices.Equal(run.runner.Commands[0], want) {
		t.Fatalf("ran %v", run.runner.Commands[0])
	}
}

func TestStoreSecretReportsAnSbxThatRefused(t *testing.T) {
	run := newFixture(func([]string) system.Result { return system.Result{Code: 1} })
	err := run.session.StoreSecret("demo-1", config.SecretValue{Secret: gitlab, Value: "x"})
	if err == nil || !strings.Contains(err.Error(), "would not store GITLAB_TOKEN for demo-1") {
		t.Fatalf("the refusal was %v", err)
	}
}

func TestDropSecretsRemovesTheSecretForOneSandbox(t *testing.T) {
	run := newFixture(nil)
	run.session.DropSecrets([]config.Secret{gitlab}, "demo-1")
	want := []string{"sbx", "secret", "rm", "--sandbox", "demo-1", "--host", "gitlab.com", "-f"}
	if len(run.runner.Commands) != 1 || !slices.Equal(run.runner.Commands[0], want) {
		t.Fatalf("ran %v", run.runner.Commands)
	}
}

func TestDropSecretsRemovesEveryHostThisSandboxHasASecretFor(t *testing.T) {
	run := newFixture(nil)
	secrets := []config.Secret{gitlab, {Name: "SIGNOZ_API_KEY", Host: "signoz.example.com"}}
	run.session.DropSecrets(secrets, "demo-1")
	var hosts []string
	for _, command := range run.runner.Commands {
		hosts = append(hosts, command[slices.Index(command, "--host")+1])
	}
	if !slices.Equal(hosts, []string{"gitlab.com", "signoz.example.com"}) {
		t.Fatalf("dropped %v", hosts)
	}
}

func TestDistinctHostsListsEachDeclaredHostOnce(t *testing.T) {
	secrets := []config.Secret{gitlab, {Name: "GITLAB_READ_TOKEN", Host: "gitlab.com"}}
	if got := session.DistinctHosts(secrets); !slices.Equal(got, []string{"gitlab.com"}) {
		t.Fatalf("listed %v", got)
	}
}
