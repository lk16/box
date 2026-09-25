package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lk16/box/internal/config"
)

// raw renders a Go value as the JSON a config file would hold.
func raw(value any) json.RawMessage {
	written, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return written
}

// pairs builds name-to-text in the order given, the way a file's keys arrive.
func pairs(names ...string) config.Pairs {
	built := config.Pairs{}
	for index := 0; index+1 < len(names); index += 2 {
		built = append(built, config.Pair{Name: names[index], Value: names[index+1]})
	}
	return built
}

// writeBoxFile writes one .box file, creating the directory it lives in.
func writeBoxFile(t *testing.T, directory, name string, contents any) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(contents)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeConfig writes a config file, creating the .box directory it lives in.
func writeConfig(t *testing.T, directory string, values any) string {
	t.Helper()
	return writeBoxFile(t, directory, config.ConfigFile, values)
}

// values builds the settings one source gives, from alternating key and text.
func values(text ...string) config.Values {
	built := config.NewValues()
	for index := 0; index+1 < len(text); index += 2 {
		built.Text[text[index]] = text[index+1]
	}
	return built
}

// configFrom builds a config from config file values and no mounts, which come from their own file.
func configFrom(t *testing.T, given config.Values, directory string) config.Config {
	t.Helper()
	built, err := config.Build(config.Merge(given, config.NewValues()), nil, nil, directory)
	if err != nil {
		t.Fatal(err)
	}
	return built
}

// wantError fails unless the error says what the test expects it to.
func wantError(t *testing.T, err error, contains string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no refusal, wanted one saying %q", contains)
	}
	if !strings.Contains(err.Error(), contains) {
		t.Fatalf("refusal was %q, wanted one saying %q", err, contains)
	}
}

// writeText overwrites a file with the exact text given, however broken it is.
func writeText(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
