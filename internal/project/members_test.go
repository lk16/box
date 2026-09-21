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
