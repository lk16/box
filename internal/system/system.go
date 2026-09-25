// Package system is everything box reaches the outside world through, behind interfaces a test fakes.
package system

import (
	"io"
	"os/exec"
	"time"
)

// NotRun is what a command that never started exits with, which is what a shell reports for it.
const NotRun = 127

// Result is what one finished command left behind.
type Result struct {
	Code   int
	Stdout string
	Stderr string
}

// Runner runs the external commands box shells out to.
type Runner interface {
	// Capture runs a command without showing its output, with the given environment.
	Capture(arguments []string, environment []string) Result
	// Timed runs a command without showing its output, ending it after the given wait.
	Timed(arguments []string, wait time.Duration) Result
	// Attach runs a command with box's own terminal and the given environment.
	Attach(arguments []string, environment []string) Result
	// Feed runs a command with text on its stdin.
	Feed(arguments []string, stdin string) Result
}

// Console is where box prints, and whether its error stream can carry colour.
type Console struct {
	Out      io.Writer
	Err      io.Writer
	Terminal bool
}

// Prompter is the terminal box gen asks its one question at.
type Prompter interface {
	// Interactive says whether there is anyone here to answer.
	Interactive() bool
	// Ask puts a question, returning false when the input ended instead.
	Ask(question string) (string, bool)
}

// Downloader reads a URL, which is all the update check needs from the network.
type Downloader interface {
	Get(url string) ([]byte, error)
}

// Deps is the outside world one box run is given.
type Deps struct {
	Run      Runner
	Console  Console
	Ask      Prompter
	Download Downloader
	Now      func() time.Time
}

// OnPath says whether a command is installed, which is how box checks what it shells out to.
func OnPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// Capture runs a command and returns its stdout, or nothing when it failed.
func Capture(runner Runner, arguments []string) string {
	result := runner.Capture(arguments, nil)
	if result.Code != 0 {
		return ""
	}
	return result.Stdout
}

// Succeeds runs a command and says only whether it worked, which is all some callers need.
func Succeeds(runner Runner, arguments []string) bool {
	return runner.Capture(arguments, nil).Code == 0
}
