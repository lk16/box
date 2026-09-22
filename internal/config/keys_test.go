package config_test

import (
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/lk16/box/internal/config"
)

// wordBoundary is where a field name starts a new word; a run of capitals stays one word.
var wordBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)

// configKey spells a Config field the way the config file spells the same setting.
func configKey(field string) string {
	return strings.ToLower(wordBoundary.ReplaceAllString(field, "${1}_${2}"))
}

func TestCPUsAndMCPAreOneWordEach(t *testing.T) {
	for field, want := range map[string]string{
		"CPUs": "cpus", "MCP": "mcp", "RootSize": "root_size", "Name": "name",
	} {
		if got := configKey(field); got != want {
			t.Errorf("%s spells %q, want %q", field, got, want)
		}
	}
}

func TestConfigKeysMatchTheConfigFields(t *testing.T) {
	var fields []string
	value := reflect.TypeOf(config.Config{})
	for index := range value.NumField() {
		fields = append(fields, configKey(value.Field(index).Name))
	}
	// required_mounts is a declaration the mounts file answers, so it is no field of its own.
	want := []string{}
	for _, key := range append(slices.Clone(config.SettingKeys), config.ContainerKeys...) {
		if key != config.RequiredMounts {
			want = append(want, key)
		}
	}
	// mounts and members are what the two files this machine owns resolve to.
	want = append(want, "mounts", "members")
	sort.Strings(fields)
	sort.Strings(want)
	if !slices.Equal(fields, want) {
		t.Fatalf("the config has fields %v, where the keys are %v", fields, want)
	}
}

func TestEverySettingKeyHasADefault(t *testing.T) {
	for _, key := range config.SettingKeys {
		if _, given := config.Defaults[key]; !given {
			t.Errorf("%s has no default", key)
		}
	}
	if len(config.Defaults) != len(config.SettingKeys) {
		t.Fatalf("there are %d defaults for %d settings", len(config.Defaults), len(config.SettingKeys))
	}
}

func TestEveryGroupSettingIsAContainerKey(t *testing.T) {
	for _, key := range config.GroupSettings {
		if !slices.Contains(config.ContainerKeys, key) {
			t.Errorf("%s is a group setting but not a container key", key)
		}
	}
}
