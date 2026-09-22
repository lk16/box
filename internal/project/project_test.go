package project_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lk16/box/internal/boxtest"
	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/project"
	"github.com/lk16/box/internal/system"
)

// real runs commands for the tests that need git to answer for itself.
var real = system.Commands{}

// member is the one member a group declares in these tests.
var member = config.Member{
	Name:      "billing-api",
	Branch:    "develop",
	GitOrigin: "https://example.com/billing-api.git",
	Path:      "../billing-api",
}

// absent builds the same member as one this machine does not have at all.
func absent() config.Member {
	missing := member
	missing.Path, missing.Missing = "", true
	return missing
}

// at builds the same member kept somewhere else on this machine.
func at(path string) config.Member {
	moved := member
	moved.Path = path
	return moved
}

// projectAt builds the project a session started at the repository root resolves to.
func projectAt(directory string) project.Project {
	return project.Project{WorkingDirectory: directory, Root: directory}
}

// subfolderProject is what a session started one folder below the repository root resolves to.
func subfolderProject() project.Project {
	return project.Project{WorkingDirectory: "/work/boxes/billing", Root: "/work/boxes", StartedIn: "billing"}
}

// groupConfig builds the config of a group working on the given members, none bringing settings.
func groupConfig(t *testing.T, directory string, members ...config.Member) config.Config {
	t.Helper()
	settings := make([]config.MemberSettings, 0, len(members))
	for _, each := range members {
		settings = append(settings, config.MemberSettings{Member: each})
	}
	built, err := config.Build(config.NewValues(), nil, settings, directory)
	if err != nil {
		t.Fatal(err)
	}
	return built
}

// makeMember creates a repository with an origin remote, the way a member sits on a host.
func makeMember(t *testing.T, directory, name string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	boxtest.MakeRepository(t, path)
	boxtest.Git(t, path, "remote", "add", "origin", "https://example.com/"+name+".git")
	return path
}

// wantError fails unless the error says what the test expects it to.
func wantError(t *testing.T, err error, contains string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no refusal, wanted one saying %q", contains)
	}
	if !strings.Contains(err.Error(), contains) {
		t.Fatalf("refusal was %q, wanted one saying %q", err, contains)
	}
}

func TestADirectoryThatIsNotARepositoryIsRejected(t *testing.T) {
	wantError(t, project.RequireGitRepository(real, t.TempDir()), "not a git repository")
}

func TestARepositoryWithNoCommitsIsRejected(t *testing.T) {
	wantError(t, project.RequireGitRepository(real, boxtest.GitInit(t, t.TempDir())), "no commits")
}

