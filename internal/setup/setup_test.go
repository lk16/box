package setup_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lk16/box/internal/boxtest"
	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/jsonx"
	"github.com/lk16/box/internal/project"
	"github.com/lk16/box/internal/setup"
	"github.com/lk16/box/internal/system"
)

// declared is how one member reads in a group's config file.
const declared = `{"billing-api": {"branch": "develop", "git_origin": "https://example.com/billing-api.git"}}`

// fixture is one gen run's fakes, and the setup that reaches the system only through them.
type fixture struct {
	prompter *boxtest.Prompter
	console  *boxtest.Console
	setup    setup.Setup
}

// newFixture builds a setup answering at a terminal that gives the answers listed, or at none.
func newFixture(terminal bool, answers ...string) *fixture {
	prompter := &boxtest.Prompter{Answers: answers, Terminal: terminal}
	console := &boxtest.Console{}
	return &fixture{
		prompter: prompter,
		console:  console,
		setup: setup.Setup{Deps: system.Deps{
			Run:     system.Commands{},
			Console: console.Handle(false),
			Ask:     prompter,
		}},
	}
}

// generate runs box gen with nobody at the terminal, which is what most of these tests want.
func generate(t *testing.T, directory string) *fixture {
	t.Helper()
	run := newFixture(false)
	code, err := run.setup.Generate(directory)
	if err != nil || code != 0 {
		t.Fatalf("gen exited %d, %v", code, err)
	}
	return run
}

// readJSON reads a file box gen wrote back as name to text.
func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var read map[string]any
	if err := json.Unmarshal(contents, &read); err != nil {
		t.Fatal(err)
	}
	return read
}

// writeConfig writes a config file, creating the .box directory it lives in.
func writeConfig(t *testing.T, directory, contents string) {
	t.Helper()
	boxtest.WriteFile(t, filepath.Join(directory, config.ConfigFile), contents)
}

func TestGenWritesBothFiles(t *testing.T) {
	directory := t.TempDir()
	generate(t, directory)
	written := readJSON(t, filepath.Join(directory, config.ConfigFile))
	if written["kit"] != config.KitDir || written["memory"] != "4g" {
		t.Fatalf("the config reads %v", written)
	}
	if mounts := readJSON(t, filepath.Join(directory, config.MountsFile)); len(mounts) != 0 {
		t.Fatalf("the mounts file reads %v", mounts)
	}
}

