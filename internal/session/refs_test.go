package session_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/lk16/box/internal/boxtest"
	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/project"
	"github.com/lk16/box/internal/session"
	"github.com/lk16/box/internal/system"
)

// sandboxRef is the one ref a sandbox left behind in these tests.
var sandboxRef = session.SandboxRef{RefName: "refs/sandboxes/demo-1/main", Commit: "abc123"}

// repository answers the git and Claude calls SettleRef makes, from a few canned replies.
type repository struct {
	count         string
	suggestion    string
	branches      string
	refuseBranch  bool
	subjectsAsked string
}

// answer stands in for everything SettleRef runs against git and Claude.
func (r *repository) answer(arguments []string) system.Result {
	joined := strings.Join(arguments, " ")
	switch {
	case strings.Contains(joined, "rev-list --count"):
		return failWhenEmpty(r.count)
	case strings.Contains(joined, "log --format=%s"):
		return system.Result{Stdout: "Add retry logic\n"}
	case arguments[0] == "claude":
		r.subjectsAsked = arguments[len(arguments)-1]
		return system.Result{Stdout: r.suggestion}
	case strings.Contains(joined, "refs/heads"):
		return system.Result{Stdout: r.branches}
	case strings.Contains(joined, "branch") && r.refuseBranch:
		return system.Result{Code: 1}
	}
	return system.Result{}
}

// failWhenEmpty turns "git could not say" into the failed command it really is.
func failWhenEmpty(count string) system.Result {
	if count == "" {
		return system.Result{Code: 1}
	}
	return system.Result{Stdout: count + "\n"}
}

// settling runs SettleRef against a repository answering with the given replies.
func settling(t *testing.T, path string, answers *repository) *fixture {
	t.Helper()
	run := newFixture(answers.answer)
	run.session.SettleRef(checkoutAt(path), sandboxRef)
	return run
}

func TestParseRefNamesExtractsSandboxNames(t *testing.T) {
	refs := "refs/sandboxes/demo-1/main\nrefs/sandboxes/demo-2/wip\nrefs/heads/main\n"
	names := session.ParseRefNames(refs)
	if len(names) != 2 || !names["demo-1"] || !names["demo-2"] {
		t.Fatalf("read %v", names)
	}
}

func TestParseSandboxRefsReadsNameAndCommit(t *testing.T) {
	refs := "refs/sandboxes/demo-1/main abc123\nrefs/sandboxes/demo-1/wip def456\n"
	want := []session.SandboxRef{
		{RefName: "refs/sandboxes/demo-1/main", Commit: "abc123"},
		{RefName: "refs/sandboxes/demo-1/wip", Commit: "def456"},
	}
	if got := session.ParseSandboxRefs(refs); !slices.Equal(got, want) {
		t.Fatalf("read %v", got)
	}
}

func TestParseSandboxRefsSkipsLinesThatAreNotARefAndACommit(t *testing.T) {
	if got := session.ParseSandboxRefs("refs/sandboxes/demo-1/main\n\n"); len(got) != 0 {
		t.Fatalf("read %v", got)
	}
}

func TestToBranchNameKebabCasesTheAnswer(t *testing.T) {
	if got := session.ToBranchName("Add Retry Logic"); got != "add-retry-logic" {
		t.Fatalf("named %q", got)
	}
}

func TestToBranchNameKeepsAtMostFiveWords(t *testing.T) {
	if got := session.ToBranchName("one two three four five six seven"); got != "one-two-three-four-five" {
		t.Fatalf("named %q", got)
	}
}

func TestToBranchNameTakesTheLastLineTheAgentWrote(t *testing.T) {
	if got := session.ToBranchName("Here is a name:\n\nfix-flaky-test\n"); got != "fix-flaky-test" {
		t.Fatalf("named %q", got)
	}
}

func TestToBranchNameIsEmptyWithoutAnAnswer(t *testing.T) {
	if got := session.ToBranchName("\n  \n"); got != "" {
		t.Fatalf("named %q", got)
	}
}

func TestToBranchNameIsEmptyWhenNothingSurvivesKebabCasing(t *testing.T) {
	if got := session.ToBranchName("!!!"); got != "" {
		t.Fatalf("named %q", got)
	}
}

func TestBranchNameCommandRunsClaudeHeadlessWithTheSubjects(t *testing.T) {
	command := session.BranchNameCommand("Add retry logic")
	if command[0] != "claude" || command[1] != "-p" {
		t.Fatalf("assembled %v", command)
	}
	if !strings.HasPrefix(command[2], config.BranchNamePrompt) || !strings.Contains(command[2], "Add retry logic") {
		t.Fatalf("the prompt reads %q", command[2])
	}
}

