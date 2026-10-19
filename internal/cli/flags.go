// Package cli reads the command line and hands the run to the command it names.
package cli

import (
	"errors"
	"flag"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/fail"
)

// Usage is what box prints when the command line names no command it knows.
const Usage = `usage: box [-h] [--name NAME] [--memory SIZE] [--cpus N] [--root-size SIZE]
           [--docker-size SIZE] [--model MODEL] [--prompt-file PATH] [--kit REF]
           [--template REF] [--mount PATH] [--without-mcp NAME]
           {run,config,gen,mount-prompt,self-update,version}

Run Claude Code inside a disposable Docker sandbox.

commands:
  run           creates the sandbox and starts Claude in it
  config        prints the settings in effect, then runs every check a run makes
  gen           writes a starter .box directory, a starter kit and the .gitignore lines box needs
  mount-prompt  prints a prompt that has an agent fill in this machine's mount paths
  self-update   replaces this copy of box with the published one
  version       prints the release this copy of box is

options:
  -h, --help          show this help message and exit
  --name NAME         sandbox base name
  --memory SIZE       memory limit, e.g. 4g
  --cpus N            number of CPUs
  --root-size SIZE    sandbox root filesystem size
  --docker-size SIZE  sandbox docker size
  --model MODEL       Claude model to run
  --prompt-file PATH  file added to the prompt
  --kit REF           sbx kit reference
  --template REF      sbx template the sandbox image comes from
  --mount PATH        read-only workspace, :rw to write
  --without-mcp NAME  configured MCP server to leave out of this run

gen, mount-prompt, self-update and version read no settings, so they reject every flag.`

// settingFlags are the flags naming a setting, spelled the way the config key is with hyphens.
var settingFlags = []string{
	"name", "memory", "cpus", "root-size", "docker-size", "model", "prompt-file", "kit", "template",
}

// repeatableFlags may be given again rather than naming one setting, so each one collects a list.
var repeatableFlags = []string{config.MountFlag, config.WithoutMCPFlag}

// Arguments are one command line: the word box was asked to run, and the settings given with it.
type Arguments struct {
	Command string
	Values  config.Values
	Mounts  []string
	// WithoutMCP names the configured mcp servers this run leaves out, which no config key holds.
	WithoutMCP []string
	// Given names every flag the user typed, as they typed it, so a message can quote them back.
	Given []string
	// Help says the command line asked for the usage rather than for a command.
	Help bool
}

// Parse reads a command line, taking flags on either side of the command word.
func Parse(argv []string) (Arguments, error) {
	command, flags, err := split(argv)
	if err != nil {
		return Arguments{}, err
	}
	if slices.Contains(flags, "-h") || slices.Contains(flags, "--help") {
		return Arguments{Help: true}, nil
	}
	if command == "" {
		return Arguments{}, errors.New("a command is required")
	}
	if !slices.Contains(config.Commands, command) {
		return Arguments{}, errors.New("box has no command named " + command)
	}
	return read(command, flags)
}

// read hands the flags on either side of the command to the flag package, as one list.
func read(command string, flags []string) (Arguments, error) {
	set := flag.NewFlagSet("box", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	held := map[string]*string{}
	for _, name := range settingFlags {
		held[name] = set.String(name, "", "")
	}
	// The repeatable flags collect this one run's own values rather than naming a setting.
	arguments := Arguments{Command: command, Values: config.NewValues()}
	set.Func(config.MountFlag, "", func(path string) error {
		arguments.Mounts = append(arguments.Mounts, path)
		return nil
	})
	set.Func(config.WithoutMCPFlag, "", func(name string) error {
		// Nothing downstream reads a name box drops, so an empty one is caught where it was typed.
		if name == "" {
			return errors.New("a server name is required")
		}
		arguments.WithoutMCP = append(arguments.WithoutMCP, name)
		return nil
	})
	if err := set.Parse(flags); err != nil {
		return Arguments{}, err
	}
	if rest := set.Args(); len(rest) > 0 {
		return Arguments{}, errors.New("box takes one command, but also got " + rest[0])
	}
	return collect(set, held, arguments), nil
}

// collect keeps only the flags the user really typed, so an unset one falls back to the file.
func collect(set *flag.FlagSet, held map[string]*string, arguments Arguments) Arguments {
	set.Visit(func(given *flag.Flag) {
		arguments.Given = append(arguments.Given, "--"+given.Name)
		if value, names := held[given.Name]; names {
			arguments.Values.Text[ConfigKey(given.Name)] = *value
		}
	})
	sort.Strings(arguments.Given)
	return arguments
}

// ConfigKey is the config key one flag names, which is its own name with underscores.
func ConfigKey(flagName string) string {
	return strings.ReplaceAll(flagName, "-", "_")
}

// split separates the command word from the flags written before and after it.
func split(argv []string) (string, []string, error) {
	command := ""
	var flags []string
	for index := 0; index < len(argv); index++ {
		argument := argv[index]
		if strings.HasPrefix(argument, "-") && argument != "-" {
			flags = append(flags, argument)
			// Every flag box knows takes a value, which the next argument holds when no = does.
			if takesValue(argument) && index+1 < len(argv) {
				index++
				flags = append(flags, argv[index])
			}
			continue
		}
		if command != "" {
			return "", nil, errors.New("box takes one command, but got " + command + " and " + argument)
		}
		command = argument
	}
	return command, flags, nil
}

// takesValue says whether a flag reads the next argument as its value.
func takesValue(argument string) bool {
	if strings.Contains(argument, "=") {
		return false
	}
	name := strings.TrimLeft(argument, "-")
	return slices.Contains(settingFlags, name) || slices.Contains(repeatableFlags, name)
}

// RequireNoFlags rejects flags passed to a command that reads the config rather than settings.
func RequireNoFlags(arguments Arguments) error {
	if len(arguments.Given) == 0 {
		return nil
	}
	return fail.Errorf("%s takes no flags, but got %s; edit %s instead",
		arguments.Command, strings.Join(arguments.Given, ", "), config.ConfigFile)
}
