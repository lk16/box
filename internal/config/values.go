package config

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/lk16/box/internal/fail"
)

// Pair is one name and the text a file gives it, or the null it answers with instead.
type Pair struct {
	Name  string
	Value string
	// Null is what only a repos file writes: a member this machine does not have at all.
	Null bool
}

// Pairs are name to text, sorted by name.
type Pairs []Pair

// Get returns the text stored under a name, or nothing when there is none.
func (p Pairs) Get(name string) string {
	for _, pair := range p {
		if pair.Name == name {
			return pair.Value
		}
	}
	return ""
}

// Has says whether a name is present at all, which an empty value still is.
func (p Pairs) Has(name string) bool {
	return slices.ContainsFunc(p, func(pair Pair) bool { return pair.Name == name })
}

// Null says whether a name is answered with JSON's null rather than with text.
func (p Pairs) Null(name string) bool {
	for _, pair := range p {
		if pair.Name == name {
			return pair.Null
		}
	}
	return false
}

// Names lists the names in the order the pairs hold them.
func (p Pairs) Names() []string {
	names := make([]string, 0, len(p))
	for _, pair := range p {
		names = append(names, pair.Name)
	}
	return names
}

// Values are one source's settings: text for a plain setting, raw JSON for a container key.
type Values struct {
	Text map[string]string
	Raw  map[string]json.RawMessage
}

// NewValues builds an empty set of settings for one source to fill in.
func NewValues() Values {
	return Values{Text: map[string]string{}, Raw: map[string]json.RawMessage{}}
}

// Setting reads one text setting, falling back to the built-in default.
func (v Values) Setting(key string) string {
	if text, given := v.Text[key]; given {
		return text
	}
	return Defaults[key]
}

// Merge layers one source's settings over another's; the later source wins.
func Merge(file, cli Values) Values {
	merged := NewValues()
	for _, source := range []Values{file, cli} {
		for key, text := range source.Text {
			merged.Text[key] = text
		}
		for key, raw := range source.Raw {
			merged.Raw[key] = raw
		}
	}
	return merged
}

// ResolvePath expands a configured path so a leading ~ works the same as in the shell.
func ResolvePath(text string) string {
	if text == "" {
		return ""
	}
	return tidy(expandHome(text))
}

// tidy drops what a reader of a path drops -- repeated slashes and "." -- and keeps every "..".
func tidy(path string) string {
	var kept []string
	for _, part := range strings.Split(path, "/") {
		// Collapsing ".." here would name a different directory whenever a symlink is in the way.
		if part != "" && part != "." {
			kept = append(kept, part)
		}
	}
	joined := strings.Join(kept, "/")
	if strings.HasPrefix(path, "/") {
		return "/" + joined
	}
	if joined == "" {
		return "."
	}
	return joined
}

// expandHome turns a leading ~ or ~name into the home directory it stands for.
func expandHome(text string) string {
	if !strings.HasPrefix(text, "~") {
		return text
	}
	name, rest, below := strings.Cut(strings.TrimPrefix(text, "~"), "/")
	home, err := homeOf(name)
	// A user this machine does not have is no home, so the path stays the text it was written as.
	if err != nil {
		return text
	}
	if !below {
		return home
	}
	return home + "/" + rest
}

// homeOf is a named user's home directory, or the home of whoever runs box when none is named.
func homeOf(name string) (string, error) {
	if name == "" {
		return os.UserHomeDir()
	}
	account, err := user.Lookup(name)
	if err != nil {
		return "", err
	}
	return account.HomeDir, nil
}

// separators are the runs of characters a kebab-cased name collapses into single hyphens.
var separators = regexp.MustCompile(`[^A-Za-z0-9]+`)

// ToKebabCase lowercases text and collapses runs of other characters into single hyphens.
func ToKebabCase(text string) string {
	return strings.ToLower(strings.Trim(separators.ReplaceAllString(text, "-"), "-"))
}

// DefaultBaseName derives a sandbox base name from the directory name.
func DefaultBaseName(directory string) string {
	name := ToKebabCase(filepath.Base(directory))
	if name == "" {
		return "box"
	}
	return name
}

