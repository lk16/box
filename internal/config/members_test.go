package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/lk16/box/internal/config"
)

// member is the one member a group declares in these tests.
var member = config.Member{
	Name:      "billing-api",
	Branch:    "develop",
	GitOrigin: "https://example.com/billing-api.git",
	Path:      "../billing-api",
}

// declared is how that member reads in a group's config file.
var declared = map[string]any{
	"billing-api": map[string]string{"branch": "develop", "git_origin": "https://example.com/billing-api.git"},
}

// placedPairs is where this machine says that member sits.
func placedPairs() config.Pairs {
	return pairs("billing-api", "../billing-api")
}

// missingMember builds the same member as one this machine does not have at all.
func missingMember() config.Member {
	absent := member
	absent.Path, absent.Missing = "", true
	return absent
}

// memberAt builds the same member kept somewhere else on this machine.
func memberAt(path string) config.Member {
	moved := member
	moved.Path = path
	return moved
}

// readMembers reads a declaration against the paths a repos file gives, or fails the test.
func readMembers(t *testing.T, declaration any, paths config.Pairs) []config.Member {
	t.Helper()
	members, err := config.ToMembers(raw(declaration), paths)
	if err != nil {
		t.Fatal(err)
	}
	return members
}

func TestAMemberIsDeclaredByNameAndPlacedByThisMachine(t *testing.T) {
	got := readMembers(t, declared, placedPairs())
	if !slices.Equal(got, []config.Member{member}) {
		t.Fatalf("read %v", got)
	}
}

func TestMembersKeepTheOrderTheyWereDeclaredIn(t *testing.T) {
	declaration := map[string]string{"branch": "main", "git_origin": "https://example.com/b.git"}
	object := json.RawMessage(`{"b": {"branch": "main", "git_origin": "https://example.com/b.git"},
		"a": {"branch": "main", "git_origin": "https://example.com/a.git"}}`)
	_ = declaration
	members, err := config.ToMembers(object, nil)
	if err != nil || members[0].Name != "b" || members[1].Name != "a" {
		t.Fatalf("read %v, %v", members, err)
	}
}

func TestAMemberThisMachineHasNotPlacedHasNoPathYet(t *testing.T) {
	if got := readMembers(t, declared, nil)[0].Path; got != "" {
		t.Fatalf("placed at %q", got)
	}
}

func TestAMemberWithoutABranchIsRejected(t *testing.T) {
	declaration := map[string]any{"billing-api": map[string]string{"git_origin": "https://example.com/b.git"}}
	_, err := config.ToMembers(raw(declaration), nil)
	wantError(t, err, "gives billing-api no branch")
}

func TestAMemberWithAnEmptyGitOriginIsRejected(t *testing.T) {
	declaration := map[string]any{"billing-api": map[string]string{"branch": "develop", "git_origin": ""}}
	_, err := config.ToMembers(raw(declaration), nil)
	wantError(t, err, "gives billing-api no git_origin")
}

func TestAMemberWithoutANameIsRejected(t *testing.T) {
	declaration := map[string]any{"": map[string]string{"branch": "main", "git_origin": "https://example.com/a.git"}}
	_, err := config.ToMembers(raw(declaration), nil)
	wantError(t, err, "no name")
}

func TestAMemberGivenAsAPathAndABranchIsRejected(t *testing.T) {
	_, err := config.ToMembers(raw(map[string]any{"../billing-api": "develop"}), nil)
	wantError(t, err, "gives ../billing-api text, where an object")
}

func TestAMemberWithAKeyBoxDoesNotKnowIsRejected(t *testing.T) {
	declaration := map[string]any{"billing-api": map[string]string{
		"branch": "develop", "git_origin": "https://example.com/b.git", "base": "main",
	}}
	_, err := config.ToMembers(raw(declaration), nil)
	wantError(t, err, "unknown keys: base")
}

func TestReposMustBeAnObject(t *testing.T) {
	_, err := config.ToMembers(raw([]string{"billing-api"}), nil)
	wantError(t, err, "must be a JSON object")
}

