"""Tests for the pure configuration and command-building helpers in box.py."""

from __future__ import annotations

import json
from pathlib import Path

import pytest

import box


def make_config() -> box.Config:
    """Build a config with every field set to a recognisable value."""
    return box.Config(
        name="demo",
        memory="8g",
        cpus="2",
        root_size="20g",
        docker_size="30g",
        model="claude-opus-5",
        prompt_file="docs/agent.md",
        kit=".sbx/kit",
        mounts=("/cache:ro",),
    )


def test_to_kebab_case_collapses_separators() -> None:
    assert box.to_kebab_case("My Project_v2") == "my-project-v2"


def test_default_base_name_falls_back_when_nothing_survives() -> None:
    assert box.default_base_name(Path("/tmp/___")) == "box"


def test_read_config_file_returns_empty_when_absent(tmp_path: Path) -> None:
    assert box.read_config_file(tmp_path / box.CONFIG_FILE) == {}


def test_read_config_file_reads_known_keys(tmp_path: Path) -> None:
    path = tmp_path / box.CONFIG_FILE
    path.write_text(json.dumps({"memory": "16g"}))
    assert box.read_config_file(path) == {"memory": "16g"}


def test_read_config_file_rejects_unknown_keys(tmp_path: Path) -> None:
    path = tmp_path / box.CONFIG_FILE
    path.write_text(json.dumps({"nope": 1}))
    with pytest.raises(box.ConfigError, match="unknown keys: nope"):
        box.read_config_file(path)


def test_read_config_file_rejects_broken_json(tmp_path: Path) -> None:
    path = tmp_path / box.CONFIG_FILE
    path.write_text("{")
    with pytest.raises(box.ConfigError, match="not valid JSON"):
        box.read_config_file(path)


def test_merge_values_prefers_cli_over_file() -> None:
    merged = box.merge_values({"memory": "16g", "cpus": "8"}, {"memory": "2g", "cpus": None})
    assert merged["memory"] == "2g"
    assert merged["cpus"] == "8"


def test_merge_values_falls_back_to_defaults() -> None:
    merged = box.merge_values({}, {})
    assert merged["memory"] == box.DEFAULTS["memory"]


def test_build_config_derives_name_from_directory() -> None:
    config = box.build_config(box.merge_values({}, {}), Path("/home/luuk/My Repo"))
    assert config.name == "my-repo"


def test_build_config_coerces_numeric_cpus() -> None:
    config = box.build_config(box.merge_values({"cpus": 6}, {}), Path("/tmp/demo"))
    assert config.cpus == "6"


def test_build_config_rejects_non_list_mounts() -> None:
    with pytest.raises(box.ConfigError, match="mounts must be a list"):
        box.build_config(box.merge_values({"mounts": "/cache"}, {}), Path("/tmp/demo"))


def test_parse_ref_names_extracts_sandbox_names() -> None:
    refs = "refs/sandboxes/demo-1/main\nrefs/sandboxes/demo-2/wip\nrefs/heads/main\n"
    assert box.parse_ref_names(refs) == {"demo-1", "demo-2"}


def test_pick_name_skips_used_names() -> None:
    assert box.pick_name("demo", {"demo-1", "demo-2"}) == "demo-3"


def test_pick_name_starts_at_one() -> None:
    assert box.pick_name("demo", set()) == "demo-1"


def test_build_create_command_includes_mounts_and_kit() -> None:
    command = box.build_create_command(make_config(), "demo-1")
    assert command == [
        "sbx",
        "create",
        "claude",
        ".",
        "/cache:ro",
        "--clone",
        "--name",
        "demo-1",
        "--memory",
        "8g",
        "--cpus",
        "2",
        "--kit",
        ".sbx/kit",
    ]


def test_build_create_command_omits_empty_kit() -> None:
    command = box.build_create_command(make_config(), "demo-1")
    assert "--kit" in command
    without_kit = box.build_create_command(
        box.build_config(box.merge_values({}, {}), Path("/tmp/demo")), "demo-1"
    )
    assert "--kit" not in without_kit


def test_build_agent_args_includes_prompt_and_model() -> None:
    assert box.build_agent_args("be careful", "claude-opus-5") == [
        "--append-system-prompt",
        "be careful",
        "--model",
        "claude-opus-5",
    ]


def test_build_agent_args_is_empty_without_settings() -> None:
    assert box.build_agent_args("", "") == []


def test_build_run_command_omits_separator_without_args() -> None:
    assert box.build_run_command("demo-1", []) == ["sbx", "run", "claude", "--name", "demo-1"]


def test_build_run_command_passes_agent_args_after_separator() -> None:
    command = box.build_run_command("demo-1", ["--model", "claude-opus-5"])
    assert command[-3:] == ["--", "--model", "claude-opus-5"]


def test_build_environment_sets_disk_limits() -> None:
    environment = box.build_environment(make_config())
    assert environment["DOCKER_SANDBOXES_ROOT_SIZE"] == "20g"
    assert environment["DOCKER_SANDBOXES_DOCKER_SIZE"] == "30g"


def test_read_system_prompt_returns_empty_without_file() -> None:
    assert box.read_system_prompt("") == ""


def test_read_system_prompt_reads_the_file(tmp_path: Path) -> None:
    path = tmp_path / "agent.md"
    path.write_text("stay in the sandbox")
    assert box.read_system_prompt(str(path)) == "stay in the sandbox"


def test_read_system_prompt_rejects_missing_file(tmp_path: Path) -> None:
    with pytest.raises(box.ConfigError, match="does not exist"):
        box.read_system_prompt(str(tmp_path / "missing.md"))


def test_read_token_strips_newlines(tmp_path: Path) -> None:
    path = tmp_path / "token"
    path.write_text("abc123\n")
    assert box.read_token(path) == "abc123"


def test_read_token_rejects_empty_file(tmp_path: Path) -> None:
    path = tmp_path / "token"
    path.write_text("")
    with pytest.raises(box.ConfigError, match="does not exist or is empty"):
        box.read_token(path)


def test_load_config_lets_cli_win_over_file(tmp_path: Path) -> None:
    (tmp_path / box.CONFIG_FILE).write_text(json.dumps({"memory": "16g", "cpus": "8"}))
    arguments = box.build_parser().parse_args(["--memory", "1g"])
    config = box.load_config(arguments, tmp_path)
    assert config.memory == "1g"
    assert config.cpus == "8"


def test_prepare_launch_requires_the_token_environment_variable() -> None:
    config = box.build_config(box.merge_values({}, {}), Path("/tmp/demo"))
    with pytest.raises(box.ConfigError, match="CLAUDE_OAUTH_TOKEN_FILE is not set"):
        box.prepare_launch(config, "")


def test_token_file_comes_from_the_environment(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv(box.TOKEN_FILE_ENV, "/secrets/token")
    assert box.token_file_from_environment() == "/secrets/token"


def test_token_file_is_empty_when_unset(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv(box.TOKEN_FILE_ENV, raising=False)
    assert box.token_file_from_environment() == ""


def test_token_file_is_not_a_config_key() -> None:
    assert "tokenFile" not in box.DEFAULTS


def test_format_value_joins_mounts() -> None:
    assert box.format_value(("/a:ro", "/b")) == "/a:ro /b"


def test_format_config_shows_the_token_path_with_the_settings() -> None:
    rendered = box.format_config(make_config(), "/secrets/token")
    assert box.TOKEN_FILE_ENV in rendered
    assert "/secrets/token" in rendered
    assert "8g" in rendered
