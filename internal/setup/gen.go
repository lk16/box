package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/jsonx"
	"github.com/lk16/box/internal/project"
	"github.com/lk16/box/internal/system"
)

// Setup is the outside world box gen and box mount-prompt reach the project through.
type Setup struct {
	Deps system.Deps
}

// Generate writes a starter .box directory, adding what is missing and keeping what is filled in.
func (s Setup) Generate(workingDirectory string) (int, error) {
	starter, answered := s.chooseStarterConfig(workingDirectory)
	if !answered {
		s.Deps.Console.Warn("box: nothing was answered, so nothing was written.")
		return 1, nil
	}
	if err := os.MkdirAll(filepath.Join(workingDirectory, config.BoxDir), 0o755); err != nil {
		return 1, err
	}
	if err := s.writeStarterConfig(filepath.Join(workingDirectory, config.ConfigFile), starter); err != nil {
		return 1, err
	}
	if err := s.writeStarterKit(workingDirectory); err != nil {
		return 1, err
	}
	if err := s.writeMounts(workingDirectory); err != nil {
		return 1, err
	}
	if err := s.writeRepos(workingDirectory); err != nil {
		return 1, err
	}
	if err := s.ignoreLocalPaths(workingDirectory); err != nil {
		return 1, err
	}
	if _, group := starter.Get(config.Repos); group {
		s.Deps.Console.Print("%s", config.GroupNextStep)
	}
	return 0, nil
}

// chooseStarterConfig picks the defaults box gen writes, asking only where there is someone to answer.
func (s Setup) chooseStarterConfig(workingDirectory string) (jsonx.Object, bool) {
	// A config that is already there is kept whatever the answer would be, so nobody is asked.
	if isFile(filepath.Join(workingDirectory, config.ConfigFile)) || !s.Deps.Ask.Interactive() {
		return StarterConfig(), true
	}
	return s.askWhatThisIsFor()
}

// askWhatThisIsFor asks which defaults to write until the answer is one of the two, or input ends.
func (s Setup) askWhatThisIsFor() (jsonx.Object, bool) {
	for {
		answer, asked := s.Deps.Ask.Ask(config.GroupQuestion)
		if !asked {
			return nil, false
		}
		switch strings.TrimSpace(answer) {
		case "", "1":
			return StarterConfig(), true
		case "2":
			return GroupConfig(), true
		}
	}
}

// writeStarterConfig writes every setting at its default, unless the project already has a config.
func (s Setup) writeStarterConfig(path string, starter jsonx.Object) error {
	return s.write(path, config.ConfigFile, jsonx.Write(starter), isFile(path))
}

// writeStarterKit writes a policy allowing the agent's own API calls, unless the project has one.
func (s Setup) writeStarterKit(workingDirectory string) error {
	path := filepath.Join(workingDirectory, config.KitSpecFile)
	spec := BuildKitSpec(config.DefaultBaseName(config.Resolve(workingDirectory)))
	return s.write(path, config.KitSpecFile, []byte(spec), isFile(path))
}

// BuildKitSpec renders the starter network policy, named after the project it belongs to.
func BuildKitSpec(baseName string) string {
	return fmt.Sprintf(config.StarterKitSpec, baseName, baseName)
}

// writeMounts adds a placeholder for every declared mount the file leaves unanswered.
func (s Setup) writeMounts(workingDirectory string) error {
	required, err := ReadRequiredMounts(workingDirectory)
	if err != nil {
		return err
	}
	path := filepath.Join(workingDirectory, config.MountsFile)
	provided, err := config.ReadPathsFile(path)
	if err != nil {
		return err
	}
	filled := fill(provided, config.Pairs(required).Names(), config.MountPlaceholder)
	if err := s.write(path, config.MountsFile, asJSON(filled), isFile(path) && len(filled) == len(provided)); err != nil {
		return err
	}
	s.warnPlaceholders(required, filled)
	return nil
}

// warnPlaceholders names the mounts whose path only this machine's owner knows.
func (s Setup) warnPlaceholders(required, filled config.Pairs) {
	names := config.UnfilledMounts(required, filled)
	if len(names) == 0 {
		return
	}
	s.Deps.Console.Warn("WARNING: replace %s in %s for:", config.MountPlaceholder, config.MountsFile)
	s.Deps.Console.Warn("%s", config.DescribeMounts(required, names))
	s.Deps.Console.Warn("or run box mount-prompt and give the prompt to an agent")
}

