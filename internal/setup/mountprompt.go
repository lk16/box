package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/lk16/box/internal/system"

	"github.com/lk16/box/internal/config"
)

// HostDescription names what a build has to match: the platform and the architecture, never one alone.
func (s Setup) HostDescription() string {
	return runtime.GOOS + " " + s.machine()
}

// machine is the architecture the way uname spells it, which is what the agent will see itself.
func (s Setup) machine() string {
	printed := strings.TrimSpace(system.Capture(s.Deps.Run, []string{"uname", "-m"}))
	// A machine without uname is no reason to print nothing, and Go's own name is close enough.
	if printed == "" {
		return runtime.GOARCH
	}
	return printed
}

// DepsPath is where a dependency lands that this machine cannot supply, following XDG_DATA_HOME.
func DepsPath() string {
	if base := os.Getenv(config.DataHomeEnv); base != "" {
		return filepath.Join(base, "box", "deps")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(config.BoxDir, "deps")
	}
	return filepath.Join(home, ".box", "deps")
}

// BuildMountPrompt renders the prompt that has an agent on this host fill in the mounts file.
func BuildMountPrompt(required config.Pairs, names []string, host, deps string) string {
	return fmt.Sprintf(config.MountPromptText, host, config.DescribeMounts(required, names), deps)
}

// MountPrompt prints the prompt for filling in this machine's mounts, or nothing when none are missing.
func (s Setup) MountPrompt(workingDirectory string) (int, error) {
	required, err := ReadRequiredMounts(workingDirectory)
	if err != nil {
		return 1, err
	}
	provided, err := config.ReadPathsFile(filepath.Join(workingDirectory, config.MountsFile))
	if err != nil {
		return 1, err
	}
	names := config.UnfilledMounts(required, provided)
	if len(names) == 0 {
		s.Deps.Console.Warn("every mount in %s already has a path", config.MountsFile)
		return 0, nil
	}
	s.Deps.Console.Print("%s", BuildMountPrompt(required, names, s.HostDescription(), DepsPath()))
	return 0, nil
}
