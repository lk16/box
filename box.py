#!/usr/bin/env python3
"""Run Claude Code inside a disposable Docker sandbox (sbx)."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import platform
import re
import shutil
import subprocess
import sys
import tempfile
import time
import urllib.request
from dataclasses import asdict, dataclass
from pathlib import Path

# Everything box reads from a project lives in one directory, so a project has one box footprint.
BOX_DIR = ".box"
CONFIG_FILE = f"{BOX_DIR}/config.json"

# Mounts name paths on one machine, so they live apart from the settings a project shares.
MOUNTS_FILE = f"{BOX_DIR}/mounts.json"

GITIGNORE_FILE = ".gitignore"

# The sandbox's network policy, which sbx reads from the directory holding a spec.yaml.
KIT_DIR = f"{BOX_DIR}/kit"
KIT_SPEC_FILE = f"{KIT_DIR}/spec.yaml"

# What box gen writes where it cannot know the path, so an unfilled mount fails loudly.
MOUNT_PLACEHOLDER = "/placeholder/for/real/path"

# Commands that read no settings, so a flag means nothing to them: they work on the project's
# files, or on box itself.
SETUP_COMMANDS = ("gen", "mount-prompt", "self-update")

# box.py has no version, so being current means hashing the same as the published copy.
UPDATE_URL = "https://raw.githubusercontent.com/lk16/box/main/box.py"

# How a fork points the check at its own copy, or switches it off by setting it to nothing.
UPDATE_URL_ENV = "BOX_UPDATE_URL"
UPDATE_INTERVAL_SECONDS = 60 * 60
UPDATE_TIMEOUT_SECONDS = 2

# What a half-written update is called until it is moved over the copy it replaces.
INCOMING_SUFFIX = ".incoming"

# The update notice competes with whatever the agent prints, so it is coloured to stand out.
RED = "\033[31m"
RESET = "\033[0m"

# What a reader of plain text asks for, and every other tool honours: no escape codes at all.
NO_COLOUR_ENV = "NO_COLOR"

SECRET_HOST = "api.anthropic.com"
SECRET_ENV = "CLAUDE_CODE_OAUTH_TOKEN"

# The token path is deliberately the one setting that is not a flag or a config file key.
TOKEN_FILE_ENV = "CLAUDE_OAUTH_TOKEN_FILE"

# Where this machine keeps the values behind secret_hosts, for the same reason: the environment only.
SECRETS_FILE_ENV = "BOX_SECRETS_FILE"

# What a shell accepts as a variable name, which is what sbx puts the placeholder in.
ENVIRONMENT_NAME = re.compile(r"[A-Za-z_][A-Za-z0-9_]*")

# What docker --env-file would keep as part of the value, so box refuses the line instead.
QUOTES = "\"'"

# What box config shows for a setting nothing was given for, rather than an empty column.
UNSET = "(unset)"

# Mounts are read-only unless the user opts out, so a sandbox cannot write to the host by accident.
READ_WRITE_SUFFIX = ":rw"
READ_ONLY_SUFFIX = ":ro"

# The one flag whose argparse dest is not its own name, since it collects a list of paths.
MOUNT_FLAG = "--mount"
MOUNT_DEST = "mounts"

# Where sbx's git daemon lands a sandbox's work: refs/sandboxes/<sandbox>/<branch>.
SANDBOX_REFS = "refs/sandboxes"

# Who sbx exec runs as unless it is told otherwise, and who a member's clone has to belong to.
SANDBOX_USER = "agent"

# Where a bundle lands on its way in or out, which is the one directory both sides can write.
SANDBOX_TEMP = "/tmp"

# What a repository already holds, which is what new work is counted against.
MAIN_KNOWN = "HEAD"
MEMBER_KNOWN = "--remotes=origin"

# What a command that never started exits with, which is what a shell reports for the same thing.
NOT_RUN = 127

# What box shells out to, checked once up front so a missing one is a message and not a failed run.
REQUIRED_BINARIES = ("sbx", "git", "claude")

# The sbx release box is written against, whose spec layout older ones reject.
SBX_MINIMUM = (0, 38, 0)

# The release number in what sbx version prints: "sbx version: v0.38.0 <commit>".
SBX_VERSION_PATTERN = re.compile(r"v?(\d+)\.(\d+)\.(\d+)")

# The sbx diagnose check that compares the CLI with the daemon it talks to.
VERSION_MATCH_CHECK = "Version match"

# What a diagnose check reports when what it looked at is as it should be.
CHECK_PASSED = "pass"

# The shebang takes whatever python3 comes first, which on a stock macOS is Xcode's 3.9.
PYTHON_MINIMUM = (3, 9)

# A branch name is a courtesy, so the agent naming it gets one turn and no more.
BRANCH_NAME_TIMEOUT_SECONDS = 10
BRANCH_NAME_WORDS = 5

BRANCH_NAME_PROMPT = f"""Name a git branch after the work these commit subjects describe.
Answer with the name and nothing else: kebab-case, at most {BRANCH_NAME_WORDS} words, shorter is
better.

Commit subjects:"""

MISSING_BINARIES_HELP = """these commands are not on PATH: {missing}.
box shells out to sbx to create the sandbox, to git to fetch the sandbox's work back, and to claude
to name the branch that work lands on, so all three have to be installed."""

NO_CONFIG_HELP = f"""this project has no {CONFIG_FILE}, so box has no settings to run with.
Run box gen to write a starter one, then name a model in it."""

KIT_HELP = f"""kit is not set, so the sandbox would run without a network policy.
Point it at a kit directory holding a spec.yaml in {CONFIG_FILE} or with --kit; box gen writes a
starter one at {KIT_SPEC_FILE} and points kit at it."""

KIT_FILE_HELP = f"""kit names a file, and sbx reads anything that is not a directory as a zip
artifact. Point it at the directory holding spec.yaml, e.g. {KIT_DIR} rather than
{KIT_SPEC_FILE}."""

MODEL_HELP = f"""model is not set, so the sandbox's own Claude version would pick the model.
That version need not match the one on this host. Name the model in {CONFIG_FILE} or with --model."""

MOUNTS_IGNORED_HELP = f"""{MOUNTS_FILE} is not ignored by git.
It names folders on this machine, so committing it would put paths that exist only here into
everyone else's clone. Add a {MOUNTS_FILE} line to .gitignore."""

# What box writes that holds one machine's own files, and the reason it must stay uncommitted.
LOCAL_PATHS = {MOUNTS_FILE: MOUNTS_IGNORED_HELP}

NOT_A_REPOSITORY_HELP = """this is not a git repository.
box hands the agent a clone of this directory, so there has to be something here to clone. Run
git init and commit, then try again."""

NO_COMMITS_HELP = """this git repository has no commits.
box hands the agent a clone of this directory, and a repository with no commits clones to nothing
the agent can work from or branch off. Make at least one commit, then try again."""

SBX_TOO_OLD_HELP = """this sbx is v{found}, and box needs v{minimum} or newer.
The kits box writes use the spec layout that release introduced, which older ones reject. Upgrade
sbx, then try again."""

VERSION_MISMATCH_HELP = """the sbx client and its daemon are different versions: {message}.
The daemon keeps running across an sbx upgrade, so the old one keeps answering until it is
restarted. Run sbx daemon restart, then try again."""

TOKEN_FILE_HELP = f"""{TOKEN_FILE_ENV} is not set. Set it up once:
  1. Run: claude setup-token
  2. Save the printed token to a file, e.g. ~/.secrets/claude-oauth.token
  3. Export {TOKEN_FILE_ENV} to point at that file, e.g. via direnv."""

# The one config key that is not a setting, so it is the one key whose value is not a string.
REQUIRED_MOUNTS = "required_mounts"

# What every environment variable the sandbox gets a value for may be sent to, as name to host.
SECRET_HOSTS = "secret_hosts"

# The repositories a group works on besides the one box runs in, as path to the branch to start from.
REPOS = "repos"

# The config keys holding an object rather than the text of a setting.
OBJECT_KEYS = (REQUIRED_MOUNTS, SECRET_HOSTS, REPOS)

# What a group of repositories adds to a config, which box gen writes only when asked for a group.
GROUP_SETTINGS = (REPOS, SECRET_HOSTS, "mcp")

SECRETS_FILE_HELP = f"""{{config_file}} declares {SECRET_HOSTS}, but {SECRETS_FILE_ENV} is not set.
Set it up once:
  1. Write a file holding one NAME=value line per secret, e.g. ~/.secrets/box.env
  2. Export {SECRETS_FILE_ENV} to point at that file, e.g. via direnv.
Keep that file outside every repository and every mount, since the sandbox can read those."""

MEMBER_TEMPLATE_HELP = """{path} sets template, and a sandbox runs one image.
A member cannot bring its own, so the template the group names has to cover every member of it."""

MEMBER_INSIDE_HELP = f"""{REPOS} names {{path}}, which is inside {{directory}}.
Its clone would sit inside the sandbox's own clone and leave it dirty, so a member has to live
outside the repository box runs in."""

MEMBER_MOUNTED_HELP = f"""{REPOS} names {{path}}, which the mount {{directory}} would hand over whole.
A member reaches the sandbox as a bundle of its commits, never as a mount, so nothing it does not
track can go with it."""

SECRET_INSIDE_HELP = """{variable} points at {path}, which is inside {directory}.
The sandbox can read everything there, so the file holding secrets has to sit somewhere else."""

GROUP_QUESTION = """Is this for one project, or for a group of projects?
  1  one project: the code in this folder
  2  a group: several projects next to this folder
Type 1 or 2 (Enter means 1): """

GROUP_NEXT_STEP = f"""Next: list each project under "{REPOS}" in {CONFIG_FILE}, like "../api": "main",
where main is the branch to start from."""

