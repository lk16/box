package update_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lk16/box/internal/boxtest"
	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/system"
	"github.com/lk16/box/internal/update"
)

// released is what the module proxy answers with for a published release.
func released(version string) []byte {
	return []byte(`{"Version": "` + version + `", "Time": "2026-01-01T00:00:00Z"}`)
}

// now is the fixed moment these tests run at, since a check is paced by the clock.
var now = time.Unix(1000, 0)

// fixture is one update run's fakes, and the check that reaches the world only through them.
type fixture struct {
	runner   *boxtest.Runner
	console  *boxtest.Console
	download *boxtest.Downloader
	update   update.Update
}

// newFixture builds an installed box whose one request answers with the given release.
func newFixture(version string, download *boxtest.Downloader, terminal bool) *fixture {
	runner := &boxtest.Runner{}
	console := &boxtest.Console{}
	return &fixture{
		runner:   runner,
		console:  console,
		download: download,
		update: update.Update{
			Version: version,
			Deps: system.Deps{
				Run:      runner,
				Console:  console.Handle(terminal),
				Download: download,
				Now:      func() time.Time { return now },
			},
		},
	}
}

// installed is a box that came from go install, checking against a proxy that answers one release.
func installed(t *testing.T, version, latest string) *fixture {
	t.Helper()
	t.Setenv(config.CacheHomeEnv, t.TempDir())
	boxtest.Unset(t, config.UpdateURLEnv)
	return newFixture(version, &boxtest.Downloader{Body: released(latest)}, false)
}

func TestTheUpdateURLIsBoxOwnPublishedModule(t *testing.T) {
	boxtest.Unset(t, config.UpdateURLEnv)
	if got := update.URL(); got != update.DefaultURL {
		t.Fatalf("the check reads %q", got)
	}
}

func TestAForkCanPointTheUpdateCheckAtItsOwnCopy(t *testing.T) {
	t.Setenv(config.UpdateURLEnv, "https://example.com/@latest")
	if got := update.URL(); got != "https://example.com/@latest" {
		t.Fatalf("the check reads %q", got)
	}
}

func TestCachePathFollowsXDGCacheHome(t *testing.T) {
	t.Setenv(config.CacheHomeEnv, "/tmp/xdg")
	if got := update.CachePath(); got != "/tmp/xdg/box/update-check.json" {
		t.Fatalf("the cache is at %q", got)
	}
}

func TestCachePathFallsBackToDotCache(t *testing.T) {
	t.Setenv(config.CacheHomeEnv, "")
	t.Setenv("HOME", "/home/someone")
	if got := update.CachePath(); got != "/home/someone/.cache/box/update-check.json" {
		t.Fatalf("the cache is at %q", got)
	}
}

func TestAStoredCheckTimeIsFresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update-check.json")
	if err := update.StoreCheckTime(path, now); err != nil {
		t.Fatal(err)
	}
	if !update.CheckedRecently(path, now) {
		t.Fatal("a check just stored reads as stale")
	}
}

func TestAStoredCheckTimeExpires(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update-check.json")
	if err := update.StoreCheckTime(path, now); err != nil {
		t.Fatal(err)
	}
	if update.CheckedRecently(path, now.Add(update.Interval+time.Second)) {
		t.Fatal("an expired check reads as fresh")
	}
}

func TestAMissingCacheReadsAsNeverChecked(t *testing.T) {
	if update.CheckedRecently(filepath.Join(t.TempDir(), "absent.json"), now) {
		t.Fatal("a missing cache reads as a fresh check")
	}
}

func TestACacheBoxCannotReadReadsAsNeverChecked(t *testing.T) {
	for name, contents := range map[string]string{
		"broken":     "{not json",
		"no key":     `{"checked": 1000.0}`,
		"not a time": `{"checked_at": "yesterday"}`,
	} {
		path := filepath.Join(t.TempDir(), "update-check.json")
		boxtest.WriteFile(t, path, contents)
		if update.CheckedRecently(path, now) {
			t.Errorf("a cache that is %s reads as a fresh check", name)
		}
	}
}

