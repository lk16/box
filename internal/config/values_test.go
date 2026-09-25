package config_test

import (
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lk16/box/internal/config"
)

func TestToKebabCaseCollapsesSeparators(t *testing.T) {
	if got := config.ToKebabCase("My Project_v2"); got != "my-project-v2" {
		t.Fatalf("kebab-cased to %q", got)
	}
}

func TestDefaultBaseNameFallsBackWhenNothingSurvives(t *testing.T) {
	if got := config.DefaultBaseName("/tmp/___"); got != "box" {
		t.Fatalf("named %q", got)
	}
}

func TestTheConfigFileLivesInTheBoxDirectory(t *testing.T) {
	if config.ConfigFile != filepath.Join(config.BoxDir, "config.json") {
		t.Fatalf("the config file is %q", config.ConfigFile)
	}
}

func TestReadConfigFileReturnsEmptyWhenAbsent(t *testing.T) {
	read, err := config.ReadConfigFile(filepath.Join(t.TempDir(), config.ConfigFile))
	if err != nil || len(read.Text) != 0 || len(read.Raw) != 0 {
		t.Fatalf("read %v, %v", read, err)
	}
}

func TestReadConfigFileReadsKnownKeys(t *testing.T) {
	read, err := config.ReadConfigFile(writeConfig(t, t.TempDir(), map[string]any{"memory": "16g"}))
	if err != nil || read.Text["memory"] != "16g" || len(read.Text) != 1 {
		t.Fatalf("read %v, %v", read.Text, err)
	}
}

func TestReadConfigFileRejectsUnknownKeys(t *testing.T) {
	_, err := config.ReadConfigFile(writeConfig(t, t.TempDir(), map[string]any{"nope": 1}))
	wantError(t, err, "unknown keys: nope")
}

func TestReadConfigFileRejectsAJSONArray(t *testing.T) {
	_, err := config.ReadConfigFile(writeBoxFile(t, t.TempDir(), config.ConfigFile, []string{"memory"}))
	wantError(t, err, "must contain a JSON object")
}

func TestReadConfigFileRejectsBrokenJSON(t *testing.T) {
	path := writeConfig(t, t.TempDir(), map[string]any{})
	writeText(t, path, "{")
	_, err := config.ReadConfigFile(path)
	wantError(t, err, "not valid JSON")
}

func TestReadConfigFileRejectsANullSetting(t *testing.T) {
	_, err := config.ReadConfigFile(writeConfig(t, t.TempDir(), map[string]any{"model": nil}))
	wantError(t, err, "gives model null, which is not text or a number")
}

func TestReadConfigFileRejectsASettingThatIsAList(t *testing.T) {
	_, err := config.ReadConfigFile(writeConfig(t, t.TempDir(), map[string]any{"memory": []int{1, 2}}))
	wantError(t, err, "gives memory a list, which is not text or a number")
}

func TestReadConfigFileRejectsASettingThatIsABoolean(t *testing.T) {
	_, err := config.ReadConfigFile(writeConfig(t, t.TempDir(), map[string]any{"model": true}))
	wantError(t, err, "gives model a boolean, which is not text or a number")
}

func TestReadConfigFileTakesANumberAsTheStringItSpells(t *testing.T) {
	read, err := config.ReadConfigFile(writeConfig(t, t.TempDir(), map[string]any{"cpus": 4}))
	if err != nil || read.Text["cpus"] != "4" {
		t.Fatalf("read %v, %v", read.Text, err)
	}
}

func TestReadConfigFileKeepsRequiredMountsAnObject(t *testing.T) {
	declared := map[string]string{"go": "the Go toolchain"}
	path := writeConfig(t, t.TempDir(), map[string]any{config.RequiredMounts: declared})
	read, err := config.ReadConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	described, err := config.AsDescriptions(read.Raw[config.RequiredMounts])
	if err != nil || described.Get("go") != "the Go toolchain" {
		t.Fatalf("read %v, %v", described, err)
	}
}

func TestMergePrefersTheLaterSource(t *testing.T) {
	if got := config.Merge(values("memory", "16g"), values("memory", "2g")).Setting("memory"); got != "2g" {
		t.Fatalf("merged to %q", got)
	}
}