# Sandbox facts that hold for every project, always sent ahead of the project's own prompt file.
BASE_PROMPT = """You are running unattended in a network-restricted sandbox. Treat the next
message as your only input from the user -- nobody is available to answer
follow-ups, so make reasonable assumptions and keep going rather than
asking a question and waiting.

Commit as you go, one feature or fix per commit, rather than saving it all
for the end. Only committed work survives sandbox removal -- if the session
is cut off mid-task, uncommitted changes are gone for good.

Git hooks the project's own tooling installs are not set up here, so run the
project's checks by hand before committing.

The sandbox runs as a different user than the host, so PATH and any tool or
package caches do not point at the host's copies. A mounted host directory
keeps the path it has on the host, and there may be several homes to choose
between, so find what you need rather than assuming where it sits.

Network access is limited to an allowlist, so fetching a dependency that is
not already cached fails with 403. That is a sandbox limit, not a bug in the
code: verify what you can without it, and ask for a specific host to be
allowed rather than working around it.

If you cannot reasonably finish the task, stop, state concisely what is
blocking you, and suggest a solution -- do not keep flailing."""

# The agent starts at the repository root, so a session started below it is told where that was.
STARTED_IN_PROMPT = "You start at the repository root; this session was started from {started_in} inside it."

# What the agent is told about the other repositories a group session works on.
MEMBERS_PROMPT = """This sandbox also holds a clone of each repository below, made from a fetch of
its host copy just now:

{members}

Each one sits at the path it has on the host, on the branch named beside it, with every origin/*
branch there too. Nothing can be pushed anywhere. Commit on a branch, and every branch holding new
commits comes back to the host as a branch in that repository."""

# The network policy box gen writes, kept here with BASE_PROMPT so one script stays the whole of box.
STARTER_KIT_SPEC = """# A starting point rather than a finished policy: the agent's own API calls,
# and nothing else. Add a host for every dependency the project's checks fetch, or mount a warmed
# cache and keep them offline. Whatever is missing here is a 403 in the sandbox, not a bug.
schemaVersion: "2"
kind: mixin
name: {name}-network-policy
displayName: {name} network policy
description: The agent's own API calls and nothing else

permissions:
  network:
    allow:
      - api.anthropic.com:443
"""

# How a rejected value is named, so the message spells the type the way the JSON file does.
JSON_TYPE_NAMES: dict[type, str] = {
    type(None): "null",
    bool: "a boolean",
    int: "a number",
    float: "a number",
    list: "a list",
    dict: "an object",
}

# Config keys and their fallbacks, used for both the JSON file and the CLI.
DEFAULTS: dict[str, object] = {
    "name": "",
    "memory": "4g",
    "cpus": "4",
    "root_size": "10g",
    "docker_size": "10g",
    "model": "",
    "prompt_file": "",
    "kit": "",
    "template": "",
    "mcp": "",
    REQUIRED_MOUNTS: {},
    SECRET_HOSTS: {},
    REPOS: {},
}

# What box gen writes for one project: today's settings, so a config it writes runs on an older box.
STARTER_CONFIG: dict[str, object] = {
    **{key: value for key, value in DEFAULTS.items() if key not in GROUP_SETTINGS},
    "kit": KIT_DIR,
}

# What box gen writes for a group: the same starter, plus the keys only a group has anything to say for.
GROUP_CONFIG: dict[str, object] = {**STARTER_CONFIG, **{key: DEFAULTS[key] for key in GROUP_SETTINGS}}


class ConfigError(Exception):
    """Raised when the effective configuration cannot be used."""


@dataclass(frozen=True)
class Config:
    """Effective settings for one box run."""

    name: str
    memory: str
    cpus: str
    root_size: str
    docker_size: str
    model: str
    prompt_file: str
    kit: str
    template: str
    mcp: str
    mounts: tuple[str, ...]
    secret_hosts: tuple[Secret, ...]
    repos: tuple[Member, ...]
    members: tuple[MemberSettings, ...]


@dataclass(frozen=True)
class Member:
    """One repository a group works on besides the one box runs in, and where its clone starts."""

    path: str
    base: str


@dataclass(frozen=True)
class MemberSettings:
    """What a member's own .box/ adds to the session it is part of."""

    member: Member
    mounts: tuple[str, ...]
    kit: str
    prompt_file: str


@dataclass(frozen=True)
class Secret:
    """One environment variable the sandbox gets, and the one host its value may be sent to."""

    name: str
    host: str


@dataclass(frozen=True)
class SecretValue:
    """One declared secret, and the value this machine holds for it."""

    secret: Secret
    value: str


@dataclass(frozen=True)
class Project:
    """Where box was run, the repository root sbx clones, and the path from one to the other."""

    working_directory: Path
    root: Path
    started_in: str


@dataclass(frozen=True)
class Launch:
    """Everything resolved before the sandbox is created."""

    project: Project
    sandbox_name: str
    secrets: tuple[SecretValue, ...]
    agent_args: list[str]


# box's own token is a secret like any other, and always the first one a sandbox is given.
OAUTH_SECRET = Secret(name=SECRET_ENV, host=SECRET_HOST)


@dataclass(frozen=True)
class SandboxRef:
    """One ref a sandbox left behind, and the commit it points at."""

    ref_name: str
    commit: str


@dataclass(frozen=True)
class Checkout:
    """A repository a sandbox's work comes back to, and what it already holds."""

    path: Path
    known_commits: str


@dataclass(frozen=True)
class Bundle:
    """One member's committed history on its way into the sandbox, and where its clone belongs."""

    member: Member
    path: Path
    origin: str
    bundle_file: Path


def to_kebab_case(text: str) -> str:
    """Lowercase text and collapse runs of non-alphanumeric characters into single hyphens."""
    return re.sub(r"[^A-Za-z0-9]+", "-", text).strip("-").lower()


def default_base_name(directory: Path) -> str:
    """Derive a sandbox base name from the directory name."""
    name = to_kebab_case(directory.name)
    if not name:
        return "box"
    return name


def resolve_path(text: str) -> Path:
    """Expand a configured path so a leading ~ works the same as in the shell."""
    return Path(text).expanduser()


def load_json(path: Path) -> object:
    """Parse a JSON file, returning nothing when it is absent."""
    if not path.is_file():
        return None
    try:
        return json.loads(path.read_text())
    except json.JSONDecodeError as error:
        raise ConfigError(f"{path} is not valid JSON: {error}") from error


def name_of_type(value: object) -> str:
    """Name a JSON value's type the way the file that holds it spells it."""
    return JSON_TYPE_NAMES.get(type(value), type(value).__name__)


def to_text_value(path: Path, name: str, value: object) -> str:
    """Take a JSON scalar as the string box passes on, rejecting what has no spelling as one."""
    if isinstance(value, str):
        return value
    # A number spells itself; null, true and a container become "None", "True" and garbage.
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise ConfigError(f"{path} gives {name} {name_of_type(value)}, which is not text or a number")
    return str(value)


def as_text_values(path: Path, values: dict[str, object]) -> dict[str, str]:
    """Take every value in a JSON object as the string box passes on."""
    return {str(name): to_text_value(path, str(name), value) for name, value in values.items()}


def read_config_file(path: Path) -> dict[str, object]:
    """Read config values from the JSON file, returning empty values when it is absent."""
    loaded = load_json(path)
    if loaded is None:
        return {}
    if not isinstance(loaded, dict):
        raise ConfigError(f"{path} must contain a JSON object")
    unknown = sorted(set(loaded) - set(DEFAULTS))
    if unknown:
        raise ConfigError(f"{path} has unknown keys: {', '.join(unknown)}")
    settings = {key: value for key, value in loaded.items() if key not in OBJECT_KEYS}
    values: dict[str, object] = dict(as_text_values(path, settings))
    for key in OBJECT_KEYS:
        if key in loaded:
            values[key] = loaded[key]
    return values


def read_mounts_file(path: Path) -> dict[str, str]:
    """Read name to path from the mounts file, returning nothing when it is absent."""
    loaded = load_json(path)
    if loaded is None:
        return {}
    if not isinstance(loaded, dict):
        raise ConfigError(f"{path} must contain a JSON object of name to path")
    return as_text_values(path, loaded)


def as_descriptions(value: object) -> dict[str, str]:
    """Normalise the required_mounts value into name to description."""
    if not isinstance(value, dict):
        raise ConfigError(f"{REQUIRED_MOUNTS} must be a JSON object of name to description")
    return as_text_values(Path(CONFIG_FILE), value)


def describe_mounts(required: dict[str, str], names: list[str]) -> str:
    """List named mounts with what the project expects to find in each."""
    return "\n".join(f"  {name}: {required[name]}" for name in names)


def unfilled_mounts(required: dict[str, str], provided: dict[str, str]) -> list[str]:
    """Name the declared mounts that have no path on this machine yet."""
    return [name for name in required if provided.get(name, "") in ("", MOUNT_PLACEHOLDER)]


def require_named_mounts(required: dict[str, str], provided: dict[str, str]) -> None:
    """Reject a mounts file that does not answer the project's declaration exactly."""
    unknown = sorted(set(provided) - set(required))
    if unknown:
        raise ConfigError(f"{MOUNTS_FILE} names mounts {CONFIG_FILE} does not declare: {', '.join(unknown)}")
    unfilled = unfilled_mounts(required, provided)
    if not unfilled:
        return
    raise ConfigError(
        f"{MOUNTS_FILE} has no path on this machine for:\n{describe_mounts(required, unfilled)}\n"
        f"Run box gen to add every declared name, then replace {MOUNT_PLACEHOLDER}."
    )


def scoped(scope: str, values: dict[str, str]) -> dict[str, str]:
    """Name every entry by the member it came from, so a message says which one is at fault."""
    return {f"{scope}: {name}": value for name, value in values.items()}


def order_mounts(required: dict[str, str], provided: dict[str, str]) -> list[str]:
    """Return the declared mounts' paths in declaration order, so the sbx args never shuffle."""
    require_named_mounts(required, provided)
    return [provided[name] for name in required]


def to_secret(name: str, host: str) -> Secret:
    """Take one declared secret, rejecting a name or a host box could never send a value to."""
    if not ENVIRONMENT_NAME.fullmatch(name):
        raise ConfigError(f"{SECRET_HOSTS} name {name} is not a valid environment variable name")
    # Dropping this sandbox's secrets by host would take box's own token with them.
    if name == SECRET_ENV:
        raise ConfigError(f"{SECRET_HOSTS} cannot name {SECRET_ENV}, which carries box's own token")
    if not host:
        raise ConfigError(f"{SECRET_HOSTS} gives {name} no host, so there is nowhere its value may go")
    if host == SECRET_HOST:
        raise ConfigError(f"{SECRET_HOSTS} cannot name {SECRET_HOST}, which carries box's own token")
    return Secret(name=name, host=host)


