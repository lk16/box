package cli_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lk16/box/internal/boxtest"
	"github.com/lk16/box/internal/cli"
	"github.com/lk16/box/internal/config"
)

// declared is how one member reads in a group's config file.
const declared = `{"billing-api": {"branch": "develop", "git_origin": "https://example.com/billing-api.git"}}`

// loadFrom resolves the settings one command line and one directory come out with.
func loadFrom(t *testing.T, directory string, argv ...string) config.Config {
	t.Helper()
	settings, err := cli.LoadConfig(parse(t, argv...), directory)
	if err != nil {
		t.Fatal(err)
	}
	return settings
}

// writeBox writes one .box file, creating the directory it lives in.
func writeBox(t *testing.T, directory, name, contents string) {
	t.Helper()
	boxtest.WriteFile(t, filepath.Join(directory, name), contents)
}

func TestLoadConfigLetsCLIWinOverFile(t *testing.T) {
	directory := t.TempDir()
	writeBox(t, directory, config.ConfigFile, `{"memory": "16g", "cpus": "8"}`)
	settings := loadFrom(t, directory, "run", "--memory", "1g")
	if settings.Memory != "1g" || settings.CPUs != "8" {
		t.Fatalf("resolved to %+v", settings)
	}
}

func TestLoadConfigTakesANumericCPUsFromTheConfigFile(t *testing.T) {
	directory := t.TempDir()
	writeBox(t, directory, config.ConfigFile, `{"cpus": 6}`)
	if got := loadFrom(t, directory, "run").CPUs; got != "6" {
		t.Fatalf("resolved to %q", got)
	}
}

func TestLoadConfigResolvesTheTemplateFromEitherSide(t *testing.T) {
	directory := t.TempDir()
	if got := loadFrom(t, directory, "run").Template; got != "" {
		t.Fatalf("the default template is %q", got)
	}
	if got := loadFrom(t, directory, "run", "--template", "frlg-sandbox:1").Template; got != "frlg-sandbox:1" {
		t.Fatalf("the flag resolved to %q", got)
	}
	writeBox(t, directory, config.ConfigFile, `{"template": "frlg-sandbox:1"}`)
	if got := loadFrom(t, directory, "run").Template; got != "frlg-sandbox:1" {
		t.Fatalf("the file resolved to %q", got)
	}
	if got := loadFrom(t, directory, "run", "--template", "frlg-sandbox:2").Template; got != "frlg-sandbox:2" {
		t.Fatalf("the flag did not win: %q", got)
	}
}

func TestLoadConfigTakesTheMCPServersFromTheConfigFile(t *testing.T) {
	directory := t.TempDir()
	if got := loadFrom(t, directory, "run").MCP; len(got) != 0 {
		t.Fatalf("the default mcp is %v", got)
	}
	writeBox(t, directory, config.ConfigFile, `{"mcp": ["postgres", "kubernetes"]}`)
	if got := loadFrom(t, directory, "run").MCP; !slices.Equal(got, []string{"postgres", "kubernetes"}) {
		t.Fatalf("resolved to %v", got)
	}
}

func TestLoadConfigReadsTheDeclaredSecrets(t *testing.T) {
	directory := t.TempDir()
	writeBox(t, directory, config.ConfigFile, `{"secret_hosts": {"GITLAB_TOKEN": "gitlab.com"}}`)
	want := []config.Secret{{Name: "GITLAB_TOKEN", Host: "gitlab.com"}}
	if got := loadFrom(t, directory, "run").SecretHosts; !slices.Equal(got, want) {
		t.Fatalf("resolved to %v", got)
	}
}

func TestLoadConfigTakesMountsFromTheMountsFile(t *testing.T) {
	directory := t.TempDir()
	writeBox(t, directory, config.ConfigFile, `{"required_mounts": {"cache": "the build cache"}}`)
	writeBox(t, directory, config.MountsFile, `{"cache": "/cache"}`)
	if got := loadFrom(t, directory, "run").Mounts; !slices.Equal(got, []string{"/cache:ro"}) {
		t.Fatalf("resolved to %v", got)
	}
}

func TestLoadConfigAddsMountFlagsToTheMountsFile(t *testing.T) {
	directory := t.TempDir()
	writeBox(t, directory, config.ConfigFile, `{"required_mounts": {"cache": "the build cache"}}`)
	writeBox(t, directory, config.MountsFile, `{"cache": "/cache"}`)
	got := loadFrom(t, directory, "run", "--mount", "/other", "--mount", "/third:rw").Mounts
	if !slices.Equal(got, []string{"/cache:ro", "/other:ro", "/third"}) {
		t.Fatalf("resolved to %v", got)
	}
}

func TestLoadConfigTakesMountFlagsWithoutAMountsFile(t *testing.T) {
	if got := loadFrom(t, t.TempDir(), "run", "--mount", "/other").Mounts; !slices.Equal(got, []string{"/other:ro"}) {
		t.Fatalf("resolved to %v", got)
	}
}

func TestLoadConfigHasNoMountsWithoutAMountsFile(t *testing.T) {
	if got := loadFrom(t, t.TempDir(), "run").Mounts; len(got) != 0 {
		t.Fatalf("resolved to %v", got)
	}
}

func TestLoadConfigReadsTheMembersAndWhereThisMachineKeepsThem(t *testing.T) {
	directory := t.TempDir()
	writeBox(t, directory, config.ConfigFile, `{"repos": `+declared+`}`)
	writeBox(t, directory, config.ReposFile, `{"billing-api": "../billing-api"}`)
	members := loadFrom(t, directory, "run").Repos
	if len(members) != 1 || members[0].Name != "billing-api" || members[0].Branch != "develop" {
		t.Fatalf("resolved to %v", members)
	}
}

func TestAMembersMountsComeAfterTheGroupsAndBeforeTheFlags(t *testing.T) {
	root := t.TempDir()
	member := filepath.Join(root, "billing-api")
	writeBox(t, member, config.ConfigFile, `{"required_mounts": {"go": "the Go toolchain"}}`)
	writeBox(t, member, config.MountsFile, `{"go": "/usr/local/go"}`)
	boxes := filepath.Join(root, "boxes")
	writeBox(t, boxes, config.ConfigFile,
		`{"repos": `+declared+`, "required_mounts": {"cache": "the cache"}}`)
	writeBox(t, boxes, config.MountsFile, `{"cache": "/cache"}`)
	writeBox(t, boxes, config.ReposFile, `{"billing-api": "../billing-api"}`)
	got := loadFrom(t, boxes, "run", "--mount", "/extra").Mounts
	if !slices.Equal(got, []string{"/cache:ro", "/usr/local/go:ro", "/extra:ro"}) {
		t.Fatalf("resolved to %v", got)
	}
}

func TestLoadConfigReportsAConfigItCannotRead(t *testing.T) {
	directory := t.TempDir()
	writeBox(t, directory, config.ConfigFile, `{"nope": 1}`)
	_, err := cli.LoadConfig(parse(t, "run"), directory)
	if err == nil || !strings.Contains(err.Error(), "unknown keys: nope") {
		t.Fatalf("the refusal was %v", err)
	}
}
