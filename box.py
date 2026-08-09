#!/usr/bin/env python3
"""Run Claude Code inside a disposable Docker sandbox (sbx)."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import platform
import re
import shlex
import subprocess
import sys
import time
import urllib.request
from dataclasses import asdict, dataclass
from pathlib import Path

# Everything box reads from a project lives in one directory, so a project has one box footprint.
BOX_DIR = ".box"
CONFIG_FILE = f"{BOX_DIR}/config.json"

# Mounts name paths on one machine, so they live apart from the settings a project shares.
MOUNTS_FILE = f"{BOX_DIR}/mounts.json"

# Where a dependency lands that this machine cannot supply, such as a Linux toolchain on macOS.
DEPS_DIR = f"{BOX_DIR}/deps"
GITIGNORE_FILE = ".gitignore"

# What box gen writes where it cannot know the path, so an unfilled mount fails loudly.
MOUNT_PLACEHOLDER = "/placeholder/for/real/path"

# Commands that work on the project's files, so settings and their flags mean nothing to them.
SETUP_COMMANDS = ("gen", "mount-prompt")

# box.py has no version, so being current means hashing the same as the published copy.
UPDATE_URL = "https://raw.githubusercontent.com/lk16/box/main/box.py"

# How a fork points the check at its own copy, or switches it off by setting it to nothing.
UPDATE_URL_ENV = "BOX_UPDATE_URL"
UPDATE_INTERVAL_SECONDS = 60 * 60
UPDATE_TIMEOUT_SECONDS = 2

# The update notice competes with whatever the agent prints, so it is coloured to stand out.
RED = "\033[31m"
RESET = "\033[0m"

# What a reader of plain text asks for, and every other tool honours: no escape codes at all.
NO_COLOUR_ENV = "NO_COLOR"

SECRET_HOST = "api.anthropic.com"
SECRET_ENV = "CLAUDE_CODE_OAUTH_TOKEN"

# The token path is deliberately the one setting that is not a flag or a config file key.
TOKEN_FILE_ENV = "CLAUDE_OAUTH_TOKEN_FILE"

# Mounts are read-only unless the user opts out, so a sandbox cannot write to the host by accident.
READ_WRITE_SUFFIX = ":rw"

# The one flag whose argparse dest is not its own name, since it collects a list of paths.
MOUNT_FLAG = "--mount"
MOUNT_DEST = "mounts"

# Where sbx's git daemon lands a sandbox's work: refs/sandboxes/<sandbox>/<branch>.
SANDBOX_REFS = "refs/sandboxes"

# What a command that never started exits with, which is what a shell reports for the same thing.
NOT_RUN = 127

# A branch name is a courtesy, so the agent naming it gets one turn and no more.
BRANCH_NAME_TIMEOUT_SECONDS = 10
BRANCH_NAME_WORDS = 5

BRANCH_NAME_PROMPT = f"""Name a git branch after the work these commit subjects describe.
Answer with the name and nothing else: kebab-case, at most {BRANCH_NAME_WORDS} words, shorter is
better.

Commit subjects:"""

KIT_HELP = f"""kit is not set, so the sandbox would run without a network policy.
Point it at a kit directory holding a spec.yaml, e.g. .sbx/kit, in {CONFIG_FILE} or with --kit."""

KIT_FILE_HELP = """kit names a file, and sbx reads anything that is not a directory as a zip
artifact. Point it at the directory holding spec.yaml, e.g. .sbx/kit rather than
.sbx/kit/spec.yaml."""

MODEL_HELP = f"""model is not set, so the sandbox's own Claude version would pick the model.
That version need not match the one on this host. Name the model in {CONFIG_FILE} or with --model."""

MOUNTS_IGNORED_HELP = f"""{MOUNTS_FILE} is not ignored by git.
It names folders on this machine, so committing it would put paths that exist only here into
everyone else's clone. Add a {MOUNTS_FILE} line to .gitignore."""

