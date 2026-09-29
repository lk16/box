// Package update says whether a newer box is published, and takes it when asked to.
package update

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/fail"
	"github.com/lk16/box/internal/system"
)

// Where box is published, and where the check asks what the newest release is. See docs/updates.md.
const (
	ModulePath  = "github.com/lk16/box"
	InstallPath = ModulePath + "/cmd/box"
	DefaultURL  = "https://proxy.golang.org/" + ModulePath + "/@latest"
	ReleasesURL = "https://" + ModulePath + "/releases"
)

// Devel is what go build stamps a binary with, which is a checkout rather than an install.
const Devel = "(devel)"

// The build settings only a binary built from a checkout carries.
const (
	vcsRevision = "vcs.revision"
	vcsModified = "vcs.modified"
)

// How often the check runs, and how long it waits, so a command is never held up by it.
const (
	Interval = time.Hour
	Timeout  = 2 * time.Second
)

// The update notice competes with whatever the agent prints, so it is coloured to stand out.
const (
	Red   = "\033[31m"
	Reset = "\033[0m"
)

// Update is the check that says whether a newer box is published, and the command that takes it.
type Update struct {
	Deps system.Deps
	Install
}

// Install is how this box came to be on the machine, which decides how it is updated.
type Install struct {
	// Version is the release this box was installed as, or nothing when go build made it.
	Version string
	// Downloaded says the release page supplied this binary, so go install must not replace it.
	Downloaded bool
	// Revision is the commit a checkout was built from, marked -dirty when it had uncommitted changes.
	Revision string
}

// String is what box version names this copy as, the way go version names a devel toolchain.
func (i Install) String() string {
	if i.Version != "" {
		return i.Version
	}
	return strings.TrimSpace("devel " + i.Revision)
}

// Identify says how this box was installed: by the release build that stamped it, or by go itself.
func Identify(info *debug.BuildInfo, stamped string) Install {
	if stamped != "" {
		return Install{Version: stamped, Downloaded: true}
	}
	return Install{Version: Release(info), Revision: revision(info)}
}

// revision is the short commit a checkout build came from, or nothing for any other build.
func revision(info *debug.BuildInfo) string {
	if info == nil {
		return ""
	}
	commit, dirty := "", ""
	for _, setting := range info.Settings {
		switch {
		case setting.Key == vcsRevision:
			commit = setting.Value[:min(7, len(setting.Value))]
		case setting.Key == vcsModified && setting.Value == "true":
			dirty = "-dirty"
		}
	}
	if commit == "" {
		return ""
	}
	return commit + dirty
}

// DownloadURL is where the release page serves the newest binary built for this platform.
func DownloadURL() string {
	return ReleasesURL + "/latest/download/box-" + runtime.GOOS + "-" + runtime.GOARCH
}

// URL is where the newest release is read from, which a fork or a vendored copy can point elsewhere.
func URL() string {
	if named, set := os.LookupEnv(config.UpdateURLEnv); set {
		return named
	}
	return DefaultURL
}

// CachePath is where the check remembers what it last saw, following XDG_CACHE_HOME.
func CachePath() string {
	if base := os.Getenv(config.CacheHomeEnv); base != "" {
		return filepath.Join(base, "box", "update-check.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".cache", "box", "update-check.json")
	}
	return filepath.Join(home, ".cache", "box", "update-check.json")
}

// IsInstalled says whether this box came from go install, which is the only copy worth nagging.
func IsInstalled(version string) bool {
	return version != ""
}

// Release is what a box was installed as: what go install stamped, or nothing for a checkout.
func Release(info *debug.BuildInfo) string {
	if info == nil {
		return ""
	}
	// Only a build from a checkout carries VCS data, and that copy is someone's work in progress.
	for _, setting := range info.Settings {
		if setting.Key == vcsRevision {
			return ""
		}
	}
	if info.Main.Version == Devel {
		return ""
	}
	return info.Main.Version
}

// ParseLatest pulls the newest release out of what the module proxy answered.
func ParseLatest(body []byte) (string, error) {
	var answer struct {
		Version string `json:"Version"`
	}
	if err := json.Unmarshal(body, &answer); err != nil || answer.Version == "" {
		return "", fail.Errorf("the answer names no release")
	}
	return answer.Version, nil
}

// Newer says whether latest is a later vX.Y.Z than current, since the proxy's @latest lags a release.
func Newer(latest, current string) bool {
	var have, want [3]int
	if _, err := fmt.Sscanf(current, "v%d.%d.%d", &have[0], &have[1], &have[2]); err != nil {
		return false
	}
	if _, err := fmt.Sscanf(latest, "v%d.%d.%d", &want[0], &want[1], &want[2]); err != nil {
		return false
	}
	return slices.Compare(want[:], have[:]) > 0
}

// checkTime is what the cache holds: when the last check ran.
type checkTime struct {
	CheckedAt time.Time `json:"checked_at"`
}

// CheckedRecently says whether the last check is fresh enough that this run has nothing to add.
func CheckedRecently(path string, now time.Time) bool {
	contents, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var stored checkTime
	if err := json.Unmarshal(contents, &stored); err != nil || stored.CheckedAt.IsZero() {
		return false
	}
	return now.Sub(stored.CheckedAt) <= Interval
}

// StoreCheckTime remembers when the check ran, so the rest of the hour is quiet.
func StoreCheckTime(path string, now time.Time) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	stored, err := json.Marshal(checkTime{CheckedAt: now})
	if err != nil {
		return err
	}
	return os.WriteFile(path, stored, 0o644)
}

// InstallCommand is how box replaces itself, which is the line the README installs it with.
func InstallCommand() []string {
	return []string{"go", "install", InstallPath + "@latest"}
}