def to_secrets(value: object) -> tuple[Secret, ...]:
    """Normalise the secret_hosts value into the variables the sandbox gets, and where each may go."""
    if not isinstance(value, dict):
        raise ConfigError(f"{SECRET_HOSTS} must be a JSON object of name to host")
    declared = as_text_values(Path(CONFIG_FILE), value)
    return tuple(to_secret(name, host) for name, host in declared.items())


def to_member(path: str, base: str) -> Member:
    """Take one member repository, rejecting a clone box would not know where to start."""
    if not path:
        raise ConfigError(f"{REPOS} names a repository with no path")
    # origin/HEAD is written when a clone is made and goes stale, so the branch is named here.
    if not base:
        raise ConfigError(f"{REPOS} gives {path} no branch to start from")
    return Member(path=path, base=base)


def to_members(value: object) -> tuple[Member, ...]:
    """Normalise the repos value into the members of this group, in the order they were declared."""
    if not isinstance(value, dict):
        raise ConfigError(f"{REPOS} must be a JSON object of path to branch")
    declared = as_text_values(Path(CONFIG_FILE), value)
    return tuple(to_member(path, base) for path, base in declared.items())


def merge_values(file_values: dict[str, object], cli_values: dict[str, object]) -> dict[str, object]:
    """Layer CLI values over file values over defaults; CLI wins."""
    merged = dict(DEFAULTS)
    merged.update(file_values)
    for key, value in cli_values.items():
        if value is None:
            continue
        merged[key] = value
    return merged


def mount_path(path: str) -> str:
    """Return the path a mount names, rejecting an empty one and any suffix box does not know."""
    if not path:
        raise ConfigError("a mount must name a path")
    if ":" in path:
        raise ConfigError(f"mount {path} has an unknown suffix; mounts are read-only unless you add :rw")
    return path


def to_workspace(mount: str) -> str:
    """Turn a configured mount into an sbx workspace spec, read-only unless :rw was asked for."""
    if mount.endswith(READ_WRITE_SUFFIX):
        return str(resolve_path(mount_path(mount[: -len(READ_WRITE_SUFFIX)])))
    return f"{resolve_path(mount_path(mount))}{READ_ONLY_SUFFIX}"


def to_workspaces(mounts: list[str]) -> tuple[str, ...]:
    """Turn the configured mounts into sbx workspace specs, passing a repeated one once."""
    workspaces: list[str] = []
    for mount in mounts:
        workspace = to_workspace(mount)
        if workspace in workspaces:
            continue
        require_one_access(workspaces, workspace)
        workspaces.append(workspace)
    return tuple(workspaces)


def require_one_access(workspaces: list[str], workspace: str) -> None:
    """Refuse a path one place mounts read-only and another writable, since only one can hold."""
    for other in workspaces:
        if mount_target(other) != mount_target(workspace):
            continue
        raise ConfigError(f"mount {mount_target(workspace)} is asked for as both {other} and {workspace}")


def setting(values: dict[str, object], key: str) -> str:
    """Read one merged setting, which reading the config file has already made a string."""
    value = values[key]
    if not isinstance(value, str):
        raise ConfigError(f"{key} is {name_of_type(value)}, which is not text")
    return value


def build_config(
    values: dict[str, object],
    mounts: list[str],
    members: tuple[MemberSettings, ...],
    working_directory: Path,
) -> Config:
    """Turn merged config values into a Config, filling in the derived sandbox name."""
    name = setting(values, "name")
    if not name:
        name = default_base_name(working_directory)
    return Config(
        name=name,
        memory=setting(values, "memory"),
        cpus=setting(values, "cpus"),
        root_size=setting(values, "root_size"),
        docker_size=setting(values, "docker_size"),
        model=setting(values, "model"),
        prompt_file=setting(values, "prompt_file"),
        kit=setting(values, "kit"),
        template=setting(values, "template"),
        mcp=setting(values, "mcp"),
        mounts=to_workspaces(mounts),
        secret_hosts=to_secrets(values[SECRET_HOSTS]),
        repos=to_members(values[REPOS]),
        members=members,
    )


def build_parser() -> argparse.ArgumentParser:
    """Define the command line interface."""
    parser = argparse.ArgumentParser(
        prog="box",
        description="Run Claude Code inside a disposable Docker sandbox.",
    )
    # Flag names match the config keys, so argparse derives every dest but MOUNT_FLAG's.
    parser.add_argument("--name", metavar="NAME", help="sandbox base name")
    parser.add_argument("--memory", metavar="SIZE", help="memory limit, e.g. 4g")
    parser.add_argument("--cpus", metavar="N", help="number of CPUs")
    parser.add_argument("--root-size", metavar="SIZE", help="sandbox root filesystem size")
    parser.add_argument("--docker-size", metavar="SIZE", help="sandbox docker size")
    parser.add_argument("--model", metavar="MODEL", help="Claude model to run")
    parser.add_argument("--prompt-file", metavar="PATH", help="file added to the prompt")
    parser.add_argument("--kit", metavar="REF", help="sbx kit reference")
    parser.add_argument("--template", metavar="REF", help="sbx template the sandbox image comes from")
    parser.add_argument("--mcp", metavar="NAMES", help="MCP servers sbx mcp add registered, comma-separated")
    parser.add_argument(
        MOUNT_FLAG, dest=MOUNT_DEST, metavar="PATH", action="append", help="read-only workspace, :rw to write"
    )
    parser.add_argument(
        "command",
        choices=["run", "config", "gen", "mount-prompt", "self-update"],
        help=f"run starts a sandbox, config prints the settings in effect, "
        f"gen writes a starter {BOX_DIR} directory, "
        "mount-prompt asks an agent to fill in this machine's paths, "
        "self-update replaces this box with the published one",
    )
    return parser


def or_unset(text: str) -> str:
    """Name what a setting holds nothing for, since a blank column reads as a missing row."""
    if not text:
        return UNSET
    return text


def format_value(value: object) -> str:
    """Render one config value, joining the mount list into a readable line."""
    if isinstance(value, tuple):
        return or_unset(" ".join(str(item) for item in value))
    return or_unset(str(value))


def format_secret(secret: Secret) -> str:
    """Name one declared secret and the host its value may go to, which is never the value."""
    return f"{secret.name}->{secret.host}"


def format_member(member: Member) -> str:
    """Name one member repository and the branch its clone starts from."""
    return f"{member.path}@{member.base}"


def format_member_settings(settings: MemberSettings) -> str:
    """Name what one member's own .box/ adds to this run, which for most members is nothing."""
    added = list(settings.mounts)
    if settings.kit:
        added.append(f"kit={settings.kit}")
    if settings.prompt_file:
        added.append(f"prompt_file={settings.prompt_file}")
    if not added:
        return f"{settings.member.path}: nothing"
    return f"{settings.member.path}: {' '.join(added)}"


def format_config(config: Config, token_file: str, secrets_file: str) -> str:
    """Render the settings in effect, the secret paths included, as aligned key/value lines."""
    items: dict[str, object] = {
        TOKEN_FILE_ENV: token_file,
        SECRETS_FILE_ENV: secrets_file,
        **asdict(config),
        SECRET_HOSTS: tuple(format_secret(secret) for secret in config.secret_hosts),
        REPOS: tuple(format_member(member) for member in config.repos),
        "members": tuple(format_member_settings(settings) for settings in config.members),
    }
    width = max(len(key) for key in items)
    lines = [f"  {key.ljust(width)}  {format_value(value)}" for key, value in items.items()]
    return "\n".join(["config in effect:", *lines])


def run_quietly(command: list[str]) -> subprocess.CompletedProcess[str]:
    """Run a command without showing its output, reporting a missing binary as a non-zero exit."""
    try:
        return subprocess.run(command, capture_output=True, text=True, check=False)
    except OSError as error:
        return subprocess.CompletedProcess(args=command, returncode=NOT_RUN, stdout="", stderr=str(error))


def capture(command: list[str]) -> str:
    """Run a command and return its stdout, or an empty string when it fails."""
    result = run_quietly(command)
    if result.returncode != 0:
        return ""
    return result.stdout


def succeeds(command: list[str]) -> bool:
    """Run a command and say only whether it worked, which is all some callers need to know."""
    return run_quietly(command).returncode == 0


def parse_ref_names(refs_output: str) -> set[str]:
    """Pull sandbox names out of refs/sandboxes/<name>/<branch> ref lines."""
    names = set()
    for line in refs_output.splitlines():
        parts = line.strip().split("/")
        if len(parts) < 4:
            continue
        names.add(parts[2])
    return names


def taken_names(checkouts: list[Checkout]) -> set[str]:
    """Collect sandbox names that are either running or still hold git refs in any repository."""
    names = set(capture(["sbx", "ls", "-q"]).split())
    for checkout in checkouts:
        command = ["git", "-C", str(checkout.path), "for-each-ref", "--format=%(refname)", SANDBOX_REFS]
        names |= parse_ref_names(capture(command))
    return names


def pick_name(base_name: str, used: set[str]) -> str:
    """Return the first <base>-<n> name that is free."""
    number = 1
    while f"{base_name}-{number}" in used:
        number += 1
    return f"{base_name}-{number}"


def read_token(path: Path) -> str:
    """Read the OAuth token, stripped of whatever whitespace the file was saved with."""
    if not path.is_file():
        raise ConfigError(f"token file {path} does not exist")
    # A stray newline or space reaches the agent as part of the token and fails far from here.
    token = path.read_text().strip()
    if not token:
        raise ConfigError(f"token file {path} is empty")
    return token


def is_quoted(value: str) -> bool:
    """Whether a value is wrapped in matching quotes, which docker would keep as part of it."""
    if len(value) < 2:
        return False
    if value[0] not in QUOTES:
        return False
    return value[0] == value[-1]


