package config_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/lk16/box/internal/config"
)

func TestToMCPServersRefusesTheNamesAsOneCommaSeparatedText(t *testing.T) {
	_, err := config.ToMCPServers(raw("postgres,kubernetes"))
	wantError(t, err, "mcp must be a JSON list of server names")
}

func TestToMCPServersRefusesANameThatIsNotText(t *testing.T) {
	_, err := config.ToMCPServers(raw([]int{5}))
	wantError(t, err, "mcp holds a number, which is not a server name")
}

func TestToMCPServersRefusesAnEmptyName(t *testing.T) {
	_, err := config.ToMCPServers(raw([]string{""}))
	wantError(t, err, "mcp holds an empty server name")
}

func TestToMCPServersRefusesANameSbxWouldSplitInTwo(t *testing.T) {
	_, err := config.ToMCPServers(raw([]string{"postgres,kubernetes"}))
	wantError(t, err, "postgres,kubernetes holds a comma")
}

func TestToMCPServersKeepsTheOrderListed(t *testing.T) {
	servers, err := config.ToMCPServers(raw([]string{"postgres", "kubernetes"}))
	if err != nil || !slices.Equal(servers, []string{"postgres", "kubernetes"}) {
		t.Fatalf("read %v, %v", servers, err)
	}
}

func TestBuildSystemPromptIsTheBasePromptWithoutAProjectPrompt(t *testing.T) {
	if got := config.BuildSystemPrompt([]string{config.BasePrompt}); got != config.BasePrompt {
		t.Fatal("the base prompt was changed by being the only part")
	}
}

func TestBuildSystemPromptPutsTheProjectPromptLast(t *testing.T) {
	combined := config.BuildSystemPrompt([]string{config.BasePrompt, "project rules"})
	if !strings.HasPrefix(combined, config.BasePrompt) || !strings.HasSuffix(combined, "project rules") {
		t.Fatalf("combined to %q", combined)
	}
}

func TestBuildSystemPromptLeavesOutAPartThisRunHasNothingFor(t *testing.T) {
	combined := config.BuildSystemPrompt([]string{config.BasePrompt, "", "project rules"})
	if combined != config.BasePrompt+"\n\nproject rules" {
		t.Fatalf("combined to %q", combined)
	}
}

func TestReadSystemPromptReturnsEmptyWithoutFile(t *testing.T) {
	read, err := config.ReadSystemPrompt("")
	if err != nil || read != "" {
		t.Fatalf("read %q, %v", read, err)
	}
}

func TestReadSystemPromptReadsTheFile(t *testing.T) {
	path := tokenFile(t, "stay in the sandbox")
	read, err := config.ReadSystemPrompt(path)
	if err != nil || read != "stay in the sandbox" {
		t.Fatalf("read %q, %v", read, err)
	}
}

func TestReadSystemPromptRejectsMissingFile(t *testing.T) {
	_, err := config.ReadSystemPrompt(t.TempDir() + "/missing.md")
	wantError(t, err, "does not exist")
}

func TestEveryCommandBoxShellsOutToIsRequired(t *testing.T) {
	if !slices.Equal(config.RequiredBinaries, []string{"sbx", "git", "claude"}) {
		t.Fatalf("box shells out to %v", config.RequiredBinaries)
	}
}

func TestConfigIsNotASetupCommand(t *testing.T) {
	if slices.Contains(config.SetupCommands, "config") {
		t.Fatal("config was listed as a setup command")
	}
}

func TestTheBasePromptSaysTheSessionIsUnattended(t *testing.T) {
	if !strings.Contains(config.BasePrompt, "unattended") {
		t.Fatal("the base prompt does not say the session is unattended")
	}
}