func TestGenWritesAConfigBoxCanReadBack(t *testing.T) {
	directory := t.TempDir()
	generate(t, directory)
	read, err := config.ReadConfigFile(filepath.Join(directory, config.ConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	if read.Setting("kit") != config.KitDir {
		t.Fatalf("the kit reads %q", read.Setting("kit"))
	}
}

func TestTheStarterConfigHoldsNothingOnlyAGroupNeeds(t *testing.T) {
	for _, key := range config.GroupSettings {
		if _, held := setup.StarterConfig().Get(key); held {
			t.Errorf("the starter holds %s", key)
		}
	}
}

func TestTheGroupStarterIsTheStarterPlusWhatOnlyAGroupNeeds(t *testing.T) {
	group := setup.GroupConfig()
	for _, pair := range setup.StarterConfig() {
		if held, ok := group.Get(pair.Key); !ok || string(held) != string(pair.Value) {
			t.Errorf("the group starter gives %s %s", pair.Key, held)
		}
	}
	want := map[string]string{config.Repos: "{}", config.SecretHosts: "{}", config.MCP: "[]"}
	for key, value := range want {
		if held, _ := group.Get(key); string(held) != value {
			t.Errorf("the group starter gives %s %s, want %s", key, held, value)
		}
	}
}

func TestGenWritesAGroupConfigBoxCanReadBack(t *testing.T) {
	directory := t.TempDir()
	boxtest.WriteFile(t, filepath.Join(directory, config.ConfigFile), string(jsonx.Write(setup.GroupConfig())))
	read, err := config.ReadConfigFile(filepath.Join(directory, config.ConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	if read.Container(config.Repos) == nil {
		t.Fatal("the group config lost its repos key")
	}
}

func TestGenWritesAStarterKit(t *testing.T) {
	directory := t.TempDir()
	generate(t, directory)
	contents, err := os.ReadFile(filepath.Join(directory, config.KitSpecFile))
	if err != nil {
		t.Fatal(err)
	}
	spec := string(contents)
	if !strings.Contains(spec, "api.anthropic.com:443") || !strings.HasSuffix(spec, "\n") {
		t.Fatalf("the kit reads:\n%s", spec)
	}
}

func TestTheStarterKitIsNamedAfterTheProject(t *testing.T) {
	if !strings.Contains(setup.BuildKitSpec("my-repo"), "name: my-repo-network-policy") {
		t.Fatal("the kit is not named after the project")
	}
}

func TestTheStarterKitUsesTheV2BlockNames(t *testing.T) {
	spec := setup.BuildKitSpec("demo")
	// sbx v0.38.0 renamed caps to permissions, and its strict loader rejects the old name.
	if !strings.Contains(spec, "permissions:") || strings.Contains(spec, "caps:") {
		t.Fatalf("the kit reads:\n%s", spec)
	}
}

func TestTheStarterKitSaysItIsAStartingPoint(t *testing.T) {
	if !strings.Contains(setup.BuildKitSpec("demo"), "starting point") {
		t.Fatal("the kit does not say it is a starting point")
	}
}

func TestGenPointsTheKitSettingAtTheKitItWrites(t *testing.T) {
	directory := t.TempDir()
	generate(t, directory)
	read, err := config.ReadConfigFile(filepath.Join(directory, config.ConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(directory, read.Setting("kit"))); err != nil || !info.IsDir() {
		t.Fatalf("the kit setting points at %q", read.Setting("kit"))
	}
}

func TestGenKeepsAKitTheProjectAlreadyHas(t *testing.T) {
	directory := t.TempDir()
	spec := filepath.Join(directory, config.KitSpecFile)
	boxtest.WriteFile(t, spec, "kind: mixin\n")
	generate(t, directory)
	contents, _ := os.ReadFile(spec)
	if string(contents) != "kind: mixin\n" {
		t.Fatalf("the kit reads %q", contents)
	}
}

func TestGenSaysWhatItWroteAndWhatItKept(t *testing.T) {
	directory := t.TempDir()
	generate(t, directory)
	run := generate(t, directory)
	if !strings.Contains(run.console.Printed(), "kept    "+config.KitSpecFile) {
		t.Fatalf("gen said:\n%s", run.console.Printed())
	}
}

func TestGenKeepsAnExistingConfig(t *testing.T) {
	directory := t.TempDir()
	writeConfig(t, directory, `{"memory": "16g"}`)
	generate(t, directory)
	if got := readJSON(t, filepath.Join(directory, config.ConfigFile)); got["memory"] != "16g" {
		t.Fatalf("the config reads %v", got)
	}
}

func TestGenKeepsAnExistingMountsFile(t *testing.T) {
	directory := t.TempDir()
	boxtest.WriteFile(t, filepath.Join(directory, config.MountsFile), `{"cache": "/cache"}`)
	writeConfig(t, directory, `{"required_mounts": {"cache": "the build cache"}}`)
	generate(t, directory)
	if got := readJSON(t, filepath.Join(directory, config.MountsFile)); got["cache"] != "/cache" {
		t.Fatalf("the mounts file reads %v", got)
	}
}

func TestGenScaffoldsAPlaceholderForEveryDeclaredMount(t *testing.T) {
	directory := t.TempDir()
	writeConfig(t, directory, `{"required_mounts": {"go": "the Go toolchain"}}`)
	generate(t, directory)
	if got := readJSON(t, filepath.Join(directory, config.MountsFile)); got["go"] != config.MountPlaceholder {
		t.Fatalf("the mounts file reads %v", got)
	}
}

func TestGenAddsDeclaredNamesTheMountsFileIsMissing(t *testing.T) {
	directory := t.TempDir()
	writeConfig(t, directory, `{"required_mounts": {"go": "the Go toolchain", "cargo": "the cargo home"}}`)
	boxtest.WriteFile(t, filepath.Join(directory, config.MountsFile), `{"go": "/usr/local/go"}`)
	generate(t, directory)
	got := readJSON(t, filepath.Join(directory, config.MountsFile))
	if got["go"] != "/usr/local/go" || got["cargo"] != config.MountPlaceholder {
		t.Fatalf("the mounts file reads %v", got)
	}
}

func TestGenLeavesAnUndeclaredNameForBoxToReject(t *testing.T) {
	directory := t.TempDir()
	writeConfig(t, directory, `{"required_mounts": {}}`)
	boxtest.WriteFile(t, filepath.Join(directory, config.MountsFile), `{"typo": "/cache"}`)
	generate(t, directory)
	if got := readJSON(t, filepath.Join(directory, config.MountsFile)); got["typo"] != "/cache" {
		t.Fatalf("the mounts file reads %v", got)
	}
}

func TestGenWarnsAboutEveryPlaceholderItWrote(t *testing.T) {
	directory := t.TempDir()
	writeConfig(t, directory, `{"required_mounts": {"go": "the Go toolchain"}}`)
	run := generate(t, directory)
	if !strings.Contains(run.console.Warned(), "go: the Go toolchain") {
		t.Fatalf("gen warned:\n%s", run.console.Warned())
	}
}

func TestGenIsSilentWhenNothingNeedsFillingIn(t *testing.T) {
	run := generate(t, t.TempDir())
	if run.console.Warned() != "" {
		t.Fatalf("gen warned:\n%s", run.console.Warned())
	}
}

func TestGenGivesEveryDeclaredMemberAnEmptyPath(t *testing.T) {
	directory := t.TempDir()
	writeConfig(t, directory, `{"repos": `+declared+`}`)
	generate(t, directory)
	if got := readJSON(t, filepath.Join(directory, config.ReposFile)); got["billing-api"] != "" {
		t.Fatalf("the repos file reads %v", got)
	}
}

func TestGenKeepsAMembersPathAlreadyFilledIn(t *testing.T) {
	directory := t.TempDir()
	writeConfig(t, directory, `{"repos": `+declared+`}`)
	boxtest.WriteFile(t, filepath.Join(directory, config.ReposFile), `{"billing-api": "../billing-api"}`)
	generate(t, directory)
	if got := readJSON(t, filepath.Join(directory, config.ReposFile)); got["billing-api"] != "../billing-api" {
		t.Fatalf("the repos file reads %v", got)
	}
}

func TestGenNamesEachMemberStillNeedingAPathAndWhereToCloneItFrom(t *testing.T) {
	directory := t.TempDir()
	writeConfig(t, directory, `{"repos": `+declared+`}`)
	run := generate(t, directory)
	if !strings.Contains(run.console.Warned(), "billing-api: https://example.com/billing-api.git") {
		t.Fatalf("gen warned:\n%s", run.console.Warned())
	}
}

func TestGenWritesNoReposFileForOneProject(t *testing.T) {
	directory := t.TempDir()
	generate(t, directory)
	if _, err := os.Stat(filepath.Join(directory, config.ReposFile)); err == nil {
		t.Fatal("a single project was given a repos file")
	}
}

func TestGenAcceptsAnExistingBoxDirectory(t *testing.T) {
	directory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(directory, config.BoxDir), 0o755); err != nil {
		t.Fatal(err)
	}
	generate(t, directory)
}

func TestGenCreatesAGitignoreHoldingEveryLocalPath(t *testing.T) {
	boxtest.Isolate(t)
	directory := boxtest.GitInit(t, t.TempDir())
	generate(t, directory)
	contents, err := os.ReadFile(filepath.Join(directory, config.GitignoreFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != config.MountsFile+"\n"+config.ReposFile+"\n" {
		t.Fatalf("the gitignore reads %q", contents)
	}
}

func TestGenWritesNoGitignoreOutsideARepository(t *testing.T) {
	directory := t.TempDir()
	generate(t, directory)
	run := generate(t, directory)
	if _, err := os.Stat(filepath.Join(directory, config.GitignoreFile)); err == nil {
		t.Fatal("a gitignore was written outside a repository")
	}
	if !strings.Contains(run.console.Printed(), "not a git repository") {
		t.Fatalf("gen said:\n%s", run.console.Printed())
	}
}

func TestGenKeepsWhatTheGitignoreAlreadyHeld(t *testing.T) {
	boxtest.Isolate(t)
	directory := boxtest.GitInit(t, t.TempDir())
	boxtest.WriteFile(t, filepath.Join(directory, config.GitignoreFile), "*.log\n")
	generate(t, directory)
	contents, _ := os.ReadFile(filepath.Join(directory, config.GitignoreFile))
	if string(contents) != "*.log\n"+config.MountsFile+"\n"+config.ReposFile+"\n" {
		t.Fatalf("the gitignore reads %q", contents)
	}
}

func TestGenLeavesAnAlreadyIgnoredGitignoreAlone(t *testing.T) {
	boxtest.Isolate(t)
	directory := boxtest.GitInit(t, t.TempDir())
	boxtest.WriteFile(t, filepath.Join(directory, config.GitignoreFile), config.BoxDir+"/\n")
	generate(t, directory)
	contents, _ := os.ReadFile(filepath.Join(directory, config.GitignoreFile))
	if string(contents) != config.BoxDir+"/\n" {
		t.Fatalf("the gitignore reads %q", contents)
	}
}

func TestAppendLineStartsANewLineWhenOneIsMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), config.GitignoreFile)
	boxtest.WriteFile(t, path, "*.log")
	if err := setup.AppendLine(path, "build"); err != nil {
		t.Fatal(err)
	}
	contents, _ := os.ReadFile(path)
	if string(contents) != "*.log\nbuild\n" {
		t.Fatalf("the file reads %q", contents)
	}
}

func TestAppendLineCreatesTheFileWhenAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), config.GitignoreFile)
	if err := setup.AppendLine(path, "build"); err != nil {
		t.Fatal(err)
	}
	contents, _ := os.ReadFile(path)
	if string(contents) != "build\n" {
		t.Fatalf("the file reads %q", contents)
	}
}