def parse_secrets_file(path: Path, contents: str) -> dict[str, str]:
    """Read NAME=value lines the way docker --env-file does, so one file can serve both."""
    values = {}
    for number, line in enumerate(contents.splitlines(), start=1):
        if not line.strip() or line.startswith("#"):
            continue
        name, separator, value = line.partition("=")
        if not separator:
            raise ConfigError(f"{path} line {number} is not NAME=value")
        # An invalid name is what "export NAME=" and a space around the = come out as.
        if not ENVIRONMENT_NAME.fullmatch(name):
            raise ConfigError(f"{path} line {number} does not start with a variable name")
        if is_quoted(value):
            raise ConfigError(f"{path} line {number} quotes its value, which docker would keep")
        values[name] = value
    return values


def read_secrets_file(path: Path) -> dict[str, str]:
    """Read the values behind the declared secrets, which box never prints."""
    if not path.is_file():
        raise ConfigError(f"secrets file {path} does not exist")
    return parse_secrets_file(path, path.read_text())


def read_secret_values(secrets: tuple[Secret, ...], secrets_file: str) -> tuple[SecretValue, ...]:
    """Give every declared secret the value this machine has for it, and nothing else that file holds."""
    if not secrets:
        return ()
    if not secrets_file:
        raise ConfigError(SECRETS_FILE_HELP.format(config_file=CONFIG_FILE))
    path = resolve_path(secrets_file)
    values = read_secrets_file(path)
    stored = []
    for secret in secrets:
        value = values.get(secret.name, "")
        if not value:
            raise ConfigError(f"{path} has no value for {secret.name}, which {CONFIG_FILE} declares")
        stored.append(SecretValue(secret=secret, value=value))
    return tuple(stored)


def read_system_prompt(prompt_file: str) -> str:
    """Read the extra system prompt, or return nothing when no file is configured."""
    if not prompt_file:
        return ""
    path = resolve_path(prompt_file)
    if not path.is_file():
        raise ConfigError(f"prompt file {path} does not exist")
    return path.read_text()


def build_system_prompt(parts: list[str]) -> str:
    """Join what the agent is told, leaving out every part this run has nothing to say for."""
    return "\n\n".join(part for part in parts if part)


def build_started_in_prompt(project: Project) -> str:
    """Say which folder the session was started from, which is nothing when that is the root."""
    if not project.started_in:
        return ""
    return STARTED_IN_PROMPT.format(started_in=project.started_in)


def build_members_prompt(config: Config, project: Project) -> str:
    """Say where each member's clone is, what it starts on, and how its commits come back."""
    if not config.repos:
        return ""
    directory = project.working_directory
    where = [f"  {member_path(directory, member)} on {member.base}" for member in config.repos]
    return MEMBERS_PROMPT.format(members="\n".join(where))


def build_member_prompts(config: Config, project: Project) -> list[str]:
    """Read each member's own prompt, headed with the path its clone sits at."""
    prompts = []
    for settings in config.members:
        if not settings.prompt_file:
            continue
        path = member_path(project.working_directory, settings.member)
        prompts.append(f"{path}:\n\n{read_system_prompt(settings.prompt_file)}")
    return prompts


def build_environment(config: Config) -> dict[str, str]:
    """Copy the current environment and add the sbx disk limits."""
    environment = dict(os.environ)
    environment["DOCKER_SANDBOXES_ROOT_SIZE"] = config.root_size
    environment["DOCKER_SANDBOXES_DOCKER_SIZE"] = config.docker_size
    return environment


def repository_root(working_directory: Path) -> Path:
    """Find the root of the repository box runs in, which is what sbx clones."""
    root = capture(["git", "-C", str(working_directory), "rev-parse", "--show-toplevel"]).strip()
    if not root:
        return working_directory
    return Path(root)


def path_below(root: Path, working_directory: Path) -> str:
    """Name the working directory relative to the root, which is nothing when it is the root."""
    try:
        relative = working_directory.resolve().relative_to(root.resolve())
    except ValueError:
        return ""
    if str(relative) == ".":
        return ""
    return str(relative)


def build_project(working_directory: Path) -> Project:
    """Locate the repository sbx clones, and where inside it this session was started."""
    root = repository_root(working_directory)
    return Project(
        working_directory=working_directory, root=root, started_in=path_below(root, working_directory)
    )


def clone_path(project: Project) -> str:
    """The path sbx clones: the working directory itself, unless box was run below the root."""
    if not project.started_in:
        return "."
    return str(project.root)


def kits(config: Config) -> list[str]:
    """List the kits this sandbox runs under: the group's, then each member's, and each one once."""
    wanted = []
    for kit in [config.kit, *[settings.kit for settings in config.members]]:
        if not kit or kit in wanted:
            continue
        wanted.append(kit)
    return wanted


def build_create_command(config: Config, project: Project, sandbox_name: str) -> list[str]:
    """Assemble the sbx create invocation."""
    command = ["sbx", "create", "claude", clone_path(project)]
    command.extend(config.mounts)
    command.extend(["--clone", "--name", sandbox_name])
    command.extend(["--memory", config.memory, "--cpus", config.cpus])
    # Two kits are one allowlist, so a member's hosts are added to the group's rather than replacing them.
    for kit in kits(config):
        command.extend(["--kit", kit])
    # An unset template leaves the image to sbx, which is what almost every project wants.
    if config.template:
        command.extend(["--template", config.template])
    # The names are the user's own registered servers, so an unset mcp asks sbx for none of them.
    if config.mcp:
        command.extend(["--static-mcp", config.mcp])
    return command


def build_agent_args(config: Config, system_prompt: str) -> list[str]:
    """Assemble the arguments passed through to the Claude CLI."""
    args = []
    if system_prompt:
        args.extend(["--append-system-prompt", system_prompt])
    if config.model:
        args.extend(["--model", config.model])
    return args


def build_run_command(sandbox_name: str, agent_args: list[str]) -> list[str]:
    """Assemble the sbx run invocation."""
    command = ["sbx", "run", "claude", "--name", sandbox_name]
    if agent_args:
        command.append("--")
        command.extend(agent_args)
    return command


def distinct_hosts(secrets: tuple[Secret, ...]) -> list[str]:
    """List the hosts this sandbox has secrets for, box's own token host first and each one once."""
    hosts = [SECRET_HOST]
    for secret in secrets:
        if secret.host in hosts:
            continue
        hosts.append(secret.host)
    return hosts


def drop_secrets(secrets: tuple[Secret, ...], sandbox_name: str) -> None:
    """Remove every stored secret for this sandbox name, ignoring failures."""
    for host in distinct_hosts(secrets):
        capture(["sbx", "secret", "rm", "--sandbox", sandbox_name, "--host", host, "-f"])


def build_store_command(sandbox_name: str, secret: Secret) -> list[str]:
    """Assemble the sbx secret invocation, which takes the value on stdin rather than as an argument."""
    return [
        "sbx",
        "secret",
        "set-custom",
        "--sandbox",
        sandbox_name,
        "--host",
        secret.host,
        "--env",
        secret.name,
    ]


def store_secret(sandbox_name: str, stored: SecretValue) -> None:
    """Hand one secret's value to sbx over stdin so it never lands in the shell history."""
    try:
        result = subprocess.run(
            build_store_command(sandbox_name, stored.secret), input=stored.value, text=True, check=False
        )
    except OSError as error:
        raise ConfigError(f"could not run sbx: {error}") from error
    if result.returncode != 0:
        raise ConfigError(f"sbx would not store {stored.secret.name} for {sandbox_name}")


def print_recovery(path: Path, sandbox_name: str) -> None:
    """Print how to look inside a sandbox box kept, take work out of it, and remove it by hand."""
    print(f"Inspect:  sbx exec {sandbox_name} git -C {path} diff", file=sys.stderr)
    print(f"Recover:  sbx cp {sandbox_name}:{path}/<file> .", file=sys.stderr)
    print(f"Then remove manually once safe: sbx rm --force {sandbox_name}", file=sys.stderr)


def warn_dirty(path: Path, sandbox_name: str, dirty: str) -> None:
    """Tell the user how to recover uncommitted work left behind in a sandbox."""
    warning = f"WARNING: {path} in sandbox {sandbox_name} has uncommitted changes -- not removing it."
    print(warning, file=sys.stderr)
    print(dirty, file=sys.stderr)
    print_recovery(path, sandbox_name)


def warn_unchecked(path: Path, sandbox_name: str, reason: str) -> None:
    """Tell the user box could not find out whether removing a sandbox would lose work."""
    print(f"WARNING: {reason}.", file=sys.stderr)
    print(f"box cannot tell whether sandbox {sandbox_name} holds work -- not removing it.", file=sys.stderr)
    print_recovery(path, sandbox_name)


def plural(count: str, noun: str) -> str:
    """Render a count and what it counts, so a single commit does not read as "1 commits"."""
    if count == "1":
        return f"1 {noun}"
    return f"{count} {noun}s"


def to_branch_name(text: str) -> str:
    """Turn an agent's answer into a branch name: its last line, kebab-cased and cut to five words."""
    lines = [line for line in text.splitlines() if line.strip()]
    if not lines:
        return ""
    words = to_kebab_case(lines[-1]).split("-")
    return "-".join(words[:BRANCH_NAME_WORDS]).strip("-")


def build_branch_name_command(subjects: str) -> list[str]:
    """Assemble the headless Claude invocation that names a branch after the sandbox's work."""
    return ["claude", "-p", f"{BRANCH_NAME_PROMPT}\n{subjects}"]


def suggest_branch_name(subjects: str) -> str:
    """Ask Claude for a branch name, returning nothing when it fails, stalls or is not installed."""
    try:
        result = subprocess.run(
            build_branch_name_command(subjects),
            capture_output=True,
            text=True,
            check=False,
            timeout=BRANCH_NAME_TIMEOUT_SECONDS,
        )
    except (subprocess.TimeoutExpired, OSError):
        return ""
    if result.returncode != 0:
        return ""
    return to_branch_name(result.stdout)


def parse_sandbox_refs(refs_output: str) -> list[SandboxRef]:
    """Pull the ref name and commit out of for-each-ref lines."""
    refs = []
    for line in refs_output.splitlines():
        parts = line.split()
        if len(parts) != 2:
            continue
        refs.append(SandboxRef(ref_name=parts[0], commit=parts[1]))
    return refs


