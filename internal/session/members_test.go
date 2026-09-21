package session_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lk16/box/internal/boxtest"
	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/project"
	"github.com/lk16/box/internal/session"
	"github.com/lk16/box/internal/system"
)

// makeClonedMember creates a repository box can really fetch from, by cloning one next to it.
func makeClonedMember(t *testing.T, directory, name string) string {
	t.Helper()
	origin := boxtest.MakeRepository(t, filepath.Join(directory, name+"-origin"))
	boxtest.Git(t, origin, "branch", "-M", "develop")
	path := filepath.Join(directory, name)
	if err := exec.Command("git", "clone", "-q", origin, path).Run(); err != nil {
		t.Fatal(err)
	}
	return path
}

// bundleAt builds what a fetched and packed member hands to the commands that clone it.
func bundleAt(directory string) session.Bundle {
	return session.Bundle{
		Member:     member,
		Path:       "/work/billing-api",
		BundleFile: filepath.Join(directory, "1-billing-api.bundle"),
	}
}

func TestBundleMemberPacksWhatTheCloneIsMadeFrom(t *testing.T) {
	boxtest.Isolate(t)
	directory := t.TempDir()
	makeClonedMember(t, directory, "billing-api")
	boxes := boxtest.MakeRepository(t, filepath.Join(directory, "boxes"))
	bundles, err := realSession(t).BundleMembers(groupConfig(t, boxes, member), projectAt(boxes), directory)
	if err != nil {
		t.Fatal(err)
	}
	if bundles[0].Path != memberPathUnder(directory) {
		t.Fatalf("bundled %q", bundles[0].Path)
	}
	if _, err := os.Stat(bundles[0].BundleFile); err != nil {
		t.Fatalf("no bundle was written: %v", err)
	}
	heads := boxtest.Git(t, directory, "bundle", "list-heads", bundles[0].BundleFile)
	if !strings.Contains(heads, "refs/remotes/origin/develop") {
		t.Fatalf("the bundle holds %q", heads)
	}
}

func TestAMemberWhoseOriginLacksTheBaseIsRejected(t *testing.T) {
	boxtest.Isolate(t)
	directory := t.TempDir()
	makeClonedMember(t, directory, "billing-api")
	boxes := boxtest.MakeRepository(t, filepath.Join(directory, "boxes"))
	wrong := member
	wrong.Branch = "nope"
	_, err := realSession(t).BundleMembers(groupConfig(t, boxes, wrong), projectAt(boxes), directory)
	if err == nil || !strings.Contains(err.Error(), "no branch nope on origin") {
		t.Fatalf("the refusal was %v", err)
	}
}

func TestAMemberBoxCannotFetchStopsTheRun(t *testing.T) {
	boxtest.Isolate(t)
	directory := t.TempDir()
	path := makeClonedMember(t, directory, "billing-api")
	boxtest.Git(t, path, "remote", "set-url", "origin", filepath.Join(directory, "gone"))
	boxes := boxtest.MakeRepository(t, filepath.Join(directory, "boxes"))
	_, err := realSession(t).BundleMembers(groupConfig(t, boxes, member), projectAt(boxes), directory)
	if err == nil || !strings.Contains(err.Error(), "git fetch origin failed") {
		t.Fatalf("the refusal was %v", err)
	}
}

func TestNothingIsFetchedWithoutMembers(t *testing.T) {
	directory := t.TempDir()
	bundles, err := realSession(t).BundleMembers(fullConfig(), projectAt(directory), directory)
	if err != nil || len(bundles) != 0 {
		t.Fatalf("bundled %v, %v", bundles, err)
	}
}

func TestTheCloneCommandsCopyTheBundleAndBuildACloneFromIt(t *testing.T) {
	directory := t.TempDir()
	commands := session.CloneCommands(bundleAt(directory), "demo-1")
	inside := "/tmp/1-billing-api.bundle"
	want := []([]string){
		{"sbx", "cp", filepath.Join(directory, "1-billing-api.bundle"), "demo-1:" + inside},
		{"sbx", "exec", "-u", "root", "demo-1", "mkdir", "-p", "/work/billing-api"},
	}
	for index, expected := range want {
		if !slices.Equal(commands[index], expected) {
			t.Fatalf("command %d is %v", index, commands[index])
		}
	}
	if !slices.Equal(tail(commands[2], 2), []string{"agent:agent", "/work/billing-api"}) {
		t.Fatalf("the chown is %v", commands[2])
	}
	if !slices.Equal(commands[3], []string{"sbx", "exec", "demo-1", "git", "init", "-q", "/work/billing-api"}) {
		t.Fatalf("the init is %v", commands[3])
	}
	if !slices.Equal(tail(commands[4], 2), []string{"origin", "https://example.com/billing-api.git"}) {
		t.Fatalf("the remote is %v", commands[4])
	}
	if !slices.Equal(tail(commands[5], 2), []string{inside, "refs/remotes/origin/*:refs/remotes/origin/*"}) {
		t.Fatalf("the fetch is %v", commands[5])
	}
	if !slices.Equal(tail(commands[6], 4), []string{"-c", "develop", "--track", "origin/develop"}) {
		t.Fatalf("the switch is %v", commands[6])
	}
}

