package config

import (
	"os"
	"slices"
	"strings"

	"github.com/lk16/box/internal/fail"
)

// Config is the effective settings for one box run.
type Config struct {
	Name        string
	Memory      string
	CPUs        string
	RootSize    string
	DockerSize  string
	Model       string
	PromptFile  string
	Kit         string
	Template    string
	MCP         []string
	Mounts      []string
	SecretHosts []Secret
	Repos       []Member
	Members     []MemberSettings
}

// Build turns merged config values into a Config, filling in the derived sandbox name.
func Build(values Values, mounts []string, members []MemberSettings, workingDirectory string) (Config, error) {
	name := values.Setting("name")
	if name == "" {
		name = DefaultBaseName(workingDirectory)
	}
	servers, err := ToMCPServers(values.Raw[MCP])
	if err != nil {
		return Config{}, err
	}
	workspaces, err := ToWorkspaces(mounts)
	if err != nil {
		return Config{}, err
	}
	secrets, err := ToSecrets(values.Raw[SecretHosts])
	if err != nil {
		return Config{}, err
	}
	repos := make([]Member, 0, len(members))
	for _, settings := range members {
		// A member this machine does not have is left out of everything this run creates.
		if settings.Member.Missing {
			continue
		}
		repos = append(repos, settings.Member)
	}
	return Config{
		Name: name, Memory: values.Setting("memory"), CPUs: values.Setting("cpus"),
		RootSize: values.Setting("root_size"), DockerSize: values.Setting("docker_size"),
		Model: values.Setting("model"), PromptFile: values.Setting("prompt_file"),
		Kit: values.Setting("kit"), Template: values.Setting("template"),
		MCP: servers, Mounts: workspaces, SecretHosts: secrets, Repos: repos, Members: members,
	}, nil
}

// MissingRepos are the declared members this machine does not have, which this run works without.
func (c Config) MissingRepos() []Member {
	var missing []Member
	for _, settings := range c.Members {
		if settings.Member.Missing {
			missing = append(missing, settings.Member)
		}
	}
	return missing
}

// Kits lists the kits a sandbox runs under: the group's, then each member's, and each one once.
func (c Config) Kits() []string {
	kits := []string{c.Kit}
	for _, settings := range c.Members {
		kits = append(kits, settings.Kit)
	}
	var wanted []string
	for _, kit := range kits {
		if kit == "" || slices.Contains(wanted, kit) {
			continue
		}
		wanted = append(wanted, kit)
	}
	return wanted
}

// MemberMounts collects what the members ask to have mounted, in the order the members come.
func MemberMounts(members []MemberSettings) []string {
	var mounts []string
	for _, settings := range members {
		mounts = append(mounts, settings.Mounts...)
	}
	return mounts
}

// ReadSystemPrompt reads the extra system prompt, or nothing when no file is configured.
func ReadSystemPrompt(promptFile string) (string, error) {
	if promptFile == "" {
		return "", nil
	}
	path := ResolvePath(promptFile)
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fail.Errorf("prompt file %s does not exist", path)
	}
	return string(contents), nil
}

// BuildSystemPrompt joins what the agent is told, leaving out every part this run has nothing for.
func BuildSystemPrompt(parts []string) string {
	var kept []string
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, "\n\n")
}