// LoadJSON parses a JSON file, returning nothing when it is absent.
func LoadJSON(path string) (json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	var raw json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fail.Errorf("%s is not valid JSON: %s", path, err)
	}
	return raw, nil
}

// asObject reads a value as a JSON object, and says whether it was one.
func asObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	var object map[string]json.RawMessage
	// A null unmarshals into a nil map without complaint, and it is no object.
	err := json.Unmarshal(raw, &object)
	return object, err == nil && object != nil
}

// sortedKeys lists an object's keys in order, so everything read from one comes out the same way.
func sortedKeys(object map[string]json.RawMessage) []string {
	return slices.Sorted(maps.Keys(object))
}

// decode reads one JSON value, keeping each number spelled the way the file wrote it.
func decode(raw json.RawMessage) any {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil
	}
	return value
}

// typeName names a value's type the way the file that holds it spells it.
func typeName(raw json.RawMessage) string {
	switch decode(raw).(type) {
	case map[string]any:
		return "an object"
	case []any:
		return "a list"
	case string:
		return "text"
	case bool:
		return "a boolean"
	case json.Number:
		return "a number"
	}
	return "null"
}

// asText takes a JSON scalar as the string box passes on, rejecting what has no spelling as one.
func asText(path, name string, raw json.RawMessage) (string, error) {
	switch value := decode(raw).(type) {
	case string:
		return value, nil
	// A number spells itself; null, true and a container have no spelling box could pass on.
	case json.Number:
		return value.String(), nil
	}
	return "", fail.Errorf("%s gives %s %s, which is not text or a number", path, name, typeName(raw))
}

// asPairs takes every value in a JSON object as the string box passes on.
func asPairs(path string, object map[string]json.RawMessage) (Pairs, error) {
	pairs := make(Pairs, 0, len(object))
	for _, name := range sortedKeys(object) {
		text, err := asText(path, name, object[name])
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, Pair{Name: name, Value: text})
	}
	return pairs, nil
}

// ReadConfigFile reads the settings a project committed, returning none when it has no config.
func ReadConfigFile(path string) (Values, error) {
	values := NewValues()
	raw, err := LoadJSON(path)
	if raw == nil || err != nil {
		return values, err
	}
	object, ok := asObject(raw)
	if !ok {
		return values, fail.Errorf("%s must contain a JSON object", path)
	}
	if err := rejectUnknownKeys(path, object); err != nil {
		return values, err
	}
	for _, key := range sortedKeys(object) {
		if slices.Contains(ContainerKeys, key) {
			values.Raw[key] = object[key]
			continue
		}
		text, err := asText(path, key, object[key])
		if err != nil {
			return values, err
		}
		values.Text[key] = text
	}
	return values, nil
}

// rejectUnknownKeys refuses a config holding a key box has no setting for, so typos surface at once.
func rejectUnknownKeys(path string, object map[string]json.RawMessage) error {
	var unknown []string
	for key := range object {
		if !slices.Contains(SettingKeys, key) && !slices.Contains(ContainerKeys, key) {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fail.Errorf("%s has unknown keys: %s", path, strings.Join(unknown, ", "))
}

// readNamedPaths reads one of this machine's own files as the JSON object of name to path it holds.
func readNamedPaths(path string) (map[string]json.RawMessage, error) {
	raw, err := LoadJSON(path)
	if raw == nil || err != nil {
		return nil, err
	}
	object, ok := asObject(raw)
	if !ok {
		return nil, fail.Errorf("%s must contain a JSON object of name to path", path)
	}
	return object, nil
}

// ReadPathsFile reads name to path from one of this machine's own files, or nothing when absent.
func ReadPathsFile(path string) (Pairs, error) {
	object, err := readNamedPaths(path)
	if object == nil || err != nil {
		return nil, err
	}
	return asPairs(path, object)
}

// AsDescriptions normalises the required_mounts value into name to description.
func AsDescriptions(raw json.RawMessage) (Pairs, error) {
	if raw == nil {
		return nil, nil
	}
	object, ok := asObject(raw)
	if !ok {
		return nil, fail.Errorf("%s must be a JSON object of name to description", RequiredMounts)
	}
	return asPairs(ConfigFile, object)
}