func TestAMemberWithNoPathNamesTheReposFileAndTheOriginToClone(t *testing.T) {
	directory := t.TempDir()
	_, err := config.ReadRepos(directory, raw(declared))
	wantError(t, err, filepath.Join(directory, config.ReposFile)+" is missing a path for:\n  billing-api: "+member.GitOrigin)
}

func TestAMemberWithAnEmptyPathIsRejected(t *testing.T) {
	directory := t.TempDir()
	writeBoxFile(t, directory, config.ReposFile, map[string]string{"billing-api": ""})
	_, err := config.ReadRepos(directory, raw(declared))
	wantError(t, err, "is missing a path for")
}

func TestAMemberAnsweredWithANullIsOneThisMachineDoesNotHave(t *testing.T) {
	directory := t.TempDir()
	writeBoxFile(t, directory, config.ReposFile, map[string]any{"billing-api": nil})
	members, err := config.ReadRepos(directory, raw(declared))
	if err != nil || !slices.Equal(members, []config.Member{missingMember()}) {
		t.Fatalf("read %v, %v", members, err)
	}
}

func TestANullForAMemberTheConfigDoesNotDeclareIsRejected(t *testing.T) {
	directory := t.TempDir()
	writeBoxFile(t, directory, config.ReposFile, map[string]any{"billing-api": "../billing-api", "typo": nil})
	_, err := config.ReadRepos(directory, raw(declared))
	wantError(t, err, "does not declare: typo")
}

func TestTheRefusalForAnUnplacedMemberOffersTheNullForOneThisMachineLacks(t *testing.T) {
	_, err := config.ReadRepos(t.TempDir(), raw(declared))
	wantError(t, err, "Give a member null instead of a path")
}

func TestAMemberThisMachineDoesNotHaveIsLeftOutOfTheRunItIsDeclaredIn(t *testing.T) {
	built := configWithMember(t, config.MemberSettings{Member: missingMember()})
	if len(built.Repos) != 0 {
		t.Fatalf("the run works on %v", built.Repos)
	}
	if !slices.Equal(built.MissingRepos(), []config.Member{missingMember()}) {
		t.Fatalf("missing %v", built.MissingRepos())
	}
}

func TestAMemberThisMachineDoesNotHaveBringsNothingOfTheGroupsOwn(t *testing.T) {
	directory := t.TempDir()
	// A path of nothing would resolve to the group itself, whose own template a member may not set.
	writeConfig(t, directory, map[string]string{"template": "frlg-sandbox:1"})
	settings, err := config.ReadMemberSettings(directory, missingMember())
	if err != nil || settings.Kit != "" || settings.PromptFile != "" || len(settings.Mounts) != 0 {
		t.Fatalf("read %v, %v", settings, err)
	}
}

func TestAPathForAMemberTheConfigDoesNotDeclareIsRejected(t *testing.T) {
	directory := t.TempDir()
	writeBoxFile(t, directory, config.ReposFile, map[string]string{"billing-api": "../billing-api", "typo": "../typo"})
	_, err := config.ReadRepos(directory, raw(declared))
	wantError(t, err, "does not declare: typo")
}

func TestTwoMembersWithOneOriginAreRejected(t *testing.T) {
	directory := t.TempDir()
	declaration := json.RawMessage(`{"api": {"branch": "main", "git_origin": "git@example.com:team/api.git"},
		"api-worktree": {"branch": "main", "git_origin": "https://example.com/team/api"}}`)
	writeBoxFile(t, directory, config.ReposFile, map[string]string{"api": "../api", "api-worktree": "../api-worktree"})
	_, err := config.ReadRepos(directory, declaration)
	wantError(t, err, "gives api and api-worktree one git_origin")
}

func TestASingleProjectNeedsNoReposFile(t *testing.T) {
	members, err := config.ReadRepos(t.TempDir(), nil)
	if err != nil || len(members) != 0 {
		t.Fatalf("read %v, %v", members, err)
	}
}