func TestGenAsksNothingWhereThereIsNobodyToAsk(t *testing.T) {
	directory := t.TempDir()
	run := generate(t, directory)
	if len(run.prompter.Questions) != 0 {
		t.Fatalf("gen asked %v", run.prompter.Questions)
	}
}

// answering runs box gen at a terminal that gives the answers listed.
func answering(t *testing.T, directory string, answers ...string) *fixture {
	t.Helper()
	run := newFixture(true, answers...)
	if _, err := run.setup.Generate(directory); err != nil {
		t.Fatal(err)
	}
	return run
}

func TestGenWritesTheStarterForOneProject(t *testing.T) {
	directory := t.TempDir()
	run := answering(t, directory, "1")
	if _, group := readJSON(t, filepath.Join(directory, config.ConfigFile))[config.Repos]; group {
		t.Fatal("answering 1 wrote a group config")
	}
	if !strings.Contains(run.prompter.Questions[0], "one project") {
		t.Fatalf("gen asked %q", run.prompter.Questions[0])
	}
}

func TestAnEmptyAnswerMeansOneProject(t *testing.T) {
	directory := t.TempDir()
	answering(t, directory, "")
	if _, group := readJSON(t, filepath.Join(directory, config.ConfigFile))[config.Repos]; group {
		t.Fatal("an empty answer wrote a group config")
	}
}