def sandbox_refs(checkout: Checkout, sandbox_name: str) -> list[SandboxRef]:
    """Read the refs this sandbox's work was fetched into."""
    command = [
        "git",
        "-C",
        str(checkout.path),
        "for-each-ref",
        "--format=%(refname) %(objectname)",
        f"{SANDBOX_REFS}/{sandbox_name}",
    ]
    return parse_sandbox_refs(capture(command))


def new_commits(checkout: Checkout, commit: str) -> list[str]:
    """Name the commits a ref holds that its repository does not, the way git spells a range."""
    return [commit, "--not", checkout.known_commits]


def count_new_commits(checkout: Checkout, commit: str) -> str:
    """Count the sandbox's commits this repository lacks, returning nothing when git could not say."""
    command = ["git", "-C", str(checkout.path), "rev-list", "--count", *new_commits(checkout, commit)]
    return capture(command).strip()


def new_commit_subjects(checkout: Checkout, commit: str) -> str:
    """Read the subjects of the sandbox's commits, which are what a branch gets named after."""
    return capture(["git", "-C", str(checkout.path), "log", "--format=%s", *new_commits(checkout, commit)])


def local_branch_names(checkout: Checkout) -> set[str]:
    """Collect the branch names this repository already has."""
    command = ["git", "-C", str(checkout.path), "for-each-ref", "--format=%(refname:short)", "refs/heads"]
    return set(capture(command).split())


def pick_branch_name(branch: str, used: set[str]) -> str:
    """Return the suggested name, numbered from two when the repository already has it."""
    if branch not in used:
        return branch
    number = 2
    while f"{branch}-{number}" in used:
        number += 1
    return f"{branch}-{number}"


def create_branch(checkout: Checkout, branch: str, commit: str) -> bool:
    """Point a new branch at the sandbox's commit, saying whether git accepted it."""
    return succeeds(["git", "-C", str(checkout.path), "branch", branch, commit])


def delete_ref(checkout: Checkout, ref_name: str) -> None:
    """Drop a ref, which is safe to leave behind when it fails."""
    capture(["git", "-C", str(checkout.path), "update-ref", "-d", ref_name])


def settle_ref(checkout: Checkout, ref: SandboxRef) -> None:
    """Turn one sandbox ref into a branch, drop it when it holds nothing, and keep it otherwise."""
    count = count_new_commits(checkout, ref.commit)
    if not count:
        print(f"box: git could not read {ref.ref_name}, so it was kept.", file=sys.stderr)
        return
    if count == "0":
        delete_ref(checkout, ref.ref_name)
        print(f"box: {ref.ref_name} held no commits, so it was dropped.", file=sys.stderr)
        return
    suggested = suggest_branch_name(new_commit_subjects(checkout, ref.commit))
    if not suggested:
        print(f"box: naming a branch failed, so the work stayed on {ref.ref_name}.", file=sys.stderr)
        return
    branch = pick_branch_name(suggested, local_branch_names(checkout))
    if not create_branch(checkout, branch, ref.commit):
        print(f"box: git refused branch {branch}, so the work stayed on {ref.ref_name}.", file=sys.stderr)
        return
    delete_ref(checkout, ref.ref_name)
    print(f"box: branch {branch} holds {plural(count, 'commit')} from {ref.ref_name}.", file=sys.stderr)


def settle_sandbox_refs(checkout: Checkout, sandbox_name: str) -> None:
    """Give the sandbox's committed work a branch, so nothing is left addressable only by ref."""
    for ref in sandbox_refs(checkout, sandbox_name):
        settle_ref(checkout, ref)


def member_origin(path: Path) -> str:
    """Read a member's origin URL, so its clone names the same project the host copy does."""
    return capture(["git", "-C", str(path), "remote", "get-url", "origin"]).strip()


def fetch_member(path: Path) -> None:
    """Bring a member's origin refs up to date, with the terminal attached so ssh can ask for a key."""
    print(f"box: fetching {path}", file=sys.stderr)
    # A fetch writes the origin/* refs and their objects and nothing else, so the checkout is untouched.
    if subprocess.run(["git", "-C", str(path), "fetch", "origin"], check=False).returncode != 0:
        raise ConfigError(f"git fetch origin failed in {path}")


def has_base(path: Path, base: str) -> bool:
    """Whether the branch a member's clone starts from is one origin has."""
    return succeeds(["git", "-C", str(path), "rev-parse", "--verify", f"refs/remotes/origin/{base}"])


def bundle_member(project: Project, member: Member, number: int, directory: Path) -> Bundle:
    """Fetch one member and pack the history its clone is made from into the bundle directory."""
    path = member_path(project.working_directory, member)
    fetch_member(path)
    if not has_base(path, member.base):
        raise ConfigError(f"{member.path} has no branch {member.base} on origin")
    bundle_file = directory / f"{number}-{path.name}.bundle"
    command = ["git", "-C", str(path), "bundle", "create", str(bundle_file), "--remotes=origin"]
    if not succeeds(command):
        raise ConfigError(f"git could not bundle {member.path}")
    return Bundle(member=member, path=path, origin=member_origin(path), bundle_file=bundle_file)


def bundle_members(config: Config, project: Project, directory: Path) -> list[Bundle]:
    """Fetch every member one at a time, and pack what each clone is made from."""
    return [
        bundle_member(project, member, number, directory)
        for number, member in enumerate(config.repos, start=1)
    ]


def build_clone_commands(bundle: Bundle, sandbox_name: str) -> list[list[str]]:
    """Assemble what turns a copied bundle into a clone on the member's own host path."""
    inside = f"{SANDBOX_TEMP}/{bundle.bundle_file.name}"
    path = str(bundle.path)
    base = bundle.member.base
    root = ["sbx", "exec", "-u", "root", sandbox_name]
    user = ["sbx", "exec", sandbox_name]
    return [
        ["sbx", "cp", str(bundle.bundle_file), f"{sandbox_name}:{inside}"],
        # A member's parent need not belong to the default user, and git init would fail there.
        [*root, "mkdir", "-p", path],
        [*root, "chown", f"{SANDBOX_USER}:{SANDBOX_USER}", path],
        [*user, "git", "init", "-q", path],
        [*user, "git", "-C", path, "remote", "add", "origin", bundle.origin],
        [*user, "git", "-C", path, "fetch", "-q", inside, "refs/remotes/origin/*:refs/remotes/origin/*"],
        [*user, "git", "-C", path, "switch", "-q", "-c", base, "--track", f"origin/{base}"],
    ]


def clone_member(bundle: Bundle, sandbox_name: str) -> bool:
    """Put one member's clone in the sandbox, saying whether every step of it worked."""
    for command in build_clone_commands(bundle, sandbox_name):
        if succeeds(command):
            continue
        return False
    return True


def build_status_command(path: Path, sandbox_name: str) -> list[str]:
    """Assemble the sbx exec that asks the sandbox whether one clone holds uncommitted work."""
    return ["sbx", "exec", sandbox_name, "git", "-C", str(path), "status", "--porcelain"]


def main_checkout(project: Project) -> Checkout:
    """The repository box runs in, whose work is what its own checkout does not already hold."""
    return Checkout(path=project.root, known_commits=MAIN_KNOWN)


def member_checkouts(config: Config, project: Project) -> list[Checkout]:
    """The members, whose work is what none of their origin branches hold."""
    checkouts = []
    for member in config.repos:
        path = member_path(project.working_directory, member)
        checkouts.append(Checkout(path=path, known_commits=MEMBER_KNOWN))
    return checkouts


def build_checkouts(config: Config, project: Project) -> list[Checkout]:
    """List every repository a sandbox's work comes back to: the one box runs in, then the members."""
    return [main_checkout(project), *member_checkouts(config, project)]


def fetch_member_work(checkout: Checkout, sandbox_name: str, directory: Path) -> bool:
    """Bundle one member's branches inside the sandbox and fetch them into the member on this host."""
    name = f"{sandbox_name}-{checkout.path.name}.bundle"
    inside = f"{SANDBOX_TEMP}/{name}"
    path = str(checkout.path)
    bundle = ["git", "-C", path, "bundle", "create", inside, "--branches"]
    if not succeeds(["sbx", "exec", sandbox_name, *bundle]):
        return False
    here = directory / name
    if not succeeds(["sbx", "cp", f"{sandbox_name}:{inside}", str(here)]):
        return False
    refspec = f"+refs/heads/*:{SANDBOX_REFS}/{sandbox_name}/*"
    return succeeds(["git", "-C", path, "fetch", str(here), refspec])


def fetch_committed_work(members: list[Checkout], sandbox_name: str) -> Checkout | None:
    """Bring every clone's commits back, or name the repository whose work stayed in the sandbox."""
    # A member is a remote of nothing, so its commits come back the way they went in: as a bundle.
    with tempfile.TemporaryDirectory() as directory:
        for checkout in members:
            if fetch_member_work(checkout, sandbox_name, Path(directory)):
                continue
            return checkout
    return None


def clones_are_committed(checkouts: list[Checkout], sandbox_name: str) -> bool:
    """Say whether every clone in the sandbox is committed, warning about the first that is not."""
    for checkout in checkouts:
        status = run_quietly(build_status_command(checkout.path, sandbox_name))
        if status.returncode != 0:
            warn_unchecked(checkout.path, sandbox_name, "sbx exec could not read the sandbox's git status")
            return False
        if status.stdout.strip():
            warn_dirty(checkout.path, sandbox_name, status.stdout)
            return False
    return True


def cleanup(config: Config, launch: Launch) -> None:
    """Pull committed work back, then drop the sandbox unless work would be lost."""
    sandbox_name = launch.sandbox_name
    main = main_checkout(launch.project)
    members = member_checkouts(config, launch.project)
    remote = f"sandbox-{sandbox_name}"
    # Removal follows, so "I could not tell" must never be read as "there is nothing to lose".
    if not succeeds(["git", "fetch", remote]):
        warn_unchecked(main.path, sandbox_name, f"git fetch {remote} failed, so its commits are not here")
        return
    unfetched = fetch_committed_work(members, sandbox_name)
    if unfetched is not None:
        reason = f"the sandbox's commits in {unfetched.path} are not here"
        warn_unchecked(unfetched.path, sandbox_name, reason)
        return
    checkouts = [main, *members]
    if not clones_are_committed(checkouts, sandbox_name):
        return
    for checkout in checkouts:
        settle_sandbox_refs(checkout, sandbox_name)
    drop_secrets(config.secret_hosts, sandbox_name)
    subprocess.run(["sbx", "rm", "--force", sandbox_name], check=False)


