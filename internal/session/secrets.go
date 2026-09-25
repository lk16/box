package session

import (
	"slices"

	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/fail"
	"github.com/lk16/box/internal/system"
)

// DistinctHosts lists the hosts a sandbox has secrets for, box's own token host first and each once.
func DistinctHosts(secrets []config.Secret) []string {
	hosts := []string{config.SecretHost}
	for _, secret := range secrets {
		if !slices.Contains(hosts, secret.Host) {
			hosts = append(hosts, secret.Host)
		}
	}
	return hosts
}

// DropSecrets removes every stored secret for this sandbox name, ignoring failures.
func (s Session) DropSecrets(secrets []config.Secret, sandboxName string) {
	for _, host := range DistinctHosts(secrets) {
		s.Deps.Run.Capture([]string{"sbx", "secret", "rm", "--sandbox", sandboxName, "--host", host, "-f"}, nil)
	}
}

// StoreCommand assembles the sbx secret invocation, which takes the value on stdin, not as an argument.
func StoreCommand(sandboxName string, secret config.Secret) []string {
	return []string{
		"sbx", "secret", "set-custom",
		"--sandbox", sandboxName,
		"--host", secret.Host,
		"--env", secret.Name,
	}
}

// StoreSecret hands one secret's value to sbx over stdin so it never lands in the shell history.
func (s Session) StoreSecret(sandboxName string, stored config.SecretValue) error {
	if s.Deps.Run.Feed(StoreCommand(sandboxName, stored.Secret), stored.Value).Code != 0 {
		return fail.Errorf("sbx would not store %s for %s", stored.Secret.Name, sandboxName)
	}
	return nil
}

// succeeds runs a command and says only whether it worked, which is all some callers need to know.
func (s Session) succeeds(arguments []string) bool {
	return system.Succeeds(s.Deps.Run, arguments)
}

// capture runs a command and returns its stdout, or nothing when it failed.
func (s Session) capture(arguments []string) string {
	return system.Capture(s.Deps.Run, arguments)
}
