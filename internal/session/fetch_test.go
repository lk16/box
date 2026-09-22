package session_test

import (
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lk16/box/internal/boxtest"
	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/session"
	"github.com/lk16/box/internal/system"
)

// membersNamed declares one member per name, each sitting next to the folder the group runs in.
func membersNamed(names ...string) []config.Member {
	members := make([]config.Member, 0, len(names))
	for _, name := range names {
		members = append(members, config.Member{
			Name:      name,
			Branch:    "develop",
			GitOrigin: "https://example.com/" + name + ".git",
			Path:      "../" + name,
		})
	}
	return members
}

// fetchedPath is the member a fetch is for, or nothing when the command is some other one.
func fetchedPath(arguments []string) string {
	if len(arguments) == 5 && slices.Equal(arguments[3:], []string{"fetch", "origin"}) {
		return arguments[2]
	}
	return ""
}

// bundleGroup packs a group's members with the fakes answering, failing rather than hanging forever.
func bundleGroup(t *testing.T, run *fixture, members ...config.Member) error {
	t.Helper()
	directory := t.TempDir()
	boxes := filepath.Join(directory, "boxes")
	settings := groupConfig(t, boxes, members...)
	done := make(chan error, 1)
	go func() { _, err := run.session.BundleMembers(settings, projectAt(boxes), directory); done <- err }()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("the fetches never all arrived, so they did not run at once")
		return nil
	}
}

func TestEveryMembersFetchRunsAtOnce(t *testing.T) {
	var together sync.WaitGroup
	together.Add(3)
	run := newFixture(func(arguments []string) system.Result {
		if fetchedPath(arguments) != "" {
			together.Done()
			// Each fetch waits for the other two, so one after another would never get past here.
			together.Wait()
		}
		return system.Result{}
	})
	if err := bundleGroup(t, run, membersNamed("alpha", "beta", "omega")...); err != nil {
		t.Fatal(err)
	}
}

func TestAFetchIsReportedWhereReposNamesIt(t *testing.T) {
	last := make(chan struct{})
	run := newFixture(func(arguments []string) system.Result {
		switch path := fetchedPath(arguments); {
		case path == "":
			return system.Result{}
		// The member named first is held back until the one named last is through with its fetch.
		case strings.HasSuffix(path, "alpha"):
			<-last
		case strings.HasSuffix(path, "omega"):
			close(last)
		}
		return system.Result{Stderr: "   abcdef1..abcdef2  develop -> origin/develop\n"}
	})
	if err := bundleGroup(t, run, membersNamed("alpha", "omega")...); err != nil {
		t.Fatal(err)
	}
	warned := run.console.Warned()
	if strings.Index(warned, "alpha") > strings.Index(warned, "omega") {
		t.Fatalf("the fetches were reported as %q", warned)
	}
}

func TestWhatGitSaidAboutAFetchFollowsTheMemberItIsAbout(t *testing.T) {
	run := newFixture(func(arguments []string) system.Result {
		if fetchedPath(arguments) == "" {
			return system.Result{}
		}
		return system.Result{Stderr: "   abcdef1..abcdef2  develop -> origin/develop\n"}
	})
	if err := bundleGroup(t, run, membersNamed("alpha")...); err != nil {
		t.Fatal(err)
	}
	want := "alpha\n   abcdef1..abcdef2  develop -> origin/develop\n"
	if !strings.HasSuffix(run.console.Warned(), want) {
		t.Fatalf("the fetch was reported as %q", run.console.Warned())
	}
}

func TestAFetchThatFailedIsAskedAgainWithTheTerminal(t *testing.T) {
	run := newFixture(failFirstFetch())
	if err := bundleGroup(t, run, membersNamed("alpha")...); err != nil {
		t.Fatal(err)
	}
	if len(run.runner.Attached) != 1 || fetchedPath(run.runner.Attached[0]) == "" {
		t.Fatalf("the terminal was given to %v", run.runner.Attached)
	}
}

