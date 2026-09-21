package config

import (
	"encoding/json"
	"os"
	"slices"
	"strings"

	"github.com/lk16/box/internal/fail"
	"github.com/lk16/box/internal/jsonx"
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

// toMCPServer takes one MCP server name, rejecting what sbx could not be handed as that one name.
func toMCPServer(raw json.RawMessage) (string, error) {
	name, ok := jsonx.AsString(raw)
	if !ok {
		return "", fail.Errorf("%s holds %s, which is not a server name", MCP, jsonx.TypeName(raw))
	}
	if name == "" {
		return "", fail.Errorf("%s holds an empty server name", MCP)
	}
	// sbx takes every name in one comma-separated argument, so a comma would split this one in two.
	if strings.Contains(name, ",") {
		return "", fail.Errorf("%s name %s holds a comma, which sbx would read as two servers", MCP, name)
	}
	return name, nil
}

// ToMCPServers normalises the mcp value into the registered servers the sandbox may use, in order.
func ToMCPServers(raw json.RawMessage) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	values, ok := jsonx.AsArray(raw)
	if !ok {
		return nil, fail.Errorf("%s must be a JSON list of server names", MCP)
	}
	names := make([]string, 0, len(values))
	for _, value := range values {
		name, err := toMCPServer(value)
		if err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, nil
}

// Build turns merged config values into a Config, filling in the derived sandbox name.
func Build(values Values, mounts []string, members []MemberSettings, workingDirectory string) (Config, error) {
	name := values.Setting("name")
	if name == "" {
		name = DefaultBaseName(workingDirectory)
	}
	servers, err := ToMCPServers(values.Container(MCP))
	if err != nil {
		return Config{}, err
	}
	workspaces, err := ToWorkspaces(mounts)
	if err != nil {
		return Config{}, err
	}
	secrets, err := ToSecrets(values.Container(SecretHosts))
	if err != nil {
		return Config{}, err
	}
	repos := make([]Member, 0, len(members))
	for _, settings := range members {
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

// Kits lists the kits a sandbox runs under: the group's, then each member's, and each one once.
func (c Config) Kits() []string {
	var wanted []string
	for _, kit := range append([]string{c.Kit}, memberKits(c.Members)...) {
		if kit == "" || slices.Contains(wanted, kit) {
			continue
		}
		wanted = append(wanted, kit)
	}
	return wanted
}

// memberKits lists what each member asks to run under, in the order the group declared them.
func memberKits(members []MemberSettings) []string {
	kits := make([]string, 0, len(members))
	for _, settings := range members {
		kits = append(kits, settings.Kit)
	}
	return kits
}

// MemberMounts collects what the members ask to have mounted, in the order the group declared them.
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