func TestSuggestBranchNameKebabCasesWhatClaudePrinted(t *testing.T) {
	run := newFixture(func([]string) system.Result { return system.Result{Stdout: "Add Retry Logic\n"} })
	if got := run.session.SuggestBranchName("Add retry logic"); got != "add-retry-logic" {
		t.Fatalf("named %q", got)
	}
}

func TestSuggestBranchNameSendsTheSubjectsToAHeadlessClaude(t *testing.T) {
	run := newFixture(func([]string) system.Result { return system.Result{Stdout: "add-retry-logic\n"} })
	run.session.SuggestBranchName("Add retry logic")
	wantCommand(t, run.runner, session.BranchNameCommand("Add retry logic")...)
}

func TestSuggestBranchNameIsEmptyWhenClaudeFailsOrIsNotInstalled(t *testing.T) {
	run := newFixture(func([]string) system.Result { return system.Result{Code: system.NotRun} })
	if got := run.session.SuggestBranchName("Add retry logic"); got != "" {
		t.Fatalf("named %q", got)
	}
}

func TestPickBranchNameKeepsAFreeName(t *testing.T) {
	if got := session.PickBranchName("add-retry-logic", map[string]bool{"main": true}); got != "add-retry-logic" {
		t.Fatalf("named %q", got)
	}
}

func TestPickBranchNameNumbersATakenNameFromTwo(t *testing.T) {
	used := map[string]bool{"add-retry-logic": true, "add-retry-logic-2": true}
	if got := session.PickBranchName("add-retry-logic", used); got != "add-retry-logic-3" {
		t.Fatalf("named %q", got)
	}
}

func TestPickNameSkipsUsedNames(t *testing.T) {
	if got := session.PickName("demo", map[string]bool{"demo-1": true, "demo-2": true}); got != "demo-3" {
		t.Fatalf("named %q", got)
	}
}

func TestPickNameStartsAtOne(t *testing.T) {
	if got := session.PickName("demo", nil); got != "demo-1" {
		t.Fatalf("named %q", got)
	}
}

func TestPluralKeepsOneSingular(t *testing.T) {
	if got := session.Plural("1", "commit"); got != "1 commit" {
		t.Fatalf("read %q", got)
	}
}

func TestPluralMakesEveryOtherCountPlural(t *testing.T) {
	if got := session.Plural("2", "commit"); got != "2 commits" {
		t.Fatalf("read %q", got)
	}
	if got := session.Plural("0", "commit"); got != "0 commits" {
		t.Fatalf("read %q", got)
	}
}

func TestSettleRefBranchesTheWorkAndDropsTheRef(t *testing.T) {
	run := settling(t, "/work/demo", &repository{count: "3", suggestion: "add-retry-logic", branches: "main"})
	wantCommand(t, run.runner, "git", "-C", "/work/demo", "branch", "add-retry-logic", "abc123")
	wantCommand(t, run.runner, "git", "-C", "/work/demo", "update-ref", "-d", sandboxRef.RefName)
}

func TestSettleRefNamesTheBranchAfterTheCommits(t *testing.T) {
	answers := &repository{count: "3", suggestion: "add-retry-logic"}
	settling(t, "/work/demo", answers)
	if !strings.HasSuffix(answers.subjectsAsked, "Add retry logic\n") {
		t.Fatalf("named after %q", answers.subjectsAsked)
	}
}

func TestSettleRefNumbersABranchTheRepositoryAlreadyHas(t *testing.T) {
	answers := &repository{count: "1", suggestion: "add-retry-logic", branches: "add-retry-logic"}
	run := settling(t, "/work/demo", answers)
	wantCommand(t, run.runner, "git", "-C", "/work/demo", "branch", "add-retry-logic-2", "abc123")
}

func TestSettleRefSaysWhereTheWorkEndedUp(t *testing.T) {
	run := settling(t, "/work/demo", &repository{count: "3", suggestion: "add-retry-logic"})
	wantWarning(t, run.console, "box: /work/demo: branch add-retry-logic holds 3 commits")
	wantWarning(t, run.console, sandboxRef.RefName)
}

func TestSettleRefCountsASingleCommitInTheSingular(t *testing.T) {
	run := settling(t, "/work/demo", &repository{count: "1", suggestion: "add-retry-logic"})
	wantWarning(t, run.console, "holds 1 commit from")
}

func TestSettleRefDropsARefHoldingNoCommits(t *testing.T) {
	run := settling(t, "/work/demo", &repository{count: "0", suggestion: "add-retry-logic"})
	wantNoCommand(t, run.runner, "git", "-C", "/work/demo", "branch", "add-retry-logic", "abc123")
	wantCommand(t, run.runner, "git", "-C", "/work/demo", "update-ref", "-d", sandboxRef.RefName)
}

