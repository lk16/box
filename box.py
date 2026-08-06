#!/usr/bin/env python3
"""Run Claude Code inside a disposable Docker sandbox (sbx)."""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
from dataclasses import asdict, dataclass
from pathlib import Path

CONFIG_FILE = ".box.json"
SECRET_HOST = "api.anthropic.com"
SECRET_ENV = "CLAUDE_CODE_OAUTH_TOKEN"

# The token path is deliberately the one setting that is not a flag or a config file key.
TOKEN_FILE_ENV = "CLAUDE_OAUTH_TOKEN_FILE"

# Mounts are read-only unless the user opts out, so a sandbox cannot write to the host by accident.
READ_WRITE_SUFFIX = ":rw"

KIT_HELP = f"""kit is not set, so the sandbox would run without a network policy.
Point it at a kit directory holding a spec.yaml, e.g. .sbx/kit, in {CONFIG_FILE} or with --kit."""

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

pre-commit is not installed here, so its git hook will not run. Run the
project's own checks by hand before committing.

The sandbox runs as a different user than the host, so PATH and any tool or
package caches do not point at the host's copies. Host home directories are
mounted under /home/*/ and more than one exists, so join the glob matches
rather than assuming a single path:

    export PATH="$PATH:$(echo /home/*/.local/bin | tr ' ' ':')"

Network access is limited to an allowlist, so fetching a dependency that is
not already cached fails with 403. That is a sandbox limit, not a bug in the
code: verify what you can without it, and ask for a specific host to be
allowed rather than working around it.

