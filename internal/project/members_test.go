package project_test

import (
	"path/filepath"
	"testing"

	"github.com/lk16/box/internal/boxtest"
	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/project"
)

func TestAMemberBesideTheRepositoryIsAccepted(t *testing.T) {
	boxtest.Isolate(t)
	directory := t.TempDir()
	makeMember(t, directory, "billing-api")
	boxes := boxtest.MakeRepository(t, filepath.Join(directory, "boxes"))
	if err := project.RequireMembers(real, groupConfig(t, boxes, member), projectAt(boxes)); err != nil {
		t.Fatal(err)
	}
}

func TestAMemberWhoseOriginIsSpelledAnotherWayIsAccepted(t *testing.T) {
	boxtest.Isolate(t)
	directory := t.TempDir()
	path := makeMember(t, directory, "billing-api")
	boxtest.Git(t, path, "remote", "set-url", "origin", "git@example.com:billing-api.git")
	boxes := boxtest.MakeRepository(t, filepath.Join(directory, "boxes"))
	if err := project.RequireMembers(real, groupConfig(t, boxes, member), projectAt(boxes)); err != nil {
		t.Fatal(err)
	}
}

func TestAMemberThatIsNotThereIsRejectedWithItsFullPath(t *testing.T) {
	directory := t.TempDir()
	boxes := boxtest.MakeRepository(t, filepath.Join(directory, "boxes"))
	settings := groupConfig(t, boxes, at("../nothing"))
	want := config.Resolve(filepath.Join(directory, "nothing")) + ", which is no directory on this machine"
	wantError(t, project.RequireMembers(real, settings, projectAt(boxes)), want)
}

func TestAMemberThatIsNotARepositoryIsRejected(t *testing.T) {
	directory := t.TempDir()
	boxtest.WriteFile(t, filepath.Join(directory, "billing-api", "keep"), "")
	boxes := boxtest.MakeRepository(t, filepath.Join(directory, "boxes"))
	settings := groupConfig(t, boxes, member)
	wantError(t, project.RequireMembers(real, settings, projectAt(boxes)), "not a git repository")
}

func TestAMemberWithoutAnOriginRemoteIsRejected(t *testing.T) {
	directory := t.TempDir()
	boxtest.MakeRepository(t, filepath.Join(directory, "billing-api"))
	boxes := boxtest.MakeRepository(t, filepath.Join(directory, "boxes"))
	settings := groupConfig(t, boxes, member)
	wantError(t, project.RequireMembers(real, settings, projectAt(boxes)), "no origin remote")
}

func TestAMemberWhoseOriginIsAnotherRepositoryIsRejected(t *testing.T) {
	directory := t.TempDir()
	path := makeMember(t, directory, "billing-api")
	boxtest.Git(t, path, "remote", "set-url", "origin", "https://example.com/other.git")
	boxes := boxtest.MakeRepository(t, filepath.Join(directory, "boxes"))
	settings := groupConfig(t, boxes, member)
	want := "sits at " + config.Resolve(path) + ", whose origin is https://example.com/other.git"
	wantError(t, project.RequireMembers(real, settings, projectAt(boxes)), want)
}

func TestAMemberInsideTheRepositoryBoxRunsInIsRejected(t *testing.T) {
	boxtest.Isolate(t)
	boxes := boxtest.MakeRepository(t, filepath.Join(t.TempDir(), "boxes"))
	makeMember(t, boxes, "billing-api")
	settings := groupConfig(t, boxes, at("./billing-api"))
	wantError(t, project.RequireMembers(real, settings, projectAt(boxes)), "would sit inside")
}

func TestAMountHoldingAMemberIsRejected(t *testing.T) {
	boxtest.Isolate(t)
	directory := t.TempDir()
	makeMember(t, directory, "billing-api")
	boxes := boxtest.MakeRepository(t, filepath.Join(directory, "boxes"))
	settings, err := config.Build(config.NewValues(), []string{directory},
		[]config.MemberSettings{{Member: member}}, boxes)
	if err != nil {
		t.Fatal(err)
	}
	wantError(t, project.RequireMembers(real, settings, projectAt(boxes)), "would hand over whole")
}