DEPS_IGNORED_HELP = f"""{DEPS_DIR}/ is not ignored by git.
It holds dependencies fetched for this machine, such as toolchains built for the sandbox's
platform rather than this one, which belong in nobody's history. Add a {DEPS_DIR}/ line to
.gitignore."""

# What box writes that holds one machine's own files, and the reason each must stay uncommitted.
LOCAL_PATHS = {MOUNTS_FILE: MOUNTS_IGNORED_HELP, f"{DEPS_DIR}/": DEPS_IGNORED_HELP}

NOT_A_REPOSITORY_HELP = """this is not a git repository.
box hands the agent a clone of this directory, so there has to be something here to clone. Run
git init and commit, then try again."""

NO_COMMITS_HELP = """this git repository has no commits.
box hands the agent a clone of this directory, and a repository with no commits clones to nothing
the agent can work from or branch off. Make at least one commit, then try again."""

TOKEN_FILE_HELP = f"""{TOKEN_FILE_ENV} is not set. Set it up once:
  1. Run: claude setup-token
  2. Save the printed token to a file, e.g. ~/.secrets/claude-oauth.token
  3. Export {TOKEN_FILE_ENV} to point at that file, e.g. via direnv."""

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

# The one config key that is not a setting, so it is the one key whose value is not a string.
REQUIRED_MOUNTS = "required_mounts"

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
    REQUIRED_MOUNTS: {},
}


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
    mounts: tuple[str, ...]


@dataclass(frozen=True)
class Launch:
    """Everything resolved before the sandbox is created."""

    sandbox_name: str
    token: str
    agent_args: list[str]


@dataclass(frozen=True)
class SandboxRef:
    """One ref a sandbox left behind, and the commit it points at."""

    ref_name: str
    commit: str


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
    settings = {key: value for key, value in loaded.items() if key != REQUIRED_MOUNTS}
    values: dict[str, object] = dict(as_text_values(path, settings))
    if REQUIRED_MOUNTS in loaded:
        values[REQUIRED_MOUNTS] = loaded[REQUIRED_MOUNTS]
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


def order_mounts(required: dict[str, str], provided: dict[str, str]) -> list[str]:
    """Return the declared mounts' paths in declaration order, so the sbx args never shuffle."""
    require_named_mounts(required, provided)
    return [provided[name] for name in required]


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
    return f"{resolve_path(mount_path(mount))}:ro"


def to_workspaces(mounts: list[str]) -> tuple[str, ...]:
    """Turn the configured mounts into sbx workspace specs."""
    return tuple(to_workspace(mount) for mount in mounts)


def setting(values: dict[str, object], key: str) -> str:
    """Read one merged setting, which reading the config file has already made a string."""
    value = values[key]
    if not isinstance(value, str):
        raise ConfigError(f"{key} is {name_of_type(value)}, which is not text")
    return value


def build_config(values: dict[str, object], mounts: list[str], working_directory: Path) -> Config:
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
        mounts=to_workspaces(mounts),
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
    parser.add_argument(
        MOUNT_FLAG, dest=MOUNT_DEST, metavar="PATH", action="append", help="read-only workspace, :rw to write"
    )
    parser.add_argument(
        "command",
        choices=["run", "config", "gen", "mount-prompt"],
        help=f"run starts a sandbox, config prints the settings in effect, "
        f"gen writes a starter {BOX_DIR} directory, "
        "mount-prompt asks an agent to fill in this machine's paths",
    )
    return parser


def format_value(value: object) -> str:
    """Render one config value, joining the mount list into a readable line."""
    if isinstance(value, tuple):
        return " ".join(str(item) for item in value)
    return str(value)


def format_config(config: Config, token_file: str) -> str:
    """Render the settings in effect, token path included, as aligned key/value lines."""
    items: dict[str, object] = {TOKEN_FILE_ENV: token_file, **asdict(config)}
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


