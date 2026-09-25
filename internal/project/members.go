package project

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/fail"
	"github.com/lk16/box/internal/system"
)

// MemberOrigin reads a member's origin URL, so its clone names the same project the host copy does.
func MemberOrigin(runner system.Runner, path string) string {
	return strings.TrimSpace(system.Capture(runner, Git(path, "remote", "get-url", "origin")))
}

// requireOrigin refuses a path holding some other repository than the member is declared as.
func requireOrigin(runner system.Runner, member config.Member, path, reposFile string) error {
	origin := MemberOrigin(runner, path)
	if origin == "" {
		return fail.Errorf("%s sits at %s, which has no origin remote", member.Name, path)
	}
	// How a teammate spells the URL is theirs, so only the host and the path have to agree.
	if config.NormalizeOrigin(origin) == config.NormalizeOrigin(member.GitOrigin) {
		return nil
	}
	return fail.Errorf(config.OriginMismatchHelp,
		member.Name, path, origin, member.GitOrigin, member.Name, reposFile, member.GitOrigin)
}

// requireUnmounted refuses a mount that would hand the sandbox everything a member holds, tracked or not.
func requireUnmounted(settings config.Config, member config.Member, path string) error {
	for _, workspace := range settings.Mounts {
		directory := config.MountTarget(workspace)
		if config.IsInside(path, directory) {
			return fail.Errorf(config.MemberMountedHelp, member.Name, path, directory)
		}
	}
	return nil
}

// RequireMembers refuses a member box could not clone, or whose path holds another repository.
func RequireMembers(runner system.Runner, settings config.Config, project Project) error {
	reposFile := filepath.Join(project.WorkingDirectory, config.ReposFile)
	for _, member := range settings.Repos {
		if err := requireMember(runner, settings, project, member, reposFile); err != nil {
			return err
		}
	}
	return nil
}

// requireMember runs every check one member has to pass before a sandbox is created for it.
func requireMember(runner system.Runner, settings config.Config, project Project, member config.Member, reposFile string) error {
	path := config.MemberPath(project.WorkingDirectory, member)
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return fail.Errorf("%s puts %s at %s, which is no directory on this machine", reposFile, member.Name, path)
	}
	if !IsGitRepository(runner, path) {
		return fail.Errorf("%s sits at %s, which is not a git repository", member.Name, path)
	}
	if err := requireOrigin(runner, member, path, reposFile); err != nil {
		return err
	}
	if config.IsInside(path, project.Root) {
		return fail.Errorf(config.MemberInsideHelp, member.Name, path, project.Root)
	}
	if err := requireUnmounted(settings, member, path); err != nil {
		return err
	}
	return RequireIgnoredLocalPaths(runner, path, path+": ")
}

// RequireSecretOutside refuses a file of secrets the sandbox could read for itself.
func RequireSecretOutside(variable, secretFile string, reachable []string) error {
	if secretFile == "" {
		return nil
	}
	path := config.ResolvePath(secretFile)
	for _, directory := range reachable {
		if config.IsInside(path, directory) {
			return fail.Errorf(config.SecretInsideHelp, variable, path, directory)
		}
	}
	return nil
}

// RequireSecrets refuses secrets the sandbox could read itself, and declarations with no value here.
func RequireSecrets(settings config.Config, project Project, tokenFile string) error {
	reachable := ReachablePaths(settings, project)
	if err := RequireSecretOutside(config.TokenFileEnv, tokenFile, reachable); err != nil {
		return err
	}
	// An unset BOX_SECRETS_FILE is only missing when the project declares secrets to read from it.
	if len(settings.SecretHosts) == 0 {
		return nil
	}
	secretsFile := os.Getenv(config.SecretsEnv)
	if err := RequireSecretOutside(config.SecretsEnv, secretsFile, reachable); err != nil {
		return err
	}
	_, err := config.ReadSecretValues(settings.SecretHosts, secretsFile)
	return err
}

// Require runs every check on the settings and the project that does not create anything.
func Require(runner system.Runner, settings config.Config, project Project, tokenFile string) error {
	// Nothing box does works without these, so they come before anything about this project.
	if err := RequireBinaries(); err != nil {
		return err
	}
	// A first-timer has no settings to be told about yet, so the missing file comes next.
	if err := RequireConfigFile(project.WorkingDirectory); err != nil {
		return err
	}
	if err := RequireSettings(settings); err != nil {
		return err
	}
	if err := RequireGitRepository(runner, project.WorkingDirectory); err != nil {
		return err
	}
	if err := RequireIgnoredLocalPaths(runner, project.WorkingDirectory, ""); err != nil {
		return err
	}
	if err := RequireMembers(runner, settings, project); err != nil {
		return err
	}
	return RequireSecrets(settings, project, tokenFile)
}
