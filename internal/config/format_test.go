package config_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/lk16/box/internal/config"
)

// fullConfig builds a config with every field set to a recognisable value.
func fullConfig() config.Config {
	return config.Config{
		Name: "demo", Memory: "8g", CPUs: "2", RootSize: "20g", DockerSize: "30g",
		Model: "claude-opus-5", PromptFile: "docs/project-prompt.md", Kit: "registry/kit",
		Template: "frlg-sandbox:1", MCP: []string{"postgres", "kubernetes"}, Mounts: []string{"/cache:ro"},
	}
}

func TestFormatShowsTheTokenPathWithTheSettings(t *testing.T) {
	rendered := config.Format(fullConfig(), "/secrets/token", "/secrets/box.env")
	for _, part := range []string{config.TokenFileEnv, "/secrets/token", "8g"} {
		if !strings.Contains(rendered, part) {
			t.Errorf("%q is missing from the rendering", part)
		}
	}
}

func TestFormatPrintsTheTemplate(t *testing.T) {
	rendered := config.Format(fullConfig(), "/secrets/token", "/secrets/box.env")
	if !regexp.MustCompile(`(?m)^\s+template\s+frlg-sandbox:1$`).MatchString(rendered) {
		t.Fatalf("the template is missing from:\n%s", rendered)
	}
}

func TestFormatNamesAnUnsetTemplate(t *testing.T) {
	rendered := config.Format(configFrom(t, config.NewValues(), t.TempDir()), "", "")
	if !regexp.MustCompile(`(?m)^\s+template\s+` + regexp.QuoteMeta(config.Unset) + `$`).MatchString(rendered) {
		t.Fatalf("an unset template is not named in:\n%s", rendered)
	}
}

func TestFormatPrintsTheMCPServers(t *testing.T) {
	rendered := config.Format(fullConfig(), "/secrets/token", "/secrets/box.env")
	if !regexp.MustCompile(`(?m)^\s+mcp\s+postgres kubernetes$`).MatchString(rendered) {
		t.Fatalf("the mcp servers are missing from:\n%s", rendered)
	}
}

func TestFormatJoinsMounts(t *testing.T) {
	built := fullConfig()
	built.Mounts = []string{"/a:ro", "/b"}
	if !strings.Contains(config.Format(built, "", ""), "/a:ro /b") {
		t.Fatal("the mounts were not joined into one line")
	}
}

func TestFormatNamesASettingNothingWasGivenFor(t *testing.T) {
	rendered := config.Format(configFrom(t, config.NewValues(), t.TempDir()), "", "")
	if !strings.Contains(rendered, config.Unset) {
		t.Fatal("nothing was named as unset")
	}
}

func TestFormatLeavesNoLineEndingInWhitespace(t *testing.T) {
	rendered := config.Format(configFrom(t, config.NewValues(), t.TempDir()), "", "")
	for _, line := range strings.Split(rendered, "\n") {
		if line != strings.TrimRight(line, " \t") {
			t.Fatalf("a line ends in whitespace: %q", line)
		}
	}
}

func TestFormatAlignsEveryValueInOneColumn(t *testing.T) {
	rendered := config.Format(fullConfig(), "/secrets/token", "/secrets/box.env")
	columns := map[int]bool{}
	for _, line := range strings.Split(rendered, "\n")[1:] {
		columns[strings.Index(line, strings.Fields(line)[1])] = true
	}
	if len(columns) != 1 {
		t.Fatalf("values start in %d different columns", len(columns))
	}
}

func TestFormatNamesEachDeclaredSecretAndWhereItMayGo(t *testing.T) {
	given := config.NewValues()
	given.Raw[config.SecretHosts] = raw(map[string]string{"GITLAB_TOKEN": "gitlab.com"})
	rendered := config.Format(configFrom(t, given, t.TempDir()), "/secrets/token", "/secrets/box.env")
	if !strings.Contains(rendered, "GITLAB_TOKEN->gitlab.com") {
		t.Fatal("the declared secret is missing from the rendering")
	}
	if !regexp.MustCompile(`(?m)^\s+` + config.SecretsEnv + `\s+/secrets/box\.env$`).MatchString(rendered) {
		t.Fatalf("the secrets file is missing from:\n%s", rendered)
	}
}

func TestFormatNamesEachMemberItsBranchAndItsPath(t *testing.T) {
	built := fullConfig()
	built.Repos = []config.Member{member}
	if !strings.Contains(config.Format(built, "", ""), "billing-api@develop->../billing-api") {
		t.Fatal("the member is missing from the rendering")
	}
}

func TestFormatNamesWhatAMemberBringsOfItsOwn(t *testing.T) {
	settings := config.MemberSettings{Member: member, Mounts: []string{"/usr/local/go"}}
	if !strings.Contains(config.Format(configWithMember(t, settings), "", ""), "billing-api: /usr/local/go") {
		t.Fatal("what the member brings is missing from the rendering")
	}
}

func TestFormatSaysWhenAMemberBringsNothing(t *testing.T) {
	settings := config.MemberSettings{Member: member}
	if !strings.Contains(config.Format(configWithMember(t, settings), "", ""), "billing-api: nothing") {
		t.Fatal("a member bringing nothing is not said so")
	}
}

func TestFormatSaysAMemberIsNotOnThisMachine(t *testing.T) {
	settings := config.MemberSettings{Member: missingMember()}
	rendered := config.Format(configWithMember(t, settings), "", "")
	if !strings.Contains(rendered, "billing-api: "+config.MissingHere) {
		t.Fatalf("a member this machine does not have reads:\n%s", rendered)
	}
}

func TestFormatNamesAnEmptyListOfMounts(t *testing.T) {
	built := fullConfig()
	built.Mounts = nil
	if !regexp.MustCompile(`(?m)^\s+mounts\s+` + regexp.QuoteMeta(config.Unset) + `$`).MatchString(config.Format(built, "", "")) {
		t.Fatal("an empty mount list is not named as unset")
	}
}

func TestEveryConfigKeyIsAFieldOfTheConfigAndTheOtherWayAround(t *testing.T) {
	rendered := config.Format(fullConfig(), "", "")
	// required_mounts is answered by the mounts file, so it resolves to mounts rather than staying.
	for _, key := range append(slices.Clone(config.SettingKeys), config.MCP, config.SecretHosts, config.Repos) {
		if !regexp.MustCompile(`(?m)^\s+` + key + `\s`).MatchString(rendered) {
			t.Errorf("%s is missing from the rendered config", key)
		}
	}
	if regexp.MustCompile(`(?m)^\s+` + config.RequiredMounts + `\s`).MatchString(rendered) {
		t.Errorf("%s is rendered, though it resolves to mounts", config.RequiredMounts)
	}
	for _, field := range []string{"mounts", "members"} {
		if !regexp.MustCompile(`(?m)^\s+` + field + `\s`).MatchString(rendered) {
			t.Errorf("%s is missing from the rendered config", field)
		}
	}
}