def own_mounts(working_directory: Path, required: dict[str, str]) -> list[str]:
    """Read the mounts one .box/ declares and this machine answers, in declaration order."""
    provided = read_mounts_file(working_directory / MOUNTS_FILE)
    return order_mounts(required, provided)


def member_setting(values: dict[str, object], key: str) -> str:
    """Read one setting a member's config holds, which is nothing when it holds none."""
    if key not in values:
        return ""
    return setting(values, key)


def member_kit(directory: Path, kit: str) -> str:
    """Resolve a member's kit against the member itself, since its path is written relative to it."""
    if not kit:
        return ""
    path = directory / resolve_path(kit)
    # A kit that is not on disk is a reference sbx resolves itself, and no path of this machine's.
    if not path.exists():
        return kit
    return str(path)


def read_member_settings(working_directory: Path, member: Member) -> MemberSettings:
    """Read what a member's own .box/ adds, which is nothing at all when it has none."""
    directory = member_path(working_directory, member)
    path = directory / CONFIG_FILE
    # A member with no box setup of its own is a repository to clone and nothing more.
    if not path.is_file():
        return MemberSettings(member=member, mounts=(), kit="", prompt_file="")
    values = read_config_file(path)
    if member_setting(values, "template"):
        raise ConfigError(MEMBER_TEMPLATE_HELP.format(path=member.path))
    required = as_descriptions(values.get(REQUIRED_MOUNTS, {}))
    provided = read_mounts_file(directory / MOUNTS_FILE)
    mounts = order_mounts(scoped(member.path, required), scoped(member.path, provided))
    prompt_file = member_setting(values, "prompt_file")
    if prompt_file:
        prompt_file = str(directory / resolve_path(prompt_file))
    return MemberSettings(
        member=member,
        mounts=tuple(mounts),
        kit=member_kit(directory, member_setting(values, "kit")),
        prompt_file=prompt_file,
    )


def read_members(working_directory: Path, members: tuple[Member, ...]) -> tuple[MemberSettings, ...]:
    """Read every member's own settings, in the order the group declared them."""
    return tuple(read_member_settings(working_directory, member) for member in members)


def member_mounts(members: tuple[MemberSettings, ...]) -> list[str]:
    """Collect what the members ask to have mounted, in the order the group declared them."""
    mounts: list[str] = []
    for settings in members:
        mounts.extend(settings.mounts)
    return mounts


def load_config(arguments: argparse.Namespace, working_directory: Path) -> Config:
    """Combine the JSON files and the CLI arguments into the effective config."""
    cli_values = {key: value for key, value in vars(arguments).items() if key in DEFAULTS}
    file_values = read_config_file(working_directory / CONFIG_FILE)
    values = merge_values(file_values, cli_values)
    members = read_members(working_directory, to_members(values[REPOS]))
    own = own_mounts(working_directory, as_descriptions(values[REQUIRED_MOUNTS]))
    # The flags come last, since they are this one run's rather than anyone's settings.
    mounts = own + member_mounts(members) + list(arguments.mounts or [])
    return build_config(values, mounts, members, working_directory)


def token_file_from_environment() -> str:
    """Read the token path from the environment, which is the only place it comes from."""
    return os.environ.get(TOKEN_FILE_ENV, "")


def member_path(working_directory: Path, member: Member) -> Path:
    """Where a member sits on this host, which is where its clone sits in the sandbox."""
    return (working_directory / resolve_path(member.path)).resolve()


def secrets_file_from_environment() -> str:
    """Read the secrets path from the environment, for the same reason the token path comes from it."""
    return os.environ.get(SECRETS_FILE_ENV, "")


def mount_target(workspace: str) -> Path:
    """The host path one sbx workspace spec names, whether the sandbox may write to it or not."""
    if workspace.endswith(READ_ONLY_SUFFIX):
        return Path(workspace[: -len(READ_ONLY_SUFFIX)])
    return Path(workspace)


def reachable_paths(config: Config, project: Project) -> list[Path]:
    """Every host directory the sandbox can read: the repository it clones, its members, and the mounts."""
    members = [member_path(project.working_directory, member) for member in config.repos]
    return [project.root, *members] + [mount_target(workspace) for workspace in config.mounts]


def is_inside(path: Path, directory: Path) -> bool:
    """Whether a path lies in a directory, symlinks resolved, the directory itself included."""
    try:
        path.resolve().relative_to(directory.resolve())
    except ValueError:
        return False
    return True


def require_secret_outside(variable: str, secret_file: str, reachable: list[Path]) -> None:
    """Refuse a file of secrets the sandbox could read for itself."""
    if not secret_file:
        return
    path = resolve_path(secret_file)
    for directory in reachable:
        if not is_inside(path, directory):
            continue
        raise ConfigError(SECRET_INSIDE_HELP.format(variable=variable, path=path, directory=directory))


def has_origin(path: Path) -> bool:
    """Whether a repository has the remote its clone is made from and counts its commits against."""
    return succeeds(["git", "-C", str(path), "remote", "get-url", "origin"])


def require_unmounted(config: Config, member: Member, path: Path) -> None:
    """Refuse a mount that would hand the sandbox everything a member holds, tracked or not."""
    for workspace in config.mounts:
        directory = mount_target(workspace)
        if not is_inside(path, directory):
            continue
        raise ConfigError(MEMBER_MOUNTED_HELP.format(path=member.path, directory=directory))


def require_members(config: Config, project: Project) -> None:
    """Refuse a member box could not clone, and two members that would land on one another."""
    seen: dict[Path, str] = {}
    for member in config.repos:
        path = member_path(project.working_directory, member)
        if not is_git_repository(path):
            raise ConfigError(f"{REPOS} names {member.path}, which is not a git repository")
        if not has_origin(path):
            raise ConfigError(f"{REPOS} names {member.path}, which has no origin remote")
        if is_inside(path, project.root):
            raise ConfigError(MEMBER_INSIDE_HELP.format(path=member.path, directory=project.root))
        if path in seen:
            raise ConfigError(f"{REPOS} names one repository twice: {seen[path]} and {member.path}")
        seen[path] = member.path
        require_unmounted(config, member, path)
        require_ignored_local_paths(path, f"{member.path}: ")


def require_secrets(config: Config, project: Project, token_file: str) -> None:
    """Refuse secrets the sandbox could read itself, and declarations this machine has no value for."""
    reachable = reachable_paths(config, project)
    require_secret_outside(TOKEN_FILE_ENV, token_file, reachable)
    # An unset BOX_SECRETS_FILE is only missing when the project declares secrets to read from it.
    if not config.secret_hosts:
        return
    secrets_file = secrets_file_from_environment()
    require_secret_outside(SECRETS_FILE_ENV, secrets_file, reachable)
    read_secret_values(config.secret_hosts, secrets_file)


def require_settings(config: Config) -> None:
    """Reject settings whose default would be a silent risk rather than a convenience."""
    if not config.kit:
        raise ConfigError(KIT_HELP)
    # A kit that is not on disk is a reference sbx resolves itself, so only a local file is wrong.
    if resolve_path(config.kit).is_file():
        raise ConfigError(KIT_FILE_HELP)
    for settings in config.members:
        if resolve_path(settings.kit).is_file():
            raise ConfigError(f"{settings.member.path}: {KIT_FILE_HELP}")
    if not config.model:
        raise ConfigError(MODEL_HELP)


def is_git_repository(working_directory: Path) -> bool:
    """Whether git reads this directory as a working tree, which is what sbx --clone needs."""
    return succeeds(["git", "-C", str(working_directory), "rev-parse", "--git-dir"])


def has_commits(working_directory: Path) -> bool:
    """Whether HEAD names a commit, which a repository nobody has committed to yet does not."""
    return succeeds(["git", "-C", str(working_directory), "rev-parse", "--verify", "HEAD"])


def require_git_repository(working_directory: Path) -> None:
    """Refuse to run where sbx create --clone would have nothing to clone."""
    if not is_git_repository(working_directory):
        raise ConfigError(NOT_A_REPOSITORY_HELP)
    if not has_commits(working_directory):
        raise ConfigError(NO_COMMITS_HELP)


def is_git_ignored(working_directory: Path, relative_path: str) -> bool:
    """Ask git whether a path is ignored; check-ignore exits 0 only when it is."""
    return succeeds(["git", "-C", str(working_directory), "check-ignore", "-q", relative_path])


def require_ignored_local_paths(directory: Path, scope: str) -> None:
    """Refuse to run while anything holding this machine's own files could be committed."""
    for relative_path, help_text in LOCAL_PATHS.items():
        if not (directory / relative_path).exists():
            continue
        if is_git_ignored(directory, relative_path):
            continue
        raise ConfigError(f"{scope}{help_text}")


def missing_binaries(names: tuple[str, ...]) -> list[str]:
    """Name the commands PATH does not have, in the order box would need them."""
    return [name for name in names if shutil.which(name) is None]


def build_sbx_version_command() -> list[str]:
    """Assemble the sbx version invocation, whose line names the release box is talking to."""
    return ["sbx", "version"]


def parse_sbx_version(version_output: str) -> tuple[int, ...]:
    """Pull the release number out of sbx version's line, or nothing when it holds none."""
    match = SBX_VERSION_PATTERN.search(version_output)
    if match is None:
        return ()
    return tuple(int(part) for part in match.groups())


def require_supported_sbx() -> None:
    """Refuse to run on an sbx older than the release box's kits and commands are written against."""
    # No sbx means nothing to read, and the commands that need it reject its absence themselves.
    if shutil.which("sbx") is None:
        return
    found = parse_sbx_version(capture(build_sbx_version_command()))
    # A line box cannot read is no evidence of an old sbx, so it must not stop a working command.
    if not found:
        return
    if found >= SBX_MINIMUM:
        return
    raise ConfigError(SBX_TOO_OLD_HELP.format(found=to_version(found), minimum=to_version(SBX_MINIMUM)))