func TestARepositoryWithACommitIsAccepted(t *testing.T) {
	if err := project.RequireGitRepository(real, boxtest.MakeRepository(t, t.TempDir())); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryRootFindsTheRootFromASubfolder(t *testing.T) {
	directory := boxtest.MakeRepository(t, t.TempDir())
	subfolder := filepath.Join(directory, "services", "api")
	boxtest.WriteFile(t, filepath.Join(subfolder, "keep"), "")
	if got := project.RepositoryRoot(real, subfolder); got != config.Resolve(directory) {
		t.Fatalf("the root is %q", got)
	}
}

func TestRepositoryRootIsTheWorkingDirectoryOutsideARepository(t *testing.T) {
	directory := t.TempDir()
	if got := project.RepositoryRoot(real, directory); got != directory {
		t.Fatalf("the root is %q", got)
	}
}

func TestPathBelowNamesTheFolderTheSessionStartedIn(t *testing.T) {
	if got := config.PathBelow("/work/boxes", "/work/boxes/billing"); got != "billing" {
		t.Fatalf("started in %q", got)
	}
}

func TestPathBelowIsEmptyAtTheRootItself(t *testing.T) {
	if got := config.PathBelow("/work/boxes", "/work/boxes"); got != "" {
		t.Fatalf("started in %q", got)
	}
}

func TestPathBelowIsEmptyWhenTheWorkingDirectoryIsElsewhere(t *testing.T) {
	if got := config.PathBelow("/work/boxes", "/work/other"); got != "" {
		t.Fatalf("started in %q", got)
	}
}

func TestBuildKnowsWhereASubfolderSessionStarted(t *testing.T) {
	directory := boxtest.MakeRepository(t, t.TempDir())
	subfolder := filepath.Join(directory, "billing")
	boxtest.WriteFile(t, filepath.Join(subfolder, "keep"), "")
	built := project.Build(real, subfolder)
	if built.Root != config.Resolve(directory) || built.StartedIn != "billing" {
		t.Fatalf("built %+v", built)
	}
}

func TestBuildAtTheRootStartedInNothing(t *testing.T) {
	directory := boxtest.MakeRepository(t, t.TempDir())
	if got := project.Build(real, directory).StartedIn; got != "" {
		t.Fatalf("started in %q", got)
	}
}

func TestSbxClonesTheWorkingDirectoryAtTheRoot(t *testing.T) {
	if got := projectAt("/work/demo").ClonePath(); got != "." {
		t.Fatalf("clones %q", got)
	}
}

func TestSbxClonesTheRootWhenBoxRanBelowIt(t *testing.T) {
	if got := subfolderProject().ClonePath(); got != "/work/boxes" {
		t.Fatalf("clones %q", got)
	}
}

func TestThePromptSaysNothingAboutASessionStartedAtTheRoot(t *testing.T) {
	if got := projectAt("/work/demo").StartedInPrompt(); got != "" {
		t.Fatalf("said %q", got)
	}
}

func TestThePromptNamesTheFolderASubfolderSessionStartedIn(t *testing.T) {
	if !strings.Contains(subfolderProject().StartedInPrompt(), "billing") {
		t.Fatal("the folder is missing from the prompt")
	}
}

func TestTheCheckoutsAreTheRepositoryBoxRunsInAndThenItsMembers(t *testing.T) {
	directory := t.TempDir()
	boxes := filepath.Join(directory, "boxes")
	checkouts := project.Checkouts(groupConfig(t, boxes, member), projectAt(boxes))
	if checkouts[0] != (project.Checkout{Path: boxes, KnownCommits: config.MainKnown}) {
		t.Fatalf("the first checkout is %+v", checkouts[0])
	}
	want := config.Resolve(filepath.Join(directory, "billing-api"))
	if checkouts[1].Path != want || checkouts[1].KnownCommits != config.MemberKnown {
		t.Fatalf("the second checkout is %+v", checkouts[1])
	}
}

func TestTheSandboxReadsTheRepositoryAndEveryMount(t *testing.T) {
	settings := config.Config{Mounts: []string{"/cache:ro"}}
	got := project.ReachablePaths(settings, projectAt("/work/demo"))
	if len(got) != 2 || got[0] != "/work/demo" || got[1] != "/cache" {
		t.Fatalf("reachable: %v", got)
	}
}

func TestTheSandboxReadsEveryMemberToo(t *testing.T) {
	directory := t.TempDir()
	boxes := filepath.Join(directory, "boxes")
	want := config.Resolve(filepath.Join(directory, "billing-api"))
	for _, path := range project.ReachablePaths(groupConfig(t, boxes, member), projectAt(boxes)) {
		if path == want {
			return
		}
	}
	t.Fatalf("%s is not reachable", want)
}

func TestThePromptSaysNothingAboutMembersWithoutAny(t *testing.T) {
	if got := project.MembersPrompt(config.Config{}, projectAt(t.TempDir())); got != "" {
		t.Fatalf("said %q", got)
	}
}

func TestThePromptNamesEachMemberAndWhatItStartsOn(t *testing.T) {
	directory := t.TempDir()
	boxes := filepath.Join(directory, "boxes")
	prompt := project.MembersPrompt(groupConfig(t, boxes, member), projectAt(boxes))
	want := config.Resolve(filepath.Join(directory, "billing-api")) + " on develop"
	if !strings.Contains(prompt, want) || !strings.Contains(prompt, "comes back to the host") {
		t.Fatalf("the prompt reads:\n%s", prompt)
	}
}

func TestThePromptNamesEveryRepositoryThisHostDoesNotHave(t *testing.T) {
	prompt := project.MissingMembersPrompt(groupConfig(t, t.TempDir(), absent()))
	if !strings.Contains(prompt, "  billing-api: "+member.GitOrigin) || !strings.Contains(prompt, "no clone") {
		t.Fatalf("the prompt reads:\n%s", prompt)
	}
}

func TestThePromptSaysNothingAboutMissingMembersWhenThisHostHasThemAll(t *testing.T) {
	if got := project.MissingMembersPrompt(groupConfig(t, t.TempDir(), member)); got != "" {
		t.Fatalf("said %q", got)
	}
}

func TestAMemberThisHostDoesNotHaveIsNoPathTheSandboxReads(t *testing.T) {
	settings := groupConfig(t, "/work/boxes", absent())
	if got := project.ReachablePaths(settings, projectAt("/work/boxes")); len(got) != 1 {
		t.Fatalf("reachable: %v", got)
	}
	if got := project.Checkouts(settings, projectAt("/work/boxes")); len(got) != 1 {
		t.Fatalf("work comes back to: %v", got)
	}
}

func TestAMembersPromptComesAfterTheSectionNamingTheMembers(t *testing.T) {
	directory := t.TempDir()
	path := makeMember(t, directory, "billing-api")
	boxtest.WriteFile(t, filepath.Join(path, "agent.md"), "billing rules")
	settings := config.Config{Members: []config.MemberSettings{
		{Member: member, PromptFile: filepath.Join(path, "agent.md")},
	}}
	boxes := filepath.Join(directory, "boxes")
	prompts, err := project.MemberPrompts(settings, projectAt(boxes))
	want := config.Resolve(path) + ":\n\nbilling rules"
	if err != nil || len(prompts) != 1 || prompts[0] != want {
		t.Fatalf("read %q, %v", prompts, err)
	}
}

// makeRepositoryWithConfig creates a repository holding a config, a mounts file and a .gitignore.
func makeRepositoryWithConfig(t *testing.T, directory, gitignore string) string {
	t.Helper()
	boxtest.MakeRepository(t, directory)
	boxtest.WriteFile(t, filepath.Join(directory, config.GitignoreFile), gitignore)
	boxtest.WriteFile(t, filepath.Join(directory, config.ConfigFile), "{}")
	boxtest.WriteFile(t, filepath.Join(directory, config.MountsFile), `{"cache": "/cache"}`)
	return directory
}

func TestAGitignoredMountsFileIsAccepted(t *testing.T) {
	boxtest.Isolate(t)
	directory := makeRepositoryWithConfig(t, t.TempDir(), config.MountsFile+"\n")
	if err := project.RequireIgnoredLocalPaths(real, directory, ""); err != nil {
		t.Fatal(err)
	}
}

func TestAnIgnoredBoxDirectoryCoversTheMountsFile(t *testing.T) {
	boxtest.Isolate(t)
	directory := makeRepositoryWithConfig(t, t.TempDir(), config.BoxDir+"/\n")
	if err := project.RequireIgnoredLocalPaths(real, directory, ""); err != nil {
		t.Fatal(err)
	}
}

func TestACommittableMountsFileIsRejected(t *testing.T) {
	boxtest.Isolate(t)
	directory := makeRepositoryWithConfig(t, t.TempDir(), "*.log\n")
	wantError(t, project.RequireIgnoredLocalPaths(real, directory, ""), "not ignored by git")
}

func TestACommittableReposFileIsRejected(t *testing.T) {
	boxtest.Isolate(t)
	directory := makeRepositoryWithConfig(t, t.TempDir(), config.MountsFile+"\n")
	boxtest.WriteFile(t, filepath.Join(directory, config.ReposFile), `{"billing-api": "../billing-api"}`)
	wantError(t, project.RequireIgnoredLocalPaths(real, directory, ""), "repos.json is not ignored by git")
}

func TestNoMountsFileNeedsNoGitignoreEntry(t *testing.T) {
	boxtest.Isolate(t)
	if err := project.RequireIgnoredLocalPaths(real, boxtest.GitInit(t, t.TempDir()), ""); err != nil {
		t.Fatal(err)
	}
}

func TestIsGitIgnoredReadsTheGitignore(t *testing.T) {
	boxtest.Isolate(t)
	directory := makeRepositoryWithConfig(t, t.TempDir(), config.MountsFile+"\n")
	if !project.IsGitIgnored(real, directory, config.MountsFile) {
		t.Fatal("an ignored file was read as committable")
	}
}

func TestIsGitIgnoredSaysNoToAPathTheGitignoreMisses(t *testing.T) {
	boxtest.Isolate(t)
	directory := makeRepositoryWithConfig(t, t.TempDir(), "*.log\n")
	if project.IsGitIgnored(real, directory, config.MountsFile) {
		t.Fatal("a committable file was read as ignored")
	}
}

func TestAProjectWithNoConfigIsSentToGen(t *testing.T) {
	err := project.RequireConfigFile(t.TempDir())
	wantError(t, err, "Run box gen")
	wantError(t, err, config.ConfigFile)
}

func TestAProjectWithAConfigIsAccepted(t *testing.T) {
	directory := t.TempDir()
	boxtest.WriteFile(t, filepath.Join(directory, config.ConfigFile), "{}")
	if err := project.RequireConfigFile(directory); err != nil {
		t.Fatal(err)
	}
}

// settingsFor builds the config a values map and no mounts resolve to.
func settingsFor(t *testing.T, text map[string]string, directory string) config.Config {
	t.Helper()
	given := config.NewValues()
	for key, value := range text {
		given.Text[key] = value
	}
	built, err := config.Build(given, nil, nil, directory)
	if err != nil {
		t.Fatal(err)
	}
	return built
}

func TestRequireSettingsNeedsAKit(t *testing.T) {
	wantError(t, project.RequireSettings(settingsFor(t, nil, t.TempDir())), "kit is not set")
}

func TestRequireSettingsNeedsAModel(t *testing.T) {
	settings := settingsFor(t, map[string]string{"kit": "registry/kit"}, t.TempDir())
	wantError(t, project.RequireSettings(settings), "model is not set")
}

func TestAKitNamingAFileIsRejected(t *testing.T) {
	directory := t.TempDir()
	spec := filepath.Join(directory, "spec.yaml")
	boxtest.WriteFile(t, spec, "kit: {}\n")
	settings := settingsFor(t, map[string]string{"kit": spec, "model": "claude-opus-5"}, directory)
	wantError(t, project.RequireSettings(settings), "kit names a file")
}

func TestAKitNamingADirectoryIsAccepted(t *testing.T) {
	directory := t.TempDir()
	kit := filepath.Join(directory, "kit")
	boxtest.WriteFile(t, filepath.Join(kit, "spec.yaml"), "kit: {}\n")
	settings := settingsFor(t, map[string]string{"kit": kit, "model": "claude-opus-5"}, directory)
	if err := project.RequireSettings(settings); err != nil {
		t.Fatal(err)
	}
}

func TestAKitThatIsNotOnDiskIsLeftToSbx(t *testing.T) {
	settings := settingsFor(t, map[string]string{"kit": "some/registry/ref", "model": "claude-opus-5"}, t.TempDir())
	if err := project.RequireSettings(settings); err != nil {
		t.Fatal(err)
	}
}

func TestAMemberKitNamingAFileIsRejected(t *testing.T) {
	directory := t.TempDir()
	spec := filepath.Join(directory, "spec.yaml")
	boxtest.WriteFile(t, spec, "kind: mixin\n")
	settings := settingsFor(t, map[string]string{"kit": "registry/kit", "model": "claude-opus-5"}, directory)
	settings.Members = []config.MemberSettings{{Member: member, Kit: spec}}
	wantError(t, project.RequireSettings(settings), "billing-api: kit names a file")
}

func TestASecretsFileOutsideEverythingTheSandboxReadsIsAccepted(t *testing.T) {
	if err := project.RequireSecretOutside(config.SecretsEnv, "/secrets/box.env", []string{"/work/demo"}); err != nil {
		t.Fatal(err)
	}
}

func TestASecretsFileInTheRepositoryIsRejected(t *testing.T) {
	err := project.RequireSecretOutside(config.SecretsEnv, "/work/demo/.env", []string{"/work/demo"})
	wantError(t, err, "can read everything there")
}

func TestASecretsFileInAMountIsRejected(t *testing.T) {
	err := project.RequireSecretOutside(config.TokenFileEnv, "/cache/token", []string{"/work/demo", "/cache"})
	wantError(t, err, "can read everything there")
}

func TestASecretsFileThatIsNotSetIsNowhere(t *testing.T) {
	if err := project.RequireSecretOutside(config.SecretsEnv, "", []string{"/work/demo"}); err != nil {
		t.Fatal(err)
	}
}

func TestASymlinkIntoTheRepositoryIsRejected(t *testing.T) {
	directory := t.TempDir()
	inside := filepath.Join(directory, "repo")
	boxtest.WriteFile(t, filepath.Join(inside, "box.env"), "A=1\n")
	link := filepath.Join(directory, "link.env")
	boxtest.Symlink(t, filepath.Join(inside, "box.env"), link)
	wantError(t, project.RequireSecretOutside(config.SecretsEnv, link, []string{inside}), "can read everything there")
}

func TestMissingBinariesIsEmptyWhenPathHasEveryOne(t *testing.T) {
	boxtest.StubBinaries(t)
	if got := project.MissingBinaries(config.RequiredBinaries); len(got) != 0 {
		t.Fatalf("missing %v", got)
	}
}

func TestMissingBinariesNamesWhatPathLacks(t *testing.T) {
	got := project.MissingBinaries([]string{"git", "definitely-not-a-binary-on-this-machine"})
	if len(got) != 1 || got[0] != "definitely-not-a-binary-on-this-machine" {
		t.Fatalf("missing %v", got)
	}
}

func TestRequireBinariesAcceptsAMachineThatHasThemAll(t *testing.T) {
	boxtest.StubBinaries(t)
	if err := project.RequireBinaries(); err != nil {
		t.Fatal(err)
	}
}

func TestRequireBinariesNamesEveryCommandThatIsMissing(t *testing.T) {
	boxtest.EmptyPath(t)
	err := project.RequireBinaries()
	wantError(t, err, "sbx, git, claude")
	wantError(t, err, "not on PATH")
}

// diagnoseReport renders diagnose JSON the way sbx prints it, holding the given checks.
func diagnoseReport(t *testing.T, checks ...map[string]string) string {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{"version": "1.0", "checks": checks})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

// versionCheck builds the one diagnose check that compares the CLI with its daemon.
func versionCheck(status, message string) map[string]string {
	return map[string]string{"name": config.VersionMatchCheck, "status": status, "message": message}
}

// answering builds a runner that answers one command with canned output and refuses any other.
func answering(t *testing.T, expected []string, stdout string) *boxtest.Runner {
	t.Helper()
	return &boxtest.Runner{Answer: func(arguments []string) system.Result {
		if strings.Join(arguments, " ") != strings.Join(expected, " ") {
			t.Fatalf("ran %v, expected %v", arguments, expected)
		}
		return system.Result{Stdout: stdout}
	}}
}

func TestTheSbxVersionCommandAsksSbxForItsVersion(t *testing.T) {
	if got := strings.Join(project.SbxVersionCommand(), " "); got != "sbx version" {
		t.Fatalf("asks %q", got)
	}
}

func TestParseSbxVersionReadsTheReleaseOutOfThePrintedLine(t *testing.T) {
	printed := "sbx version: v0.38.0 c022b14634c4bea846ca12870d1d5e97d5868b54\n"
	if got := project.ToVersion(project.ParseSbxVersion(printed)); got != "0.38.0" {
		t.Fatalf("read %q", got)
	}
}

func TestParseSbxVersionReadsAReleasePrintedWithoutAV(t *testing.T) {
	if got := project.ToVersion(project.ParseSbxVersion("sbx version: 1.2.3\n")); got != "1.2.3" {
		t.Fatalf("read %q", got)
	}
}

func TestParseSbxVersionReturnsNothingForALineHoldingNoRelease(t *testing.T) {
	for _, printed := range []string{"", "sbx version: unknown\n"} {
		if got := project.ParseSbxVersion(printed); len(got) != 0 {
			t.Errorf("%q read as %v", printed, got)
		}
	}
}

func TestRequireSupportedSbxAcceptsTheReleaseBoxIsWrittenAgainst(t *testing.T) {
	boxtest.StubBinaries(t)
	runner := answering(t, project.SbxVersionCommand(), "sbx version: v0.38.0 c022b146\n")
	if err := project.RequireSupportedSbx(runner); err != nil {
		t.Fatal(err)
	}
}

func TestRequireSupportedSbxAcceptsANewerRelease(t *testing.T) {
	boxtest.StubBinaries(t)
	runner := answering(t, project.SbxVersionCommand(), "sbx version: v0.39.1 c022b146\n")
	if err := project.RequireSupportedSbx(runner); err != nil {
		t.Fatal(err)
	}
}

func TestRequireSupportedSbxRefusesAnOlderRelease(t *testing.T) {
	boxtest.StubBinaries(t)
	runner := answering(t, project.SbxVersionCommand(), "sbx version: v0.37.9 c022b146\n")
	err := project.RequireSupportedSbx(runner)
	wantError(t, err, "this sbx is v0.37.9")
	wantError(t, err, "v0.38.0 or newer")
}

func TestRequireSupportedSbxRefusesAReleaseOlderInItsFirstNumber(t *testing.T) {
	boxtest.StubBinaries(t)
	runner := answering(t, project.SbxVersionCommand(), "sbx version: v0.9.0 c022b146\n")
	wantError(t, project.RequireSupportedSbx(runner), "this sbx is v0.9.0")
}

func TestRequireSupportedSbxAcceptsAVersionLineItCannotRead(t *testing.T) {
	boxtest.StubBinaries(t)
	if err := project.RequireSupportedSbx(answering(t, project.SbxVersionCommand(), "")); err != nil {
		t.Fatal(err)
	}
}

func TestRequireSupportedSbxSkipsAMachineWithoutSbx(t *testing.T) {
	boxtest.EmptyPath(t)
	runner := &boxtest.Runner{Answer: func([]string) system.Result {
		t.Fatal("a machine without sbx must not be asked for its version")
		return system.Result{}
	}}
	if err := project.RequireSupportedSbx(runner); err != nil {
		t.Fatal(err)
	}
}

func TestTheDiagnoseCommandAsksForJSON(t *testing.T) {
	if got := strings.Join(project.DiagnoseCommand(), " "); got != "sbx diagnose -o json" {
		t.Fatalf("asks %q", got)
	}
}

func TestParseVersionMismatchIsQuietWhenTheVersionsAgree(t *testing.T) {
	if got := project.ParseVersionMismatch(diagnoseReport(t, versionCheck("pass", "v0.38.0"))); got != "" {
		t.Fatalf("said %q", got)
	}
}

func TestParseVersionMismatchReturnsTheFailedCheckMessage(t *testing.T) {
	report := diagnoseReport(t, versionCheck("fail", "client v0.38.0, daemon v0.37.0"))
	if got := project.ParseVersionMismatch(report); got != "client v0.38.0, daemon v0.37.0" {
		t.Fatalf("said %q", got)
	}
}

func TestParseVersionMismatchSaysSomethingWhenTheCheckSaysNothing(t *testing.T) {
	report := diagnoseReport(t, versionCheck("fail", ""))
	if got := project.ParseVersionMismatch(report); got != "sbx did not say which versions" {
		t.Fatalf("said %q", got)
	}
}

func TestParseVersionMismatchIsQuietWhenTheCheckIsAbsent(t *testing.T) {
	report := diagnoseReport(t, map[string]string{"name": "Daemon", "status": "fail", "message": "not running"})
	if got := project.ParseVersionMismatch(report); got != "" {
		t.Fatalf("said %q", got)
	}
}

func TestParseVersionMismatchIsQuietOnOutputThatIsNotDiagnoseJSON(t *testing.T) {
	for _, printed := range []string{"", "not json", `{"summary": {}}`, `{"checks": ["not a check"]}`} {
		if got := project.ParseVersionMismatch(printed); got != "" {
			t.Errorf("%q said %q", printed, got)
		}
	}
}

func TestRequireMatchingVersionsAcceptsAgreeingVersions(t *testing.T) {
	boxtest.StubBinaries(t)
	runner := answering(t, project.DiagnoseCommand(), diagnoseReport(t, versionCheck("pass", "v0.38.0")))
	if err := project.RequireMatchingVersions(runner); err != nil {
		t.Fatal(err)
	}
}

func TestRequireMatchingVersionsRefusesADaemonFromAnotherRelease(t *testing.T) {
	boxtest.StubBinaries(t)
	report := diagnoseReport(t, versionCheck("fail", "client v0.38.0, daemon v0.37.0"))
	err := project.RequireMatchingVersions(answering(t, project.DiagnoseCommand(), report))
	wantError(t, err, "daemon v0.37.0")
	wantError(t, err, "sbx daemon restart")
}

func TestRequireMatchingVersionsAcceptsADiagnoseThatAnsweredNothing(t *testing.T) {
	boxtest.StubBinaries(t)
	if err := project.RequireMatchingVersions(answering(t, project.DiagnoseCommand(), "")); err != nil {
		t.Fatal(err)
	}
}

func TestRequireMatchingVersionsSkipsAMachineWithoutSbx(t *testing.T) {
	boxtest.EmptyPath(t)
	runner := &boxtest.Runner{Answer: func([]string) system.Result {
		t.Fatal("a machine without sbx must not be diagnosed")
		return system.Result{}
	}}
	if err := project.RequireMatchingVersions(runner); err != nil {
		t.Fatal(err)
	}
}

func TestRequireSettingsAcceptsACompleteConfig(t *testing.T) {
	settings := config.Config{
		Name: "demo", Memory: "8g", CPUs: "2", Model: "claude-opus-5", Kit: "registry/kit",
		Template: "frlg-sandbox:1", Mounts: []string{"/cache:ro"},
	}
	if err := project.RequireSettings(settings); err != nil {
		t.Fatal(err)
	}
}

func TestOwnMountsAnswerTheDeclarationOfTheDirectoryBoxRunsIn(t *testing.T) {
	directory := t.TempDir()
	boxtest.WriteFile(t, filepath.Join(directory, config.MountsFile), `{"cache": "/cache"}`)
	provided, err := config.ReadPathsFile(filepath.Join(directory, config.MountsFile))
	if err != nil {
		t.Fatal(err)
	}
	required := config.Pairs{{Name: "cache", Value: "the build cache"}}
	ordered, err := config.OrderMounts(required, provided)
	if err != nil || len(ordered) != 1 || ordered[0] != "/cache" {
		t.Fatalf("ordered %v, %v", ordered, err)
	}
}

func TestOwnMountsAreEmptyWithoutADeclaration(t *testing.T) {
	directory := t.TempDir()
	provided, err := config.ReadPathsFile(filepath.Join(directory, config.MountsFile))
	if err != nil {
		t.Fatal(err)
	}
	ordered, err := config.OrderMounts(nil, provided)
	if err != nil || len(ordered) != 0 {
		t.Fatalf("ordered %v, %v", ordered, err)
	}
}
