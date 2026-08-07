"""Tests for the pure configuration and command-building helpers in box.py."""

from __future__ import annotations

import json
import subprocess
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


def write_box_file(directory: Path, name: str, contents: object) -> Path:
    """Write one .box file, creating the directory it lives in."""
    path = directory / name
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(contents))
    return path


def write_config(directory: Path, values: dict[str, object]) -> Path:
    """Write a config file, creating the .box directory it lives in."""
    return write_box_file(directory, box.CONFIG_FILE, values)


def build_config(values: dict[str, object], directory: Path) -> box.Config:
    """Build a config with no mounts, which come from their own file."""
    return box.build_config(box.merge_values(values, {}), [], directory)


def test_to_kebab_case_collapses_separators() -> None:
    assert box.to_kebab_case("My Project_v2") == "my-project-v2"


def test_default_base_name_falls_back_when_nothing_survives() -> None:
    assert box.default_base_name(Path("/tmp/___")) == "box"


def test_the_config_file_lives_in_the_box_directory() -> None:
    assert Path(box.CONFIG_FILE) == Path(box.BOX_DIR) / "config.json"


def test_read_config_file_returns_empty_when_absent(tmp_path: Path) -> None:
    assert box.read_config_file(tmp_path / box.CONFIG_FILE) == {}


def test_read_config_file_reads_known_keys(tmp_path: Path) -> None:
    assert box.read_config_file(write_config(tmp_path, {"memory": "16g"})) == {"memory": "16g"}


def test_read_config_file_rejects_unknown_keys(tmp_path: Path) -> None:
    path = write_config(tmp_path, {"nope": 1})
    with pytest.raises(box.ConfigError, match="unknown keys: nope"):
        box.read_config_file(path)


def test_read_config_file_rejects_broken_json(tmp_path: Path) -> None:
    path = write_config(tmp_path, {})
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
    config = build_config({}, Path("/home/luuk/My Repo"))
    assert config.name == "my-repo"


def test_build_config_coerces_numeric_cpus() -> None:
    config = build_config({"cpus": 6}, Path("/tmp/demo"))
    assert config.cpus == "6"


def test_a_bare_mount_is_read_only() -> None:
    assert box.to_workspace("/cache") == "/cache:ro"


def test_a_rw_mount_drops_the_suffix() -> None:
    assert box.to_workspace("/cache:rw") == "/cache"


def test_an_explicit_ro_suffix_is_rejected() -> None:
    with pytest.raises(box.ConfigError, match="read-only unless you add :rw"):
        box.to_workspace("/cache:ro")


def test_an_unknown_mount_suffix_is_rejected() -> None:
    with pytest.raises(box.ConfigError, match="unknown suffix"):
        box.to_workspace("/cache:rx")


