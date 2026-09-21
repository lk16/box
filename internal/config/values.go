package config

import (
	"encoding/json"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/lk16/box/internal/fail"
	"github.com/lk16/box/internal/jsonx"
)

// Pair is one name and the text a file gives it.
type Pair struct {
	Name  string
	Value string
}

// Pairs are name to text, in the order the file that holds them wrote them.
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

// Names lists the names in the order the file wrote them.
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

// Container reads one container key, or nothing when this source named none.
func (v Values) Container(key string) json.RawMessage {
	return v.Raw[key]
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
	raw, err := jsonx.Parse(data)
	if err != nil {
		return nil, fail.Errorf("%s is not valid JSON: %s", path, err)
	}
	return raw, nil
}

// asText takes a JSON scalar as the string box passes on, rejecting what has no spelling as one.
func asText(path, name string, raw json.RawMessage) (string, error) {
	if text, ok := jsonx.AsString(raw); ok {
		return text, nil
	}
	// A number spells itself; null, true and a container have no spelling box could pass on.
	if number, ok := jsonx.AsNumber(raw); ok {
		return number, nil
	}
	return "", fail.Errorf("%s gives %s %s, which is not text or a number", path, name, jsonx.TypeName(raw))
}

// asPairs takes every value in a JSON object as the string box passes on.
func asPairs(path string, object jsonx.Object) (Pairs, error) {
	pairs := make(Pairs, 0, len(object))
	for _, member := range object {
		text, err := asText(path, member.Key, member.Value)
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, Pair{Name: member.Key, Value: text})
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
	object, ok := jsonx.AsObject(raw)
	if !ok {
		return values, fail.Errorf("%s must contain a JSON object", path)
	}
	if err := rejectUnknownKeys(path, object); err != nil {
		return values, err
	}
	for _, member := range object {
		if slices.Contains(ContainerKeys, member.Key) {
			values.Raw[member.Key] = member.Value
			continue
		}
		text, err := asText(path, member.Key, member.Value)
		if err != nil {
			return values, err
		}
		values.Text[member.Key] = text
	}
	return values, nil
}

// rejectUnknownKeys refuses a config holding a key box has no setting for, so typos surface at once.
func rejectUnknownKeys(path string, object jsonx.Object) error {
	var unknown []string
	for _, key := range object.Keys() {
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

// ReadPathsFile reads name to path from one of this machine's own files, or nothing when absent.
func ReadPathsFile(path string) (Pairs, error) {
	raw, err := LoadJSON(path)
	if raw == nil || err != nil {
		return nil, err
	}
	object, ok := jsonx.AsObject(raw)
	if !ok {
		return nil, fail.Errorf("%s must contain a JSON object of name to path", path)
	}
	return asPairs(path, object)
}

// AsDescriptions normalises the required_mounts value into name to description.
func AsDescriptions(raw json.RawMessage) (Pairs, error) {
	if raw == nil {
		return nil, nil
	}
	object, ok := jsonx.AsObject(raw)
	if !ok {
		return nil, fail.Errorf("%s must be a JSON object of name to description", RequiredMounts)
	}
	return asPairs(ConfigFile, object)
}