func TestSettleRefNamesTheRepositoryADroppedRefWasIn(t *testing.T) {
	run := settling(t, "/work/billing-api", &repository{count: "0", suggestion: "add-retry-logic"})
	wantWarning(t, run.console, "box: /work/billing-api: "+sandboxRef.RefName+" held no")
}

func TestSettleRefKeepsTheRefWhenNamingFails(t *testing.T) {
	run := settling(t, "/work/demo", &repository{count: "2"})
	wantNoCommand(t, run.runner, "git", "-C", "/work/demo", "update-ref", "-d", sandboxRef.RefName)
	wantWarning(t, run.console, sandboxRef.RefName)
}

func TestSettleRefKeepsTheRefWhenGitRefusesTheBranch(t *testing.T) {
	answers := &repository{count: "2", suggestion: "add-retry-logic", refuseBranch: true}
	run := settling(t, "/work/demo", answers)
	wantNoCommand(t, run.runner, "git", "-C", "/work/demo", "update-ref", "-d", sandboxRef.RefName)
	wantWarning(t, run.console, "git refused branch add-retry-logic")
}

func TestSettleRefKeepsTheRefWhenGitCannotCountTheCommits(t *testing.T) {
	run := settling(t, "/work/demo", &repository{suggestion: "add-retry-logic"})
	wantNoCommand(t, run.runner, "git", "-C", "/work/demo", "update-ref", "-d", sandboxRef.RefName)
	wantWarning(t, run.console, "git could not read")
}

func TestTheRepositoryBoxRunsInCountsWhatItsCheckoutLacks(t *testing.T) {
	want := []string{"abc123", "--not", "HEAD"}
	if got := session.NewCommits(checkoutAt("/work/demo"), "abc123"); !slices.Equal(got, want) {
		t.Fatalf("counts %v", got)
	}
}

func TestAMemberCountsWhatNoneOfItsOriginBranchesHold(t *testing.T) {
	checkout := project.Checkout{Path: "/work/billing-api", KnownCommits: config.MemberKnown}
	want := []string{"abc123", "--not", "--remotes=origin"}
	if got := session.NewCommits(checkout, "abc123"); !slices.Equal(got, want) {
		t.Fatalf("counts %v", got)
	}
}

func TestTakenNamesAsksBothSbxAndGit(t *testing.T) {
	run := newFixture(func(arguments []string) system.Result {
		if arguments[0] == "sbx" {
			return system.Result{Stdout: "demo-1\n"}
		}
		return system.Result{Stdout: config.SandboxRefs + "/demo-2/main\n"}
	})
	names := run.session.TakenNames([]project.Checkout{checkoutAt("/work/demo")})
	if len(names) != 2 || !names["demo-1"] || !names["demo-2"] {
		t.Fatalf("taken: %v", names)
	}
	wantCommand(t, run.runner, "sbx", "ls", "-q")
	wantCommand(t, run.runner, "git", "-C", "/work/demo", "for-each-ref", "--format=%(refname)", config.SandboxRefs)
}

func TestTakenNamesReadsTheRefsWaitingInAMember(t *testing.T) {
	run := newFixture(func(arguments []string) system.Result {
		if arguments[0] == "sbx" || arguments[2] != "/work/billing-api" {
			return system.Result{}
		}
		return system.Result{Stdout: config.SandboxRefs + "/demo-2/wip\n"}
	})
	checkouts := []project.Checkout{
		checkoutAt("/work/boxes"),
		{Path: "/work/billing-api", KnownCommits: config.MemberKnown},
	}
	if names := run.session.TakenNames(checkouts); len(names) != 1 || !names["demo-2"] {
		t.Fatalf("taken: %v", names)
	}
}

// realSession builds a session that really runs git, for the checks git has to answer itself.
func realSession(t *testing.T) session.Session {
	t.Helper()
	boxtest.Isolate(t)
	return session.Session{Deps: system.Deps{
		Run:     system.Commands{},
		Console: (&boxtest.Console{}).Handle(false),
	}}
}

// repositoryWithSandboxWork builds a repository whose HEAD lacks the commits a sandbox ref points at.
func repositoryWithSandboxWork(t *testing.T, directory string) string {
	t.Helper()
	boxtest.MakeRepository(t, directory)
	base := boxtest.Git(t, directory, "rev-parse", "HEAD")
	boxtest.CommitFile(t, directory, "one.txt", "Add one")
	work := boxtest.CommitFile(t, directory, "two.txt", "Add two")
	boxtest.Git(t, directory, "update-ref", config.SandboxRefs+"/demo-1/main", work)
	boxtest.Git(t, directory, "reset", "--hard", "-q", base)
	return work
}

func TestCountNewCommitsCountsWhatThisCheckoutLacks(t *testing.T) {
	directory := t.TempDir()
	work := repositoryWithSandboxWork(t, directory)
	if got := realSession(t).CountNewCommits(checkoutAt(directory), work); got != "2" {
		t.Fatalf("counted %q", got)
	}
}

