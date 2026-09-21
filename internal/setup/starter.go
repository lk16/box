// Package setup writes the files a project needs before box will run in it, and asks for the rest.
package setup

import (
	"slices"

	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/jsonx"
)

// StarterConfig is what box gen writes for one project: today's settings, so an older box runs it.
func StarterConfig() jsonx.Object {
	starter := jsonx.Object{}
	for _, key := range append(slices.Clone(config.SettingKeys), config.RequiredMounts) {
		starter = starter.Set(key, defaultValue(key))
	}
	// gen writes the kit it also writes, so model is the only setting left to fill in.
	return starter.Set("kit", jsonx.Text(config.KitDir))
}

// GroupConfig is the starter plus the keys only a group has anything to say for.
func GroupConfig() jsonx.Object {
	group := StarterConfig()
	for _, key := range config.GroupSettings {
		group = group.Set(key, defaultValue(key))
	}
	return group
}

// defaultValue is what one config key holds before anyone has filled it in.
func defaultValue(key string) jsonx.RawValue {
	if key == config.MCP {
		return jsonx.Text([]string{})
	}
	if slices.Contains(config.ContainerKeys, key) {
		return jsonx.Text(map[string]string{})
	}
	return jsonx.Text(config.Defaults[key])
}