func TestAFetchNoTerminalCanFixStopsTheRun(t *testing.T) {
	run := newFixture(func(arguments []string) system.Result {
		if fetchedPath(arguments) == "" {
			return system.Result{}
		}
		return system.Result{Code: 1}
	})
	err := bundleGroup(t, run, membersNamed("alpha")...)
	if err == nil || !strings.Contains(err.Error(), "git fetch origin failed in") {
		t.Fatalf("the refusal was %v", err)
	}
}

func TestNoMemberIsBundledUntilEveryFetchIsIn(t *testing.T) {
	// The fetches answer at once, so what they count is counted the way they run.
	var fetches, early atomic.Int32
	run := newFixture(func(arguments []string) system.Result {
		if fetchedPath(arguments) != "" {
			fetches.Add(1)
		}
		if slices.Contains(arguments, "bundle") && fetches.Load() < 2 {
			early.Add(1)
		}
		return system.Result{}
	})
	if err := bundleGroup(t, run, membersNamed("alpha", "omega")...); err != nil {
		t.Fatal(err)
	}
	if early.Load() != 0 {
		t.Fatalf("%d members were bundled before every fetch was in", early.Load())
	}
}

func TestAFetchTellsGitAndSshToFailRatherThanAskTheTerminal(t *testing.T) {
	boxtest.Unset(t, config.GitSSHCommandEnv)
	environment := session.FetchEnvironment()
	if !slices.Contains(environment, config.GitPromptEnv+"=0") {
		t.Fatalf("git may still prompt: %v", lastTwo(environment))
	}
	if !slices.Contains(environment, config.GitSSHCommandEnv+"=ssh -o BatchMode=yes") {
		t.Fatalf("ssh may still ask: %v", lastTwo(environment))
	}
}

func TestAFetchKeepsTheSshCommandTheUserSet(t *testing.T) {
	t.Setenv(config.GitSSHCommandEnv, "ssh -i /keys/work")
	want := config.GitSSHCommandEnv + "=ssh -i /keys/work -o BatchMode=yes"
	if environment := session.FetchEnvironment(); !slices.Contains(environment, want) {
		t.Fatalf("ssh runs as %v", lastTwo(environment))
	}
}

func TestTheFetchCommandWritesOriginAndNothingElse(t *testing.T) {
	want := []string{"git", "-C", "/work/billing-api", "fetch", "origin"}
	if got := session.FetchCommand("/work/billing-api"); !slices.Equal(got, want) {
		t.Fatalf("the fetch is %v", got)
	}
}

// lastTwo is the end of an environment, which is where a fetch's own settings sit.
func lastTwo(environment []string) []string {
	return environment[len(environment)-2:]
}

// failFirstFetch answers the first fetch as failed and every later one as done.
func failFirstFetch() func(arguments []string) system.Result {
	failed := false
	return func(arguments []string) system.Result {
		if fetchedPath(arguments) == "" || failed {
			return system.Result{}
		}
		failed = true
		return system.Result{Code: 1}
	}
}

// watchedSession builds a session that really runs git, with everything it said kept to read back.
func watchedSession(t *testing.T) (session.Session, *boxtest.Console) {
	t.Helper()
	boxtest.Isolate(t)
	console := &boxtest.Console{}
	run := session.Session{Deps: system.Deps{Run: system.Commands{}, Console: console.Handle(false)}}
	return run, console
}

func TestAFetchReallyBringsANewUpstreamCommitIn(t *testing.T) {
	boxtest.Isolate(t)
	directory := t.TempDir()
	path := makeClonedMember(t, directory, "billing-api")
	work := boxtest.CommitFile(t, filepath.Join(directory, "billing-api-origin"), "one.txt", "Add one")
	boxes := boxtest.MakeRepository(t, filepath.Join(directory, "boxes"))
	run, console := watchedSession(t)
	if _, err := run.BundleMembers(groupConfig(t, boxes, member), projectAt(boxes), directory); err != nil {
		t.Fatal(err)
	}
	if got := boxtest.Git(t, path, "rev-parse", "refs/remotes/origin/develop"); got != work {
		t.Fatalf("origin/develop is at %s, wanted %s", got, work)
	}
	if !strings.Contains(console.Warned(), "-> origin/develop") {
		t.Fatalf("the fetch was reported as %q", console.Warned())
	}
}