func TestStoreCheckTimeCreatesTheCacheDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "update-check.json")
	if err := update.StoreCheckTime(path, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestAGoBuildIsNoInstall(t *testing.T) {
	if update.IsInstalled(update.Devel) || update.IsInstalled("") {
		t.Fatal("a checkout was taken for an install")
	}
	if !update.IsInstalled("v0.1.0") {
		t.Fatal("an install was taken for a checkout")
	}
}

func TestParseLatestReadsTheReleaseTheProxyNames(t *testing.T) {
	latest, err := update.ParseLatest(released("v0.2.0"))
	if err != nil || latest != "v0.2.0" {
		t.Fatalf("read %q, %v", latest, err)
	}
}

func TestParseLatestRejectsAnAnswerNamingNoRelease(t *testing.T) {
	for _, body := range []string{"", "not json", "{}", `{"Version": ""}`} {
		if _, err := update.ParseLatest([]byte(body)); err == nil {
			t.Errorf("%q was read as a release", body)
		}
	}
}

func TestNoMessageWhenTheReleasesMatch(t *testing.T) {
	run := newFixture("v0.1.0", &boxtest.Downloader{}, false)
	if got := update.Message(run.update.Deps.Console, "v0.1.0", "v0.1.0"); got != "" {
		t.Fatalf("said %q", got)
	}
}

func TestTheUpdateMessageNamesTheCommandThatTakesIt(t *testing.T) {
	run := newFixture("v0.1.0", &boxtest.Downloader{}, false)
	if !strings.Contains(update.Message(run.update.Deps.Console, "v0.1.0", "v0.2.0"), "box self-update") {
		t.Fatal("the message does not name box self-update")
	}
}

func TestANoticeIsRedOnATerminal(t *testing.T) {
	boxtest.Unset(t, config.NoColourEnv)
	terminal := (&boxtest.Console{}).Handle(true)
	if got := update.InRed(terminal, "an update"); got != update.Red+"an update"+update.Reset {
		t.Fatalf("coloured to %q", got)
	}
}

func TestANoticeIsPlainTextWhenStderrIsNotATerminal(t *testing.T) {
	boxtest.Unset(t, config.NoColourEnv)
	if got := update.InRed((&boxtest.Console{}).Handle(false), "an update"); got != "an update" {
		t.Fatalf("coloured to %q", got)
	}
}

func TestANoticeIsPlainTextWhenNoColorIsSet(t *testing.T) {
	t.Setenv(config.NoColourEnv, "1")
	if got := update.InRed((&boxtest.Console{}).Handle(true), "an update"); got != "an update" {
		t.Fatalf("coloured to %q", got)
	}
}

func TestTheUpdateMessageCarriesNoEscapeCodesIntoAPipe(t *testing.T) {
	boxtest.Unset(t, config.NoColourEnv)
	plain := (&boxtest.Console{}).Handle(false)
	if strings.Contains(update.Message(plain, "v0.1.0", "v0.2.0"), update.Red) {
		t.Fatal("escape codes reached a pipe")
	}
}

func TestWarnWhenOutdatedSaysAnUpdateIsAvailable(t *testing.T) {
	run := installed(t, "v0.1.0", "v0.2.0")
	run.update.WarnWhenOutdated()
	if !strings.Contains(run.console.Warned(), "box self-update") {
		t.Fatalf("warned:\n%s", run.console.Warned())
	}
}

func TestWarnWhenOutdatedWarnsAtMostOnceAnHour(t *testing.T) {
	run := installed(t, "v0.1.0", "v0.2.0")
	run.update.WarnWhenOutdated()
	if !strings.Contains(run.console.Warned(), "box self-update") {
		t.Fatalf("warned:\n%s", run.console.Warned())
	}
	run.console.Err.Reset()
	run.update.WarnWhenOutdated()
	if run.console.Warned() != "" {
		t.Fatalf("warned again:\n%s", run.console.Warned())
	}
	if len(run.download.URLs) != 1 {
		t.Fatalf("the proxy was asked %d times", len(run.download.URLs))
	}
}

func TestAnEmptyUpdateURLChecksNothing(t *testing.T) {
	t.Setenv(config.CacheHomeEnv, t.TempDir())
	t.Setenv(config.UpdateURLEnv, "")
	run := newFixture("v0.1.0", &boxtest.Downloader{Body: released("v0.2.0")}, false)
	run.update.WarnWhenOutdated()
	if run.console.Warned() != "" || len(run.download.URLs) != 0 {
		t.Fatalf("warned %q after %d requests", run.console.Warned(), len(run.download.URLs))
	}
}

func TestWarnWhenOutdatedStaysSilentWhenTheCheckFails(t *testing.T) {
	t.Setenv(config.CacheHomeEnv, t.TempDir())
	boxtest.Unset(t, config.UpdateURLEnv)
	run := newFixture("v0.1.0", &boxtest.Downloader{Err: errors.New("no network")}, false)
	run.update.WarnWhenOutdated()
	if run.console.Warned() != "" || run.console.Printed() != "" {
		t.Fatalf("said %q / %q", run.console.Printed(), run.console.Warned())
	}
}

func TestWarnWhenOutdatedRecordsACheckThatFailed(t *testing.T) {
	t.Setenv(config.CacheHomeEnv, t.TempDir())
	boxtest.Unset(t, config.UpdateURLEnv)
	run := newFixture("v0.1.0", &boxtest.Downloader{Err: errors.New("no network")}, false)
	run.update.WarnWhenOutdated()
	run.update.WarnWhenOutdated()
	if len(run.download.URLs) != 1 {
		t.Fatalf("the proxy was asked %d times", len(run.download.URLs))
	}
}

func TestWarnWhenOutdatedSurvivesACacheItCannotWrite(t *testing.T) {
	unwritable := filepath.Join(t.TempDir(), "cache")
	boxtest.WriteFile(t, unwritable, "not a directory")
	t.Setenv(config.CacheHomeEnv, unwritable)
	boxtest.Unset(t, config.UpdateURLEnv)
	run := newFixture("v0.1.0", &boxtest.Downloader{Body: released("v0.1.0")}, false)
	run.update.WarnWhenOutdated()
	if run.console.Warned() != "" {
		t.Fatalf("warned:\n%s", run.console.Warned())
	}
}

func TestWarnWhenOutdatedSaysNothingAboutACheckedOutBox(t *testing.T) {
	t.Setenv(config.CacheHomeEnv, t.TempDir())
	boxtest.Unset(t, config.UpdateURLEnv)
	run := newFixture(update.Devel, &boxtest.Downloader{Body: released("v0.2.0")}, false)
	run.update.WarnWhenOutdated()
	if run.console.Warned() != "" || len(run.download.URLs) != 0 {
		t.Fatalf("warned %q after %d requests", run.console.Warned(), len(run.download.URLs))
	}
}

func TestSelfUpdateInstallsThePublishedRelease(t *testing.T) {
	run := installed(t, "v0.1.0", "v0.2.0")
	code, err := run.update.SelfUpdate()
	if err != nil || code != 0 {
		t.Fatalf("exited %d, %v", code, err)
	}
	if !run.runner.Ran(update.InstallCommand()...) {
		t.Fatalf("ran %v", run.runner.Commands)
	}
	if !strings.Contains(run.console.Printed(), "updated box to v0.2.0") {
		t.Fatalf("said:\n%s", run.console.Printed())
	}
}

func TestSelfUpdateSaysWhenThereIsNothingToTake(t *testing.T) {
	run := installed(t, "v0.1.0", "v0.1.0")
	code, err := run.update.SelfUpdate()
	if err != nil || code != 0 {
		t.Fatalf("exited %d, %v", code, err)
	}
	if !strings.Contains(run.console.Printed(), "already the published release") {
		t.Fatalf("said:\n%s", run.console.Printed())
	}
	if len(run.runner.Commands) != 0 {
		t.Fatalf("ran %v", run.runner.Commands)
	}
}

func TestSelfUpdateReportsAURLItCannotRead(t *testing.T) {
	t.Setenv(config.CacheHomeEnv, t.TempDir())
	boxtest.Unset(t, config.UpdateURLEnv)
	run := newFixture("v0.1.0", &boxtest.Downloader{Err: errors.New("no network")}, false)
	_, err := run.update.SelfUpdate()
	if err == nil || !strings.Contains(err.Error(), "could not read") {
		t.Fatalf("the refusal was %v", err)
	}
}

func TestSelfUpdateRejectsAnEmptyAnswer(t *testing.T) {
	t.Setenv(config.CacheHomeEnv, t.TempDir())
	boxtest.Unset(t, config.UpdateURLEnv)
	run := newFixture("v0.1.0", &boxtest.Downloader{Body: []byte{}}, false)
	_, err := run.update.SelfUpdate()
	if err == nil || !strings.Contains(err.Error(), "names no release") {
		t.Fatalf("the refusal was %v", err)
	}
}

func TestSelfUpdateRefusesACheckout(t *testing.T) {
	run := installed(t, update.Devel, "v0.2.0")
	_, err := run.update.SelfUpdate()
	if err == nil || !strings.Contains(err.Error(), "checkout rather than an install") {
		t.Fatalf("the refusal was %v", err)
	}
}

func TestSelfUpdateHasNowhereToUpdateFromWithoutAURL(t *testing.T) {
	t.Setenv(config.UpdateURLEnv, "")
	run := newFixture("v0.1.0", &boxtest.Downloader{}, false)
	_, err := run.update.SelfUpdate()
	if err == nil || !strings.Contains(err.Error(), "nowhere to update from") {
		t.Fatalf("the refusal was %v", err)
	}
}

func TestSelfUpdateReportsAnInstallThatFailed(t *testing.T) {
	run := installed(t, "v0.1.0", "v0.2.0")
	run.runner.Answer = func([]string) system.Result { return system.Result{Code: 1} }
	_, err := run.update.SelfUpdate()
	if err == nil || !strings.Contains(err.Error(), "go install") {
		t.Fatalf("the refusal was %v", err)
	}
}

func TestTheUpdateMessageSaysWhenGoIsNotOnPathToTakeIt(t *testing.T) {
	boxtest.Unset(t, config.NoColourEnv)
	boxtest.EmptyPath(t)
	message := update.Message((&boxtest.Console{}).Handle(false), "v0.1.0", "v0.2.0")
	if !strings.Contains(message, "go is not on PATH") || strings.Contains(message, "self-update") {
		t.Fatalf("said %q", message)
	}
}

func TestSelfUpdateSaysWhenGoIsNotOnPathToTakeIt(t *testing.T) {
	run := installed(t, "v0.1.0", "v0.2.0")
	boxtest.EmptyPath(t)
	_, err := run.update.SelfUpdate()
	if err == nil || !strings.Contains(err.Error(), "go is not on PATH") {
		t.Fatalf("the refusal was %v", err)
	}
	if len(run.runner.Commands) != 0 {
		t.Fatalf("ran %v", run.runner.Commands)
	}
}
