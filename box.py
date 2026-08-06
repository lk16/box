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

# Config keys and their fallbacks, used for both the JSON file and the CLI.
DEFAULTS: dict[str, object] = {
    "tokenFile": "",
    "name": "",
    "memory": "4g",
    "cpus": "4",
    "rootSize": "10g",
    "dockerSize": "10g",
    "model": "",
    "promptFile": "",
    "kit": "",
    "mounts": [],
}


class ConfigError(Exception):
    """Raised when the effective configuration cannot be used."""


@dataclass(frozen=True)
class Config:
    """Effective settings for one box run."""

    token_file: str
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


def as_mount_list(value: object) -> tuple[str, ...]:
    """Normalise the mounts value into a tuple of workspace specs."""
    if not isinstance(value, list):
        raise ConfigError("mounts must be a list of paths")
    return tuple(str(item) for item in value)


def build_config(values: dict[str, object], working_directory: Path) -> Config:
    """Turn merged config values into a Config, filling in the derived sandbox name."""
    name = str(values["name"])
    if not name:
        name = default_base_name(working_directory)
    return Config(
        token_file=str(values["tokenFile"]),
        name=name,
        memory=str(values["memory"]),
        cpus=str(values["cpus"]),
        root_size=str(values["rootSize"]),
        docker_size=str(values["dockerSize"]),
        model=str(values["model"]),
        prompt_file=str(values["promptFile"]),
        kit=str(values["kit"]),
        mounts=as_mount_list(values["mounts"]),
    )


def build_parser() -> argparse.ArgumentParser:
    """Define the command line interface."""
    parser = argparse.ArgumentParser(
        prog="box",
        description="Run Claude Code inside a disposable Docker sandbox.",
    )
    parser.add_argument("--token-file", dest="tokenFile", metavar="PATH", help="file holding the OAuth token")
    parser.add_argument("--name", dest="name", metavar="NAME", help="sandbox base name")
    parser.add_argument("--memory", dest="memory", metavar="SIZE", help="memory limit, e.g. 4g")
    parser.add_argument("--cpus", dest="cpus", metavar="N", help="number of CPUs")
    parser.add_argument("--root-size", dest="rootSize", metavar="SIZE", help="sandbox root filesystem size")
    parser.add_argument("--docker-size", dest="dockerSize", metavar="SIZE", help="sandbox docker size")
    parser.add_argument("--model", dest="model", metavar="MODEL", help="Claude model to run")
    parser.add_argument("--prompt-file", dest="promptFile", metavar="PATH", help="file added to the prompt")
    parser.add_argument("--kit", dest="kit", metavar="REF", help="sbx kit reference")
    parser.add_argument("--mount", dest="mounts", metavar="SPEC", action="append", help="extra workspace")
    parser.add_argument("-v", "--verbose", action="store_true", help="print the config in effect")
    return parser


def format_value(value: object) -> str:
    """Render one config value, joining the mount list into a readable line."""
    if isinstance(value, tuple):
        return " ".join(str(item) for item in value)
    return str(value)


def format_config(config: Config) -> str:
    """Render the effective config as aligned key/value lines."""
    items = asdict(config)
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


def prepare_launch(config: Config) -> Launch:
    """Resolve everything that can still fail before the sandbox exists."""
    if not config.token_file:
        raise ConfigError(f"tokenFile is not set; pass --token-file or set it in {CONFIG_FILE}")
    token = read_token(resolve_path(config.token_file))
    agent_args = build_agent_args(read_system_prompt(config.prompt_file), config.model)
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
        if arguments.verbose:
            print(format_config(config))
        launch = prepare_launch(config)
    except ConfigError as error:
        print(f"box: {error}", file=sys.stderr)
        return 1
    return run_session(config, launch)


if __name__ == "__main__":
    sys.exit(main())
