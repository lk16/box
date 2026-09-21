package system_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

// missingBinary is a name no machine has, which is how a command that cannot start is asked for.
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

func TestACtrlCExitsWithWhatAShellReportsForOne(t *testing.T) {
	console := &bytes.Buffer{}
	var exited []int
	signals := make(chan os.Signal, 1)
	signals <- os.Interrupt
	close(signals)
	system.Interrupts{
		Console:  system.Console{Out: console, Err: console},
		Attached: func() bool { return false },
		Exit:     func(code int) { exited = append(exited, code) },
	}.Watch(signals)
	if len(exited) != 1 || exited[0] != system.Interrupted {
		t.Fatalf("exited %v", exited)
	}
	if !strings.Contains(console.String(), "box: interrupted.") {
		t.Fatalf("said %q", console.String())
	}
}

func TestACtrlCWhileAChildOwnsTheTerminalIsLeftToTheChild(t *testing.T) {
	console := &bytes.Buffer{}
	var exited []int
	signals := make(chan os.Signal, 1)
	signals <- os.Interrupt
	close(signals)
	system.Interrupts{
		Console:  system.Console{Out: console, Err: console},
		Attached: func() bool { return true },
		Exit:     func(code int) { exited = append(exited, code) },
	}.Watch(signals)
	if len(exited) != 0 || console.String() != "" {
		t.Fatalf("exited %v and said %q", exited, console.String())
	}
}

func TestAnAttachedChildIsCountedWhileItRuns(t *testing.T) {
	if system.ChildAttached() {
		t.Fatal("a child was counted before one was started")
	}
	held := make(chan bool, 1)
	go func() {
		// The script outlives the poll below, so the count has to be up while it runs.
		(system.Commands{}).Attach(script(t, "sleep 0.5\n"), nil)
		held <- true
	}()
	attached := false
	for attempt := 0; attempt < 50 && !attached; attempt++ {
		attached = system.ChildAttached()
		time.Sleep(10 * time.Millisecond)
	}
	<-held
	if !attached {
		t.Fatal("a child holding the terminal was never counted")
	}
	if system.ChildAttached() {
		t.Fatal("a child was still counted after it finished")
	}
}

func TestPrintWritesOneLineToStdout(t *testing.T) {
	var out, problems bytes.Buffer
	console := system.Console{Out: &out, Err: &problems}
	console.Print("wrote %s", "the file")
	if out.String() != "wrote the file\n" || problems.Len() != 0 {
		t.Fatalf("printed %q and warned %q", out.String(), problems.String())
	}
}

func TestGetReadsWhatTheURLServes(t *testing.T) {
	served := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"Version": "v0.2.0"}`))
	}))
	defer served.Close()
	body, err := system.Web{Timeout: 5 * time.Second}.Get(served.URL)
	if err != nil || string(body) != `{"Version": "v0.2.0"}` {
		t.Fatalf("read %q, %v", body, err)
	}
}

func TestGetRefusesAnAnswerThatIsNotASuccess(t *testing.T) {
	served := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusForbidden)
	}))
	defer served.Close()
	_, err := system.Web{Timeout: 5 * time.Second}.Get(served.URL)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("answered %v", err)
	}
}

func TestGetReportsAURLItCannotReach(t *testing.T) {
	if _, err := (system.Web{Timeout: time.Second}).Get("http://127.0.0.1:1/nothing"); err == nil {
		t.Fatal("an unreachable URL was read")
	}
}

func TestTheTerminalReadsTheLineTypedBackAtIt(t *testing.T) {
	withStdin(t, "2\n")
	prompter := &system.Terminal{}
	if answer, asked := prompter.Ask("one or two? "); !asked || answer != "2" {
		t.Fatalf("answered %q, %v", answer, asked)
	}
}

func TestTheTerminalKeepsItsPlaceAcrossQuestions(t *testing.T) {
	withStdin(t, "yes\n2\n")
	prompter := &system.Terminal{}
	first, _ := prompter.Ask("one or two? ")
	second, asked := prompter.Ask("one or two? ")
	if first != "yes" || second != "2" || !asked {
		t.Fatalf("answered %q then %q", first, second)
	}
}

func TestTheTerminalReportsInputThatEnded(t *testing.T) {
	withStdin(t, "")
	if _, asked := (&system.Terminal{}).Ask("one or two? "); asked {
		t.Fatal("an ended input was read as an answer")
	}
}

func TestAPipeIsNobodyToAsk(t *testing.T) {
	withStdin(t, "2\n")
	if (&system.Terminal{}).Interactive() {
		t.Fatal("a pipe was taken for someone to ask")
	}
}

// withStdin points os.Stdin at a pipe holding the given text for the length of one test.
func withStdin(t *testing.T, contents string) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.WriteString(contents); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	was := os.Stdin
	os.Stdin = reader
	t.Cleanup(func() { os.Stdin = was; _ = reader.Close() })
}