def taken_names() -> set[str]:
    """Collect sandbox names that are either running or still hold git refs."""
    running = set(capture(["sbx", "ls", "-q"]).split())
    refs = parse_ref_names(capture(["git", "for-each-ref", "--format=%(refname)", "refs/sandboxes"]))
    return running | refs


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


def read_system_prompt(prompt_file: str) -> str:
    """Read the extra system prompt, or return nothing when no file is configured."""
    if not prompt_file:
        return ""
    path = resolve_path(prompt_file)
    if not path.is_file():
        raise ConfigError(f"prompt file {path} does not exist")
    return path.read_text()


def build_system_prompt(project_prompt: str) -> str:
    """Put the built-in sandbox instructions in front of the project's own prompt."""
    if not project_prompt:
        return BASE_PROMPT
    return f"{BASE_PROMPT}\n\n{project_prompt}"


def build_environment(config: Config) -> dict[str, str]:
    """Copy the current environment and add the sbx disk limits."""
    environment = dict(os.environ)
    environment["DOCKER_SANDBOXES_ROOT_SIZE"] = config.root_size
    environment["DOCKER_SANDBOXES_DOCKER_SIZE"] = config.docker_size
    return environment


def build_create_command(config: Config, sandbox_name: str) -> list[str]:
    """Assemble the sbx create invocation."""
    command = ["sbx", "create", "claude", "."]
    command.extend(config.mounts)
    command.extend(["--clone", "--name", sandbox_name])
    command.extend(["--memory", config.memory, "--cpus", config.cpus])
    if config.kit:
        command.extend(["--kit", config.kit])
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


def drop_secret(sandbox_name: str) -> None:
    """Remove any stored secret for this sandbox name, ignoring failures."""
    capture(["sbx", "secret", "rm", sandbox_name, "--host", SECRET_HOST, "-f"])


def store_secret(sandbox_name: str, token: str) -> None:
    """Hand the OAuth token to sbx over stdin so it never lands in the shell history."""
    command = ["sbx", "secret", "set-custom", sandbox_name, "--host", SECRET_HOST, "--env", SECRET_ENV]
    try:
        result = subprocess.run(command, input=token, text=True, check=False)
    except OSError as error:
        raise ConfigError(f"could not run sbx: {error}") from error
    if result.returncode != 0:
        raise ConfigError(f"sbx would not store the OAuth token for {sandbox_name}")


def print_recovery(sandbox_name: str) -> None:
    """Print how to look inside a sandbox box kept, take work out of it, and remove it by hand."""
    print(f"Inspect:  sbx exec {sandbox_name} git -C {Path.cwd()} diff", file=sys.stderr)
    print(f"Recover:  sbx cp {sandbox_name}:{Path.cwd()}/<file> .", file=sys.stderr)
    print(f"Then remove manually once safe: sbx rm --force {sandbox_name}", file=sys.stderr)


def warn_dirty(sandbox_name: str, dirty: str) -> None:
    """Tell the user how to recover uncommitted work left behind in a sandbox."""
    print(f"WARNING: sandbox {sandbox_name} has uncommitted changes -- not removing it.", file=sys.stderr)
    print(dirty, file=sys.stderr)
    print_recovery(sandbox_name)


def warn_unchecked(sandbox_name: str, reason: str) -> None:
    """Tell the user box could not find out whether removing a sandbox would lose work."""
    print(f"WARNING: {reason}.", file=sys.stderr)
    print(f"box cannot tell whether sandbox {sandbox_name} holds work -- not removing it.", file=sys.stderr)
    print_recovery(sandbox_name)


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


def sandbox_refs(sandbox_name: str) -> list[SandboxRef]:
    """Read the refs this sandbox's work was fetched into."""
    command = ["git", "for-each-ref", "--format=%(refname) %(objectname)", f"{SANDBOX_REFS}/{sandbox_name}"]
    return parse_sandbox_refs(capture(command))


def count_new_commits(commit: str) -> str:
    """Count the sandbox's commits this checkout lacks, returning nothing when git could not say."""
    return capture(["git", "rev-list", "--count", f"HEAD..{commit}"]).strip()