def test_a_mount_expands_a_leading_tilde(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("HOME", "/home/someone")
    assert box.to_workspace("~/.cargo") == "/home/someone/.cargo:ro"


def test_a_rw_mount_expands_a_leading_tilde(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("HOME", "/home/someone")
    assert box.to_workspace("~/scratch:rw") == "/home/someone/scratch"


def test_the_mounts_file_lives_in_the_box_directory() -> None:
    assert Path(box.MOUNTS_FILE) == Path(box.BOX_DIR) / "mounts.json"


def test_mounts_is_not_a_config_key() -> None:
    assert "mounts" not in box.DEFAULTS


def test_read_mounts_file_returns_nothing_when_absent(tmp_path: Path) -> None:
    assert box.read_mounts_file(tmp_path / box.MOUNTS_FILE) == {}


def test_read_mounts_file_reads_the_named_paths(tmp_path: Path) -> None:
    path = write_box_file(tmp_path, box.MOUNTS_FILE, {"cargo": "~/.cargo", "go": "/usr/local/go"})
    assert box.read_mounts_file(path) == {"cargo": "~/.cargo", "go": "/usr/local/go"}


def test_read_mounts_file_rejects_a_json_array(tmp_path: Path) -> None:
    path = write_box_file(tmp_path, box.MOUNTS_FILE, ["/cache"])
    with pytest.raises(box.ConfigError, match="must contain a JSON object"):
        box.read_mounts_file(path)


def test_read_mounts_file_rejects_broken_json(tmp_path: Path) -> None:
    path = write_box_file(tmp_path, box.MOUNTS_FILE, {})
    path.write_text("{")
    with pytest.raises(box.ConfigError, match="not valid JSON"):
        box.read_mounts_file(path)


def test_as_descriptions_rejects_a_json_array() -> None:
    with pytest.raises(box.ConfigError, match="required_mounts must be a JSON object"):
        box.as_descriptions(["go_mod_cache"])


def test_order_mounts_follows_the_declaration_not_the_mounts_file() -> None:
    required = {"go": "the Go toolchain", "cargo": "the cargo home"}
    provided = {"cargo": "~/.cargo", "go": "/usr/local/go"}
    assert box.order_mounts(required, provided) == ["/usr/local/go", "~/.cargo"]


def test_a_declared_mount_with_no_path_is_rejected() -> None:
    with pytest.raises(box.ConfigError, match="go_mod_cache: the Go module cache"):
        box.order_mounts({"go_mod_cache": "the Go module cache"}, {})


def test_a_placeholder_path_is_rejected() -> None:
    required = {"go_mod_cache": "the Go module cache"}
    with pytest.raises(box.ConfigError, match="has no path on this machine"):
        box.order_mounts(required, {"go_mod_cache": box.MOUNT_PLACEHOLDER})


def test_an_empty_path_is_rejected() -> None:
    required = {"go_mod_cache": "the Go module cache"}
    with pytest.raises(box.ConfigError, match="has no path on this machine"):
        box.order_mounts(required, {"go_mod_cache": ""})


def test_an_undeclared_mount_name_is_rejected() -> None:
    with pytest.raises(box.ConfigError, match="does not declare: typo"):
        box.order_mounts({"cargo": "the cargo home"}, {"cargo": "~/.cargo", "typo": "/cache"})


def test_the_error_names_every_unfilled_mount() -> None:
    required = {"go": "the Go toolchain", "cargo": "the cargo home"}
    with pytest.raises(box.ConfigError, match="go: the Go toolchain\n  cargo: the cargo home"):
        box.order_mounts(required, {})


def test_no_declared_mounts_needs_no_mounts_file() -> None:
    assert box.order_mounts({}, {}) == []


def test_build_config_applies_the_mount_default() -> None:
    config = box.build_config(box.merge_values({}, {}), ["/a", "/b:rw"], Path("/tmp/demo"))
    assert config.mounts == ("/a:ro", "/b")


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
    without_kit = box.build_create_command(build_config({}, Path("/tmp/demo")), "demo-1")
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


def test_build_system_prompt_is_the_base_prompt_without_a_project_prompt() -> None:
    assert box.build_system_prompt("") == box.BASE_PROMPT


def test_build_system_prompt_puts_the_project_prompt_last() -> None:
    combined = box.build_system_prompt("project rules")
    assert combined.startswith(box.BASE_PROMPT)
    assert combined.endswith("project rules")


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
    write_config(tmp_path, {"memory": "16g", "cpus": "8"})
    arguments = box.build_parser().parse_args(["run", "--memory", "1g"])
    config = box.load_config(arguments, tmp_path)
    assert config.memory == "1g"
    assert config.cpus == "8"


def test_load_config_takes_mounts_from_the_mounts_file(tmp_path: Path) -> None:
    write_config(tmp_path, {"required_mounts": {"cache": "the build cache"}})
    write_box_file(tmp_path, box.MOUNTS_FILE, {"cache": "/cache"})
    arguments = box.build_parser().parse_args(["run"])
    assert box.load_config(arguments, tmp_path).mounts == ("/cache:ro",)


def test_load_config_adds_mount_flags_to_the_mounts_file(tmp_path: Path) -> None:
    write_config(tmp_path, {"required_mounts": {"cache": "the build cache"}})
    write_box_file(tmp_path, box.MOUNTS_FILE, {"cache": "/cache"})
    arguments = box.build_parser().parse_args(["run", "--mount", "/other", "--mount", "/third:rw"])
    assert box.load_config(arguments, tmp_path).mounts == ("/cache:ro", "/other:ro", "/third")


def test_load_config_takes_mount_flags_without_a_mounts_file(tmp_path: Path) -> None:
    arguments = box.build_parser().parse_args(["run", "--mount", "/other"])
    assert box.load_config(arguments, tmp_path).mounts == ("/other:ro",)


def test_load_config_has_no_mounts_without_a_mounts_file(tmp_path: Path) -> None:
    arguments = box.build_parser().parse_args(["run"])
    assert box.load_config(arguments, tmp_path).mounts == ()


def make_repository(directory: Path, gitignore: str) -> Path:
    """Create a git repository holding a mounts file and the given .gitignore."""
    subprocess.run(["git", "init", "-q", str(directory)], check=True)
    (directory / ".gitignore").write_text(gitignore)
    write_box_file(directory, box.MOUNTS_FILE, ["/cache"])
    return directory


def test_a_gitignored_mounts_file_is_accepted(tmp_path: Path) -> None:
    box.require_ignored_mounts(make_repository(tmp_path, f"{box.MOUNTS_FILE}\n"))


def test_an_ignored_box_directory_covers_the_mounts_file(tmp_path: Path) -> None:
    box.require_ignored_mounts(make_repository(tmp_path, f"{box.BOX_DIR}/\n"))


def test_a_committable_mounts_file_is_rejected(tmp_path: Path) -> None:
    repository = make_repository(tmp_path, "*.log\n")
    with pytest.raises(box.ConfigError, match="not ignored by git"):
        box.require_ignored_mounts(repository)


def test_no_mounts_file_needs_no_gitignore_entry(tmp_path: Path) -> None:
    subprocess.run(["git", "init", "-q", str(tmp_path)], check=True)
    box.require_ignored_mounts(tmp_path)


def test_prepare_launch_rejects_a_committable_mounts_file(tmp_path: Path) -> None:
    repository = make_repository(tmp_path, "")
    with pytest.raises(box.ConfigError, match="not ignored by git"):
        box.prepare_launch(make_config(), "/secrets/token", repository)


def test_gen_is_a_command() -> None:
    assert box.build_parser().parse_args(["gen"]).command == "gen"


def test_run_is_a_command() -> None:
    assert box.build_parser().parse_args(["run"]).command == "run"


def test_config_is_a_command() -> None:
    assert box.build_parser().parse_args(["config"]).command == "config"


def test_config_takes_the_same_flags_as_run() -> None:
    arguments = box.build_parser().parse_args(["config", "--memory", "8g"])
    assert arguments.memory == "8g"


def test_config_is_not_a_setup_command() -> None:
    assert "config" not in box.SETUP_COMMANDS


def test_show_config_prints_the_settings_and_returns_zero(
    capsys: pytest.CaptureFixture[str],
) -> None:
    assert box.show_config(make_config(), "/secrets/token") == 0
    printed = capsys.readouterr().out
    assert "claude-opus-5" in printed
    assert "/secrets/token" in printed


def test_a_command_is_required() -> None:
    with pytest.raises(SystemExit):
        box.build_parser().parse_args([])


def test_an_unknown_command_is_rejected() -> None:
    with pytest.raises(SystemExit):
        box.build_parser().parse_args(["nope"])


def test_gen_alone_takes_no_flags() -> None:
    box.require_no_flags(box.build_parser().parse_args(["gen"]))


def test_gen_rejects_a_setting_flag() -> None:
    arguments = box.build_parser().parse_args(["--memory", "8g", "gen"])
    with pytest.raises(box.ConfigError, match="gen takes no flags, but got --memory"):
        box.require_no_flags(arguments)


def test_gen_rejects_verbose() -> None:
    arguments = box.build_parser().parse_args(["-v", "gen"])
    with pytest.raises(box.ConfigError, match="--verbose"):
        box.require_no_flags(arguments)


def test_gen_rejects_a_mount_flag() -> None:
    arguments = box.build_parser().parse_args(["--mount", "/cache", "gen"])
    with pytest.raises(box.ConfigError, match="--mounts"):
        box.require_no_flags(arguments)


def test_gen_names_every_flag_it_was_given() -> None:
    arguments = box.build_parser().parse_args(["--memory", "8g", "--cpus", "8", "gen"])
    with pytest.raises(box.ConfigError, match="--cpus, --memory"):
        box.require_no_flags(arguments)


def test_gen_writes_both_files(tmp_path: Path) -> None:
    box.generate(tmp_path)
    assert json.loads((tmp_path / box.CONFIG_FILE).read_text()) == box.DEFAULTS
    assert json.loads((tmp_path / box.MOUNTS_FILE).read_text()) == {}


def test_gen_writes_a_config_box_can_read_back(tmp_path: Path) -> None:
    box.generate(tmp_path)
    assert box.read_config_file(tmp_path / box.CONFIG_FILE) == box.DEFAULTS


def test_gen_keeps_an_existing_config(tmp_path: Path) -> None:
    write_config(tmp_path, {"memory": "16g"})
    box.generate(tmp_path)
    assert box.read_config_file(tmp_path / box.CONFIG_FILE) == {"memory": "16g"}


def test_gen_keeps_an_existing_mounts_file(tmp_path: Path) -> None:
    write_box_file(tmp_path, box.MOUNTS_FILE, {"cache": "/cache"})
    write_config(tmp_path, {"required_mounts": {"cache": "the build cache"}})
    box.generate(tmp_path)
    assert box.read_mounts_file(tmp_path / box.MOUNTS_FILE) == {"cache": "/cache"}


def test_mount_prompt_is_a_command() -> None:
    assert box.build_parser().parse_args(["mount-prompt"]).command == "mount-prompt"


def test_mount_prompt_takes_no_flags() -> None:
    arguments = box.build_parser().parse_args(["--memory", "8g", "mount-prompt"])
    with pytest.raises(box.ConfigError, match="mount-prompt takes no flags"):
        box.require_no_flags(arguments)


def test_the_prompt_carries_the_names_and_descriptions() -> None:
    required = {"go": "the Go toolchain", "cargo": "the cargo home"}
    prompt = box.build_mount_prompt(required, ["go", "cargo"], "darwin")
    assert "go: the Go toolchain" in prompt
    assert "cargo: the cargo home" in prompt


def test_the_prompt_names_the_file_the_placeholder_and_the_platform() -> None:
    prompt = box.build_mount_prompt({"go": "the Go toolchain"}, ["go"], "darwin")
    assert box.MOUNTS_FILE in prompt
    assert box.MOUNT_PLACEHOLDER in prompt
    assert "darwin" in prompt


def test_the_prompt_holds_the_rules_box_enforces() -> None:
    prompt = box.build_mount_prompt({"go": "the Go toolchain"}, ["go"], "linux")
    assert ":rw" in prompt
    assert "Never guess" in prompt


def test_the_prompt_covers_a_key_that_is_not_in_the_file_yet() -> None:
    prompt = box.build_mount_prompt({"go": "the Go toolchain"}, ["go"], "linux")
    assert "adding the key where it is missing" in prompt


def test_mount_prompt_needs_no_mounts_file_at_all(tmp_path: Path, capsys: pytest.CaptureFixture[str]) -> None:
    write_config(tmp_path, {"required_mounts": {"go": "the Go toolchain"}})
    assert not (tmp_path / box.MOUNTS_FILE).exists()
    assert box.mount_prompt(tmp_path) == 0
    assert "go: the Go toolchain" in capsys.readouterr().out


def test_mount_prompt_asks_only_about_unfilled_mounts(
    tmp_path: Path, capsys: pytest.CaptureFixture[str]
) -> None:
    declared = {"go": "the Go toolchain", "cargo": "the cargo home"}
    write_config(tmp_path, {"required_mounts": declared})
    write_box_file(tmp_path, box.MOUNTS_FILE, {"go": "/usr/local/go", "cargo": box.MOUNT_PLACEHOLDER})
    assert box.mount_prompt(tmp_path) == 0
    printed = capsys.readouterr().out
    assert "cargo: the cargo home" in printed
    assert "go: the Go toolchain" not in printed


def test_mount_prompt_prints_nothing_when_every_mount_has_a_path(
    tmp_path: Path, capsys: pytest.CaptureFixture[str]
) -> None:
    write_config(tmp_path, {"required_mounts": {"go": "the Go toolchain"}})
    write_box_file(tmp_path, box.MOUNTS_FILE, {"go": "/usr/local/go"})
    assert box.mount_prompt(tmp_path) == 0
    assert capsys.readouterr().out == ""


def test_mount_prompt_prints_nothing_without_declared_mounts(
    tmp_path: Path, capsys: pytest.CaptureFixture[str]
) -> None:
    assert box.mount_prompt(tmp_path) == 0
    assert capsys.readouterr().out == ""


def test_mount_prompt_asks_about_a_name_the_mounts_file_lacks(
    tmp_path: Path, capsys: pytest.CaptureFixture[str]
) -> None:
    write_config(tmp_path, {"required_mounts": {"go": "the Go toolchain"}})
    box.mount_prompt(tmp_path)
    assert "go: the Go toolchain" in capsys.readouterr().out


def test_gen_scaffolds_a_placeholder_for_every_declared_mount(tmp_path: Path) -> None:
    write_config(tmp_path, {"required_mounts": {"go": "the Go toolchain"}})
    box.generate(tmp_path)
    assert box.read_mounts_file(tmp_path / box.MOUNTS_FILE) == {"go": box.MOUNT_PLACEHOLDER}


def test_gen_adds_declared_names_the_mounts_file_is_missing(tmp_path: Path) -> None:
    declared = {"go": "the Go toolchain", "cargo": "the cargo home"}
    write_config(tmp_path, {"required_mounts": declared})
    write_box_file(tmp_path, box.MOUNTS_FILE, {"go": "/usr/local/go"})
    box.generate(tmp_path)
    assert box.read_mounts_file(tmp_path / box.MOUNTS_FILE) == {
        "go": "/usr/local/go",
        "cargo": box.MOUNT_PLACEHOLDER,
    }


def test_gen_leaves_an_undeclared_name_for_box_to_reject(tmp_path: Path) -> None:
    write_config(tmp_path, {"required_mounts": {}})
    write_box_file(tmp_path, box.MOUNTS_FILE, {"typo": "/cache"})
    box.generate(tmp_path)
    assert box.read_mounts_file(tmp_path / box.MOUNTS_FILE) == {"typo": "/cache"}


def test_gen_warns_about_every_placeholder_it_wrote(
    tmp_path: Path, capsys: pytest.CaptureFixture[str]
) -> None:
    write_config(tmp_path, {"required_mounts": {"go": "the Go toolchain"}})
    box.generate(tmp_path)
    assert "go: the Go toolchain" in capsys.readouterr().err


def test_gen_is_silent_when_nothing_needs_filling_in(
    tmp_path: Path, capsys: pytest.CaptureFixture[str]
) -> None:
    box.generate(tmp_path)
    assert capsys.readouterr().err == ""


def test_gen_accepts_an_existing_box_directory(tmp_path: Path) -> None:
    (tmp_path / box.BOX_DIR).mkdir()
    assert box.generate(tmp_path) == 0


def test_gen_leaves_a_project_box_will_run_in(tmp_path: Path) -> None:
    subprocess.run(["git", "init", "-q", str(tmp_path)], check=True)
    box.generate(tmp_path)
    box.require_ignored_mounts(tmp_path)


def test_gen_creates_a_gitignore_holding_the_mounts_file(tmp_path: Path) -> None:
    subprocess.run(["git", "init", "-q", str(tmp_path)], check=True)
    box.generate(tmp_path)
    assert (tmp_path / box.GITIGNORE_FILE).read_text() == f"{box.MOUNTS_FILE}\n"


def test_gen_keeps_what_the_gitignore_already_held(tmp_path: Path) -> None:
    subprocess.run(["git", "init", "-q", str(tmp_path)], check=True)
    (tmp_path / box.GITIGNORE_FILE).write_text("*.log\n")
    box.generate(tmp_path)
    assert (tmp_path / box.GITIGNORE_FILE).read_text() == f"*.log\n{box.MOUNTS_FILE}\n"


def test_gen_leaves_an_already_ignored_gitignore_alone(tmp_path: Path) -> None:
    subprocess.run(["git", "init", "-q", str(tmp_path)], check=True)
    (tmp_path / box.GITIGNORE_FILE).write_text(f"{box.BOX_DIR}/\n")
    box.generate(tmp_path)
    assert (tmp_path / box.GITIGNORE_FILE).read_text() == f"{box.BOX_DIR}/\n"


def test_append_line_starts_a_new_line_when_one_is_missing(tmp_path: Path) -> None:
    path = tmp_path / box.GITIGNORE_FILE
    path.write_text("*.log")
    box.append_line(path, "build")
    assert path.read_text() == "*.log\nbuild\n"


def test_append_line_creates_the_file_when_absent(tmp_path: Path) -> None:
    path = tmp_path / box.GITIGNORE_FILE
    box.append_line(path, "build")
    assert path.read_text() == "build\n"


def test_prepare_launch_requires_the_token_environment_variable() -> None:
    with pytest.raises(box.ConfigError, match="CLAUDE_OAUTH_TOKEN_FILE is not set"):
        box.prepare_launch(make_config(), "", Path("/tmp/demo"))


def test_prepare_launch_requires_a_kit() -> None:
    config = build_config({}, Path("/tmp/demo"))
    with pytest.raises(box.ConfigError, match="kit is not set"):
        box.prepare_launch(config, "/secrets/token", Path("/tmp/demo"))


def test_prepare_launch_requires_a_model() -> None:
    config = build_config({"kit": ".sbx/kit"}, Path("/tmp/demo"))
    with pytest.raises(box.ConfigError, match="model is not set"):
        box.prepare_launch(config, "/secrets/token", Path("/tmp/demo"))


def test_require_settings_accepts_a_complete_config() -> None:
    box.require_settings(make_config())


def test_token_file_comes_from_the_environment(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv(box.TOKEN_FILE_ENV, "/secrets/token")
    assert box.token_file_from_environment() == "/secrets/token"


def test_token_file_is_empty_when_unset(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv(box.TOKEN_FILE_ENV, raising=False)
    assert box.token_file_from_environment() == ""


def test_token_file_is_not_a_config_key() -> None:
    assert "token_file" not in box.DEFAULTS


def test_every_config_key_is_snake_case() -> None:
    assert all(key == key.lower() for key in box.DEFAULTS)


def test_config_keys_match_the_config_fields() -> None:
    config = build_config({}, Path("/tmp/demo"))
    settings = set(box.DEFAULTS) - {"required_mounts"}
    assert set(vars(config)) == settings | {"mounts"}


def test_flags_use_the_config_keys_with_hyphens() -> None:
    arguments = box.build_parser().parse_args(["run", "--root-size", "20g", "--prompt-file", "p.md"])
    assert vars(arguments)["root_size"] == "20g"
    assert vars(arguments)["prompt_file"] == "p.md"


def test_format_value_joins_mounts() -> None:
    assert box.format_value(("/a:ro", "/b")) == "/a:ro /b"


def test_format_config_shows_the_token_path_with_the_settings() -> None:
    rendered = box.format_config(make_config(), "/secrets/token")
    assert box.TOKEN_FILE_ENV in rendered
    assert "/secrets/token" in rendered
    assert "8g" in rendered
