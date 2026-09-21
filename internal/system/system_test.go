package system_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lk16/box/internal/system"
)

// script writes an executable shell script and returns the command that runs it.
func script(t *testing.T, body string) []string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "script")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return []string{path}
}

const missingBinary = "definitely-not-a-binary-on-this-machine"

func TestCaptureReturnsWhatTheCommandPrinted(t *testing.T) {
	if got := system.Capture(system.Commands{}, script(t, "echo hello\n")); got != "hello\n" {
		t.Fatalf("captured %q", got)
	}
}

func TestCaptureIsEmptyWhenTheCommandExitsNonZero(t *testing.T) {
	if got := system.Capture(system.Commands{}, script(t, "echo hello\nexit 1\n")); got != "" {
		t.Fatalf("captured %q", got)
	}
}

func TestCaptureIsEmptyWhenTheCommandIsNotInstalled(t *testing.T) {
	if got := system.Capture(system.Commands{}, []string{missingBinary}); got != "" {
		t.Fatalf("captured %q", got)
	}
}

func TestAMissingBinaryReadsAsTheCodeAShellReports(t *testing.T) {
	if got := (system.Commands{}).Capture([]string{missingBinary}).Code; got != system.NotRun {
		t.Fatalf("exited %d, want %d", got, system.NotRun)
	}
}

func TestSucceedsIsTrueWhenTheCommandExitsZero(t *testing.T) {
	if !system.Succeeds(system.Commands{}, script(t, "exit 0\n")) {
		t.Fatal("a command that worked was reported as failed")
	}
}

func TestSucceedsIsFalseWhenTheCommandExitsNonZero(t *testing.T) {
	if system.Succeeds(system.Commands{}, script(t, "exit 1\n")) {
		t.Fatal("a command that failed was reported as working")
	}
}

func TestSucceedsIsFalseWhenTheCommandIsNotInstalled(t *testing.T) {
	if system.Succeeds(system.Commands{}, []string{missingBinary}) {
		t.Fatal("a command nobody has was reported as working")
	}
}

func TestTimedEndsACommandThatWillNotFinish(t *testing.T) {
	result := (system.Commands{}).Timed(script(t, "sleep 5\n"), 50*time.Millisecond)
	if result.Code == 0 {
		t.Fatal("a command that was killed was reported as working")
	}
}

func TestTimedReturnsTheAnswerOfACommandThatFinishedInTime(t *testing.T) {
	result := (system.Commands{}).Timed(script(t, "echo quick\n"), 10*time.Second)
	if result.Code != 0 || result.Stdout != "quick\n" {
		t.Fatalf("answered %d %q", result.Code, result.Stdout)
	}
}

func TestFeedPutsTheTextOnTheCommandsStdin(t *testing.T) {
	seen := filepath.Join(t.TempDir(), "seen")
	result, err := (system.Commands{}).Feed(script(t, "cat > "+seen+"\n"), "sk-ant-secret")
	if err != nil || result.Code != 0 {
		t.Fatalf("feeding answered %v %d", err, result.Code)
	}
	contents, _ := os.ReadFile(seen)
	if string(contents) != "sk-ant-secret" {
		t.Fatalf("the command read %q", contents)
	}
}

func TestFeedReportsACommandThatCouldNotStart(t *testing.T) {
	if _, err := (system.Commands{}).Feed([]string{missingBinary}, "x"); err == nil {
		t.Fatal("a missing command was reported as started")
	}
}

func TestFeedReportsACommandThatRefused(t *testing.T) {
	result, err := (system.Commands{}).Feed(script(t, "exit 1\n"), "x")
	if err != nil || result.Code != 1 {
		t.Fatalf("answered %v %d", err, result.Code)
	}
}

func TestOnPathFindsACommandThisMachineHas(t *testing.T) {
	if !system.OnPath("git") {
		t.Fatal("git was not found on PATH")
	}
}

func TestOnPathMissesACommandNobodyHas(t *testing.T) {
	if system.OnPath(missingBinary) {
		t.Fatal("a command nobody has was found on PATH")
	}
}

func TestAPipeIsNoTerminal(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close(); _ = writer.Close() }()
	if system.IsTerminal(reader) {
		t.Fatal("a pipe was taken for a terminal")
	}
}

func TestDevNullIsNoTerminal(t *testing.T) {
	empty, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = empty.Close() }()
	// A stat calls /dev/null a character device, which is why box asks the kernel instead.
	if system.IsTerminal(empty) {
		t.Fatal("/dev/null was taken for a terminal")
	}
}

func TestAFileIsNoTerminal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = opened.Close() }()
	if system.IsTerminal(opened) {
		t.Fatal("a file was taken for a terminal")
	}
}
