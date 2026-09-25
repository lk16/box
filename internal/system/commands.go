package system

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"
)

// Commands is the Runner that really starts processes.
type Commands struct{}

// Capture runs a command without showing its output, reporting a missing binary as a failed run.
func (Commands) Capture(arguments []string, environment []string) Result {
	command := exec.Command(arguments[0], arguments[1:]...)
	command.Env = environment
	return collect(command)
}

// Timed runs a command without showing its output, killing it once the wait is up.
func (Commands) Timed(arguments []string, wait time.Duration) Result {
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	result := collect(exec.CommandContext(ctx, arguments[0], arguments[1:]...))
	// A killed command has no answer to give, so it reads as the failure it is.
	if ctx.Err() != nil {
		return Result{Code: NotRun}
	}
	return result
}

// Attach runs a command on box's own terminal, leaving Ctrl-C to whoever is in front of it.
func (Commands) Attach(arguments []string, environment []string) Result {
	command := exec.Command(arguments[0], arguments[1:]...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	command.Env = environment
	// The child owns the terminal, so its own Ctrl-C must not take box down with it.
	attached.Add(1)
	defer attached.Add(-1)
	return Result{Code: codeOf(command.Run())}
}

// Feed runs a command with text on its stdin, so a secret never lands on a command line.
func (Commands) Feed(arguments []string, stdin string) Result {
	command := exec.Command(arguments[0], arguments[1:]...)
	command.Stdin = strings.NewReader(stdin)
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	return Result{Code: codeOf(command.Run())}
}

// collect runs a command with its output kept rather than shown.
func collect(command *exec.Cmd) Result {
	var out, problems bytes.Buffer
	command.Stdout, command.Stderr = &out, &problems
	// A killed command may leave a child holding the pipe, and box must not wait on that child.
	command.WaitDelay = time.Second
	err := command.Run()
	return Result{Code: codeOf(err), Stdout: out.String(), Stderr: problems.String()}
}

// codeOf reads what a command exited with, calling one that never started what a shell calls it.
func codeOf(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return NotRun
}

// attached counts the children that own the terminal right now, so Ctrl-C reaches them alone.
var attached atomic.Int32

// ChildAttached says whether a command box started is holding the terminal at this moment.
func ChildAttached() bool {
	return attached.Load() > 0
}