def new_commit_subjects(commit: str) -> str:
    """Read the subjects of the sandbox's commits, which are what a branch gets named after."""
    return capture(["git", "log", "--format=%s", f"HEAD..{commit}"])


def local_branch_names() -> set[str]:
    """Collect the branch names this repository already has."""
    return set(capture(["git", "for-each-ref", "--format=%(refname:short)", "refs/heads"]).split())


def pick_branch_name(branch: str, used: set[str]) -> str:
    """Return the suggested name, numbered from two when the repository already has it."""
    if branch not in used:
        return branch
    number = 2
    while f"{branch}-{number}" in used:
        number += 1
    return f"{branch}-{number}"


def create_branch(branch: str, commit: str) -> bool:
    """Point a new branch at the sandbox's commit, saying whether git accepted it."""
    return succeeds(["git", "branch", branch, commit])


def delete_ref(ref_name: str) -> None:
    """Drop a ref, which is safe to leave behind when it fails."""
    capture(["git", "update-ref", "-d", ref_name])


def settle_ref(ref: SandboxRef) -> None:
    """Turn one sandbox ref into a branch, drop it when it holds nothing, and keep it otherwise."""
    count = count_new_commits(ref.commit)
    if not count:
        print(f"box: git could not read {ref.ref_name}, so it was kept.", file=sys.stderr)
        return
    if count == "0":
        delete_ref(ref.ref_name)
        print(f"box: {ref.ref_name} held no commits, so it was dropped.", file=sys.stderr)
        return
    suggested = suggest_branch_name(new_commit_subjects(ref.commit))
    if not suggested:
        print(f"box: naming a branch failed, so the work stayed on {ref.ref_name}.", file=sys.stderr)
        return
    branch = pick_branch_name(suggested, local_branch_names())
    if not create_branch(branch, ref.commit):
        print(f"box: git refused branch {branch}, so the work stayed on {ref.ref_name}.", file=sys.stderr)
        return
    delete_ref(ref.ref_name)
    print(f"box: branch {branch} holds {plural(count, 'commit')} from {ref.ref_name}.", file=sys.stderr)


def settle_sandbox_refs(sandbox_name: str) -> None:
    """Give the sandbox's committed work a branch, so nothing is left addressable only by ref."""
    for ref in sandbox_refs(sandbox_name):
        settle_ref(ref)


def cleanup(sandbox_name: str) -> None:
    """Pull committed work back, then drop the sandbox unless work would be lost."""
    remote = f"sandbox-{sandbox_name}"
    # Removal follows, so "I could not tell" must never be read as "there is nothing to lose".
    if not succeeds(["git", "fetch", remote]):
        warn_unchecked(sandbox_name, f"git fetch {remote} failed, so its commits are not here")
        return
    status = run_quietly(["sbx", "exec", sandbox_name, "git", "-C", str(Path.cwd()), "status", "--porcelain"])
    if status.returncode != 0:
        warn_unchecked(sandbox_name, "sbx exec could not read the sandbox's git status")
        return
    if status.stdout.strip():
        warn_dirty(sandbox_name, status.stdout)
        return
    settle_sandbox_refs(sandbox_name)
    drop_secret(sandbox_name)
    subprocess.run(["sbx", "rm", "--force", sandbox_name], check=False)


def resolve_mounts(extra: list[str], working_directory: Path, required: dict[str, str]) -> list[str]:
    """Add the mounts given as flags to the named ones in the mounts file."""
    provided = read_mounts_file(working_directory / MOUNTS_FILE)
    return order_mounts(required, provided) + extra


def load_config(arguments: argparse.Namespace, working_directory: Path) -> Config:
    """Combine the JSON files and the CLI arguments into the effective config."""
    cli_values = {key: value for key, value in vars(arguments).items() if key in DEFAULTS}
    file_values = read_config_file(working_directory / CONFIG_FILE)
    values = merge_values(file_values, cli_values)
    extra = list(arguments.mounts or [])
    mounts = resolve_mounts(extra, working_directory, as_descriptions(values[REQUIRED_MOUNTS]))
    return build_config(values, mounts, working_directory)