// writeRepos adds an empty path for every member a group declares and its repos file leaves out.
func (s Setup) writeRepos(workingDirectory string) error {
	values, err := config.ReadConfigFile(filepath.Join(workingDirectory, config.ConfigFile))
	if err != nil {
		return err
	}
	// A single project has no members, so it gets no file to fill in.
	declared := values.Container(config.Repos)
	if declared == nil {
		return nil
	}
	path := filepath.Join(workingDirectory, config.ReposFile)
	provided, err := config.ReadPathsFile(path)
	if err != nil {
		return err
	}
	members, err := config.ToMembers(declared, provided)
	if err != nil {
		return err
	}
	filled := fill(provided, memberNames(members), "")
	s.warnUnplaced(members)
	return s.write(path, config.ReposFile, asJSON(filled), isFile(path) && len(filled) == len(provided))
}

// warnUnplaced names the members whose path only this machine's owner knows, with each origin.
func (s Setup) warnUnplaced(members []config.Member) {
	var unplaced []config.Member
	for _, member := range members {
		if member.Path == "" {
			unplaced = append(unplaced, member)
		}
	}
	if len(unplaced) == 0 {
		return
	}
	s.Deps.Console.Warn("WARNING: fill in where each of these sits on this machine in %s:", config.ReposFile)
	s.Deps.Console.Warn("%s", config.DescribeMembers(unplaced))
}

// ignoreLocalPaths writes the .gitignore entries box would otherwise refuse to run without.
func (s Setup) ignoreLocalPaths(workingDirectory string) error {
	// Outside a repository check-ignore answers for nothing, so every gen would append again.
	if !project.IsGitRepository(s.Deps.Run, workingDirectory) {
		s.Deps.Console.Print("skipped %s, since this is not a git repository", config.GitignoreFile)
		return nil
	}
	for _, name := range project.LocalPathNames() {
		if project.IsGitIgnored(s.Deps.Run, workingDirectory, name) {
			continue
		}
		if err := AppendLine(filepath.Join(workingDirectory, config.GitignoreFile), name); err != nil {
			return err
		}
		s.Deps.Console.Print("ignored %s in %s", name, config.GitignoreFile)
	}
	return nil
}

// write puts a file in place, or says it kept what was already there.
func (s Setup) write(path, name string, contents []byte, keep bool) error {
	if keep {
		s.Deps.Console.Print("kept    %s", name)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		return err
	}
	s.Deps.Console.Print("written %s", name)
	return nil
}

// AppendLine adds a line to a file, starting a new one when the file does not end in a newline.
func AppendLine(path, line string) error {
	existing, err := os.ReadFile(path)
	if err != nil {
		existing = nil
	}
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		existing = append(existing, '\n')
	}
	return os.WriteFile(path, append(existing, []byte(line+"\n")...), 0o644)
}

// ReadRequiredMounts reads what the project declares it needs mounted.
func ReadRequiredMounts(workingDirectory string) (config.Pairs, error) {
	values, err := config.ReadConfigFile(filepath.Join(workingDirectory, config.ConfigFile))
	if err != nil {
		return nil, err
	}
	return config.AsDescriptions(values.Container(config.RequiredMounts))
}

// fill answers every declared name, keeping the paths already filled in.
func fill(provided config.Pairs, names []string, unknown string) config.Pairs {
	filled := append(config.Pairs{}, provided...)
	for _, name := range names {
		if !filled.Has(name) {
			filled = append(filled, config.Pair{Name: name, Value: unknown})
		}
	}
	return filled
}

// memberNames lists the members a group declares, in the order it declared them.
func memberNames(members []config.Member) []string {
	names := make([]string, 0, len(members))
	for _, member := range members {
		names = append(names, member.Name)
	}
	return names
}

// asJSON renders name-to-text the way a hand-edited file spells it.
func asJSON(pairs config.Pairs) []byte {
	object := jsonx.Object{}
	for _, pair := range pairs {
		object = object.Set(pair.Name, jsonx.Text(pair.Value))
	}
	return jsonx.Write(object)
}

// isFile says whether a path names a file rather than a directory or nothing at all.
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
