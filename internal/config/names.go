// Package config turns flags and a project's .box files into the settings one run uses.
package config

// Everything box reads from a project lives in one directory, so a project has one box footprint.
const (
	BoxDir     = ".box"
	ConfigFile = BoxDir + "/config.json"

	// MountsFile names paths on one machine, so it lives apart from the settings a project shares.
	MountsFile = BoxDir + "/mounts.json"

	// ReposFile says where a group's members sit here, kept apart for the same reason.
	ReposFile = BoxDir + "/repos.json"

	GitignoreFile = ".gitignore"

	// KitDir holds the sandbox's network policy, which sbx reads from the directory holding a spec.
	KitDir      = BoxDir + "/kit"
	KitSpecFile = KitDir + "/spec.yaml"
)

// MountPlaceholder is what box gen writes where it cannot know the path, so an unfilled mount fails loudly.
const MountPlaceholder = "/placeholder/for/real/path"

// SetupCommands read no settings, so a flag means nothing to them.
var SetupCommands = []string{"gen", "mount-prompt", "self-update"}

// Commands are every word box answers to, in the order its help lists them.
var Commands = []string{"run", "config", "gen", "mount-prompt", "self-update"}

// The token and the values behind secret_hosts come from the environment and from nowhere else.
const (
	SecretHost    = "api.anthropic.com"
	SecretEnv     = "CLAUDE_CODE_OAUTH_TOKEN"
	TokenFileEnv  = "CLAUDE_OAUTH_TOKEN_FILE"
	SecretsEnv    = "BOX_SECRETS_FILE"
	NoColourEnv   = "NO_COLOR"
	UpdateURLEnv  = "BOX_UPDATE_URL"
	DataHomeEnv   = "XDG_DATA_HOME"
	CacheHomeEnv  = "XDG_CACHE_HOME"
	RootSizeEnv   = "DOCKER_SANDBOXES_ROOT_SIZE"
	DockerSizeEnv = "DOCKER_SANDBOXES_DOCKER_SIZE"
)

// A fetch that shares the terminal with every other fetch may ask nothing. See docs/fetching.md.
const (
	GitPromptEnv     = "GIT_TERMINAL_PROMPT"
	GitSSHCommandEnv = "GIT_SSH_COMMAND"
	BatchModeSSH     = "-o BatchMode=yes"
)

// Unset is what box config shows for a setting nothing was given for, rather than an empty column.
const Unset = "(unset)"

// Mounts are read-only unless the user opts out, so a sandbox cannot write to the host by accident.
const (
	ReadWriteSuffix = ":rw"
	ReadOnlySuffix  = ":ro"
)

// MountFlag is the one flag that is repeatable, so its config key is a declaration rather than a setting.
const MountFlag = "mount"

// The config keys holding an object or a list rather than the text of a setting.
const (
	RequiredMounts = "required_mounts"
	SecretHosts    = "secret_hosts"
	Repos          = "repos"
	MCP            = "mcp"
)

// What a group's config says about each member, which is the same on every machine.
const (
	MemberBranch = "branch"
	MemberOrigin = "git_origin"
	MemberShape  = "an object of " + MemberBranch + " and " + MemberOrigin
)

// SettingKeys hold the text of a setting, in the order box gen writes them.
var SettingKeys = []string{
	"name", "memory", "cpus", "root_size", "docker_size", "model", "prompt_file", "kit", "template",
}

// ContainerKeys hold an object or a list rather than text, in the order box gen writes them.
var ContainerKeys = []string{MCP, RequiredMounts, SecretHosts, Repos}

// GroupSettings are what a group of repositories adds, which box gen writes only when asked for one.
var GroupSettings = []string{Repos, SecretHosts, MCP}

// Defaults are the fallbacks behind every text setting.
var Defaults = map[string]string{
	"name": "", "memory": "4g", "cpus": "4", "root_size": "10g", "docker_size": "10g",
	"model": "", "prompt_file": "", "kit": "", "template": "",
}

// SandboxRefs is where sbx's git daemon lands a sandbox's work: refs/sandboxes/<sandbox>/<branch>.
const SandboxRefs = "refs/sandboxes"

// SandboxUser is who sbx exec runs as unless told otherwise, and who a member's clone has to belong to.
const SandboxUser = "agent"

// SandboxTemp is where a bundle lands on its way in or out, the one directory both sides can write.
const SandboxTemp = "/tmp"

// What a repository already holds, which is what new work is counted against.
const (
	MainKnown   = "HEAD"
	MemberKnown = "--remotes=origin"
)

// RequiredBinaries are what box shells out to, checked up front so a missing one is a message.
var RequiredBinaries = []string{"sbx", "git", "claude"}

// SbxMinimum is the sbx release box is written against, whose spec layout older ones reject.
var SbxMinimum = []int{0, 38, 0}

// VersionMatchCheck is the sbx diagnose check comparing the CLI with the daemon it talks to.
const VersionMatchCheck = "Version match"

// CheckPassed is what a diagnose check reports when what it looked at is as it should be.
const CheckPassed = "pass"