def build_diagnose_command() -> list[str]:
    """Assemble the sbx diagnose invocation, whose JSON compares the CLI with its daemon."""
    return ["sbx", "diagnose", "-o", "json"]


def parse_version_mismatch(diagnose_output: str) -> str:
    """Pull a failed version check's message out of diagnose JSON, or nothing when versions agree."""
    try:
        checks = json.loads(diagnose_output)["checks"]
    except (json.JSONDecodeError, TypeError, KeyError):
        return ""
    for check in checks:
        if not isinstance(check, dict):
            continue
        if check.get("name") != VERSION_MATCH_CHECK:
            continue
        if check.get("status") == CHECK_PASSED:
            return ""
        message = str(check.get("message", ""))
        if not message:
            return "sbx did not say which versions"
        return message
    return ""


def require_matching_versions() -> None:
    """Refuse to run when the sbx CLI and the daemon it talks to come from different releases."""
    # No sbx means nothing to compare, and the commands that need it reject its absence themselves.
    if shutil.which("sbx") is None:
        return
    # A daemon that is not running is no mismatch: sbx starts its own version when it needs one.
    mismatch = parse_version_mismatch(run_quietly(build_diagnose_command()).stdout)
    if not mismatch:
        return
    raise ConfigError(VERSION_MISMATCH_HELP.format(message=mismatch))


def require_binaries() -> None:
    """Refuse to run without the commands box shells out to, rather than failing at the first one."""
    missing = missing_binaries(REQUIRED_BINARIES)
    if not missing:
        return
    raise ConfigError(MISSING_BINARIES_HELP.format(missing=", ".join(missing)))


def require_config_file(working_directory: Path) -> None:
    """Send a project with no box setup at all to the command that writes one."""
    if not (working_directory / CONFIG_FILE).is_file():
        raise ConfigError(NO_CONFIG_HELP)


def require_project(config: Config, project: Project, token_file: str) -> None:
    """Run every check on the settings and the project that does not create anything."""
    # Nothing box does works without these, so they come before anything about this project.
    require_binaries()
    # A first-timer has no settings to be told about yet, so the missing file comes next.
    require_config_file(project.working_directory)
    require_settings(config)
    require_git_repository(project.working_directory)
    require_ignored_local_paths(project.working_directory, "")
    require_members(config, project)
    require_secrets(config, project, token_file)


def prepare_launch(config: Config, project: Project, token_file: str) -> Launch:
    """Resolve everything that can still fail before the sandbox exists."""
    require_project(config, project, token_file)
    if not token_file:
        raise ConfigError(TOKEN_FILE_HELP)
    token = SecretValue(secret=OAUTH_SECRET, value=read_token(resolve_path(token_file)))
    declared = read_secret_values(config.secret_hosts, secrets_file_from_environment())
    parts = [
        BASE_PROMPT,
        build_started_in_prompt(project),
        read_system_prompt(config.prompt_file),
        build_members_prompt(config, project),
        *build_member_prompts(config, project),
    ]
    agent_args = build_agent_args(config, build_system_prompt(parts))
    return Launch(
        project=project,
        sandbox_name=pick_name(config.name, taken_names(build_checkouts(config, project))),
        secrets=(token, *declared),
        agent_args=agent_args,
    )


def remove_sandbox(config: Config, launch: Launch, reason: str) -> int:
    """Take back a sandbox that holds nothing yet, since the run it was made for cannot start."""
    drop_secrets(config.secret_hosts, launch.sandbox_name)
    subprocess.run(["sbx", "rm", "--force", launch.sandbox_name], check=False)
    print(f"box: {reason}, so {launch.sandbox_name} was removed again.", file=sys.stderr)
    return 1


def clone_members(config: Config, launch: Launch, bundles: list[Bundle]) -> int:
    """Put every member's clone in the fresh sandbox, saying whether the run can start."""
    for bundle in bundles:
        if clone_member(bundle, launch.sandbox_name):
            continue
        return remove_sandbox(config, launch, f"{bundle.member.path} could not be cloned")
    return 0


def start_session(config: Config, launch: Launch, bundles: list[Bundle]) -> int:
    """Create the sandbox, clone the members into it, run Claude, and clean up afterwards."""
    environment = build_environment(config)
    # sbx injects the placeholder env vars when the sandbox is created, so they must exist by then.
    drop_secrets(config.secret_hosts, launch.sandbox_name)
    for stored in launch.secrets:
        store_secret(launch.sandbox_name, stored)
    create = build_create_command(config, launch.project, launch.sandbox_name)
    # sbx has already said why it failed, and there is no sandbox to run in, clean up or keep.
    if subprocess.run(create, env=environment, check=False).returncode != 0:
        # Two runs can pick one name and the loser drops the winner's secret, which sbx has
        # already injected into the running sandbox, so the winner keeps working regardless.
        drop_secrets(config.secret_hosts, launch.sandbox_name)
        print(f"box: sbx create failed, so {launch.sandbox_name} was never started.", file=sys.stderr)
        return 1
    cloned = clone_members(config, launch, bundles)
    if cloned != 0:
        return cloned
    try:
        command = build_run_command(launch.sandbox_name, launch.agent_args)
        result = subprocess.run(command, env=environment, check=False)
        return result.returncode
    finally:
        cleanup(config, launch)


def run_session(config: Config, launch: Launch) -> int:
    """Pack up the members this run needs, then hand the sandbox over to the agent."""
    # The bundles are made before anything is created, so a member that cannot be read costs nothing.
    with tempfile.TemporaryDirectory() as directory:
        bundles = bundle_members(config, launch.project, Path(directory))
        return start_session(config, launch, bundles)


def to_flag(key: str) -> str:
    """Render an argparse dest the way the user typed it on the command line."""
    if key == MOUNT_DEST:
        return MOUNT_FLAG
    return "--" + key.replace("_", "-")


def require_no_flags(arguments: argparse.Namespace) -> None:
    """Reject flags passed to a command, which reads the config rather than taking settings."""
    # Every flag defaults to None, so a value at all is a value the user typed.
    given = sorted(
        to_flag(key) for key, value in vars(arguments).items() if key != "command" and value is not None
    )
    if not given:
        return
    raise ConfigError(
        f"{arguments.command} takes no flags, but got {', '.join(given)}; edit {CONFIG_FILE} instead"
    )


def to_json(contents: object) -> str:
    """Render what box gen writes, indented and newline-terminated like a hand-edited file."""
    return json.dumps(contents, indent=2) + "\n"


def append_line(path: Path, line: str) -> None:
    """Add a line to a file, starting a new one when the file does not end in a newline."""
    existing = ""
    if path.is_file():
        existing = path.read_text()
    if existing and not existing.endswith("\n"):
        existing = f"{existing}\n"
    path.write_text(f"{existing}{line}\n")


def ignore_local_paths(working_directory: Path) -> None:
    """Write the .gitignore entries box would otherwise refuse to run without."""
    # Outside a repository check-ignore answers for nothing, so every gen would append the lines again.
    if not is_git_repository(working_directory):
        print(f"skipped {GITIGNORE_FILE}, since this is not a git repository")
        return
    for relative_path in LOCAL_PATHS:
        if is_git_ignored(working_directory, relative_path):
            continue
        append_line(working_directory / GITIGNORE_FILE, relative_path)
        print(f"ignored {relative_path} in {GITIGNORE_FILE}")


def write_starter_config(path: Path, starter: dict[str, object]) -> None:
    """Write every setting at its default, unless the project already has a config."""
    if path.exists():
        print(f"kept    {CONFIG_FILE}")
        return
    path.write_text(to_json(starter))
    print(f"written {CONFIG_FILE}")


def ask_what_this_is_for() -> dict[str, object] | None:
    """Ask which defaults to write until the answer is one of the two, or the input ends."""
    while True:
        try:
            answer = input(GROUP_QUESTION).strip()
        except EOFError:
            return None
        if answer in ("", "1"):
            return STARTER_CONFIG
        if answer == "2":
            return GROUP_CONFIG


def choose_starter_config(working_directory: Path) -> dict[str, object] | None:
    """Pick the defaults box gen writes, asking only where there is someone to answer."""
    # A config that is already there is kept whatever the answer would have been, so nobody is asked.
    if (working_directory / CONFIG_FILE).is_file():
        return STARTER_CONFIG
    if not sys.stdin.isatty():
        return STARTER_CONFIG
    return ask_what_this_is_for()


def build_kit_spec(base_name: str) -> str:
    """Render the starter network policy, named after the project it belongs to."""
    return STARTER_KIT_SPEC.format(name=base_name)


def write_starter_kit(working_directory: Path) -> None:
    """Write a policy allowing the agent's own API calls, unless the project already has one."""
    path = working_directory / KIT_SPEC_FILE
    if path.exists():
        print(f"kept    {KIT_SPEC_FILE}")
        return
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(build_kit_spec(default_base_name(working_directory)))
    print(f"written {KIT_SPEC_FILE}")


def fill_mounts(required: dict[str, str], provided: dict[str, str]) -> dict[str, str]:
    """Answer every declared mount, keeping the paths already filled in."""
    filled = dict(provided)
    for name in required:
        if name in filled:
            continue
        filled[name] = MOUNT_PLACEHOLDER
    return filled


def warn_placeholders(required: dict[str, str], filled: dict[str, str]) -> None:
    """Name the mounts whose path only this machine's owner knows."""
    names = unfilled_mounts(required, filled)
    if not names:
        return
    print(f"WARNING: replace {MOUNT_PLACEHOLDER} in {MOUNTS_FILE} for:", file=sys.stderr)
    print(describe_mounts(required, names), file=sys.stderr)
    print("or run box mount-prompt and give the prompt to an agent", file=sys.stderr)


