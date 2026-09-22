package config

import (
	"fmt"
	"strings"
)

// line is one rendered setting, kept as a pair so the column stays aligned across all of them.
type line struct {
	key   string
	value string
}

// orUnset names what a setting holds nothing for, since a blank column reads as a missing row.
func orUnset(text string) string {
	if text == "" {
		return Unset
	}
	return text
}

// joined renders a list of values as one readable line.
func joined(values []string) string {
	return orUnset(strings.Join(values, " "))
}

// formatSecret names one declared secret and the host its value may go to, which is never the value.
func formatSecret(secret Secret) string {
	return secret.Name + "->" + secret.Host
}

// formatMember names one member, the branch its clone starts from, and where it sits on this machine.
func formatMember(member Member) string {
	return fmt.Sprintf("%s@%s->%s", member.Name, member.Branch, member.Path)
}

// formatMemberSettings names what one member's own .box/ adds, which for most members is nothing.
func formatMemberSettings(settings MemberSettings) string {
	if settings.Member.Missing {
		return settings.Member.Name + ": " + MissingHere
	}
	added := append([]string{}, settings.Mounts...)
	if settings.Kit != "" {
		added = append(added, "kit="+settings.Kit)
	}
	if settings.PromptFile != "" {
		added = append(added, "prompt_file="+settings.PromptFile)
	}
	if len(added) == 0 {
		return settings.Member.Name + ": nothing"
	}
	return settings.Member.Name + ": " + strings.Join(added, " ")
}

// each renders every item of a list with the given renderer.
func each[T any](items []T, render func(T) string) []string {
	rendered := make([]string, 0, len(items))
	for _, item := range items {
		rendered = append(rendered, render(item))
	}
	return rendered
}

// Format renders the settings in effect, the secret paths included, as aligned key/value lines.
func Format(config Config, tokenFile, secretsFile string) string {
	lines := []line{
		{TokenFileEnv, orUnset(tokenFile)},
		{SecretsEnv, orUnset(secretsFile)},
		{"name", orUnset(config.Name)},
		{"memory", orUnset(config.Memory)},
		{"cpus", orUnset(config.CPUs)},
		{"root_size", orUnset(config.RootSize)},
		{"docker_size", orUnset(config.DockerSize)},
		{"model", orUnset(config.Model)},
		{"prompt_file", orUnset(config.PromptFile)},
		{"kit", orUnset(config.Kit)},
		{"template", orUnset(config.Template)},
		{MCP, joined(config.MCP)},
		{"mounts", joined(config.Mounts)},
		{SecretHosts, joined(each(config.SecretHosts, formatSecret))},
		{Repos, joined(each(config.Repos, formatMember))},
		{"members", joined(each(config.Members, formatMemberSettings))},
	}
	width := 0
	for _, item := range lines {
		width = max(width, len(item.key))
	}
	rendered := []string{"config in effect:"}
	for _, item := range lines {
		rendered = append(rendered, fmt.Sprintf("  %-*s  %s", width, item.key, item.value))
	}
	return strings.Join(rendered, "\n")
}