def token_file_from_environment() -> str:
    """Read the token path from the environment, which is the only place it comes from."""
    return os.environ.get(TOKEN_FILE_ENV, "")


def require_settings(config: Config) -> None:
    """Reject settings whose default would be a silent risk rather than a convenience."""
    if not config.kit:
        raise ConfigError(KIT_HELP)
    # A kit that is not on disk is a reference sbx resolves itself, so only a local file is wrong.
    if resolve_path(config.kit).is_file():
        raise ConfigError(KIT_FILE_HELP)
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


def require_ignored_local_paths(working_directory: Path) -> None:
    """Refuse to run while anything holding this machine's own files could be committed."""
    for relative_path, help_text in LOCAL_PATHS.items():
        if not (working_directory / relative_path).exists():
            continue
        if is_git_ignored(working_directory, relative_path):
            continue
        raise ConfigError(help_text)


def require_project(config: Config, working_directory: Path) -> None:
    """Run every check on the settings and the project that does not create anything."""
    require_settings(config)
    require_git_repository(working_directory)
    require_ignored_local_paths(working_directory)


def prepare_launch(config: Config, token_file: str, working_directory: Path) -> Launch:
    """Resolve everything that can still fail before the sandbox exists."""
    require_project(config, working_directory)
    if not token_file:
        raise ConfigError(TOKEN_FILE_HELP)
    token = read_token(resolve_path(token_file))
    system_prompt = build_system_prompt(read_system_prompt(config.prompt_file))
    agent_args = build_agent_args(config, system_prompt)
    return Launch(sandbox_name=pick_name(config.name, taken_names()), token=token, agent_args=agent_args)


def run_session(config: Config, launch: Launch) -> int:
    """Create the sandbox, run Claude in it, and clean up afterwards."""
    environment = build_environment(config)
    # sbx injects the placeholder env var when the sandbox is created, so the secret must exist by then.
    drop_secret(launch.sandbox_name)
    store_secret(launch.sandbox_name, launch.token)
    create = build_create_command(config, launch.sandbox_name)
    # sbx has already said why it failed, and there is no sandbox to run in, clean up or keep.
    if subprocess.run(create, env=environment, check=False).returncode != 0:
        # Two runs can pick one name and the loser drops the winner's secret, which sbx has
        # already injected into the running sandbox, so the winner keeps working regardless.
        drop_secret(launch.sandbox_name)
        print(f"box: sbx create failed, so {launch.sandbox_name} was never started.", file=sys.stderr)
        return 1
    try:
        command = build_run_command(launch.sandbox_name, launch.agent_args)
        result = subprocess.run(command, env=environment, check=False)
        return result.returncode
    finally:
        cleanup(launch.sandbox_name)


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


def write_starter_config(path: Path) -> None:
    """Write every setting at its default, unless the project already has a config."""
    if path.exists():
        print(f"kept    {CONFIG_FILE}")
        return
    path.write_text(to_json(DEFAULTS))
    print(f"written {CONFIG_FILE}")


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
    print("or have an agent do it: box mount-prompt | claude", file=sys.stderr)


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


def make_deps_dir(working_directory: Path) -> None:
    """Create the directory an agent puts dependencies in that this machine cannot supply."""
    path = working_directory / DEPS_DIR
    if path.is_dir():
        return
    path.mkdir(parents=True)
    print(f"created {DEPS_DIR}/")


def generate(working_directory: Path) -> int:
    """Write a starter .box directory, adding what is missing and keeping what is filled in."""
    (working_directory / BOX_DIR).mkdir(exist_ok=True)
    write_starter_config(working_directory / CONFIG_FILE)
    write_mounts(working_directory, read_required_mounts(working_directory))
    make_deps_dir(working_directory)
    ignore_local_paths(working_directory)
    return 0


