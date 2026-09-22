package cli_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lk16/box/internal/boxtest"
	"github.com/lk16/box/internal/config"
)

func TestShowConfigPrintsTheSettingsAndReturnsZero(t *testing.T) {
	directory := t.TempDir()
	runnableProject(t, directory)
	run := newFixture(withRealGit)
	if code := run.cli.Main([]string{"config"}, directory); code != 0 {
		t.Fatalf("exited %d: %s", code, run.console.Warned())
	}
	printed := run.console.Printed()
	if !strings.Contains(printed, "claude-opus-5") || !strings.Contains(printed, config.TokenFileEnv) {
		t.Fatalf("printed:\n%s", printed)
	}
}

func TestShowConfigMakesTheChecksARunWould(t *testing.T) {
	boxtest.Isolate(t)
	boxtest.StubBinaries(t)
	directory := boxtest.MakeRepository(t, t.TempDir())
	boxtest.WriteFile(t, filepath.Join(directory, config.ConfigFile), `{"model": "claude-opus-5"}`)
	run := newFixture(withRealGit)
	if code := run.cli.Main([]string{"config"}, directory); code != 1 {
		t.Fatalf("exited %d", code)
	}
	if !strings.Contains(run.console.Warned(), "kit is not set") {
		t.Fatalf("warned:\n%s", run.console.Warned())
	}
}

func TestShowConfigPrintsTheSettingsBeforeRejectingTheProject(t *testing.T) {
	boxtest.StubBinaries(t)
	directory := t.TempDir()
	boxtest.WriteFile(t, filepath.Join(directory, config.ConfigFile), `{"model": "claude-opus-5"}`)
	run := newFixture(withRealGit)
	if code := run.cli.Main([]string{"config"}, directory); code != 1 {
		t.Fatalf("exited %d", code)
	}
	// The settings are printed first, so a rejected project is read next to what it resolved to.
	if !strings.Contains(run.console.Printed(), "claude-opus-5") {
		t.Fatalf("printed:\n%s", run.console.Printed())
	}
}

func TestShowConfigRejectsACommittableMountsFile(t *testing.T) {
	boxtest.Isolate(t)
	boxtest.StubBinaries(t)
	directory := boxtest.MakeRepository(t, t.TempDir())
	boxtest.WriteFile(t, filepath.Join(directory, config.GitignoreFile), "")
	boxtest.WriteFile(t, filepath.Join(directory, config.ConfigFile),
		`{"kit": "registry/kit", "model": "claude-opus-5", "required_mounts": {"cache": "the cache"}}`)
	boxtest.WriteFile(t, filepath.Join(directory, config.MountsFile), `{"cache": "/cache"}`)
	run := newFixture(withRealGit)
	if code := run.cli.Main([]string{"config"}, directory); code != 1 {
		t.Fatalf("exited %d", code)
	}
	if !strings.Contains(run.console.Warned(), "not ignored by git") {
		t.Fatalf("warned:\n%s", run.console.Warned())
	}
}

func TestShowConfigRunsTheSecretChecks(t *testing.T) {
	directory := t.TempDir()
	runnableProject(t, directory)
	boxtest.WriteFile(t, filepath.Join(directory, config.ConfigFile),
		`{"kit": "registry/kit", "model": "claude-opus-5", "secret_hosts": {"GITLAB_TOKEN": "gitlab.com"}}`)
	run := newFixture(withRealGit)
	if code := run.cli.Main([]string{"config"}, directory); code != 1 {
		t.Fatalf("exited %d", code)
	}
	if !strings.Contains(run.console.Warned(), config.SecretsEnv) {
		t.Fatalf("warned:\n%s", run.console.Warned())
	}
}

func TestShowConfigShowsTheSecretsFilePathTheEnvironmentNames(t *testing.T) {
	directory := t.TempDir()
	runnableProject(t, directory)
	t.Setenv(config.SecretsEnv, "/secrets/box.env")
	run := newFixture(withRealGit)
	run.cli.Main([]string{"config"}, directory)
	if !strings.Contains(run.console.Printed(), "/secrets/box.env") {
		t.Fatalf("printed:\n%s", run.console.Printed())
	}
}

func TestTheTokenPathComesFromTheEnvironmentAndFromNowhereElse(t *testing.T) {
	directory := t.TempDir()
	runnableProject(t, directory)
	t.Setenv(config.TokenFileEnv, "/secrets/named-here")
	run := newFixture(withRealGit)
	run.cli.Main([]string{"config"}, directory)
	if !strings.Contains(run.console.Printed(), "/secrets/named-here") {
		t.Fatalf("printed:\n%s", run.console.Printed())
	}
	// It is the one setting with no flag and no config key, so a shared file cannot name it.
	for _, key := range append(slices.Clone(config.SettingKeys), config.ContainerKeys...) {
		if key == "token_file" {
			t.Fatal("the token path is a config key")
		}
	}
}

func TestConfigTakesTheSameFlagsAsRun(t *testing.T) {
	forConfig := parse(t, "config", "--memory", "8g", "--mount", "/a")
	forRun := parse(t, "run", "--memory", "8g", "--mount", "/a")
	if forConfig.Values.Setting("memory") != forRun.Values.Setting("memory") {
		t.Fatal("config and run read a setting flag differently")
	}
	if !slices.Equal(forConfig.Mounts, forRun.Mounts) {
		t.Fatal("config and run read a mount flag differently")
	}
}
