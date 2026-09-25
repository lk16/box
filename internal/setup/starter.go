// Package setup writes the files a project needs before box will run in it, and asks for the rest.
package setup

import (
	"bytes"
	"encoding/json"
	"slices"

	"github.com/lk16/box/internal/config"
)

// StarterConfig is what box gen writes for one project: today's settings, so an older box runs it.
func StarterConfig() map[string]any {
	starter := map[string]any{}
	for _, key := range append(slices.Clone(config.SettingKeys), config.RequiredMounts) {
		starter[key] = defaultValue(key)
	}
	// gen writes the kit it also writes, so model is the only setting left to fill in.
	starter["kit"] = config.KitDir
	return starter
}

// GroupConfig is the starter plus the keys only a group has anything to say for.
func GroupConfig() map[string]any {
	group := StarterConfig()
	for _, key := range config.GroupSettings {
		group[key] = defaultValue(key)
	}
	return group
}

// defaultValue is what one config key holds before anyone has filled it in.
func defaultValue(key string) any {
	if key == config.MCP {
		return []string{}
	}
	if slices.Contains(config.ContainerKeys, key) {
		return map[string]string{}
	}
	return config.Defaults[key]
}

// asFile renders a value the way a hand-edited file spells it: keys sorted, two spaces in.
func asFile(value any) []byte {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetIndent("", "  ")
	// A config file is read by more than Go, and only Go escapes < > and & inside a string.
	encoder.SetEscapeHTML(false)
	// Every value box writes is text, a list or an object, none of which can fail to encode.
	if err := encoder.Encode(value); err != nil {
		panic(err)
	}
	return out.Bytes()
}