def read_required_mounts(working_directory: Path) -> dict[str, str]:
    """Read what the project declares it needs mounted."""
    values = read_config_file(working_directory / CONFIG_FILE)
    return as_descriptions(values.get(REQUIRED_MOUNTS, {}))


def host_description() -> str:
    """Name what a build has to match: the platform and the architecture, never one alone."""
    return f"{sys.platform} {platform.machine()}"


def build_mount_prompt(required: dict[str, str], names: list[str], host: str) -> str:
    """Render the prompt that has an agent on this host fill in the mounts file."""
    return f"""Fill in {MOUNTS_FILE} for this machine, which runs {host}.

Give each of these a path, adding the key where it is missing and replacing
{MOUNT_PLACEHOLDER} where it is already there:

{describe_mounts(required, names)}

Run commands to find each path, and check it exists before writing it. Never guess.

The sandbox runs Linux, whatever this machine runs. Where nothing here fits -- a
toolchain built for the wrong platform or architecture, or a dependency that is
simply absent -- download a suitable one into {DEPS_DIR}/ and point the mount at
it. Say which you could not find or fetch, and leave those as they were.

Add :rw only where the description asks for write access. Change nothing else."""


def mount_prompt(working_directory: Path) -> int:
    """Print the prompt for filling in this machine's mounts, or nothing when none are missing."""
    required = read_required_mounts(working_directory)
    provided = read_mounts_file(working_directory / MOUNTS_FILE)
    names = unfilled_mounts(required, provided)
    if not names:
        print(f"every mount in {MOUNTS_FILE} already has a path", file=sys.stderr)
        return 0
    print(build_mount_prompt(required, names, host_description()))
    return 0


def setup_command(command: str, working_directory: Path) -> int:
    """Run a command that writes or reads the project's files instead of loading settings."""
    if command == "gen":
        return generate(working_directory)
    return mount_prompt(working_directory)


def show_config(config: Config, token_file: str, working_directory: Path) -> int:
    """Print the settings in effect, then run every check a run would make before starting."""
    print(format_config(config, token_file))
    # The settings are printed first, so a rejected project is read next to what it resolved to.
    require_project(config, working_directory)
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


def fetch_remote_hash(url: str) -> str:
    """Hash the published box.py."""
    with urllib.request.urlopen(url, timeout=UPDATE_TIMEOUT_SECONDS) as response:
        return hashlib.sha256(response.read()).hexdigest()


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


def update_message(script_path: Path, remote_hash: str, url: str) -> str:
    """Say an update is available, and how to take it, when this copy is not the published one."""
    if remote_hash == file_hash(script_path):
        return ""
    if not os.access(script_path, os.W_OK):
        return in_red(f"An update to box is available, but {script_path} is not writable by you.")
    quoted = shlex.quote(str(script_path))
    take_it = f"An update to box is available. Take it with:\n  curl -fsSL -o {quoted} {url}"
    return in_red(take_it)


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
        message = update_message(script_path, fetch_remote_hash(url), url)
    except Exception:
        return
    if message:
        print(message, file=sys.stderr)


def dispatch(arguments: argparse.Namespace, working_directory: Path) -> int:
    """Run a setup command, or load config and hand off to a sandbox session."""
    if arguments.command in SETUP_COMMANDS:
        require_no_flags(arguments)
        return setup_command(arguments.command, working_directory)
    config = load_config(arguments, working_directory)
    token_file = token_file_from_environment()
    if arguments.command == "config":
        return show_config(config, token_file, working_directory)
    launch = prepare_launch(config, token_file, working_directory)
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


def run_box() -> int:
    """Run box, turning a Ctrl-C into an exit code rather than a traceback."""
    try:
        return main()
    except KeyboardInterrupt:
        print("box: interrupted.", file=sys.stderr)
        return 130


if __name__ == "__main__":
    sys.exit(run_box())
