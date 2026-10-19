package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"

	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/project"
	"github.com/lk16/box/internal/session"
	"github.com/lk16/box/internal/setup"
	"github.com/lk16/box/internal/system"
	"github.com/lk16/box/internal/update"
)

// UsageExit is what a command line box could not read at all exits with, the way a shell expects.
const UsageExit = 2

// CLI is one box invocation: the outside world it reaches, and how it was installed.
type CLI struct {
	Deps system.Deps
	update.Install
}

// Main parses the command line and turns a rejected setup into one message.
func (c CLI) Main(argv []string, workingDirectory string) int {
	arguments, err := Parse(argv)
	if err != nil {
		c.Deps.Console.Warn("%s\n%s", err, Usage)
		return UsageExit
	}
	if arguments.Help {
		c.Deps.Console.Print("%s", Usage)
		return 0
	}
	update.Update{Deps: c.Deps, Install: c.Install}.WarnWhenOutdated()
	code, err := c.dispatch(arguments, workingDirectory)
	if err != nil {
		c.Deps.Console.Warn("box: %s", err)
		return 1
	}
	return code
}

// dispatch runs a setup command, or loads config and hands off to a sandbox session.
func (c CLI) dispatch(arguments Arguments, workingDirectory string) (int, error) {
	// Naming the release needs no sbx, and is what someone asks for when sbx is the problem.
	if arguments.Command == "version" {
		return c.printVersion(arguments)
	}
	// An sbx too old to run comes first: it need not have the diagnose output the next check reads.
	if err := project.RequireSupportedSbx(c.Deps.Run); err != nil {
		return 1, err
	}
	if err := project.RequireMatchingVersions(c.Deps.Run); err != nil {
		return 1, err
	}
	if slices.Contains(config.SetupCommands, arguments.Command) {
		if err := RequireNoFlags(arguments); err != nil {
			return 1, err
		}
		return c.setupCommand(arguments.Command, workingDirectory)
	}
	settings, err := LoadConfig(arguments, workingDirectory)
	if err != nil {
		return 1, err
	}
	proj := project.Build(c.Deps.Run, workingDirectory)
	tokenFile := os.Getenv(config.TokenFileEnv)
	if arguments.Command == "config" {
		return c.showConfig(settings, proj, tokenFile)
	}
	run := session.Session{Deps: c.Deps}
	launch, err := run.PrepareLaunch(settings, proj, tokenFile)
	if err != nil {
		return 1, err
	}
	return run.Run(settings, launch), nil
}

// setupCommand runs a command that needs no settings: one working on the files, or on box itself.
func (c CLI) setupCommand(command, workingDirectory string) (int, error) {
	writer := setup.Setup{Deps: c.Deps}
	switch command {
	case "gen":
		return writer.Generate(workingDirectory)
	case "self-update":
		return update.Update{Deps: c.Deps, Install: c.Install}.SelfUpdate()
	default:
		return writer.MountPrompt(workingDirectory)
	}
}

// printVersion names this copy of box and its platform, the way go version does.
func (c CLI) printVersion(arguments Arguments) (int, error) {
	if err := RequireNoFlags(arguments); err != nil {
		return 1, err
	}
	c.Deps.Console.Print("box version %s %s/%s", c.Install, runtime.GOOS, runtime.GOARCH)
	return 0, nil
}

// showConfig prints the settings in effect, then runs every check a run would make before starting.
func (c CLI) showConfig(settings config.Config, proj project.Project, tokenFile string) (int, error) {
	c.Deps.Console.Print("%s", config.Format(settings, environment(tokenFile)))
	// The settings are printed first, so a rejected project is read next to what it resolved to.
	if err := project.Require(c.Deps.Run, settings, proj, tokenFile); err != nil {
		return 1, err
	}
	return 0, nil
}

// environment is what this machine tells box, next to the settings the project's files hold.
func environment(tokenFile string) config.Environment {
	return config.Environment{
		TokenFile:   tokenFile,
		SecretsFile: os.Getenv(config.SecretsEnv),
		WithoutMCP:  os.Getenv(config.WithoutMCPEnv),
	}
}

// LoadConfig combines the JSON files and the command line into the effective config.
func LoadConfig(arguments Arguments, workingDirectory string) (config.Config, error) {
	fileValues, err := config.ReadConfigFile(filepath.Join(workingDirectory, config.ConfigFile))
	if err != nil {
		return config.Config{}, err
	}
	values := config.Merge(fileValues, arguments.Values)
	members, err := readMembers(workingDirectory, values)
	if err != nil {
		return config.Config{}, err
	}
	own, err := ownMounts(workingDirectory, values)
	if err != nil {
		return config.Config{}, err
	}
	// The flags come last, since they are this one run's rather than anyone's settings.
	mounts := append(append(own, config.MemberMounts(members)...), arguments.Mounts...)
	settings, err := config.Build(values, mounts, members, workingDirectory)
	if err != nil {
		return config.Config{}, err
	}
	return settings.WithoutServers(withoutMCP(arguments)), nil
}

// withoutMCP collects what this run leaves out of the configured mcp list, from both ways of asking.
func withoutMCP(arguments Arguments) config.WithoutMCP {
	return config.WithoutMCP{
		Flag: arguments.WithoutMCP,
		// One variable serves every project here, so it often names servers this one never configured.
		Environment: config.ParseWithoutMCP(os.Getenv(config.WithoutMCPEnv)),
	}
}

// readMembers reads every member a group declares, placed where this machine says each one sits.
func readMembers(workingDirectory string, values config.Values) ([]config.MemberSettings, error) {
	declared, err := config.ReadRepos(workingDirectory, values.Raw[config.Repos])
	if err != nil {
		return nil, err
	}
	return config.ReadMembers(workingDirectory, declared)
}

// ownMounts reads the mounts this directory's .box declares and this machine answers, in order.
func ownMounts(workingDirectory string, values config.Values) ([]string, error) {
	required, err := config.AsDescriptions(values.Raw[config.RequiredMounts])
	if err != nil {
		return nil, err
	}
	provided, err := config.ReadPathsFile(filepath.Join(workingDirectory, config.MountsFile))
	if err != nil {
		return nil, err
	}
	return config.OrderMounts(required, provided)
}