If you cannot reasonably finish the task, stop, state concisely what is
blocking you, and suggest a solution -- do not keep flailing."""

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
    "mounts": [],
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


def to_kebab_case(text: str) -> str:
    """Lowercase text and collapse runs of non-alphanumeric characters into single hyphens."""
    return re.sub(r"[^A-Za-z0-9]+", "-", text).strip("-").lower()


def default_base_name(directory: Path) -> str:
    """Derive a sandbox base name from the directory name."""
    name = to_kebab_case(directory.name)
    if not name:
        return "box"
    return name


def read_config_file(path: Path) -> dict[str, object]:
    """Read config values from the JSON file, returning empty values when it is absent."""
    if not path.is_file():
        return {}
    try:
        loaded = json.loads(path.read_text())
    except json.JSONDecodeError as error:
        raise ConfigError(f"{path} is not valid JSON: {error}") from error
    if not isinstance(loaded, dict):
        raise ConfigError(f"{path} must contain a JSON object")
    unknown = sorted(set(loaded) - set(DEFAULTS))
    if unknown:
        raise ConfigError(f"{path} has unknown keys: {', '.join(unknown)}")
    return loaded


def merge_values(file_values: dict[str, object], cli_values: dict[str, object]) -> dict[str, object]:
    """Layer CLI values over file values over defaults; CLI wins."""
    merged = dict(DEFAULTS)
    merged.update(file_values)
    for key, value in cli_values.items():
        if value is None:
            continue
        merged[key] = value
    return merged


def to_workspace(mount: str) -> str:
    """Turn a configured mount into an sbx workspace spec, read-only unless :rw was asked for."""
    if mount.endswith(READ_WRITE_SUFFIX):
        return mount[: -len(READ_WRITE_SUFFIX)]
    if ":" in mount:
        raise ConfigError(f"mount {mount} has an unknown suffix; mounts are read-only unless you add :rw")
    return f"{mount}:ro"


def as_mount_list(value: object) -> tuple[str, ...]:
    """Normalise the mounts value into a tuple of sbx workspace specs."""
    if not isinstance(value, list):
        raise ConfigError("mounts must be a list of paths")
    return tuple(to_workspace(str(item)) for item in value)


def build_config(values: dict[str, object], working_directory: Path) -> Config:
    """Turn merged config values into a Config, filling in the derived sandbox name."""
    name = str(values["name"])
    if not name:
        name = default_base_name(working_directory)
    return Config(
        name=name,
        memory=str(values["memory"]),
        cpus=str(values["cpus"]),
        root_size=str(values["root_size"]),
        docker_size=str(values["docker_size"]),
        model=str(values["model"]),
        prompt_file=str(values["prompt_file"]),
        kit=str(values["kit"]),
        mounts=as_mount_list(values["mounts"]),
    )


def build_parser() -> argparse.ArgumentParser:
    """Define the command line interface."""
    parser = argparse.ArgumentParser(
        prog="box",
        description="Run Claude Code inside a disposable Docker sandbox.",
    )
    # Flag names match the config keys, so argparse derives every dest but the repeatable one.
    parser.add_argument("--name", metavar="NAME", help="sandbox base name")
    parser.add_argument("--memory", metavar="SIZE", help="memory limit, e.g. 4g")
    parser.add_argument("--cpus", metavar="N", help="number of CPUs")
    parser.add_argument("--root-size", metavar="SIZE", help="sandbox root filesystem size")
    parser.add_argument("--docker-size", metavar="SIZE", help="sandbox docker size")
    parser.add_argument("--model", metavar="MODEL", help="Claude model to run")
    parser.add_argument("--prompt-file", metavar="PATH", help="file added to the prompt")
    parser.add_argument("--kit", metavar="REF", help="sbx kit reference")
    parser.add_argument(
        "--mount", dest="mounts", metavar="PATH", action="append", help="read-only workspace, :rw to write"
    )
    parser.add_argument("-v", "--verbose", action="store_true", help="print the config in effect")
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


def capture(command: list[str]) -> str:
    """Run a command and return its stdout, or an empty string when it fails."""
    result = subprocess.run(command, capture_output=True, text=True, check=False)
    if result.returncode != 0:
        return ""
    return result.stdout


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


def resolve_path(text: str) -> Path:
    """Expand a configured path so a leading ~ works the same as in the shell."""
    return Path(text).expanduser()


def read_token(path: Path) -> str:
    """Read the OAuth token, stripped of newlines."""
    if not path.is_file() or path.stat().st_size == 0:
        raise ConfigError(f"token file {path} does not exist or is empty")
    return path.read_text().replace("\n", "")


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


def build_agent_args(system_prompt: str, model: str) -> list[str]:
    """Assemble the arguments passed through to the Claude CLI."""
    args = []
    if system_prompt:
        args.extend(["--append-system-prompt", system_prompt])
    if model:
        args.extend(["--model", model])
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
    subprocess.run(command, input=token, text=True, check=True)


def warn_dirty(sandbox_name: str, dirty: str) -> None:
    """Tell the user how to recover uncommitted work left behind in a sandbox."""
    print(f"WARNING: sandbox {sandbox_name} has uncommitted changes -- not removing it.", file=sys.stderr)
    print(dirty, file=sys.stderr)
    print(f"Inspect:  sbx exec {sandbox_name} git -C {Path.cwd()} diff", file=sys.stderr)
    print(f"Recover:  sbx cp {sandbox_name}:{Path.cwd()}/<file> .", file=sys.stderr)
    print(f"Then remove manually once safe: sbx rm --force {sandbox_name}", file=sys.stderr)


def cleanup(sandbox_name: str) -> None:
    """Pull committed work back, then drop the sandbox unless work would be lost."""
    capture(["git", "fetch", f"sandbox-{sandbox_name}"])
    dirty = capture(["sbx", "exec", sandbox_name, "git", "-C", str(Path.cwd()), "status", "--porcelain"])
    if dirty.strip():
        warn_dirty(sandbox_name, dirty)
        return
    drop_secret(sandbox_name)
    subprocess.run(["sbx", "rm", "--force", sandbox_name], check=False)


def load_config(arguments: argparse.Namespace, working_directory: Path) -> Config:
    """Combine the JSON file and the CLI arguments into the effective config."""
    cli_values = {key: value for key, value in vars(arguments).items() if key in DEFAULTS}
    file_values = read_config_file(working_directory / CONFIG_FILE)
    return build_config(merge_values(file_values, cli_values), working_directory)


def token_file_from_environment() -> str:
    """Read the token path from the environment, which is the only place it comes from."""
    return os.environ.get(TOKEN_FILE_ENV, "")


def require_settings(config: Config) -> None:
    """Reject settings whose default would be a silent risk rather than a convenience."""
    if not config.kit:
        raise ConfigError(KIT_HELP)


def prepare_launch(config: Config, token_file: str) -> Launch:
    """Resolve everything that can still fail before the sandbox exists."""
    require_settings(config)
    if not token_file:
        raise ConfigError(TOKEN_FILE_HELP)
    token = read_token(resolve_path(token_file))
    system_prompt = build_system_prompt(read_system_prompt(config.prompt_file))
    agent_args = build_agent_args(system_prompt, config.model)
    return Launch(sandbox_name=pick_name(config.name, taken_names()), token=token, agent_args=agent_args)


def run_session(config: Config, launch: Launch) -> int:
    """Create the sandbox, run Claude in it, and clean up afterwards."""
    environment = build_environment(config)
    subprocess.run(build_create_command(config, launch.sandbox_name), env=environment, check=True)
    try:
        drop_secret(launch.sandbox_name)
        store_secret(launch.sandbox_name, launch.token)
        command = build_run_command(launch.sandbox_name, launch.agent_args)
        result = subprocess.run(command, env=environment, check=False)
        return result.returncode
    finally:
        cleanup(launch.sandbox_name)


def main() -> int:
    """Entry point: load config, then hand off to a sandbox session."""
    arguments = build_parser().parse_args()
    try:
        config = load_config(arguments, Path.cwd())
        token_file = token_file_from_environment()
        if arguments.verbose:
            print(format_config(config, token_file))
        launch = prepare_launch(config, token_file)
    except ConfigError as error:
        print(f"box: {error}", file=sys.stderr)
        return 1
    return run_session(config, launch)


if __name__ == "__main__":
    sys.exit(main())
