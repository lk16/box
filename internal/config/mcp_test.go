package config_test

import (
	"slices"
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

func TestParseWithoutMCPReadsACommaSeparatedList(t *testing.T) {
	if got := config.ParseWithoutMCP("postgres,kubernetes"); !slices.Equal(got, []string{"postgres", "kubernetes"}) {
		t.Fatalf("read %v", got)
	}
}

func TestParseWithoutMCPTakesAnEmptyEntryAsNoName(t *testing.T) {
	got := config.ParseWithoutMCP(" postgres , , kubernetes ,")
	if !slices.Equal(got, []string{"postgres", "kubernetes"}) {
		t.Fatalf("read %v", got)
	}
}

func TestParseWithoutMCPReadsAnUnsetVariableAsNothing(t *testing.T) {
	if got := config.ParseWithoutMCP(""); len(got) != 0 {
		t.Fatalf("read %v", got)
	}
}

func TestKeepMCPPassesEveryConfiguredServerOnWhenNothingWasAsked(t *testing.T) {
	kept, omitted := config.KeepMCP([]string{"postgres", "kubernetes"}, config.WithoutMCP{})
	if !slices.Equal(kept, []string{"postgres", "kubernetes"}) || len(omitted) != 0 {
		t.Fatalf("kept %v, left out %v", kept, omitted)
	}
}

func TestKeepMCPLeavesOutWhatTheFlagNames(t *testing.T) {
	without := config.WithoutMCP{Flag: []string{"kubernetes"}}
	kept, omitted := config.KeepMCP([]string{"postgres", "kubernetes"}, without)
	if !slices.Equal(kept, []string{"postgres"}) {
		t.Fatalf("kept %v", kept)
	}
	if !slices.Equal(omitted, []config.Omitted{{Name: "kubernetes", Source: "--" + config.WithoutMCPFlag}}) {
		t.Fatalf("left out %v", omitted)
	}
}

func TestKeepMCPLeavesOutWhatTheEnvironmentNames(t *testing.T) {
	without := config.WithoutMCP{Environment: []string{"kubernetes"}}
	kept, omitted := config.KeepMCP([]string{"postgres", "kubernetes"}, without)
	if !slices.Equal(kept, []string{"postgres"}) {
		t.Fatalf("kept %v", kept)
	}
	if !slices.Equal(omitted, []config.Omitted{{Name: "kubernetes", Source: config.WithoutMCPEnv}}) {
		t.Fatalf("left out %v", omitted)
	}
}

func TestKeepMCPAddsTheTwoWaysUpAndKeepsTheConfiguredOrder(t *testing.T) {
	without := config.WithoutMCP{Flag: []string{"sentry"}, Environment: []string{"postgres"}}
	kept, _ := config.KeepMCP([]string{"postgres", "kubernetes", "sentry", "redis"}, without)
	if !slices.Equal(kept, []string{"kubernetes", "redis"}) {
		t.Fatalf("kept %v", kept)
	}
}

func TestKeepMCPNamesTheFlagForAServerBothWaysAskAbout(t *testing.T) {
	without := config.WithoutMCP{Flag: []string{"postgres"}, Environment: []string{"postgres"}}
	_, omitted := config.KeepMCP([]string{"postgres"}, without)
	if len(omitted) != 1 || omitted[0].Source != "--"+config.WithoutMCPFlag {
		t.Fatalf("left out %v", omitted)
	}
}

func TestKeepMCPIgnoresANameNothingIsConfiguredUnder(t *testing.T) {
	without := config.WithoutMCP{Flag: []string{"redis"}, Environment: []string{"sentry"}}
	kept, omitted := config.KeepMCP([]string{"postgres"}, without)
	if !slices.Equal(kept, []string{"postgres"}) || len(omitted) != 0 {
		t.Fatalf("kept %v, left out %v", kept, omitted)
	}
}
