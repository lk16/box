// Package boxtest holds the stand-ins box's tests put in front of the system, and the git they need.
package boxtest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/system"
)

// Runner answers every command from one function, and records what it was asked to run.
type Runner struct {
	// Answer says what a command leaves behind; a nil one means every command worked silently.
	Answer func(arguments []string) system.Result
	// Unstartable says which commands could not be started at all, which only Feed can report.
	Unstartable func(arguments []string) error
	Commands    [][]string
	Stdin       []string
}

// Capture records a command and answers it.
func (r *Runner) Capture(arguments []string) system.Result {
	return r.record(arguments)
}

// Timed records a command and answers it, since a fake never runs out of time.
func (r *Runner) Timed(arguments []string, _ time.Duration) system.Result {
	return r.record(arguments)
}

// Attach records a command and answers it, since a fake has no terminal to hand over.
func (r *Runner) Attach(arguments []string, _ []string) system.Result {
	return r.record(arguments)
}

// Feed records a command and the text it was fed, reporting one that could not be started.
func (r *Runner) Feed(arguments []string, stdin string) (system.Result, error) {
	r.Stdin = append(r.Stdin, stdin)
	if r.Unstartable != nil {
		if err := r.Unstartable(arguments); err != nil {
			r.Commands = append(r.Commands, arguments)
			return system.Result{}, err
		}
	}
	return r.record(arguments), nil
}

// record keeps a command and works out what this fake answers it with.
func (r *Runner) record(arguments []string) system.Result {
	r.Commands = append(r.Commands, arguments)
	if r.Answer == nil {
		return system.Result{}
	}
	return r.Answer(arguments)
}

// Ran says whether the fake was asked to run exactly this command.
func (r *Runner) Ran(arguments ...string) bool {
	for _, command := range r.Commands {
		if strings.Join(command, "\x00") == strings.Join(arguments, "\x00") {
			return true
		}
	}
	return false
}

// Index is where a command sits in the order they were run, or -1 when it was never run.
func (r *Runner) Index(arguments ...string) int {
	for at, command := range r.Commands {
		if strings.Join(command, "\x00") == strings.Join(arguments, "\x00") {
			return at
		}
	}
	return -1
}

// Prompter answers questions from a fixed list, and ends the input once that list is empty.
type Prompter struct {
	Answers   []string
	Terminal  bool
	Questions []string
}

// Interactive says whether this fake stands in for someone at a terminal.
func (p *Prompter) Interactive() bool {
	return p.Terminal
}

// Ask records the question and answers it, or reports the input ending.
func (p *Prompter) Ask(question string) (string, bool) {
	p.Questions = append(p.Questions, question)
	if len(p.Answers) == 0 {
		return "", false
	}
	answer := p.Answers[0]
	p.Answers = p.Answers[1:]
	return answer, true
}

// Downloader answers the one request the update check makes with fixed bytes, or with a failure.
type Downloader struct {
	Body []byte
	Err  error
	URLs []string
}

// Get records the URL and answers it.
func (d *Downloader) Get(url string) ([]byte, error) {
	d.URLs = append(d.URLs, url)
	return d.Body, d.Err
}

// Console is where a test's box prints, with both streams kept for it to read back.
type Console struct {
	Out bytes.Buffer
	Err bytes.Buffer
}

// Handle is the console box writes to, with the error stream marked as a terminal or not.
func (c *Console) Handle(terminal bool) system.Console {
	return system.Console{Out: &c.Out, Err: &c.Err, Terminal: terminal}
}

// Printed is everything the run wrote to stdout.
func (c *Console) Printed() string {
	return c.Out.String()
}

// Warned is everything the run wrote to stderr.
func (c *Console) Warned() string {
	return c.Err.String()
}

// Isolate keeps a developer's own git configuration out of the repositories a test creates.
func Isolate(t *testing.T) {
	t.Helper()
	// A global core.excludesFile or init.templateDir would otherwise decide what check-ignore says.
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

// StubBinaries puts a stand-in on PATH for every command box shells out to that this machine lacks.
func StubBinaries(t *testing.T) {
	t.Helper()
	// The tests fake sbx and claude rather than calling them, so box has to find them anyway.
	directory := t.TempDir()
	for _, name := range config.RequiredBinaries {
		if system.OnPath(name) {
			continue
		}
		stub := filepath.Join(directory, name)
		if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// EmptyPath leaves nothing on PATH, which is the machine that has none of what box needs.
func EmptyPath(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

// author is who a test repository's commits belong to, so no developer's own identity is needed.
var author = []string{"-c", "user.name=box", "-c", "user.email=box@example.com"}

// Git runs a git command in a test repository, insisting it worked, and returns what it printed.
func Git(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append(append([]string{"-C", directory}, author...), arguments...)...)
	printed, err := command.Output()
	if err != nil {
		t.Fatalf("git %v in %s failed: %v", arguments, directory, err)
	}
	return strings.TrimSpace(string(printed))
}

// GitInit creates a git repository with no commits in it.
func GitInit(t *testing.T, directory string) string {
	t.Helper()
	if err := exec.Command("git", "init", "-q", directory).Run(); err != nil {
		t.Fatal(err)
	}
	return directory
}

// MakeRepository creates a git repository with the one commit box needs to have something to clone.
func MakeRepository(t *testing.T, directory string) string {
	t.Helper()
	GitInit(t, directory)
	Git(t, directory, "commit", "-q", "--allow-empty", "-m", "first")
	return directory
}

// CommitFile commits one new file and returns the commit that holds it.
func CommitFile(t *testing.T, directory, name, subject string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, name), []byte(name), 0o644); err != nil {
		t.Fatal(err)
	}
	Git(t, directory, "add", name)
	Git(t, directory, "commit", "-q", "-m", subject)
	return Git(t, directory, "rev-parse", "HEAD")
}

// WriteFile writes a file and every directory above it, which a test repository often needs.
func WriteFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Symlink points one path at another, which is how a test hides a file inside a repository.
func Symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

// Unset removes an environment variable for one test, putting it back when the test ends.
func Unset(t *testing.T, name string) {
	t.Helper()
	// Setting it first registers the cleanup that restores whatever the developer really has.
	t.Setenv(name, "")
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
}
