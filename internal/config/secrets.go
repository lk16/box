package config

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"

	"github.com/lk16/box/internal/fail"
	"github.com/lk16/box/internal/jsonx"
)

// Secret is one environment variable the sandbox gets, and the one host its value may be sent to.
type Secret struct {
	Name string
	Host string
}

// SecretValue is one declared secret, and the value this machine holds for it.
type SecretValue struct {
	Secret Secret
	Value  string
}

// OAuthSecret is box's own token: a secret like any other, and always the first a sandbox gets.
var OAuthSecret = Secret{Name: SecretEnv, Host: SecretHost}

// environmentName is what a shell accepts as a variable name, which is what sbx puts the placeholder in.
var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// quotes are what docker --env-file would keep as part of a value, so box refuses the line instead.
const quotes = "\"'"

// ToSecret takes one declared secret, rejecting a name or host box could never send a value to.
func ToSecret(name, host string) (Secret, error) {
	if !environmentName.MatchString(name) {
		return Secret{}, fail.Errorf("%s name %s is not a valid environment variable name", SecretHosts, name)
	}
	// Dropping this sandbox's secrets by host would take box's own token with them.
	if name == SecretEnv {
		return Secret{}, fail.Errorf("%s cannot name %s, which carries box's own token", SecretHosts, SecretEnv)
	}
	if host == "" {
		return Secret{}, fail.Errorf("%s gives %s no host, so there is nowhere its value may go", SecretHosts, name)
	}
	if host == SecretHost {
		return Secret{}, fail.Errorf("%s cannot name %s, which carries box's own token", SecretHosts, SecretHost)
	}
	return Secret{Name: name, Host: host}, nil
}

// ToSecrets normalises the secret_hosts value into the variables the sandbox gets, and where each may go.
func ToSecrets(raw json.RawMessage) ([]Secret, error) {
	if raw == nil {
		return nil, nil
	}
	object, ok := jsonx.AsObject(raw)
	if !ok {
		return nil, fail.Errorf("%s must be a JSON object of name to host", SecretHosts)
	}
	declared, err := asPairs(ConfigFile, object)
	if err != nil {
		return nil, err
	}
	secrets := make([]Secret, 0, len(declared))
	for _, pair := range declared {
		secret, err := ToSecret(pair.Name, pair.Value)
		if err != nil {
			return nil, err
		}
		secrets = append(secrets, secret)
	}
	return secrets, nil
}

// isQuoted says whether a value is wrapped in matching quotes, which docker would keep as part of it.
func isQuoted(value string) bool {
	if len(value) < 2 || !strings.ContainsRune(quotes, rune(value[0])) {
		return false
	}
	return value[0] == value[len(value)-1]
}

// ParseSecretsFile reads NAME=value lines the way docker --env-file does, so one file serves both.
func ParseSecretsFile(path, contents string) (Pairs, error) {
	var values Pairs
	for number, line := range strings.Split(strings.TrimSuffix(contents, "\n"), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, found := strings.Cut(line, "=")
		if !found {
			return nil, fail.Errorf("%s line %d is not NAME=value", path, number+1)
		}
		// An invalid name is what "export NAME=" and a space around the = come out as.
		if !environmentName.MatchString(name) {
			return nil, fail.Errorf("%s line %d does not start with a variable name", path, number+1)
		}
		if isQuoted(value) {
			return nil, fail.Errorf("%s line %d quotes its value, which docker would keep", path, number+1)
		}
		values = append(values, Pair{Name: name, Value: value})
	}
	return values, nil
}

// ReadSecretsFile reads the values behind the declared secrets, which box never prints.
func ReadSecretsFile(path string) (Pairs, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fail.Errorf("secrets file %s does not exist", path)
	}
	return ParseSecretsFile(path, string(contents))
}

// ReadSecretValues gives every declared secret this machine's value for it, and nothing else that file holds.
func ReadSecretValues(secrets []Secret, secretsFile string) ([]SecretValue, error) {
	if len(secrets) == 0 {
		return nil, nil
	}
	if secretsFile == "" {
		return nil, fail.Errorf(SecretsFileHelp, ConfigFile)
	}
	path := ResolvePath(secretsFile)
	values, err := ReadSecretsFile(path)
	if err != nil {
		return nil, err
	}
	stored := make([]SecretValue, 0, len(secrets))
	for _, secret := range secrets {
		value := values.Get(secret.Name)
		if value == "" {
			return nil, fail.Errorf("%s has no value for %s, which %s declares", path, secret.Name, ConfigFile)
		}
		stored = append(stored, SecretValue{Secret: secret, Value: value})
	}
	return stored, nil
}

// ReadToken reads the OAuth token, stripped of whatever whitespace the file was saved with.
func ReadToken(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fail.Errorf("token file %s does not exist", path)
	}
	// A stray newline or space reaches the agent as part of the token and fails far from here.
	token := strings.TrimSpace(string(contents))
	if token == "" {
		return "", fail.Errorf("token file %s is empty", path)
	}
	return token, nil
}
