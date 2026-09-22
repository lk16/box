package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/fail"
	"github.com/lk16/box/internal/system"
)

// localPath is one file holding this machine's own paths, and why it must stay uncommitted.
type localPath struct {
	name string
	help string
}

// localPaths are what box writes that holds one machine's own files, in the order gen writes them.
var localPaths = []localPath{
	{config.MountsFile, config.MountsIgnoredHelp},
	{config.ReposFile, config.ReposIgnoredHelp},
}

// LocalPathNames lists the files box refuses to run while git could commit them.
func LocalPathNames() []string {
	names := make([]string, 0, len(localPaths))
	for _, path := range localPaths {
		names = append(names, path.name)
	}
	return names
}

// IsGitRepository says whether git reads this directory as a working tree, which sbx --clone needs.
func IsGitRepository(runner system.Runner, directory string) bool {
	return system.Succeeds(runner, git(directory, "rev-parse", "--git-dir"))
}

// hasCommits says whether HEAD names a commit, which a repository nobody has committed to does not.
func hasCommits(runner system.Runner, directory string) bool {
	return system.Succeeds(runner, git(directory, "rev-parse", "--verify", "HEAD"))
}

// RequireGitRepository refuses to run where sbx create --clone would have nothing to clone.
func RequireGitRepository(runner system.Runner, directory string) error {
	if !IsGitRepository(runner, directory) {
		return fail.Errorf("%s", config.NotARepositoryHelp)
	}
	if !hasCommits(runner, directory) {
		return fail.Errorf("%s", config.NoCommitsHelp)
	}
	return nil
}

// IsGitIgnored asks git whether a path is ignored; check-ignore exits 0 only when it is.
func IsGitIgnored(runner system.Runner, directory, relativePath string) bool {
	return system.Succeeds(runner, git(directory, "check-ignore", "-q", relativePath))
}

// RequireIgnoredLocalPaths refuses to run while anything holding this machine's files could be committed.
func RequireIgnoredLocalPaths(runner system.Runner, directory, scope string) error {
	for _, local := range localPaths {
		if _, err := os.Stat(filepath.Join(directory, local.name)); err != nil {
			continue
		}
		if IsGitIgnored(runner, directory, local.name) {
			continue
		}
		return fail.Errorf("%s%s", scope, local.help)
	}
	return nil
}

// MissingBinaries names the commands PATH does not have, in the order box would need them.
func MissingBinaries(names []string) []string {
	var missing []string
	for _, name := range names {
		if !system.OnPath(name) {
			missing = append(missing, name)
		}
	}
	return missing
}

// RequireBinaries refuses to run without the commands box shells out to, rather than at the first one.
func RequireBinaries() error {
	missing := MissingBinaries(config.RequiredBinaries)
	if len(missing) == 0 {
		return nil
	}
	return fail.Errorf(config.MissingBinariesHelp, strings.Join(missing, ", "))
}

// SbxVersionCommand asks sbx for its version, whose line names the release box is talking to.
func SbxVersionCommand() []string {
	return []string{"sbx", "version"}
}

// releaseNumber is the release in what sbx version prints: "sbx version: v0.38.0 <commit>".
var releaseNumber = regexp.MustCompile(`v?(\d+)\.(\d+)\.(\d+)`)

// ParseSbxVersion pulls the release out of sbx version's line, or nothing when it holds none.
func ParseSbxVersion(printed string) []int {
	match := releaseNumber.FindStringSubmatch(printed)
	if match == nil {
		return nil
	}
	found := make([]int, 0, 3)
	for _, part := range match[1:] {
		number, _ := strconv.Atoi(part)
		found = append(found, number)
	}
	return found
}

// ToVersion spells a version the way a release is named, so a message can compare two of them.
func ToVersion(version []int) string {
	parts := make([]string, 0, len(version))
	for _, number := range version {
		parts = append(parts, strconv.Itoa(number))
	}
	return strings.Join(parts, ".")
}

// RequireSupportedSbx refuses an sbx older than the release box's kits and commands are written against.
func RequireSupportedSbx(runner system.Runner) error {
	// No sbx means nothing to read, and the commands that need it reject its absence themselves.
	if !system.OnPath("sbx") {
		return nil
	}
	found := ParseSbxVersion(system.Capture(runner, SbxVersionCommand()))
	// A line box cannot read is no evidence of an old sbx, so it must not stop a working command.
	if len(found) == 0 || slices.Compare(found, config.SbxMinimum) >= 0 {
		return nil
	}
	return fail.Errorf(config.SbxTooOldHelp, ToVersion(found), ToVersion(config.SbxMinimum))
}

// DiagnoseCommand asks sbx for the JSON comparing the CLI with the daemon it talks to.
func DiagnoseCommand() []string {
	return []string{"sbx", "diagnose", "-o", "json"}
}

// diagnoseReport is the part of sbx diagnose's JSON box reads.
type diagnoseReport struct {
	Checks []struct {
		Name    string `json:"name"`
		Status  string `json:"status"`
		Message string `json:"message"`
	} `json:"checks"`
}

// ParseVersionMismatch pulls a failed version check's message out of diagnose JSON, or nothing.
func ParseVersionMismatch(printed string) string {
	var report diagnoseReport
	if err := json.Unmarshal([]byte(printed), &report); err != nil {
		return ""
	}
	for _, check := range report.Checks {
		if check.Name != config.VersionMatchCheck || check.Status == config.CheckPassed {
			continue
		}
		if check.Message == "" {
			return "sbx did not say which versions"
		}
		return check.Message
	}
	return ""
}

// RequireMatchingVersions refuses to run when the sbx CLI and its daemon come from different releases.
func RequireMatchingVersions(runner system.Runner) error {
	// No sbx means nothing to compare, and the commands that need it reject its absence themselves.
	if !system.OnPath("sbx") {
		return nil
	}
	// A daemon that is not running is no mismatch: sbx starts its own version when it needs one.
	mismatch := ParseVersionMismatch(runner.Capture(DiagnoseCommand(), nil).Stdout)
	if mismatch == "" {
		return nil
	}
	return fail.Errorf(config.VersionMismatchHelp, mismatch)
}

// RequireConfigFile sends a project with no box setup at all to the command that writes one.
func RequireConfigFile(workingDirectory string) error {
	if info, err := os.Stat(filepath.Join(workingDirectory, config.ConfigFile)); err == nil && !info.IsDir() {
		return nil
	}
	return fail.Errorf("%s", config.NoConfigHelp)
}

// isFile says whether a path names a file, which is what sbx reads as a zip artifact.
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// RequireSettings rejects settings whose default would be a silent risk rather than a convenience.
func RequireSettings(settings config.Config) error {
	if settings.Kit == "" {
		return fail.Errorf("%s", config.KitHelp)
	}
	// A kit that is not on disk is a reference sbx resolves itself, so only a local file is wrong.
	if isFile(config.ResolvePath(settings.Kit)) {
		return fail.Errorf("%s", config.KitFileHelp)
	}
	for _, member := range settings.Members {
		if isFile(config.ResolvePath(member.Kit)) {
			return fail.Errorf("%s: %s", member.Member.Name, config.KitFileHelp)
		}
	}
	if settings.Model == "" {
		return fail.Errorf("%s", config.ModelHelp)
	}
	return nil
}