func TestMergeKeepsTheFileValueWhereNoFlagWasGiven(t *testing.T) {
	if got := config.Merge(values("cpus", "8"), config.NewValues()).Setting("cpus"); got != "8" {
		t.Fatalf("merged to %q", got)
	}
}

func TestMergeFallsBackToDefaults(t *testing.T) {
	merged := config.Merge(config.NewValues(), config.NewValues())
	if got := merged.Setting("memory"); got != config.Defaults["memory"] {
		t.Fatalf("merged to %q", got)
	}
}

func TestBuildDerivesNameFromDirectory(t *testing.T) {
	if got := configFrom(t, config.NewValues(), "/home/luuk/My Repo").Name; got != "my-repo" {
		t.Fatalf("named %q", got)
	}
}

func TestEveryConfigKeyIsSnakeCase(t *testing.T) {
	for _, key := range append(config.SettingKeys, config.ContainerKeys...) {
		if config.ToKebabCase(key) != strings.ReplaceAll(key, "_", "-") {
			t.Errorf("%q is not snake_case", key)
		}
	}
}

func TestResolvePathExpandsALeadingTilde(t *testing.T) {
	t.Setenv("HOME", "/home/someone")
	if got := config.ResolvePath("~/.cargo"); got != "/home/someone/.cargo" {
		t.Fatalf("resolved to %q", got)
	}
}

func TestResolvePathLeavesAnAbsolutePathAlone(t *testing.T) {
	if got := config.ResolvePath("/usr/local/go"); got != "/usr/local/go" {
		t.Fatalf("resolved to %q", got)
	}
}

func TestResolvePathExpandsATildeOnItsOwn(t *testing.T) {
	t.Setenv("HOME", "/home/someone")
	if got := config.ResolvePath("~"); got != "/home/someone" {
		t.Fatalf("resolved to %q", got)
	}
}

func TestResolvePathExpandsANamedUsersHome(t *testing.T) {
	account, err := user.Current()
	if err != nil {
		t.Skip("this machine has no current user to look up")
	}
	want := filepath.Join(account.HomeDir, "cache")
	if got := config.ResolvePath("~" + account.Username + "/cache"); got != want {
		t.Fatalf("resolved to %q, want %q", got, want)
	}
}

func TestResolvePathLeavesAUserThisMachineDoesNotHaveAlone(t *testing.T) {
	if got := config.ResolvePath("~definitely-not-a-user/cache"); got != "~definitely-not-a-user/cache" {
		t.Fatalf("resolved to %q", got)
	}
}

func TestResolvePathLeavesATildeInsideAPathAlone(t *testing.T) {
	if got := config.ResolvePath("/data/~backup"); got != "/data/~backup" {
		t.Fatalf("resolved to %q", got)
	}
}

func TestResolvePathKeepsEveryDotDot(t *testing.T) {
	// Collapsing them here would name a different directory whenever a symlink is in the way.
	for path, want := range map[string]string{
		"/usr/local/../lib": "/usr/local/../lib",
		"../rel":            "../rel",
		"/a//b":             "/a/b",
		"/a/./b":            "/a/b",
		"/a/b/":             "/a/b",
		"./x":               "x",
		"/":                 "/",
		"a/b":               "a/b",
	} {
		if got := config.ResolvePath(path); got != want {
			t.Errorf("%s resolved to %q, want %q", path, got, want)
		}
	}
}

func TestResolvePathKeepsADotDotBelowAnExpandedHome(t *testing.T) {
	t.Setenv("HOME", "/home/someone")
	if got := config.ResolvePath("~/a/../b"); got != "/home/someone/a/../b" {
		t.Fatalf("resolved to %q", got)
	}
}

func TestUnderKeepsADotDotItJoinsOn(t *testing.T) {
	if got := config.Under("/work/boxes", "../billing-api"); got != "/work/boxes/../billing-api" {
		t.Fatalf("joined to %q", got)
	}
	if got := config.Under("/work/boxes", "/elsewhere"); got != "/elsewhere" {
		t.Fatalf("an absolute path joined to %q", got)
	}
}
