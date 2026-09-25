package session

import (
	"fmt"
	"os"
	"strings"

	"github.com/lk16/box/internal/config"
	"github.com/lk16/box/internal/fail"
	"github.com/lk16/box/internal/project"
	"github.com/lk16/box/internal/system"
)

// Session is one box run's outside world, so every sbx and git call goes through one place.
type Session struct {
	Deps system.Deps
}

// ParseRefNames pulls sandbox names out of refs/sandboxes/<name>/<branch> ref lines.
func ParseRefNames(refsOutput string) map[string]bool {
	names := map[string]bool{}
	for _, line := range strings.Split(refsOutput, "\n") {
		parts := strings.Split(strings.TrimSpace(line), "/")
		if len(parts) >= 4 {
			names[parts[2]] = true
		}
	}
	return names
}

// TakenNames collects sandbox names that are running or still hold git refs in any repository.
func (s Session) TakenNames(checkouts []project.Checkout) map[string]bool {
	names := map[string]bool{}
	for _, name := range strings.Fields(system.Capture(s.Deps.Run, []string{"sbx", "ls", "-q"})) {
		names[name] = true
	}
	for _, checkout := range checkouts {
		command := project.Git(checkout.Path, "for-each-ref", "--format=%(refname)", config.SandboxRefs)
		for name := range ParseRefNames(system.Capture(s.Deps.Run, command)) {
			names[name] = true
		}
	}
	return names
}

// PickName returns the first <base>-<n> name that is free.
func PickName(baseName string, used map[string]bool) string {
	for number := 1; ; number++ {
		name := fmt.Sprintf("%s-%d", baseName, number)
		if !used[name] {
			return name
		}
	}
}

// PrepareLaunch resolves everything that can still fail before the sandbox exists.
func (s Session) PrepareLaunch(settings config.Config, proj project.Project, tokenFile string) (Launch, error) {
	if err := project.Require(s.Deps.Run, settings, proj, tokenFile); err != nil {
		return Launch{}, err
	}
	if tokenFile == "" {
		return Launch{}, fail.Errorf("%s", config.TokenFileHelp)
	}
	token, err := config.ReadToken(config.ResolvePath(tokenFile))
	if err != nil {
		return Launch{}, err
	}
	declared, err := config.ReadSecretValues(settings.SecretHosts, os.Getenv(config.SecretsEnv))
	if err != nil {
		return Launch{}, err
	}
	prompt, err := systemPrompt(settings, proj)
	if err != nil {
		return Launch{}, err
	}
	return Launch{
		Project:     proj,
		SandboxName: PickName(settings.Name, s.TakenNames(project.Checkouts(settings, proj))),
		Secrets:     append([]config.SecretValue{{Secret: config.OAuthSecret, Value: token}}, declared...),
		AgentArgs:   AgentArgs(settings, prompt),
	}, nil
}

// systemPrompt joins everything this run tells the agent, in the order it is told.
func systemPrompt(settings config.Config, proj project.Project) (string, error) {
	own, err := config.ReadSystemPrompt(settings.PromptFile)
	if err != nil {
		return "", err
	}
	members, err := project.MemberPrompts(settings, proj)
	if err != nil {
		return "", err
	}
	parts := []string{
		config.BasePrompt,
		proj.StartedInPrompt(),
		own,
		project.MembersPrompt(settings, proj),
		project.MissingMembersPrompt(settings),
	}
	return config.BuildSystemPrompt(append(parts, members...)), nil
}

// Run packs up the members this run needs, then hands the sandbox over to the agent.
func (s Session) Run(settings config.Config, launch Launch) int {
	// The bundles are made before anything exists, so a member box cannot read costs nothing.
	directory, err := os.MkdirTemp("", "box")
	if err != nil {
		s.Deps.Console.Warn("box: %s", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(directory) }()
	s.warnMissingMembers(settings)
	bundles, err := s.BundleMembers(settings, launch.Project, directory)
	if err != nil {
		s.Deps.Console.Warn("box: %s", err)
		return 1
	}
	return s.start(settings, launch, bundles)
}

// warnMissingMembers names the members this machine has none of, so a run is never quietly short one.
func (s Session) warnMissingMembers(settings config.Config) {
	for _, member := range settings.MissingRepos() {
		s.Deps.Console.Warn("box: %s is not on this machine, so this sandbox runs without it", member.Name)
	}
}

// start creates the sandbox, clones the members into it, runs Claude, and cleans up afterwards.
func (s Session) start(settings config.Config, launch Launch, bundles []Bundle) int {
	environment := Environment(settings)
	// sbx injects the placeholder env vars when the sandbox is created, so they must exist by then.
	s.DropSecrets(settings.SecretHosts, launch.SandboxName)
	for _, stored := range launch.Secrets {
		if err := s.StoreSecret(launch.SandboxName, stored); err != nil {
			s.Deps.Console.Warn("box: %s", err)
			return 1
		}
	}
	create := CreateCommand(settings, launch.Project, launch.SandboxName)
	// sbx has already said why it failed, and there is no sandbox to run in, clean up or keep.
	if s.Deps.Run.Attach(create, environment).Code != 0 {
		// The loser of a name race drops a secret sbx already injected, so the winner works on.
		s.DropSecrets(settings.SecretHosts, launch.SandboxName)
		s.Deps.Console.Warn("box: sbx create failed, so %s was never started.", launch.SandboxName)
		return 1
	}
	if code := s.cloneMembers(settings, launch, bundles); code != 0 {
		return code
	}
	defer s.Cleanup(settings, launch)
	return s.Deps.Run.Attach(RunCommand(launch.SandboxName, launch.AgentArgs), environment).Code
}

// cloneMembers puts every member's clone in the fresh sandbox, saying whether the run can start.
func (s Session) cloneMembers(settings config.Config, launch Launch, bundles []Bundle) int {
	for _, bundle := range bundles {
		if s.CloneMember(bundle, launch.SandboxName) {
			continue
		}
		return s.removeSandbox(settings, launch, bundle.Member.Name+" could not be cloned")
	}
	return 0
}

// removeSandbox takes back a sandbox holding nothing yet, since the run it was made for cannot start.
func (s Session) removeSandbox(settings config.Config, launch Launch, reason string) int {
	s.DropSecrets(settings.SecretHosts, launch.SandboxName)
	s.Deps.Run.Attach([]string{"sbx", "rm", "--force", launch.SandboxName}, nil)
	s.Deps.Console.Warn("box: %s, so %s was removed again.", reason, launch.SandboxName)
	return 1
}
