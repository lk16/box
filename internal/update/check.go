package update

import (
	"os"
	"strings"

	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/fail"
	"github.com/lk16/box/internal/system"
)

// InRed colours a notice, unless stderr is no terminal or NO_COLOR asked for plain text.
func InRed(console system.Console, text string) string {
	if os.Getenv(config.NoColourEnv) != "" {
		return text
	}
	// A pipe, a log file or a CI capture would otherwise be handed the escape codes as characters.
	if !console.Terminal {
		return text
	}
	return Red + text + Reset
}

// Message says an update is available, and how to take it, when this copy is not the newest one.
func Message(console system.Console, current, latest string) string {
	if latest == current {
		return ""
	}
	if !system.OnPath("go") {
		return InRed(console, "An update to box is available, but go is not on PATH to take it.")
	}
	return InRed(console, "An update to box is available. Take it with:\n  box self-update")
}

// WarnWhenOutdated mentions a newer box on stderr once an hour, staying silent about every failure.
func (u Update) WarnWhenOutdated() {
	url := URL()
	// A copy with no update URL has nowhere to compare itself with, so there is nothing to say.
	if url == "" {
		return
	}
	// A box built in a checkout is being worked on, and git is how that one is updated.
	if !IsInstalled(u.Version) {
		return
	}
	path := CachePath()
	now := u.Deps.Now()
	if CheckedRecently(path, now) {
		return
	}
	// A failed check is still a check, so the hour it buys must not depend on the proxy answering.
	_ = StoreCheckTime(path, now)
	body, err := u.Deps.Download.Get(url)
	if err != nil {
		return
	}
	latest, err := ParseLatest(body)
	if err != nil {
		return
	}
	if message := Message(u.Deps.Console, u.Version, latest); message != "" {
		u.Deps.Console.Warn("%s", message)
	}
}

// SelfUpdate replaces this box with the published release, which go install does the whole of.
func (u Update) SelfUpdate() (int, error) {
	url := URL()
	if url == "" {
		return 1, fail.Errorf("%s is set to nothing, so there is nowhere to update from", config.UpdateURLEnv)
	}
	// A box built in a checkout is the copy someone is working on, and git is how that is updated.
	if !IsInstalled(u.Version) {
		return 1, fail.Errorf("go build made this box, so this is a checkout rather than an install")
	}
	latest, err := u.latest(url)
	if err != nil {
		return 1, err
	}
	if latest == u.Version {
		u.Deps.Console.Print("kept    box %s, which is already the published release", u.Version)
		return 0, nil
	}
	if !system.OnPath("go") {
		return 1, fail.Errorf("go is not on PATH, and box installs itself with go install")
	}
	if u.Deps.Run.Attach(InstallCommand(), nil).Code != 0 {
		return 1, fail.Errorf("%s failed, so box is still %s", join(InstallCommand()), u.Version)
	}
	u.Deps.Console.Print("updated box to %s", latest)
	return 0, nil
}

// latest reads the newest published release, naming the URL whatever went wrong.
func (u Update) latest(url string) (string, error) {
	body, err := u.Deps.Download.Get(url)
	if err != nil {
		return "", fail.Errorf("could not read %s: %s", url, err)
	}
	if len(body) == 0 {
		return "", fail.Errorf("%s served an empty answer, which names no release", url)
	}
	latest, err := ParseLatest(body)
	if err != nil {
		return "", fail.Errorf("could not read %s: %s", url, err)
	}
	return latest, nil
}

// join renders a command the way a message quotes it back to the reader.
func join(command []string) string {
	return strings.Join(command, " ")
}
