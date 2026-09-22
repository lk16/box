package setup_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/lk16/box/internal/boxtest"
	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/setup"
	"github.com/lk16/box/internal/system"
)

// goToolchain is the one declared mount these tests ask an agent to fill in.
var goToolchain = config.Pairs{{Name: "go", Value: "the Go toolchain"}}

// prompt renders the mount prompt for one declared mount, which is what most of these check.
func prompt() string {
	return setup.BuildMountPrompt(goToolchain, []string{"go"}, "darwin arm64", "~/.box/deps")
}

func TestDepsPathFollowsXDGDataHome(t *testing.T) {
	t.Setenv(config.DataHomeEnv, "/tmp/xdg")
	if got := setup.DepsPath(); got != "/tmp/xdg/box/deps" {
		t.Fatalf("deps land in %q", got)
	}
}

func TestDepsPathFallsBackToADotBoxInTheHomeDirectory(t *testing.T) {
	t.Setenv(config.DataHomeEnv, "")
	t.Setenv("HOME", "/home/someone")
	if got := setup.DepsPath(); got != "/home/someone/.box/deps" {
		t.Fatalf("deps land in %q", got)
	}
}

func TestThePromptCarriesTheNamesAndDescriptions(t *testing.T) {
	required := config.Pairs{{Name: "go", Value: "the Go toolchain"}, {Name: "cargo", Value: "the cargo home"}}
	rendered := setup.BuildMountPrompt(required, []string{"go", "cargo"}, "darwin arm64", "~/.box/deps")
	for _, part := range []string{"go: the Go toolchain", "cargo: the cargo home"} {
		if !strings.Contains(rendered, part) {
			t.Errorf("%q is missing from the prompt", part)
		}
	}
}

func TestThePromptNamesTheFileThePlaceholderAndTheHost(t *testing.T) {
	for _, part := range []string{config.MountsFile, config.MountPlaceholder, "darwin arm64"} {
		if !strings.Contains(prompt(), part) {
			t.Errorf("%q is missing from the prompt", part)
		}
	}
}

func TestThePromptHoldsTheRulesBoxEnforces(t *testing.T) {
	for _, part := range []string{
		":rw",
		"Never guess",
		"adding the key where it is missing",
		"~/.box/deps/",
		"Never give a mount a path inside this project directory",
		"The sandbox runs Linux",
	} {
		if !strings.Contains(prompt(), part) {
			t.Errorf("%q is missing from the prompt", part)
		}
	}
}

func TestTheHostDescriptionHoldsThePlatformAndTheArchitectureUnameNames(t *testing.T) {
	run := newFixture(false)
	uname := strings.TrimSpace(system.Capture(system.Commands{}, []string{"uname", "-m"}))
	if got := run.setup.HostDescription(); got != runtime.GOOS+" "+uname {
		t.Fatalf("the host is described as %q, want %q", got, runtime.GOOS+" "+uname)
	}
}

func TestTheHostDescriptionFallsBackToGoOwnArchitecture(t *testing.T) {
	run := newFixture(false)
	run.setup.Deps.Run = &boxtest.Runner{}
	if got := run.setup.HostDescription(); got != runtime.GOOS+" "+runtime.GOARCH {
		t.Fatalf("the host is described as %q", got)
	}
}

// mountPrompt runs box mount-prompt in one directory and hands back what it printed.
func mountPrompt(t *testing.T, directory string) *fixture {
	t.Helper()
	run := newFixture(false)
	code, err := run.setup.MountPrompt(directory)
	if err != nil || code != 0 {
		t.Fatalf("mount-prompt exited %d, %v", code, err)
	}
	return run
}

func TestMountPromptNeedsNoMountsFileAtAll(t *testing.T) {
	directory := t.TempDir()
	writeConfig(t, directory, `{"required_mounts": {"go": "the Go toolchain"}}`)
	run := mountPrompt(t, directory)
	if !strings.Contains(run.console.Printed(), "go: the Go toolchain") {
		t.Fatalf("mount-prompt printed:\n%s", run.console.Printed())
	}
}

func TestMountPromptAsksOnlyAboutUnfilledMounts(t *testing.T) {
	directory := t.TempDir()
	writeConfig(t, directory, `{"required_mounts": {"go": "the Go toolchain", "cargo": "the cargo home"}}`)
	boxtest.WriteFile(t, filepath.Join(directory, config.MountsFile),
		`{"go": "/usr/local/go", "cargo": "`+config.MountPlaceholder+`"}`)
	printed := mountPrompt(t, directory).console.Printed()
	if !strings.Contains(printed, "cargo: the cargo home") || strings.Contains(printed, "go: the Go toolchain") {
		t.Fatalf("mount-prompt printed:\n%s", printed)
	}
}

func TestMountPromptPrintsNothingWhenEveryMountHasAPath(t *testing.T) {
	directory := t.TempDir()
	writeConfig(t, directory, `{"required_mounts": {"go": "the Go toolchain"}}`)
	boxtest.WriteFile(t, filepath.Join(directory, config.MountsFile), `{"go": "/usr/local/go"}`)
	run := mountPrompt(t, directory)
	if run.console.Printed() != "" {
		t.Fatalf("mount-prompt printed:\n%s", run.console.Printed())
	}
	if !strings.Contains(run.console.Warned(), "already has a path") {
		t.Fatalf("mount-prompt warned:\n%s", run.console.Warned())
	}
}

func TestMountPromptPrintsNothingWithoutDeclaredMounts(t *testing.T) {
	if got := mountPrompt(t, t.TempDir()).console.Printed(); got != "" {
		t.Fatalf("mount-prompt printed:\n%s", got)
	}
}

func TestMountPromptWritesNoMountsFile(t *testing.T) {
	directory := t.TempDir()
	writeConfig(t, directory, `{"required_mounts": {"go": "the Go toolchain"}}`)
	mountPrompt(t, directory)
	if _, err := os.Stat(filepath.Join(directory, config.MountsFile)); err == nil {
		t.Fatal("mount-prompt wrote a mounts file")
	}
}

func TestMountPromptNamesThePlatformAndArchitectureThisMachineRuns(t *testing.T) {
	directory := t.TempDir()
	writeConfig(t, directory, `{"required_mounts": {"go": "the Go toolchain"}}`)
	if !strings.Contains(mountPrompt(t, directory).console.Printed(), setupFor(t).HostDescription()) {
		t.Fatal("the host is missing from the prompt")
	}
}

func TestReadRequiredMountsReadsTheDeclaration(t *testing.T) {
	directory := t.TempDir()
	writeConfig(t, directory, `{"required_mounts": {"go": "the Go toolchain"}}`)
	read, err := setup.ReadRequiredMounts(directory)
	if err != nil || read.Get("go") != "the Go toolchain" {
		t.Fatalf("read %v, %v", read, err)
	}
}

func TestReadRequiredMountsIsEmptyWithoutAConfig(t *testing.T) {
	read, err := setup.ReadRequiredMounts(t.TempDir())
	if err != nil || len(read) != 0 {
		t.Fatalf("read %v, %v", read, err)
	}
}

// setupFor is a setup that really runs commands, for the host description a test compares with.
func setupFor(t *testing.T) setup.Setup {
	t.Helper()
	return newFixture(false).setup
}
