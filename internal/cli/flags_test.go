package cli_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/lk16/box/internal/cli"
	"github.com/lk16/box/internal/config"
)

// parse reads a command line, failing the test when box could not read it at all.
func parse(t *testing.T, argv ...string) cli.Arguments {
	t.Helper()
	arguments, err := cli.Parse(argv)
	if err != nil {
		t.Fatal(err)
	}
	return arguments
}

// refuse reads a command line box is expected to reject, and returns what it said.
func refuse(t *testing.T, argv ...string) error {
	t.Helper()
	_, err := cli.Parse(argv)
	if err == nil {
		t.Fatalf("box read %v as a command line", argv)
	}
	return err
}

func TestEveryCommandIsACommand(t *testing.T) {
	for _, command := range config.Commands {
		if got := parse(t, command).Command; got != command {
			t.Errorf("%q parsed as %q", command, got)
		}
	}
}

func TestACommandIsRequired(t *testing.T) {
	if err := refuse(t); !strings.Contains(err.Error(), "a command is required") {
		t.Fatalf("said %q", err)
	}
}

func TestAnUnknownCommandIsRejected(t *testing.T) {
	if err := refuse(t, "nope"); !strings.Contains(err.Error(), "no command named nope") {
		t.Fatalf("said %q", err)
	}
}

func TestTwoCommandsAreRejected(t *testing.T) {
	if err := refuse(t, "run", "config"); !strings.Contains(err.Error(), "takes one command") {
		t.Fatalf("said %q", err)
	}
}

func TestAnUnknownFlagIsRejected(t *testing.T) {
	if err := refuse(t, "run", "--nope"); !strings.Contains(err.Error(), "nope") {
		t.Fatalf("said %q", err)
	}
}

func TestConfigTakesASettingFlag(t *testing.T) {
	if got := parse(t, "config", "--memory", "8g").Values.Setting("memory"); got != "8g" {
		t.Fatalf("read %q", got)
	}
}

func TestAFlagBeforeTheCommandIsRead(t *testing.T) {
	arguments := parse(t, "--memory", "8g", "gen")
	if arguments.Command != "gen" || arguments.Values.Setting("memory") != "8g" {
		t.Fatalf("read %+v", arguments)
	}
}

func TestAFlagWrittenWithAnEqualsIsRead(t *testing.T) {
	if got := parse(t, "run", "--memory=8g").Values.Setting("memory"); got != "8g" {
		t.Fatalf("read %q", got)
	}
}

func TestFlagsUseTheConfigKeysWithHyphens(t *testing.T) {
	arguments := parse(t, "run", "--root-size", "20g", "--prompt-file", "p.md")
	if arguments.Values.Setting("root_size") != "20g" || arguments.Values.Setting("prompt_file") != "p.md" {
		t.Fatalf("read %v", arguments.Values.Text)
	}
}

func TestConfigKeyHyphenatesAConfigKey(t *testing.T) {
	if got := cli.ConfigKey("root-size"); got != "root_size" {
		t.Fatalf("named %q", got)
	}
}

func TestASettingNoFlagGaveFallsBackToTheDefault(t *testing.T) {
	if got := parse(t, "run").Values.Setting("memory"); got != config.Defaults["memory"] {
		t.Fatalf("read %q", got)
	}
}

func TestMountIsRepeatable(t *testing.T) {
	arguments := parse(t, "run", "--mount", "/other", "--mount", "/third:rw")
	if !slices.Equal(arguments.Mounts, []string{"/other", "/third:rw"}) {
		t.Fatalf("read %v", arguments.Mounts)
	}
}

func TestSecretHostsAndReposAreNotFlags(t *testing.T) {
	arguments := parse(t, "run")
	for _, key := range []string{config.SecretHosts, config.Repos, config.MCP, config.RequiredMounts} {
		if _, given := arguments.Values.Text[key]; given {
			t.Errorf("%s was read from the command line", key)
		}
	}
}

func TestHelpAsksForTheUsage(t *testing.T) {
	if !parse(t, "--help").Help || !parse(t, "-h").Help {
		t.Fatal("help was not recognised")
	}
}

func TestGenAloneTakesNoFlags(t *testing.T) {
	if err := cli.RequireNoFlags(parse(t, "gen")); err != nil {
		t.Fatal(err)
	}
}

func TestGenRejectsASettingFlag(t *testing.T) {
	err := cli.RequireNoFlags(parse(t, "--memory", "8g", "gen"))
	if err == nil || !strings.Contains(err.Error(), "gen takes no flags, but got --memory") {
		t.Fatalf("said %v", err)
	}
}

func TestGenRejectsAMountFlag(t *testing.T) {
	err := cli.RequireNoFlags(parse(t, "--mount", "/cache", "gen"))
	if err == nil || !strings.Contains(err.Error(), "got --mount;") {
		t.Fatalf("said %v", err)
	}
}

func TestGenNamesEveryFlagItWasGiven(t *testing.T) {
	err := cli.RequireNoFlags(parse(t, "--memory", "8g", "--cpus", "8", "gen"))
	if err == nil || !strings.Contains(err.Error(), "--cpus, --memory") {
		t.Fatalf("said %v", err)
	}
}

func TestSelfUpdateAndMountPromptTakeNoFlags(t *testing.T) {
	for _, command := range []string{"self-update", "mount-prompt"} {
		err := cli.RequireNoFlags(parse(t, "--memory", "8g", command))
		if err == nil || !strings.Contains(err.Error(), command+" takes no flags") {
			t.Errorf("%s said %v", command, err)
		}
	}
}