def write_mounts(working_directory: Path, required: dict[str, str]) -> None:
    """Add a placeholder for every declared mount the file leaves unanswered."""
    path = working_directory / MOUNTS_FILE
    provided = read_mounts_file(path)
    filled = fill_mounts(required, provided)
    if path.is_file() and filled == provided:
        print(f"kept    {MOUNTS_FILE}")
        return
    path.write_text(to_json(filled))
    print(f"written {MOUNTS_FILE}")
    warn_placeholders(required, filled)


def generate(working_directory: Path) -> int:
    """Write a starter .box directory, adding what is missing and keeping what is filled in."""
    starter = choose_starter_config(working_directory)
    if starter is None:
        print("box: nothing was answered, so nothing was written.", file=sys.stderr)
        return 1
    (working_directory / BOX_DIR).mkdir(exist_ok=True)
    write_starter_config(working_directory / CONFIG_FILE, starter)
    write_starter_kit(working_directory)
    write_mounts(working_directory, read_required_mounts(working_directory))
    ignore_local_paths(working_directory)
    if REPOS in starter:
        print(GROUP_NEXT_STEP)
    return 0


def read_required_mounts(working_directory: Path) -> dict[str, str]:
    """Read what the project declares it needs mounted."""
    values = read_config_file(working_directory / CONFIG_FILE)
    return as_descriptions(values.get(REQUIRED_MOUNTS, {}))


def host_description() -> str:
    """Name what a build has to match: the platform and the architecture, never one alone."""
    return f"{sys.platform} {platform.machine()}"


def deps_path() -> Path:
    """Where a dependency lands that this machine cannot supply, following XDG_DATA_HOME."""
    base = os.environ.get("XDG_DATA_HOME", "")
    if base:
        return Path(base) / "box" / "deps"
    return Path.home() / ".box" / "deps"


def build_mount_prompt(required: dict[str, str], names: list[str], host: str, deps: str) -> str:
    """Render the prompt that has an agent on this host fill in the mounts file."""
    return f"""Fill in {MOUNTS_FILE} for this machine, which runs {host}.

Give each of these a path, adding the key where it is missing and replacing
{MOUNT_PLACEHOLDER} where it is already there:

{describe_mounts(required, names)}

Run commands to find each path, and check it exists before writing it. Never guess.

The sandbox runs Linux, whatever this machine runs. Where nothing here fits -- a
toolchain built for the wrong platform or architecture, or a dependency that is
simply absent -- download a suitable one into {deps}/, creating that directory if
needed, and point the mount at it. Say which you could not find or fetch, and
leave those as they were.

Never give a mount a path inside this project directory. The sandbox wipes its
copy of the project before cloning into it, and a mount nested inside deadlocks
that wipe.

Add :rw only where the description asks for write access. Change nothing else."""


def mount_prompt(working_directory: Path) -> int:
    """Print the prompt for filling in this machine's mounts, or nothing when none are missing."""
    required = read_required_mounts(working_directory)
    provided = read_mounts_file(working_directory / MOUNTS_FILE)
    names = unfilled_mounts(required, provided)
    if not names:
        print(f"every mount in {MOUNTS_FILE} already has a path", file=sys.stderr)
        return 0
    print(build_mount_prompt(required, names, host_description(), str(deps_path())))
    return 0


def setup_command(command: str, working_directory: Path) -> int:
    """Run a command that needs no settings: one that works on the project's files, or on box."""
    if command == "gen":
        return generate(working_directory)
    if command == "self-update":
        return self_update(Path(__file__).resolve())
    return mount_prompt(working_directory)


def show_config(config: Config, project: Project, token_file: str) -> int:
    """Print the settings in effect, then run every check a run would make before starting."""
    print(format_config(config, token_file, secrets_file_from_environment()))
    # The settings are printed first, so a rejected project is read next to what it resolved to.
    require_project(config, project, token_file)
    return 0


def cache_path() -> Path:
    """Where the update check remembers what it last saw, following XDG_CACHE_HOME."""
    base = os.environ.get("XDG_CACHE_HOME", "")
    if base:
        return Path(base) / "box" / "update-check.json"
    return Path.home() / ".cache" / "box" / "update-check.json"


def file_hash(path: Path) -> str:
    """Hash a file's bytes, which is how one copy of box.py is compared to another."""
    return hashlib.sha256(path.read_bytes()).hexdigest()


def update_url() -> str:
    """Where the published box.py lives, which a fork or a vendored copy can point elsewhere."""
    return os.environ.get(UPDATE_URL_ENV, UPDATE_URL)


def download(url: str) -> bytes:
    """Read the published box.py, which is all either the check or an update needs from the network."""
    with urllib.request.urlopen(url, timeout=UPDATE_TIMEOUT_SECONDS) as response:
        return bytes(response.read())


def fetch_remote_hash(url: str) -> str:
    """Hash the published box.py."""
    return hashlib.sha256(download(url)).hexdigest()


def checked_recently(path: Path, now: float) -> bool:
    """Whether the last check is fresh enough that this run has nothing to say."""
    try:
        checked_at = float(json.loads(path.read_text())["checked_at"])
    except (OSError, ValueError, KeyError, TypeError):
        return False
    return now - checked_at <= UPDATE_INTERVAL_SECONDS


def store_check_time(path: Path, now: float) -> None:
    """Remember when the check ran, so the rest of the hour is quiet."""
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps({"checked_at": now}))


def is_tracked_by_git(script_path: Path) -> bool:
    """Whether this box.py is a file in a git repository, which an installed copy is not."""
    command = ["git", "-C", str(script_path.parent), "ls-files", "--error-unmatch", str(script_path)]
    return succeeds(command)


def in_red(text: str) -> str:
    """Colour a notice red, unless stderr is no terminal or NO_COLOR asked for plain text."""
    if os.environ.get(NO_COLOUR_ENV, ""):
        return text
    # A pipe, a log file or a CI capture would otherwise be handed the escape codes as characters.
    if not sys.stderr.isatty():
        return text
    return f"{RED}{text}{RESET}"


def update_message(script_path: Path, remote_hash: str) -> str:
    """Say an update is available, and how to take it, when this copy is not the published one."""
    if remote_hash == file_hash(script_path):
        return ""
    if not os.access(script_path, os.W_OK):
        return in_red(f"An update to box is available, but {script_path} is not writable by you.")
    return in_red("An update to box is available. Take it with:\n  box self-update")


def replace_script(script_path: Path, published: bytes) -> None:
    """Write the published copy beside this one and move it over, so a failure changes nothing."""
    incoming = script_path.with_name(f"{script_path.name}{INCOMING_SUFFIX}")
    incoming.write_bytes(published)
    # The copy being replaced is executable, and whatever replaces it has to stay that way.
    incoming.chmod(script_path.stat().st_mode)
    os.replace(incoming, script_path)


def self_update(script_path: Path) -> int:
    """Replace this box.py with the published one, which is the whole of updating an install."""
    url = update_url()
    if not url:
        raise ConfigError(f"{UPDATE_URL_ENV} is set to nothing, so there is nowhere to update from")
    # A checked out box.py is the copy someone is working on, and git is how that one is updated.
    if is_tracked_by_git(script_path):
        raise ConfigError(f"git tracks {script_path}, so this is a checkout rather than an install")
    try:
        published = download(url)
    except OSError as error:
        raise ConfigError(f"could not read {url}: {error}") from error
    if not published:
        raise ConfigError(f"{url} served an empty file, which is no copy of box")
    if hashlib.sha256(published).hexdigest() == file_hash(script_path):
        print(f"kept    {script_path}, which is already the published copy")
        return 0
    try:
        replace_script(script_path, published)
    except OSError as error:
        raise ConfigError(f"could not write {script_path}: {error}") from error
    print(f"updated {script_path}")
    return 0


def warn_when_outdated() -> None:
    """Mention a newer box on stderr once an hour, staying silent about anything that goes wrong."""
    try:
        url = update_url()
        # An empty URL is a copy that has nowhere to compare itself with, so there is nothing to say.
        if not url:
            return
        script_path = Path(__file__).resolve()
        # A checked out box.py is being worked on, and its own changes are what differ from main.
        if is_tracked_by_git(script_path):
            return
        path = cache_path()
        now = time.time()
        if checked_recently(path, now):
            return
        # A failed check is still a check, so the hour it buys must not depend on GitHub answering.
        store_check_time(path, now)
        message = update_message(script_path, fetch_remote_hash(url))
    except Exception:
        return
    if message:
        print(message, file=sys.stderr)


def dispatch(arguments: argparse.Namespace, working_directory: Path) -> int:
    """Run a setup command, or load config and hand off to a sandbox session."""
    # An sbx too old to run comes first: it need not have the diagnose output the next check reads.
    require_supported_sbx()
    require_matching_versions()
    if arguments.command in SETUP_COMMANDS:
        require_no_flags(arguments)
        return setup_command(arguments.command, working_directory)
    config = load_config(arguments, working_directory)
    project = build_project(working_directory)
    token_file = token_file_from_environment()
    if arguments.command == "config":
        return show_config(config, project, token_file)
    launch = prepare_launch(config, project, token_file)
    return run_session(config, launch)


def main() -> int:
    """Entry point: parse the command line and turn a rejected setup into one message."""
    arguments = build_parser().parse_args()
    warn_when_outdated()
    try:
        return dispatch(arguments, Path.cwd())
    except ConfigError as error:
        print(f"box: {error}", file=sys.stderr)
        return 1


def to_version(version: tuple[int, ...]) -> str:
    """Spell a version the way a release is named, so a message can compare two of them."""
    return ".".join(str(part) for part in version)


def unsupported_python(version: tuple[int, ...]) -> str:
    """Say what box needs when this Python is older, since running anyway breaks somewhere obscure."""
    if version >= PYTHON_MINIMUM:
        return ""
    needed = to_version(PYTHON_MINIMUM)
    return f"box needs Python {needed} or newer, but this python3 is {to_version(version)}."


def run_box() -> int:
    """Run box on a Python it supports, turning a Ctrl-C into an exit code rather than a traceback."""
    too_old = unsupported_python(sys.version_info[:2])
    if too_old:
        print(f"box: {too_old}", file=sys.stderr)
        return 1
    try:
        return main()
    except KeyboardInterrupt:
        print("box: interrupted.", file=sys.stderr)
        return 130


if __name__ == "__main__":
    sys.exit(run_box())