func TestOriginsSpelledForSSHScpAndHTTPSAgree(t *testing.T) {
	spellings := []string{
		"git@Example.com:team/api.git",
		"ssh://git@example.com:22/team/api.git",
		"https://example.com/team/api/",
		"https://user@example.com/team/api.git",
	}
	for _, spelling := range spellings {
		if got := config.NormalizeOrigin(spelling); got != "example.com/team/api" {
			t.Errorf("%s reduced to %q", spelling, got)
		}
	}
}

func TestOriginsOfTwoRepositoriesDiffer(t *testing.T) {
	if config.NormalizeOrigin("git@example.com:team/api.git") == config.NormalizeOrigin("git@example.com:team/web") {
		t.Fatal("two repositories reduced to one origin")
	}
}

func TestAnOriginKeepsTheCaseOfItsPath(t *testing.T) {
	if got := config.NormalizeOrigin("https://example.com/Team/api"); got != "example.com/Team/api" {
		t.Fatalf("reduced to %q", got)
	}
}

func TestALocalOriginIsComparedAsThePathItIs(t *testing.T) {
	if got := config.NormalizeOrigin("/srv/git/api.git"); got != "/srv/git/api" {
		t.Fatalf("reduced to %q", got)
	}
}

func TestAMemberSitsWhereTheWorkingDirectorySaysItDoes(t *testing.T) {
	directory := t.TempDir()
	want := config.Resolve(filepath.Join(directory, "billing-api"))
	if got := config.MemberPath(filepath.Join(directory, "boxes"), member); got != want {
		t.Fatalf("sits at %q, want %q", got, want)
	}
}

func TestAMemberPathExpandsALeadingTilde(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("HOME", directory)
	want := config.Resolve(filepath.Join(directory, "billing-api"))
	if got := config.MemberPath(directory, memberAt("~/billing-api")); got != want {
		t.Fatalf("sits at %q, want %q", got, want)
	}
}

// makeMemberDirectory creates a directory beside the group, the way a member repository sits on a host.
func makeMemberDirectory(t *testing.T, directory, name string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// memberSettings gives the member beside a group a box config of its own, and reads what it adds.
func memberSettings(t *testing.T, directory string, given any) (config.MemberSettings, error) {
	t.Helper()
	writeConfig(t, makeMemberDirectory(t, directory, "billing-api"), given)
	return config.ReadMemberSettings(filepath.Join(directory, "boxes"), member)
}

func TestAMemberWithoutABoxDirectoryAddsNothing(t *testing.T) {
	directory := t.TempDir()
	makeMemberDirectory(t, directory, "billing-api")
	settings, err := config.ReadMemberSettings(filepath.Join(directory, "boxes"), member)
	if err != nil || settings.Kit != "" || settings.PromptFile != "" || len(settings.Mounts) != 0 {
		t.Fatalf("read %v, %v", settings, err)
	}
}

func TestAMemberMountWithNoPathNamesTheMember(t *testing.T) {
	_, err := memberSettings(t, t.TempDir(), map[string]any{
		config.RequiredMounts: map[string]string{"go": "the Go toolchain"},
	})
	wantError(t, err, "billing-api: go")
}

func TestAMountAMemberNeverDeclaredNamesTheMember(t *testing.T) {
	directory := t.TempDir()
	path := makeMemberDirectory(t, directory, "billing-api")
	writeBoxFile(t, path, config.MountsFile, map[string]string{"go": "/usr/local/go"})
	writeConfig(t, path, map[string]any{})
	_, err := config.ReadMemberSettings(filepath.Join(directory, "boxes"), member)
	wantError(t, err, "billing-api: go")
}

func TestAMembersKitIsResolvedAgainstTheMember(t *testing.T) {
	directory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(directory, "billing-api", config.KitDir), 0o755); err != nil {
		t.Fatal(err)
	}
	settings, err := memberSettings(t, directory, map[string]any{"kit": config.KitDir})
	want := filepath.Join(config.Resolve(filepath.Join(directory, "billing-api")), config.KitDir)
	if err != nil || settings.Kit != want {
		t.Fatalf("kit is %q, want %q (%v)", settings.Kit, want, err)
	}
}

