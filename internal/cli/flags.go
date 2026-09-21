// Package cli reads the command line and hands the run to the command it names.
package cli

import (
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
           [--template REF] [--mount PATH]
           {run,config,gen,mount-prompt,self-update}

Run Claude Code inside a disposable Docker sandbox.

commands:
  run           creates the sandbox and starts Claude in it
  config        prints the settings in effect, then runs every check a run makes
  gen           writes a starter .box directory, a starter kit and the .gitignore lines box needs
  mount-prompt  prints a prompt that has an agent fill in this machine's mount paths
  self-update   replaces this copy of box with the published one

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

gen, mount-prompt and self-update read no settings, so they reject every flag.`

// settingFlags are the flags naming a setting, spelled the way the config key is with hyphens.
var settingFlags = []string{
	"name", "memory", "cpus", "root-size", "docker-size", "model", "prompt-file", "kit", "template",
}

// UsageError is a command line box could not read at all, which exits the way a shell expects.
type UsageError struct {
	Message string
}

// Error names what was wrong with the command line.
func (e *UsageError) Error() string {
	return e.Message
}

// Arguments are one command line: the word box was asked to run, and the settings given with it.
type Arguments struct {
	Command string
	Values  config.Values
	Mounts  []string
	// Given names every flag the user typed, as they typed it, so a message can quote them back.
	Given []string
	// Help says the command line asked for the usage rather than for a command.
	Help bool
}

// mountList collects a repeatable flag, which is the one flag that does not name a setting.
type mountList struct {
	paths *[]string
}

// String renders what the flag has collected, which only the flag package's own help asks for.
func (m mountList) String() string {
	if m.paths == nil {
		return ""
	}
	return strings.Join(*m.paths, " ")
}

// Set adds one more path to the list.
func (m mountList) Set(path string) error {
	*m.paths = append(*m.paths, path)
	return nil
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
		return Arguments{}, &UsageError{Message: "a command is required"}
	}
	if !slices.Contains(config.Commands, command) {
		return Arguments{}, &UsageError{Message: "box has no command named " + command}
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
	var mounts []string
	set.Var(mountList{paths: &mounts}, config.MountFlag, "")
	if err := set.Parse(flags); err != nil {
		return Arguments{}, &UsageError{Message: err.Error()}
	}
	if rest := set.Args(); len(rest) > 0 {
		return Arguments{}, &UsageError{Message: "box takes one command, but also got " + rest[0]}
	}
	return collect(command, set, held, mounts), nil
}

// collect keeps only the flags the user really typed, so an unset one falls back to the file.
func collect(command string, set *flag.FlagSet, held map[string]*string, mounts []string) Arguments {
	arguments := Arguments{Command: command, Values: config.NewValues(), Mounts: mounts}
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
			return "", nil, &UsageError{Message: "box takes one command, but got " + command + " and " + argument}
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
	return slices.Contains(settingFlags, name) || name == config.MountFlag
}

// RequireNoFlags rejects flags passed to a command that reads the config rather than settings.
func RequireNoFlags(arguments Arguments) error {
	if len(arguments.Given) == 0 {
		return nil
	}
	return fail.Errorf("%s takes no flags, but got %s; edit %s instead",
		arguments.Command, strings.Join(arguments.Given, ", "), config.ConfigFile)
}
