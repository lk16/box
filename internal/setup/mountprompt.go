package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/lk16/box/internal/config"
)

// HostDescription names what a build has to match: the platform and the architecture, never one alone.
func HostDescription() string {
	return runtime.GOOS + " " + runtime.GOARCH
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
	s.Deps.Console.Print("%s", BuildMountPrompt(required, names, HostDescription(), DepsPath()))
	return 0, nil
}