func TestGenWritesTheGroupStarterForAGroup(t *testing.T) {
	directory := t.TempDir()
	run := answering(t, directory, "2")
	if _, group := readJSON(t, filepath.Join(directory, config.ConfigFile))[config.Repos]; !group {
		t.Fatal("answering 2 wrote a single project config")
	}
	if !strings.Contains(run.console.Printed(), "Next:") {
		t.Fatalf("gen said:\n%s", run.console.Printed())
	}
}

func TestAnAnswerBoxDoesNotKnowIsAskedAgain(t *testing.T) {
	directory := t.TempDir()
	run := answering(t, directory, "yes", "2")
	if len(run.prompter.Questions) != 2 {
		t.Fatalf("gen asked %v", run.prompter.Questions)
	}
	if _, group := readJSON(t, filepath.Join(directory, config.ConfigFile))[config.Repos]; !group {
		t.Fatal("the second answer was not taken")
	}
}

func TestGenWritesNothingWhenTheQuestionIsNeverAnswered(t *testing.T) {
	directory := t.TempDir()
	run := newFixture(true)
	code, err := run.setup.Generate(directory)
	if err != nil || code != 1 {
		t.Fatalf("gen exited %d, %v", code, err)
	}
	if _, err := os.Stat(filepath.Join(directory, config.BoxDir)); err == nil {
		t.Fatal("gen wrote a box directory with nothing answered")
	}
}

