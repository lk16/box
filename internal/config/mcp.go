package config

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/lk16/box/internal/fail"
)

// toMCPServer takes one MCP server name, rejecting what sbx could not be handed as that one name.
func toMCPServer(raw json.RawMessage) (string, error) {
	name, ok := decode(raw).(string)
	if !ok {
		return "", fail.Errorf("%s holds %s, which is not a server name", MCP, typeName(raw))
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
	var values []json.RawMessage
	// A null unmarshals into a nil list without complaint, and it is no list.
	if err := json.Unmarshal(raw, &values); err != nil || values == nil {
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

// WithoutMCP is what one run was asked to leave out of the configured mcp list, by each of the two ways.
type WithoutMCP struct {
	Flag        []string
	Environment []string
}

// Omitted is one configured MCP server a run leaves out, and what asked for it to go.
type Omitted struct {
	Name   string
	Source string
}

// ParseWithoutMCP reads the names BOX_WITHOUT_MCP holds, which an environment can only give as one text.
func ParseWithoutMCP(value string) []string {
	var names []string
	for _, entry := range strings.Split(value, ",") {
		// One profile's list is hand-edited and serves every project, so a stray comma asks for nothing.
		if name := strings.TrimSpace(entry); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// KeepMCP returns the configured servers this run passes on, and the ones it leaves out with what asked.
func KeepMCP(configured []string, without WithoutMCP) ([]string, []Omitted) {
	var kept []string
	var omitted []Omitted
	for _, name := range configured {
		// A name neither way asks about is every name when neither is set, which is what passes today.
		switch {
		case slices.Contains(without.Flag, name):
			omitted = append(omitted, Omitted{Name: name, Source: "--" + WithoutMCPFlag})
		case slices.Contains(without.Environment, name):
			omitted = append(omitted, Omitted{Name: name, Source: WithoutMCPEnv})
		default:
			kept = append(kept, name)
		}
	}
	return kept, omitted
}