func TestCountNewCommitsIsEmptyWhenGitCannotReadTheCommit(t *testing.T) {
	directory := t.TempDir()
	repositoryWithSandboxWork(t, directory)
	if got := realSession(t).CountNewCommits(checkoutAt(directory), "no-such-commit"); got != "" {
		t.Fatalf("counted %q", got)
	}
}

func TestNewCommitSubjectsReadsTheSubjectsOfWhatThisCheckoutLacks(t *testing.T) {
	directory := t.TempDir()
	work := repositoryWithSandboxWork(t, directory)
	if got := realSession(t).NewCommitSubjects(checkoutAt(directory), work); got != "Add two\nAdd one\n" {
		t.Fatalf("read %q", got)
	}
}

func TestSandboxRefsFindsWhatTheFetchLeft(t *testing.T) {
	directory := t.TempDir()
	work := repositoryWithSandboxWork(t, directory)
	want := []session.SandboxRef{{RefName: config.SandboxRefs + "/demo-1/main", Commit: work}}
	if got := realSession(t).SandboxRefs(checkoutAt(directory), "demo-1"); !slices.Equal(got, want) {
		t.Fatalf("found %v", got)
	}
}

func TestSandboxRefsIgnoresAnotherSandboxsRefs(t *testing.T) {
	directory := t.TempDir()
	repositoryWithSandboxWork(t, directory)
	if got := realSession(t).SandboxRefs(checkoutAt(directory), "demo-2"); len(got) != 0 {
		t.Fatalf("found %v", got)
	}
}

func TestCreateBranchPointsABranchAtTheSandboxsCommit(t *testing.T) {
	directory := t.TempDir()
	work := repositoryWithSandboxWork(t, directory)
	if !realSession(t).CreateBranch(checkoutAt(directory), "add-retry-logic", work) {
		t.Fatal("git refused a name it should have taken")
	}
	if got := boxtest.Git(t, directory, "rev-parse", "add-retry-logic"); got != work {
		t.Fatalf("the branch points at %q", got)
	}
}

func TestCreateBranchSaysNoToANameGitRefuses(t *testing.T) {
	directory := t.TempDir()
	work := repositoryWithSandboxWork(t, directory)
	if realSession(t).CreateBranch(checkoutAt(directory), "a name with spaces", work) {
		t.Fatal("git took a name it should have refused")
	}
}

func TestLocalBranchNamesReadsTheBranchesThisRepositoryHas(t *testing.T) {
	directory := t.TempDir()
	work := repositoryWithSandboxWork(t, directory)
	run := realSession(t)
	run.CreateBranch(checkoutAt(directory), "add-retry-logic", work)
	if !run.LocalBranchNames(checkoutAt(directory))["add-retry-logic"] {
		t.Fatal("a branch the repository has was not listed")
	}
}

func TestDeleteRefDropsTheRef(t *testing.T) {
	directory := t.TempDir()
	repositoryWithSandboxWork(t, directory)
	run := realSession(t)
	run.DeleteRef(checkoutAt(directory), config.SandboxRefs+"/demo-1/main")
	if got := run.SandboxRefs(checkoutAt(directory), "demo-1"); len(got) != 0 {
		t.Fatalf("the ref is still there: %v", got)
	}
}

func TestSettleSandboxRefsPutsARealSandboxsWorkOnARealBranch(t *testing.T) {
	directory := t.TempDir()
	work := repositoryWithSandboxWork(t, directory)
	boxtest.Isolate(t)
	naming := func(arguments []string) system.Result {
		if arguments[0] == "claude" {
			return system.Result{Stdout: "add-retry-logic\n"}
		}
		return system.Commands{}.Capture(arguments, nil)
	}
	run := newFixture(naming)
	run.session.SettleSandboxRefs(checkoutAt(directory), "demo-1")
	if got := boxtest.Git(t, directory, "rev-parse", "add-retry-logic"); got != work {
		t.Fatalf("the branch points at %q", got)
	}
	if got := realSession(t).SandboxRefs(checkoutAt(directory), "demo-1"); len(got) != 0 {
		t.Fatalf("the ref is still there: %v", got)
	}
}

func TestSuggestBranchNameGivesClaudeOneTurnAndNoMore(t *testing.T) {
	run := newFixture(func([]string) system.Result { return system.Result{Stdout: "add-retry-logic\n"} })
	run.session.SuggestBranchName("Add retry logic")
	// Naming a branch is a courtesy, so the agent gets one turn and the run is not held up.
	if len(run.runner.Waits) != 1 || run.runner.Waits[0] != session.BranchNameTimeout {
		t.Fatalf("claude was given %v", run.runner.Waits)
	}
}
