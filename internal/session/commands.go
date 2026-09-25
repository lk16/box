// Package session creates the sandbox, runs the agent in it, and brings the committed work back.
package session

import (
	"os"
	"strings"

	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/project"
)

// Launch is everything resolved before the sandbox exists.
type Launch struct {
	Project     project.Project
	SandboxName string
	Secrets     []config.SecretValue
	AgentArgs   []string
}

// CreateCommand assembles the sbx create invocation.
func CreateCommand(settings config.Config, proj project.Project, sandboxName string) []string {
	command := []string{"sbx", "create", "claude", proj.ClonePath()}
	command = append(command, settings.Mounts...)
	command = append(command, "--clone", "--name", sandboxName)
	// Nothing in a sandbox may write to the host unasked, and sbx shares one skills directory read-write.
	command = append(command, "--no-share-skills")
	command = append(command, "--memory", settings.Memory, "--cpus", settings.CPUs)
	// Two kits are one allowlist, so a member's hosts add to the group's rather than replacing them.
	for _, kit := range settings.Kits() {
		command = append(command, "--kit", kit)
	}
	// An unset template leaves the image to sbx, which is what almost every project wants.
	if settings.Template != "" {
		command = append(command, "--template", settings.Template)
	}
	// The names are the user's own registered servers, so an empty mcp asks sbx for none of them.
	if len(settings.MCP) > 0 {
		command = append(command, "--static-mcp", strings.Join(settings.MCP, ","))
	}
	return command
}

// AgentArgs assembles the arguments passed through to the Claude CLI.
func AgentArgs(settings config.Config, systemPrompt string) []string {
	var arguments []string
	if systemPrompt != "" {
		arguments = append(arguments, "--append-system-prompt", systemPrompt)
	}
	if settings.Model != "" {
		arguments = append(arguments, "--model", settings.Model)
	}
	return arguments
}

// RunCommand assembles the sbx run invocation.
func RunCommand(sandboxName string, agentArgs []string) []string {
	command := []string{"sbx", "run", "claude", "--name", sandboxName}
	if len(agentArgs) == 0 {
		return command
	}
	return append(append(command, "--"), agentArgs...)
}

// Environment copies the current environment and adds the sbx disk limits.
func Environment(settings config.Config) []string {
	limits := []config.Pair{
		{Name: config.RootSizeEnv, Value: settings.RootSize},
		{Name: config.DockerSizeEnv, Value: settings.DockerSize},
	}
	var environment []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		// box overrides either limit the user already exported, so the settings are what sbx sees.
		if name != config.RootSizeEnv && name != config.DockerSizeEnv {
			environment = append(environment, entry)
		}
	}
	for _, limit := range limits {
		environment = append(environment, limit.Name+"="+limit.Value)
	}
	return environment
}

// StatusCommand asks the sandbox whether one clone holds uncommitted work.
func StatusCommand(path, sandboxName string) []string {
	return []string{"sbx", "exec", sandboxName, "git", "-C", path, "status", "--porcelain"}
}