func TestACommittableMountsFileInAMemberIsRejected(t *testing.T) {
	boxtest.Isolate(t)
	directory := t.TempDir()
	path := makeMember(t, directory, "billing-api")
	boxtest.WriteFile(t, filepath.Join(path, config.MountsFile), `{"go": "/usr/local/go"}`)
	boxes := boxtest.MakeRepository(t, filepath.Join(directory, "boxes"))
	settings := groupConfig(t, boxes, member)
	want := config.Resolve(path) + ": " + config.MountsFile
	wantError(t, project.RequireMembers(real, settings, projectAt(boxes)), want)
}

func TestRequireRefusesAMachineWithoutTheCommandsBoxNeeds(t *testing.T) {
	boxtest.EmptyPath(t)
	directory := t.TempDir()
	wantError(t, project.Require(real, config.Config{}, projectAt(directory), "/secrets/token"), "not on PATH")
}

func TestRequireSendsAProjectWithNoConfigToGen(t *testing.T) {
	boxtest.StubBinaries(t)
	directory := t.TempDir()
	wantError(t, project.Require(real, config.Config{}, projectAt(directory), "/secrets/token"), "Run box gen")
}

// secretsFor builds the settings of a project declaring one secret, and nothing else.
func secretsFor(t *testing.T, declared bool) config.Config {
	t.Helper()
	if !declared {
		return config.Config{}
	}
	return config.Config{SecretHosts: []config.Secret{{Name: "GITLAB_TOKEN", Host: "gitlab.com"}}}
}

func TestSecretsAreNotLookedForWhenTheProjectDeclaresNone(t *testing.T) {
	boxtest.Unset(t, config.SecretsEnv)
	if err := project.RequireSecrets(secretsFor(t, false), projectAt(t.TempDir()), ""); err != nil {
		t.Fatal(err)
	}
}

func TestADeclaredSecretWithNoValuesFileIsRejected(t *testing.T) {
	boxtest.Unset(t, config.SecretsEnv)
	err := project.RequireSecrets(secretsFor(t, true), projectAt(t.TempDir()), "")
	wantError(t, err, config.SecretsEnv)
}

func TestAValuesFileInsideTheRepositoryIsRejected(t *testing.T) {
	directory := t.TempDir()
	inside := filepath.Join(directory, "box.env")
	boxtest.WriteFile(t, inside, "GITLAB_TOKEN=glpat-abc\n")
	t.Setenv(config.SecretsEnv, inside)
	err := project.RequireSecrets(secretsFor(t, true), projectAt(directory), "")
	wantError(t, err, "can read everything there")
}

func TestAValuesFileOutsideEverythingTheSandboxReadsIsAccepted(t *testing.T) {
	directory := t.TempDir()
	outside := filepath.Join(t.TempDir(), "box.env")
	boxtest.WriteFile(t, outside, "GITLAB_TOKEN=glpat-abc\n")
	t.Setenv(config.SecretsEnv, outside)
	if err := project.RequireSecrets(secretsFor(t, true), projectAt(directory), ""); err != nil {
		t.Fatal(err)
	}
}

func TestAValuesFileMissingADeclaredNameIsRejected(t *testing.T) {
	directory := t.TempDir()
	outside := filepath.Join(t.TempDir(), "box.env")
	boxtest.WriteFile(t, outside, "OTHER=1\n")
	t.Setenv(config.SecretsEnv, outside)
	err := project.RequireSecrets(secretsFor(t, true), projectAt(directory), "")
	wantError(t, err, "no value for GITLAB_TOKEN")
}

func TestATokenFileInsideTheRepositoryIsRejectedEvenWithNoDeclaredSecrets(t *testing.T) {
	directory := t.TempDir()
	inside := filepath.Join(directory, "token")
	boxtest.WriteFile(t, inside, "sk-ant-secret\n")
	err := project.RequireSecrets(secretsFor(t, false), projectAt(directory), inside)
	wantError(t, err, "can read everything there")
}

func TestTheLocalPathsBoxRefusesToCommitAreTheMountsAndTheReposFiles(t *testing.T) {
	names := project.LocalPathNames()
	if len(names) != 2 || names[0] != config.MountsFile || names[1] != config.ReposFile {
		t.Fatalf("the local paths are %v", names)
	}
}