func TestAMembersKitThatIsNotOnDiskIsLeftToSbx(t *testing.T) {
	settings, err := memberSettings(t, t.TempDir(), map[string]any{"kit": "registry/kit"})
	if err != nil || settings.Kit != "registry/kit" {
		t.Fatalf("kit is %q (%v)", settings.Kit, err)
	}
}

func TestAMemberThatBringsItsOwnImageIsRejected(t *testing.T) {
	_, err := memberSettings(t, t.TempDir(), map[string]any{"template": "frlg-sandbox:1"})
	wantError(t, err, "a sandbox runs one image")
}

func TestAMembersPromptFileIsResolvedAgainstTheMember(t *testing.T) {
	directory := t.TempDir()
	settings, err := memberSettings(t, directory, map[string]any{"prompt_file": "docs/agent.md"})
	want := filepath.Join(config.Resolve(filepath.Join(directory, "billing-api")), "docs", "agent.md")
	if err != nil || settings.PromptFile != want {
		t.Fatalf("prompt file is %q, want %q (%v)", settings.PromptFile, want, err)
	}
}

func TestWhatAMemberSaysAboutGroupsOfItsOwnIsIgnored(t *testing.T) {
	given := map[string]any{
		config.Repos:       map[string]any{"other": map[string]string{"branch": "main", "git_origin": "x"}},
		config.MCP:         []string{"postgres"},
		config.SecretHosts: map[string]string{},
	}
	settings, err := memberSettings(t, t.TempDir(), given)
	if err != nil || settings.Kit != "" || settings.PromptFile != "" || len(settings.Mounts) != 0 {
		t.Fatalf("read %v, %v", settings, err)
	}
}

func TestAnUnknownKeyInAMembersConfigNamesThatFile(t *testing.T) {
	directory := t.TempDir()
	_, err := memberSettings(t, directory, map[string]any{"nope": 1})
	wantError(t, err, filepath.Join(config.Resolve(filepath.Join(directory, "billing-api")), config.ConfigFile))
}

func TestTheCreateCommandPassesTheGroupsKitAndThenTheMembers(t *testing.T) {
	settings := config.MemberSettings{Member: member, Kit: "/kits/billing"}
	built := configWithMember(t, settings)
	if got := built.Kits(); !slices.Equal(got, []string{"registry/kit", "/kits/billing"}) {
		t.Fatalf("kits are %v", got)
	}
}

func TestAKitTheGroupAlreadyHasIsPassedOnce(t *testing.T) {
	settings := config.MemberSettings{Member: member, Kit: "registry/kit"}
	if got := configWithMember(t, settings).Kits(); !slices.Equal(got, []string{"registry/kit"}) {
		t.Fatalf("kits are %v", got)
	}
}

// configWithMember builds the config of a group whose one member brings settings of its own.
func configWithMember(t *testing.T, settings config.MemberSettings) config.Config {
	t.Helper()
	given := config.Merge(values("kit", "registry/kit", "model", "claude-opus-5"), config.NewValues())
	built, err := config.Build(given, nil, []config.MemberSettings{settings}, "/work/boxes")
	if err != nil {
		t.Fatal(err)
	}
	return built
}

func TestAnOriginIsTheHostAfterTheLastAt(t *testing.T) {
	// A user, a password and a port all sit before the host, and any of them may hold an @.
	for url, want := range map[string]string{
		"https://a@b@example.com/team/api.git":   "example.com/team/api",
		"ssh://user:pw@host@example.com/t/a.git": "example.com/t/a",
	} {
		if got := config.NormalizeOrigin(url); got != want {
			t.Errorf("%s reduced to %q, want %q", url, got, want)
		}
	}
}