func TestGenAsksNothingAboutAProjectThatAlreadyHasAConfig(t *testing.T) {
	directory := t.TempDir()
	writeConfig(t, directory, `{"memory": "16g"}`)
	run := answering(t, directory)
	if len(run.prompter.Questions) != 0 {
		t.Fatalf("gen asked %v", run.prompter.Questions)
	}
}

func TestGenWarnsAboutAPlaceholderOnlyOnTheFileItWrote(t *testing.T) {
	directory := t.TempDir()
	writeConfig(t, directory, `{"required_mounts": {"go": "the Go toolchain"}}`)
	if warned := generate(t, directory).console.Warned(); !strings.Contains(warned, "go: the Go toolchain") {
		t.Fatalf("the first gen warned:\n%s", warned)
	}
	// The second gen keeps the file, and the user has already been told about the placeholder.
	if warned := generate(t, directory).console.Warned(); warned != "" {
		t.Fatalf("the second gen warned:\n%s", warned)
	}
}

func TestTheStarterConfigIsEveryDefaultButTheKitGenWrites(t *testing.T) {
	want := []string{}
	for _, key := range append(slices.Clone(config.SettingKeys), config.ContainerKeys...) {
		if !slices.Contains(config.GroupSettings, key) {
			want = append(want, key)
		}
	}
	starter := setup.StarterConfig()
	if !slices.Equal(starter.Keys(), want) {
		t.Fatalf("the starter holds %v, want %v", starter.Keys(), want)
	}
	for _, key := range want {
		held, _ := starter.Get(key)
		// The kit is the one value gen fills in, since gen also writes the kit it points at.
		if key == "kit" {
			if string(held) != `"`+config.KitDir+`"` {
				t.Fatalf("the starter gives kit %s", held)
			}
			continue
		}
		if wanted := string(jsonx.Text(config.Defaults[key])); key != config.RequiredMounts && string(held) != wanted {
			t.Errorf("the starter gives %s %s, want %s", key, held, wanted)
		}
	}
	if held, _ := starter.Get(config.RequiredMounts); string(held) != "{}" {
		t.Errorf("the starter gives %s %s", config.RequiredMounts, held)
	}
}

func TestGenLeavesAProjectBoxWillRunIn(t *testing.T) {
	boxtest.Isolate(t)
	directory := boxtest.GitInit(t, t.TempDir())
	generate(t, directory)
	// gen writes the .gitignore lines box would otherwise refuse to run without.
	if err := project.RequireIgnoredLocalPaths(system.Commands{}, directory, ""); err != nil {
		t.Fatal(err)
	}
}

func TestGenLeavesAGroupBoxWillRunIn(t *testing.T) {
	boxtest.Isolate(t)
	directory := boxtest.GitInit(t, t.TempDir())
	writeConfig(t, directory, `{"repos": `+declared+`}`)
	generate(t, directory)
	if err := project.RequireIgnoredLocalPaths(system.Commands{}, directory, ""); err != nil {
		t.Fatal(err)
	}
}