func TestEveryPathReachesSbxExecAsAnArgumentOfItsOwn(t *testing.T) {
	for _, command := range session.CloneCommands(bundleAt(t.TempDir()), "demo-1") {
		for _, word := range command {
			if strings.Contains(word, " ") {
				t.Fatalf("%q reached sbx as one word holding a space", word)
			}
		}
	}
}

func TestCloneMemberSaysNoWhenAStepFails(t *testing.T) {
	run := newFixture(func(arguments []string) system.Result {
		if slices.Contains(arguments, "mkdir") {
			return system.Result{Code: 1}
		}
		return system.Result{}
	})
	if run.session.CloneMember(bundleAt(t.TempDir()), "demo-1") {
		t.Fatal("a clone that failed was reported as done")
	}
	// The steps after the one that failed are never run, since there is nothing to run them on.
	if len(run.runner.Commands) != 2 {
		t.Fatalf("ran %d commands", len(run.runner.Commands))
	}
}

func TestCloneMemberSaysYesWhenEveryStepWorks(t *testing.T) {
	run := newFixture(nil)
	if !run.session.CloneMember(bundleAt(t.TempDir()), "demo-1") {
		t.Fatal("a clone that worked was reported as failed")
	}
}

func TestFetchingAMembersWorkBundlesItInsideAndFetchesItHere(t *testing.T) {
	run := newFixture(nil)
	directory := t.TempDir()
	checkout := project.Checkout{Path: "/work/billing-api", KnownCommits: config.MemberKnown}
	if !run.session.FetchMemberWork(checkout, "demo-1", directory) {
		t.Fatal("a fetch that worked was reported as failed")
	}
	inside := "/tmp/demo-1-billing-api.bundle"
	commands := run.runner.Commands
	if !slices.Equal(commands[0][:4], []string{"sbx", "exec", "demo-1", "git"}) {
		t.Fatalf("the bundle ran as %v", commands[0])
	}
	if !slices.Equal(tail(commands[0], 4), []string{"bundle", "create", inside, "--branches"}) {
		t.Fatalf("the bundle ran as %v", commands[0])
	}
	want := []string{"sbx", "cp", "demo-1:" + inside, filepath.Join(directory, "demo-1-billing-api.bundle")}
	if !slices.Equal(commands[1], want) {
		t.Fatalf("the copy ran as %v", commands[1])
	}
	if got := commands[2][len(commands[2])-1]; got != "+refs/heads/*:"+config.SandboxRefs+"/demo-1/*" {
		t.Fatalf("the refspec is %q", got)
	}
}

func TestAMemberCountsACommitItsOriginBranchesDoNotHold(t *testing.T) {
	boxtest.Isolate(t)
	directory := t.TempDir()
	path := makeClonedMember(t, directory, "billing-api")
	work := boxtest.CommitFile(t, path, "one.txt", "Add one")
	checkout := project.Checkout{Path: path, KnownCommits: config.MemberKnown}
	run := realSession(t)
	if got := run.CountNewCommits(checkout, work); got != "1" {
		t.Fatalf("counted %q", got)
	}
	if got := run.NewCommitSubjects(checkout, work); got != "Add one\n" {
		t.Fatalf("read %q", got)
	}
}

func TestAMembersWorkComesBackToTheMemberItself(t *testing.T) {
	boxtest.Isolate(t)
	directory := t.TempDir()
	path := makeClonedMember(t, directory, "billing-api")
	work := boxtest.CommitFile(t, path, "one.txt", "Add one")
	bundle := filepath.Join(directory, "work.bundle")
	boxtest.Git(t, path, "bundle", "create", bundle, "--branches")
	boxtest.Git(t, path, "fetch", bundle, "+refs/heads/*:"+config.SandboxRefs+"/demo-1/*")
	checkout := project.Checkout{Path: path, KnownCommits: config.MemberKnown}
	if got := realSession(t).SandboxRefs(checkout, "demo-1"); len(got) == 0 || got[0].Commit != work {
		t.Fatalf("the refs are %v", got)
	}
}

// tail is the last few words of a command, which is what most of these tests assert on.
func tail(command []string, count int) []string {
	return command[len(command)-count:]
}
