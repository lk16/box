package session

import (
	"os"
	"strings"
	"sync"

	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/fail"
	"github.com/lk16/box/internal/system"
)

// FetchCommand brings one member's origin refs up to date, writing origin/* and nothing else.
func FetchCommand(path string) []string {
	return git(path, "fetch", "origin")
}

// FetchEnvironment is box's own environment with everything a fetch could ask a terminal turned off.
func FetchEnvironment() []string {
	// The fetches run at once, so not one of them may hold the terminal. See docs/fetching.md.
	settings := []string{config.GitPromptEnv + "=0", config.GitSSHCommandEnv + "=" + batchModeSSH()}
	// A command reads the last spelling of a variable, so these override what the user exported.
	return append(os.Environ(), settings...)
}

// batchModeSSH is the ssh a fetch runs, told to fail rather than ask, whatever the user set it to.
func batchModeSSH() string {
	command := os.Getenv(config.GitSSHCommandEnv)
	if command == "" {
		command = "ssh"
	}
	return command + config.BatchModeSSH
}

// fetchMembers brings every member's refs up to date at once, since each fetch waits on a network.
func (s Session) fetchMembers(paths []string) error {
	results := make([]system.Result, len(paths))
	environment := FetchEnvironment()
	var fetching sync.WaitGroup
	for index, path := range paths {
		fetching.Add(1)
		go func() {
			defer fetching.Done()
			results[index] = s.Deps.Run.Capture(FetchCommand(path), environment)
		}()
	}
	fetching.Wait()
	// Nothing is printed until all of them are done, so no member's output lands inside another's.
	for index, path := range paths {
		if err := s.reportFetch(path, results[index]); err != nil {
			return err
		}
	}
	return nil
}

// reportFetch says what one member's fetch did, and asks again with the terminal when it failed.
func (s Session) reportFetch(path string, result system.Result) error {
	if result.Code == 0 {
		s.Deps.Console.Warn("box: fetched %s", path)
		if said := fetchOutput(result); said != "" {
			s.Deps.Console.Warn("%s", said)
		}
		return nil
	}
	// A fetch that ran alone can ask for a passphrase, which is what most of them failed for.
	s.Deps.Console.Warn("box: fetching %s", path)
	if s.Deps.Run.Attach(FetchCommand(path), nil).Code != 0 {
		return fail.Errorf("git fetch origin failed in %s", path)
	}
	return nil
}

// fetchOutput is everything git said about a fetch, which is nothing when it changed nothing.
func fetchOutput(result system.Result) string {
	return strings.TrimRight(result.Stdout+result.Stderr, "\n")
}
