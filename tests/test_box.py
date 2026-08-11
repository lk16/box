"""Tests for box.py: its pure helpers, and the git and sbx work behind them."""

from __future__ import annotations

import hashlib
import json
import os
import platform
import re
import subprocess
import sys
import urllib.request
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
        prompt_file="docs/project-prompt.md",
        kit="registry/kit",
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


def config_from_values(values: dict[str, object], directory: Path) -> box.Config:
    """Build a config from config file values and no mounts, which come from their own file."""
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


def test_read_config_file_rejects_a_json_array(tmp_path: Path) -> None:
    path = write_box_file(tmp_path, box.CONFIG_FILE, ["memory"])
    with pytest.raises(box.ConfigError, match="must contain a JSON object"):
        box.read_config_file(path)


def test_read_config_file_rejects_broken_json(tmp_path: Path) -> None:
    path = write_config(tmp_path, {})
    path.write_text("{")
    with pytest.raises(box.ConfigError, match="not valid JSON"):
        box.read_config_file(path)


def test_read_config_file_rejects_a_null_setting(tmp_path: Path) -> None:
    path = write_config(tmp_path, {"model": None})
    with pytest.raises(box.ConfigError, match="gives model null, which is not text or a number"):
        box.read_config_file(path)


def test_read_config_file_rejects_a_setting_that_is_a_list(tmp_path: Path) -> None:
    path = write_config(tmp_path, {"memory": [1, 2]})
    with pytest.raises(box.ConfigError, match="gives memory a list, which is not text or a number"):
        box.read_config_file(path)


def test_read_config_file_rejects_a_setting_that_is_a_boolean(tmp_path: Path) -> None:
    path = write_config(tmp_path, {"model": True})
    with pytest.raises(box.ConfigError, match="gives model a boolean, which is not text or a number"):
        box.read_config_file(path)


def test_read_config_file_takes_a_number_as_the_string_it_spells(tmp_path: Path) -> None:
    path = write_config(tmp_path, {"cpus": 4})
    assert box.read_config_file(path) == {"cpus": "4"}


def test_read_config_file_keeps_required_mounts_an_object(tmp_path: Path) -> None:
    declared = {"go": "the Go toolchain"}
    path = write_config(tmp_path, {"required_mounts": declared})
    assert box.read_config_file(path) == {"required_mounts": declared}


def test_merge_values_prefers_cli_over_file() -> None:
    merged = box.merge_values({"memory": "16g"}, {"memory": "2g"})
    assert merged["memory"] == "2g"


def test_merge_values_keeps_the_file_value_where_no_flag_was_given() -> None:
    merged = box.merge_values({"cpus": "8"}, {"cpus": None})
    assert merged["cpus"] == "8"


def test_merge_values_falls_back_to_defaults() -> None:
    merged = box.merge_values({}, {})
    assert merged["memory"] == box.DEFAULTS["memory"]


def test_build_config_derives_name_from_directory() -> None:
    config = config_from_values({}, Path("/home/luuk/My Repo"))
    assert config.name == "my-repo"


def test_build_config_rejects_a_value_that_never_went_through_the_config_file() -> None:
    with pytest.raises(box.ConfigError, match="cpus is a number, which is not text"):
        config_from_values({"cpus": 6}, Path("/tmp/demo"))


def test_load_config_takes_a_numeric_cpus_from_the_config_file(tmp_path: Path) -> None:
    write_config(tmp_path, {"cpus": 6})
    arguments = box.build_parser().parse_args(["run"])
    assert box.load_config(arguments, tmp_path).cpus == "6"


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


def test_an_empty_mount_is_rejected() -> None:
    with pytest.raises(box.ConfigError, match="a mount must name a path"):
        box.to_workspace("")


def test_a_mount_that_is_only_a_rw_suffix_is_rejected() -> None:
    with pytest.raises(box.ConfigError, match="a mount must name a path"):
        box.to_workspace(":rw")


def test_a_rw_mount_still_rejects_a_colon_in_its_path() -> None:
    with pytest.raises(box.ConfigError, match="unknown suffix"):
        box.to_workspace("/data/a:b:rw")


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


def test_read_mounts_file_rejects_a_path_that_is_not_a_string(tmp_path: Path) -> None:
    path = write_box_file(tmp_path, box.MOUNTS_FILE, {"cache": None})
    with pytest.raises(box.ConfigError, match="gives cache null, which is not text or a number"):
        box.read_mounts_file(path)


def test_as_descriptions_rejects_a_description_that_is_not_text() -> None:
    with pytest.raises(box.ConfigError, match="gives go a list, which is not text or a number"):
        box.as_descriptions({"go": ["the Go toolchain"]})


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


def test_parse_sandbox_refs_reads_name_and_commit() -> None:
    refs = "refs/sandboxes/demo-1/main abc123\nrefs/sandboxes/demo-1/wip def456\n"
    assert box.parse_sandbox_refs(refs) == [
        box.SandboxRef(ref_name="refs/sandboxes/demo-1/main", commit="abc123"),
        box.SandboxRef(ref_name="refs/sandboxes/demo-1/wip", commit="def456"),
    ]


def test_parse_sandbox_refs_skips_lines_that_are_not_a_ref_and_a_commit() -> None:
    assert box.parse_sandbox_refs("refs/sandboxes/demo-1/main\n\n") == []


def test_to_branch_name_kebab_cases_the_answer() -> None:
    assert box.to_branch_name("Add Retry Logic") == "add-retry-logic"


def test_to_branch_name_keeps_at_most_five_words() -> None:
    assert box.to_branch_name("one two three four five six seven") == "one-two-three-four-five"


def test_to_branch_name_takes_the_last_line_the_agent_wrote() -> None:
    assert box.to_branch_name("Here is a name:\n\nfix-flaky-test\n") == "fix-flaky-test"


def test_to_branch_name_is_empty_without_an_answer() -> None:
    assert box.to_branch_name("\n  \n") == ""


def test_to_branch_name_is_empty_when_nothing_survives_kebab_casing() -> None:
    assert box.to_branch_name("!!!") == ""


def test_build_branch_name_command_runs_claude_headless_with_the_subjects() -> None:
    command = box.build_branch_name_command("Add retry logic")
    assert command[:2] == ["claude", "-p"]
    assert command[-1].startswith(box.BRANCH_NAME_PROMPT)
    assert "Add retry logic" in command[-1]


def test_pick_branch_name_keeps_a_free_name() -> None:
    assert box.pick_branch_name("add-retry-logic", {"main"}) == "add-retry-logic"


def test_pick_branch_name_numbers_a_taken_name_from_two() -> None:
    used = {"add-retry-logic", "add-retry-logic-2"}
    assert box.pick_branch_name("add-retry-logic", used) == "add-retry-logic-3"


def test_pick_name_skips_used_names() -> None:
    assert box.pick_name("demo", {"demo-1", "demo-2"}) == "demo-3"


def test_pick_name_starts_at_one() -> None:
    assert box.pick_name("demo", set()) == "demo-1"


SANDBOX_REF = box.SandboxRef(ref_name="refs/sandboxes/demo-1/main", commit="abc123")


class FakeRepository:
    """Answer the git and Claude calls settle_ref makes, recording the branches and refs it wrote."""

    def __init__(self, *, count: str, suggestion: str, branches: set[str], refuse_branch: bool) -> None:
        self.count = count
        self.suggestion = suggestion
        self.branches = branches
        self.refuse_branch = refuse_branch
        self.created: list[tuple[str, str]] = []
        self.deleted: list[str] = []
        self.named_after = ""

    def install(self, monkeypatch: pytest.MonkeyPatch) -> None:
        """Stand in for everything settle_ref runs against git and Claude."""

        def count_new_commits(commit: str) -> str:
            return self.count

        def new_commit_subjects(commit: str) -> str:
            return "Add retry logic\n"

        def local_branch_names() -> set[str]:
            return self.branches

        monkeypatch.setattr(box, "count_new_commits", count_new_commits)
        monkeypatch.setattr(box, "new_commit_subjects", new_commit_subjects)
        monkeypatch.setattr(box, "suggest_branch_name", self.suggest)
        monkeypatch.setattr(box, "local_branch_names", local_branch_names)
        monkeypatch.setattr(box, "create_branch", self.create)
        monkeypatch.setattr(box, "delete_ref", self.deleted.append)

    def suggest(self, subjects: str) -> str:
        """Record what the branch was named after, and answer with the fixed suggestion."""
        self.named_after = subjects
        return self.suggestion

    def create(self, branch: str, commit: str) -> bool:
        """Create a branch unless this repository was set up to refuse one."""
        if self.refuse_branch:
            return False
        self.created.append((branch, commit))
        return True


def test_settle_ref_branches_the_work_and_drops_the_ref(monkeypatch: pytest.MonkeyPatch) -> None:
    repository = FakeRepository(
        count="3", suggestion="add-retry-logic", branches={"main"}, refuse_branch=False
    )
    repository.install(monkeypatch)
    box.settle_ref(SANDBOX_REF)
    assert repository.created == [("add-retry-logic", "abc123")]
    assert repository.deleted == [SANDBOX_REF.ref_name]


def test_settle_ref_names_the_branch_after_the_commits(monkeypatch: pytest.MonkeyPatch) -> None:
    repository = FakeRepository(count="3", suggestion="add-retry-logic", branches=set(), refuse_branch=False)
    repository.install(monkeypatch)
    box.settle_ref(SANDBOX_REF)
    assert repository.named_after == "Add retry logic\n"


def test_settle_ref_numbers_a_branch_the_repository_already_has(monkeypatch: pytest.MonkeyPatch) -> None:
    repository = FakeRepository(
        count="1", suggestion="add-retry-logic", branches={"add-retry-logic"}, refuse_branch=False
    )
    repository.install(monkeypatch)
    box.settle_ref(SANDBOX_REF)
    assert repository.created == [("add-retry-logic-2", "abc123")]


def test_settle_ref_says_where_the_work_ended_up(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    repository = FakeRepository(count="3", suggestion="add-retry-logic", branches=set(), refuse_branch=False)
    repository.install(monkeypatch)
    box.settle_ref(SANDBOX_REF)
    printed = capsys.readouterr().err
    assert "branch add-retry-logic holds 3 commits" in printed
    assert SANDBOX_REF.ref_name in printed


def test_settle_ref_counts_a_single_commit_in_the_singular(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    repository = FakeRepository(count="1", suggestion="add-retry-logic", branches=set(), refuse_branch=False)
    repository.install(monkeypatch)
    box.settle_ref(SANDBOX_REF)
    assert "holds 1 commit from" in capsys.readouterr().err


def test_settle_ref_drops_a_ref_holding_no_commits(monkeypatch: pytest.MonkeyPatch) -> None:
    repository = FakeRepository(count="0", suggestion="add-retry-logic", branches=set(), refuse_branch=False)
    repository.install(monkeypatch)
    box.settle_ref(SANDBOX_REF)
    assert repository.created == []
    assert repository.deleted == [SANDBOX_REF.ref_name]


def test_settle_ref_keeps_the_ref_when_naming_fails(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    repository = FakeRepository(count="2", suggestion="", branches=set(), refuse_branch=False)
    repository.install(monkeypatch)
    box.settle_ref(SANDBOX_REF)
    assert repository.created == []
    assert repository.deleted == []
    assert SANDBOX_REF.ref_name in capsys.readouterr().err


def test_settle_ref_keeps_the_ref_when_git_refuses_the_branch(monkeypatch: pytest.MonkeyPatch) -> None:
    repository = FakeRepository(count="2", suggestion="add-retry-logic", branches=set(), refuse_branch=False)
    repository.refuse_branch = True
    repository.install(monkeypatch)
    box.settle_ref(SANDBOX_REF)
    assert repository.deleted == []


def test_settle_ref_keeps_the_ref_when_git_cannot_count_the_commits(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    repository = FakeRepository(count="", suggestion="add-retry-logic", branches=set(), refuse_branch=False)
    repository.install(monkeypatch)
    box.settle_ref(SANDBOX_REF)
    assert repository.created == []
    assert repository.deleted == []


def completed(returncode: int, stdout: str) -> subprocess.CompletedProcess[str]:
    """Build the result a finished claude run would hand back."""
    return subprocess.CompletedProcess(args=["claude"], returncode=returncode, stdout=stdout, stderr="")


class FakeClaude:
    """Answer the one headless claude run suggest_branch_name makes, recording how it made it."""

    def __init__(self, *, result: subprocess.CompletedProcess[str], error: Exception | None) -> None:
        self.result = result
        self.error = error
        self.commands: list[list[str]] = []
        self.keywords: dict[str, object] = {}

    def install(self, monkeypatch: pytest.MonkeyPatch) -> None:
        """Stand in for subprocess.run, which is all suggest_branch_name reaches the system by."""
        monkeypatch.setattr(subprocess, "run", self.run)

    def run(self, command: list[str], **keywords: object) -> subprocess.CompletedProcess[str]:
        """Answer a claude run, failing loudly when anything else was run instead."""
        assert command[0] == "claude"
        self.commands.append(command)
        self.keywords.update(keywords)
        if self.error:
            raise self.error
        return self.result


def answering_claude(answer: str) -> FakeClaude:
    """Build a claude that succeeds with the given answer."""
    return FakeClaude(result=completed(0, answer), error=None)


def test_suggest_branch_name_kebab_cases_what_claude_printed(monkeypatch: pytest.MonkeyPatch) -> None:
    answering_claude("Add Retry Logic\n").install(monkeypatch)
    assert box.suggest_branch_name("Add retry logic") == "add-retry-logic"


def test_suggest_branch_name_gives_claude_one_turn_and_no_more(monkeypatch: pytest.MonkeyPatch) -> None:
    claude = answering_claude("add-retry-logic\n")
    claude.install(monkeypatch)
    box.suggest_branch_name("Add retry logic")
    assert claude.keywords["timeout"] == box.BRANCH_NAME_TIMEOUT_SECONDS


def test_suggest_branch_name_sends_the_subjects_to_a_headless_claude(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    claude = answering_claude("add-retry-logic\n")
    claude.install(monkeypatch)
    box.suggest_branch_name("Add retry logic")
    assert claude.commands == [box.build_branch_name_command("Add retry logic")]


def test_suggest_branch_name_is_empty_when_claude_times_out(monkeypatch: pytest.MonkeyPatch) -> None:
    timed_out = subprocess.TimeoutExpired(cmd="claude", timeout=box.BRANCH_NAME_TIMEOUT_SECONDS)
    FakeClaude(result=completed(0, ""), error=timed_out).install(monkeypatch)
    assert box.suggest_branch_name("Add retry logic") == ""


def test_suggest_branch_name_is_empty_when_claude_fails(monkeypatch: pytest.MonkeyPatch) -> None:
    FakeClaude(result=completed(1, ""), error=None).install(monkeypatch)
    assert box.suggest_branch_name("Add retry logic") == ""


def test_suggest_branch_name_is_empty_when_claude_is_not_installed(monkeypatch: pytest.MonkeyPatch) -> None:
    FakeClaude(result=completed(0, ""), error=FileNotFoundError("claude")).install(monkeypatch)
    assert box.suggest_branch_name("Add retry logic") == ""


MISSING_BINARY = "definitely-not-a-binary-on-this-machine"


def test_capture_returns_what_the_command_printed() -> None:
    assert box.capture([sys.executable, "-c", "print('hello')"]) == "hello\n"


def test_capture_is_empty_when_the_command_exits_non_zero() -> None:
    assert box.capture([sys.executable, "-c", "print('hello'); raise SystemExit(1)"]) == ""


def test_capture_is_empty_when_the_command_is_not_installed() -> None:
    assert box.capture([MISSING_BINARY]) == ""


def test_succeeds_is_true_when_the_command_exits_zero() -> None:
    assert box.succeeds([sys.executable, "-c", ""])


def test_succeeds_is_false_when_the_command_exits_non_zero() -> None:
    assert not box.succeeds([sys.executable, "-c", "raise SystemExit(1)"])


def test_succeeds_is_false_when_the_command_is_not_installed() -> None:
    assert not box.succeeds([MISSING_BINARY])


def test_store_secret_never_puts_the_token_on_the_command_line(monkeypatch: pytest.MonkeyPatch) -> None:
    commands: list[list[str]] = []
    stdin: list[object] = []

    def run(command: list[str], **keywords: object) -> subprocess.CompletedProcess[str]:
        commands.append(command)
        stdin.append(keywords["input"])
        return subprocess.CompletedProcess(args=command, returncode=0)

    monkeypatch.setattr(subprocess, "run", run)
    box.store_secret("demo-1", "sk-ant-secret")
    assert stdin == ["sk-ant-secret"]
    assert "sk-ant-secret" not in " ".join(commands[0])


def test_store_secret_names_the_sandbox_the_host_and_the_variable(monkeypatch: pytest.MonkeyPatch) -> None:
    commands: list[list[str]] = []

    def run(command: list[str], **keywords: object) -> subprocess.CompletedProcess[str]:
        commands.append(command)
        return subprocess.CompletedProcess(args=command, returncode=0)

    monkeypatch.setattr(subprocess, "run", run)
    box.store_secret("demo-1", "sk-ant-secret")
    assert commands[0] == [
        "sbx",
        "secret",
        "set-custom",
        "demo-1",
        "--host",
        box.SECRET_HOST,
        "--env",
        box.SECRET_ENV,
    ]


def test_store_secret_reports_a_missing_sbx(monkeypatch: pytest.MonkeyPatch) -> None:
    def run(command: list[str], **keywords: object) -> subprocess.CompletedProcess[str]:
        raise FileNotFoundError("sbx")

    monkeypatch.setattr(subprocess, "run", run)
    with pytest.raises(box.ConfigError, match="could not run sbx"):
        box.store_secret("demo-1", "sk-ant-secret")


def test_store_secret_reports_an_sbx_that_refused(monkeypatch: pytest.MonkeyPatch) -> None:
    def run(command: list[str], **keywords: object) -> subprocess.CompletedProcess[str]:
        return subprocess.CompletedProcess(args=command, returncode=1)

    monkeypatch.setattr(subprocess, "run", run)
    with pytest.raises(box.ConfigError, match="would not store the OAuth token for demo-1"):
        box.store_secret("demo-1", "sk-ant-secret")


class FakeSandbox:
    """Answer the commands cleanup runs, recording every one of them in the order it ran."""

    def __init__(self, *, dirty: str, fetch_fails: bool, status_fails: bool) -> None:
        self.dirty = dirty
        self.fetch_fails = fetch_fails
        self.status_fails = status_fails
        self.commands: list[list[str]] = []

    def install(self, monkeypatch: pytest.MonkeyPatch) -> None:
        """Stand in for everything cleanup runs against git, sbx and the refs."""

        def settle_sandbox_refs(sandbox_name: str) -> None:
            self.commands.append(["settle", sandbox_name])

        monkeypatch.setattr(box, "capture", self.capture)
        monkeypatch.setattr(box, "succeeds", self.succeeds)
        monkeypatch.setattr(box, "run_quietly", self.run_quietly)
        monkeypatch.setattr(box, "settle_sandbox_refs", settle_sandbox_refs)
        monkeypatch.setattr(subprocess, "run", self.run)

    def capture(self, command: list[str]) -> str:
        """Record a command whose output cleanup does not act on."""
        self.commands.append(command)
        return ""

    def succeeds(self, command: list[str]) -> bool:
        """Record the fetch, which is the one command cleanup asks only for a verdict on."""
        self.commands.append(command)
        return not self.fetch_fails

    def run_quietly(self, command: list[str]) -> subprocess.CompletedProcess[str]:
        """Record the dirty check, answering it with this sandbox's status."""
        self.commands.append(command)
        if self.status_fails:
            return subprocess.CompletedProcess(args=command, returncode=1, stdout="", stderr="no sandbox")
        return subprocess.CompletedProcess(args=command, returncode=0, stdout=self.dirty, stderr="")

    def run(self, command: list[str], **keywords: object) -> subprocess.CompletedProcess[str]:
        """Record a command that cleanup does not read the output of."""
        self.commands.append(command)
        return subprocess.CompletedProcess(args=command, returncode=0)


def clean_sandbox() -> FakeSandbox:
    """Build a sandbox whose fetch and status check both work and report nothing to save."""
    return FakeSandbox(dirty="", fetch_fails=False, status_fails=False)


def test_cleanup_settles_the_refs_before_removing_the_sandbox(monkeypatch: pytest.MonkeyPatch) -> None:
    sandbox = clean_sandbox()
    sandbox.install(monkeypatch)
    box.cleanup("demo-1")
    settled = sandbox.commands.index(["settle", "demo-1"])
    assert settled < sandbox.commands.index(["sbx", "rm", "--force", "demo-1"])


def test_cleanup_fetches_from_the_sandbox_remote(monkeypatch: pytest.MonkeyPatch) -> None:
    sandbox = clean_sandbox()
    sandbox.install(monkeypatch)
    box.cleanup("demo-1")
    assert ["git", "fetch", "sandbox-demo-1"] in sandbox.commands


def test_cleanup_keeps_the_refs_and_the_sandbox_when_the_tree_is_dirty(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    sandbox = FakeSandbox(dirty=" M box.py\n", fetch_fails=False, status_fails=False)
    sandbox.install(monkeypatch)
    box.cleanup("demo-1")
    assert ["settle", "demo-1"] not in sandbox.commands
    assert ["sbx", "rm", "--force", "demo-1"] not in sandbox.commands
    assert "uncommitted changes" in capsys.readouterr().err


def test_cleanup_keeps_the_sandbox_when_the_fetch_fails(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    sandbox = FakeSandbox(dirty="", fetch_fails=True, status_fails=False)
    sandbox.install(monkeypatch)
    box.cleanup("demo-1")
    assert ["settle", "demo-1"] not in sandbox.commands
    assert ["sbx", "rm", "--force", "demo-1"] not in sandbox.commands
    assert "git fetch sandbox-demo-1 failed" in capsys.readouterr().err


def test_cleanup_keeps_the_sandbox_when_the_dirty_check_fails(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    sandbox = FakeSandbox(dirty="", fetch_fails=False, status_fails=True)
    sandbox.install(monkeypatch)
    box.cleanup("demo-1")
    assert ["settle", "demo-1"] not in sandbox.commands
    assert ["sbx", "rm", "--force", "demo-1"] not in sandbox.commands
    assert "could not read the sandbox's git status" in capsys.readouterr().err


def test_cleanup_says_how_to_recover_from_a_sandbox_it_kept(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    sandbox = FakeSandbox(dirty="", fetch_fails=True, status_fails=False)
    sandbox.install(monkeypatch)
    box.cleanup("demo-1")
    printed = capsys.readouterr().err
    assert "sbx exec demo-1" in printed
    assert "sbx cp demo-1:" in printed
    assert "sbx rm --force demo-1" in printed


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
        "registry/kit",
    ]


def test_build_create_command_omits_empty_kit(tmp_path: Path) -> None:
    command = box.build_create_command(config_from_values({}, tmp_path), "demo-1")
    assert "--kit" not in command


def test_build_agent_args_includes_prompt_and_model() -> None:
    assert box.build_agent_args(make_config(), "be careful") == [
        "--append-system-prompt",
        "be careful",
        "--model",
        "claude-opus-5",
    ]


def test_build_agent_args_is_empty_without_settings(tmp_path: Path) -> None:
    assert box.build_agent_args(config_from_values({}, tmp_path), "") == []


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


def test_read_token_strips_a_windows_line_ending(tmp_path: Path) -> None:
    path = tmp_path / "token"
    path.write_text("abc123\r\n")
    assert box.read_token(path) == "abc123"


def test_read_token_strips_surrounding_spaces(tmp_path: Path) -> None:
    path = tmp_path / "token"
    path.write_text("  abc123  ")
    assert box.read_token(path) == "abc123"


def test_read_token_rejects_empty_file(tmp_path: Path) -> None:
    path = tmp_path / "token"
    path.write_text("")
    with pytest.raises(box.ConfigError, match="is empty"):
        box.read_token(path)


def test_read_token_rejects_a_whitespace_only_file(tmp_path: Path) -> None:
    path = tmp_path / "token"
    path.write_text("\n  \n")
    with pytest.raises(box.ConfigError, match="is empty"):
        box.read_token(path)


def test_read_token_rejects_a_missing_file(tmp_path: Path) -> None:
    with pytest.raises(box.ConfigError, match="does not exist"):
        box.read_token(tmp_path / "absent")


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


AUTHOR = ["-c", "user.name=box", "-c", "user.email=box@example.com"]


def git(directory: Path, arguments: list[str]) -> str:
    """Run a git command in a test repository, insisting it worked, and return what it printed."""
    command = ["git", "-C", str(directory), *AUTHOR, *arguments]
    return subprocess.run(command, capture_output=True, text=True, check=True).stdout.strip()


def git_init(directory: Path) -> Path:
    """Create a git repository with no commits in it."""
    subprocess.run(["git", "init", "-q", str(directory)], check=True)
    return directory


def make_git_repository(directory: Path) -> Path:
    """Create a git repository with the one commit box needs to have something to clone."""
    git_init(directory)
    git(directory, ["commit", "-q", "--allow-empty", "-m", "first"])
    return directory


def commit_file(directory: Path, name: str, subject: str) -> str:
    """Commit one new file and return the commit that holds it."""
    (directory / name).write_text(name)
    git(directory, ["add", name])
    git(directory, ["commit", "-q", "-m", subject])
    return git(directory, ["rev-parse", "HEAD"])


def repository_with_sandbox_work(directory: Path) -> str:
    """Build a repository whose HEAD lacks the two commits a sandbox ref points at."""
    make_git_repository(directory)
    base = git(directory, ["rev-parse", "HEAD"])
    commit_file(directory, "one.txt", "Add one")
    work = commit_file(directory, "two.txt", "Add two")
    git(directory, ["update-ref", f"{box.SANDBOX_REFS}/demo-1/main", work])
    git(directory, ["reset", "--hard", "-q", base])
    return work


def make_repository(directory: Path, gitignore: str) -> Path:
    """Create a git repository holding a config, a mounts file and the given .gitignore."""
    make_git_repository(directory)
    (directory / ".gitignore").write_text(gitignore)
    write_config(directory, {})
    write_box_file(directory, box.MOUNTS_FILE, {"cache": "/cache"})
    return directory


def test_count_new_commits_counts_what_this_checkout_lacks(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    work = repository_with_sandbox_work(tmp_path)
    monkeypatch.chdir(tmp_path)
    assert box.count_new_commits(work) == "2"


def test_count_new_commits_is_empty_when_git_cannot_read_the_commit(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    repository_with_sandbox_work(tmp_path)
    monkeypatch.chdir(tmp_path)
    assert box.count_new_commits("no-such-commit") == ""


def test_new_commit_subjects_reads_the_subjects_of_what_this_checkout_lacks(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    work = repository_with_sandbox_work(tmp_path)
    monkeypatch.chdir(tmp_path)
    assert box.new_commit_subjects(work) == "Add two\nAdd one\n"


def test_sandbox_refs_finds_what_the_fetch_left(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    work = repository_with_sandbox_work(tmp_path)
    monkeypatch.chdir(tmp_path)
    assert box.sandbox_refs("demo-1") == [
        box.SandboxRef(ref_name=f"{box.SANDBOX_REFS}/demo-1/main", commit=work)
    ]


def test_sandbox_refs_ignores_another_sandboxs_refs(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    repository_with_sandbox_work(tmp_path)
    monkeypatch.chdir(tmp_path)
    assert box.sandbox_refs("demo-2") == []


def test_create_branch_points_a_branch_at_the_sandboxs_commit(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    work = repository_with_sandbox_work(tmp_path)
    monkeypatch.chdir(tmp_path)
    assert box.create_branch("add-retry-logic", work)
    assert git(tmp_path, ["rev-parse", "add-retry-logic"]) == work


def test_create_branch_says_no_to_a_name_git_refuses(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    work = repository_with_sandbox_work(tmp_path)
    monkeypatch.chdir(tmp_path)
    assert not box.create_branch("a name with spaces", work)


def test_local_branch_names_reads_the_branches_this_repository_has(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    work = repository_with_sandbox_work(tmp_path)
    monkeypatch.chdir(tmp_path)
    box.create_branch("add-retry-logic", work)
    assert "add-retry-logic" in box.local_branch_names()


def test_delete_ref_drops_the_ref(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    repository_with_sandbox_work(tmp_path)
    monkeypatch.chdir(tmp_path)
    box.delete_ref(f"{box.SANDBOX_REFS}/demo-1/main")
    assert box.sandbox_refs("demo-1") == []


def test_settle_sandbox_refs_puts_a_real_sandboxs_work_on_a_real_branch(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    work = repository_with_sandbox_work(tmp_path)
    monkeypatch.chdir(tmp_path)
    monkeypatch.setattr(box, "suggest_branch_name", lambda subjects: "add-retry-logic")
    box.settle_sandbox_refs("demo-1")
    assert git(tmp_path, ["rev-parse", "add-retry-logic"]) == work
    assert box.sandbox_refs("demo-1") == []


def test_taken_names_asks_both_sbx_and_git(monkeypatch: pytest.MonkeyPatch) -> None:
    commands: list[list[str]] = []

    def capture(command: list[str]) -> str:
        commands.append(command)
        if command[0] == "sbx":
            return "demo-1\n"
        return f"{box.SANDBOX_REFS}/demo-2/main\n"

    monkeypatch.setattr(box, "capture", capture)
    assert box.taken_names() == {"demo-1", "demo-2"}
    assert commands == [
        ["sbx", "ls", "-q"],
        ["git", "for-each-ref", "--format=%(refname)", box.SANDBOX_REFS],
    ]


def test_drop_secret_removes_the_secret_for_one_sandbox(monkeypatch: pytest.MonkeyPatch) -> None:
    commands: list[list[str]] = []

    def capture(command: list[str]) -> str:
        commands.append(command)
        return ""

    monkeypatch.setattr(box, "capture", capture)
    box.drop_secret("demo-1")
    assert commands == [["sbx", "secret", "rm", "demo-1", "--host", box.SECRET_HOST, "-f"]]


def test_every_command_box_shells_out_to_is_required() -> None:
    assert set(box.REQUIRED_BINARIES) == {"sbx", "git", "claude"}


def test_missing_binaries_is_empty_when_path_has_every_one() -> None:
    assert box.missing_binaries(box.REQUIRED_BINARIES) == []


def test_missing_binaries_names_what_path_lacks() -> None:
    assert box.missing_binaries(("git", MISSING_BINARY)) == [MISSING_BINARY]


def test_require_binaries_accepts_a_machine_that_has_them_all() -> None:
    box.require_binaries()


def test_require_binaries_names_every_command_that_is_missing(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("PATH", str(tmp_path))
    with pytest.raises(box.ConfigError, match="sbx, git, claude") as refusal:
        box.require_binaries()
    assert "not on PATH" in str(refusal.value)


def make_diagnose_report(checks: list[dict[str, str]]) -> str:
    """Render diagnose JSON the way sbx prints it, holding the given checks."""
    return json.dumps({"version": "1.0", "checks": checks})


def make_version_check(status: str, message: str) -> dict[str, str]:
    """Build the one diagnose check that compares the CLI with its daemon."""
    return {"name": box.VERSION_MATCH_CHECK, "status": status, "message": message}


def install_diagnose(monkeypatch: pytest.MonkeyPatch, stdout: str) -> None:
    """Answer the diagnose invocation with canned output, and refuse any other command."""

    def run_quietly(command: list[str]) -> subprocess.CompletedProcess[str]:
        assert command == box.build_diagnose_command()
        return subprocess.CompletedProcess(args=command, returncode=0, stdout=stdout, stderr="")

    monkeypatch.setattr(box, "run_quietly", run_quietly)


def test_the_diagnose_command_asks_for_json() -> None:
    assert box.build_diagnose_command() == ["sbx", "diagnose", "-o", "json"]


def test_parse_version_mismatch_is_quiet_when_the_versions_agree() -> None:
    report = make_diagnose_report([make_version_check("pass", "v0.38.0")])
    assert box.parse_version_mismatch(report) == ""


def test_parse_version_mismatch_returns_the_failed_check_message() -> None:
    report = make_diagnose_report([make_version_check("fail", "client v0.38.0, daemon v0.37.0")])
    assert box.parse_version_mismatch(report) == "client v0.38.0, daemon v0.37.0"


def test_parse_version_mismatch_says_something_when_the_check_says_nothing() -> None:
    report = make_diagnose_report([make_version_check("fail", "")])
    assert box.parse_version_mismatch(report) == "sbx did not say which versions"


def test_parse_version_mismatch_is_quiet_when_the_check_is_absent() -> None:
    report = make_diagnose_report([{"name": "Daemon", "status": "fail", "message": "not running"}])
    assert box.parse_version_mismatch(report) == ""


def test_parse_version_mismatch_is_quiet_on_output_that_is_not_diagnose_json() -> None:
    assert box.parse_version_mismatch("") == ""
    assert box.parse_version_mismatch("not json") == ""
    assert box.parse_version_mismatch(json.dumps({"summary": {}})) == ""
    assert box.parse_version_mismatch(json.dumps({"checks": ["not a check"]})) == ""


def test_require_matching_versions_accepts_agreeing_versions(monkeypatch: pytest.MonkeyPatch) -> None:
    install_diagnose(monkeypatch, make_diagnose_report([make_version_check("pass", "v0.38.0")]))
    box.require_matching_versions()


def test_require_matching_versions_refuses_a_daemon_from_another_release(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    report = make_diagnose_report([make_version_check("fail", "client v0.38.0, daemon v0.37.0")])
    install_diagnose(monkeypatch, report)
    with pytest.raises(box.ConfigError, match="daemon v0.37.0") as refusal:
        box.require_matching_versions()
    assert "sbx daemon restart" in str(refusal.value)


def test_require_matching_versions_accepts_a_diagnose_that_answered_nothing(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    install_diagnose(monkeypatch, "")
    box.require_matching_versions()


def test_require_matching_versions_skips_a_machine_without_sbx(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("PATH", str(tmp_path))
    box.require_matching_versions()


def test_prepare_launch_refuses_a_machine_without_sbx(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    empty = tmp_path / "bin"
    empty.mkdir()
    monkeypatch.setenv("PATH", str(empty))
    with pytest.raises(box.ConfigError, match="not on PATH"):
        box.prepare_launch(make_config(), "/secrets/token", tmp_path)


def test_a_project_with_no_config_is_sent_to_gen(tmp_path: Path) -> None:
    with pytest.raises(box.ConfigError, match="Run box gen"):
        box.require_config_file(tmp_path)


def test_a_project_with_a_config_is_accepted(tmp_path: Path) -> None:
    write_config(tmp_path, {})
    box.require_config_file(tmp_path)


def test_the_missing_config_is_named_the_way_the_file_is(tmp_path: Path) -> None:
    with pytest.raises(box.ConfigError, match=re.escape(box.CONFIG_FILE)):
        box.require_config_file(tmp_path)


def test_a_directory_that_is_not_a_repository_is_rejected(tmp_path: Path) -> None:
    with pytest.raises(box.ConfigError, match="not a git repository"):
        box.require_git_repository(tmp_path)


def test_a_repository_with_no_commits_is_rejected(tmp_path: Path) -> None:
    with pytest.raises(box.ConfigError, match="no commits"):
        box.require_git_repository(git_init(tmp_path))


def test_a_repository_with_a_commit_is_accepted(tmp_path: Path) -> None:
    box.require_git_repository(make_git_repository(tmp_path))


def test_prepare_launch_resolves_a_name_a_token_and_the_agent_args(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    repository = make_repository(tmp_path, f"{box.MOUNTS_FILE}\n")
    token = tmp_path / "token"
    token.write_text("sk-ant-secret\n")
    monkeypatch.setattr(box, "taken_names", lambda: {"demo-1"})
    prompt = tmp_path / "agent.md"
    prompt.write_text("project rules")
    config = config_from_values(
        {"name": "demo", "kit": "registry/kit", "model": "claude-opus-5", "prompt_file": str(prompt)},
        tmp_path,
    )
    launch = box.prepare_launch(config, str(token), repository)
    assert launch.sandbox_name == "demo-2"
    assert launch.token == "sk-ant-secret"
    assert launch.agent_args[0] == "--append-system-prompt"
    assert launch.agent_args[1] == f"{box.BASE_PROMPT}\n\nproject rules"
    assert launch.agent_args[2:] == ["--model", "claude-opus-5"]


def test_prepare_launch_rejects_a_directory_that_is_not_a_repository(tmp_path: Path) -> None:
    write_config(tmp_path, {})
    with pytest.raises(box.ConfigError, match="not a git repository"):
        box.prepare_launch(make_config(), "/secrets/token", tmp_path)


def test_prepare_launch_rejects_a_repository_with_no_commits(tmp_path: Path) -> None:
    write_config(git_init(tmp_path), {})
    with pytest.raises(box.ConfigError, match="no commits"):
        box.prepare_launch(make_config(), "/secrets/token", tmp_path)


def test_a_gitignored_mounts_file_is_accepted(tmp_path: Path) -> None:
    box.require_ignored_local_paths(make_repository(tmp_path, f"{box.MOUNTS_FILE}\n"))


def test_an_ignored_box_directory_covers_the_mounts_file(tmp_path: Path) -> None:
    box.require_ignored_local_paths(make_repository(tmp_path, f"{box.BOX_DIR}/\n"))


def test_a_committable_mounts_file_is_rejected(tmp_path: Path) -> None:
    repository = make_repository(tmp_path, "*.log\n")
    with pytest.raises(box.ConfigError, match="not ignored by git"):
        box.require_ignored_local_paths(repository)


def test_the_deps_dir_lives_in_the_box_directory() -> None:
    assert Path(box.DEPS_DIR) == Path(box.BOX_DIR) / "deps"


def test_a_committable_deps_dir_is_rejected(tmp_path: Path) -> None:
    repository = make_repository(tmp_path, f"{box.MOUNTS_FILE}\n")
    (repository / box.DEPS_DIR).mkdir(parents=True)
    with pytest.raises(box.ConfigError, match="deps/ is not ignored by git"):
        box.require_ignored_local_paths(repository)


def test_an_ignored_deps_dir_is_accepted(tmp_path: Path) -> None:
    repository = make_repository(tmp_path, f"{box.MOUNTS_FILE}\n{box.DEPS_DIR}/\n")
    (repository / box.DEPS_DIR).mkdir(parents=True)
    box.require_ignored_local_paths(repository)


def test_an_absent_deps_dir_needs_no_gitignore_entry(tmp_path: Path) -> None:
    repository = make_repository(tmp_path, f"{box.MOUNTS_FILE}\n")
    assert not (repository / box.DEPS_DIR).exists()
    box.require_ignored_local_paths(repository)


def test_gen_creates_the_deps_dir(tmp_path: Path) -> None:
    box.generate(tmp_path)
    assert (tmp_path / box.DEPS_DIR).is_dir()


def test_gen_leaves_an_existing_deps_dir_alone(tmp_path: Path) -> None:
    kept = tmp_path / box.DEPS_DIR / "go"
    kept.mkdir(parents=True)
    box.generate(tmp_path)
    assert kept.is_dir()


def test_the_prompt_offers_the_deps_dir_when_nothing_here_fits() -> None:
    prompt = box.build_mount_prompt({"go": "the Go toolchain"}, ["go"], "darwin arm64")
    assert f"{box.DEPS_DIR}/" in prompt


def test_the_prompt_says_the_sandbox_runs_linux_whatever_this_machine_runs() -> None:
    prompt = box.build_mount_prompt({"go": "the Go toolchain"}, ["go"], "darwin arm64")
    assert "The sandbox runs Linux" in prompt


def test_no_mounts_file_needs_no_gitignore_entry(tmp_path: Path) -> None:
    subprocess.run(["git", "init", "-q", str(tmp_path)], check=True)
    box.require_ignored_local_paths(tmp_path)


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
    parser = box.build_parser()
    for_config = vars(parser.parse_args(["config"]))
    for_run = vars(parser.parse_args(["run"]))
    assert set(for_config) == set(for_run)


def test_config_takes_a_setting_flag() -> None:
    assert box.build_parser().parse_args(["config", "--memory", "8g"]).memory == "8g"


def test_config_is_not_a_setup_command() -> None:
    assert "config" not in box.SETUP_COMMANDS


def test_show_config_prints_the_settings_and_returns_zero(
    capsys: pytest.CaptureFixture[str], tmp_path: Path
) -> None:
    write_config(make_git_repository(tmp_path), {})
    assert box.show_config(make_config(), "/secrets/token", tmp_path) == 0
    printed = capsys.readouterr().out
    assert "claude-opus-5" in printed
    assert "/secrets/token" in printed


def test_show_config_makes_the_checks_a_run_would(tmp_path: Path) -> None:
    write_config(make_git_repository(tmp_path), {"model": "claude-opus-5"})
    config = config_from_values({"model": "claude-opus-5"}, tmp_path)
    with pytest.raises(box.ConfigError, match="kit is not set"):
        box.show_config(config, "/secrets/token", tmp_path)


def test_show_config_rejects_a_committable_mounts_file(tmp_path: Path) -> None:
    repository = make_repository(tmp_path, "")
    with pytest.raises(box.ConfigError, match="not ignored by git"):
        box.show_config(make_config(), "/secrets/token", repository)


def test_show_config_prints_the_settings_before_rejecting_the_project(
    tmp_path: Path, capsys: pytest.CaptureFixture[str]
) -> None:
    with pytest.raises(box.ConfigError):
        box.show_config(make_config(), "/secrets/token", tmp_path)
    assert "claude-opus-5" in capsys.readouterr().out


def test_cache_path_follows_xdg_cache_home(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("XDG_CACHE_HOME", "/tmp/xdg")
    assert box.cache_path() == Path("/tmp/xdg/box/update-check.json")


def test_cache_path_falls_back_to_dot_cache(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("XDG_CACHE_HOME", raising=False)
    monkeypatch.setenv("HOME", "/home/someone")
    assert box.cache_path() == Path("/home/someone/.cache/box/update-check.json")


def test_file_hash_matches_the_same_bytes(tmp_path: Path) -> None:
    first = tmp_path / "a"
    second = tmp_path / "b"
    first.write_text("same")
    second.write_text("same")
    assert box.file_hash(first) == box.file_hash(second)


def test_file_hash_differs_on_different_bytes(tmp_path: Path) -> None:
    first = tmp_path / "a"
    second = tmp_path / "b"
    first.write_text("one")
    second.write_text("two")
    assert box.file_hash(first) != box.file_hash(second)


def test_a_stored_check_time_is_fresh(tmp_path: Path) -> None:
    path = tmp_path / "update-check.json"
    box.store_check_time(path, 1000.0)
    assert box.checked_recently(path, 1000.0)


def test_a_stored_check_time_expires(tmp_path: Path) -> None:
    path = tmp_path / "update-check.json"
    box.store_check_time(path, 1000.0)
    assert not box.checked_recently(path, 1000.0 + box.UPDATE_INTERVAL_SECONDS + 1)


def test_a_missing_cache_reads_as_never_checked(tmp_path: Path) -> None:
    assert not box.checked_recently(tmp_path / "absent.json", 1000.0)


def test_a_broken_cache_reads_as_never_checked(tmp_path: Path) -> None:
    path = tmp_path / "update-check.json"
    path.write_text("{not json")
    assert not box.checked_recently(path, 1000.0)


def test_store_check_time_creates_the_cache_directory(tmp_path: Path) -> None:
    path = tmp_path / "nested" / "update-check.json"
    box.store_check_time(path, 1000.0)
    assert path.is_file()


def test_no_update_message_when_the_hashes_match(tmp_path: Path) -> None:
    script = tmp_path / "box.py"
    script.write_text("print()")
    assert box.update_message(script, box.file_hash(script)) == ""


def test_the_update_message_names_the_command_that_takes_it(tmp_path: Path) -> None:
    script = tmp_path / "box"
    script.write_text("print()")
    assert "box self-update" in box.update_message(script, "a-different-hash")


def test_the_update_message_says_when_the_script_cannot_be_written(tmp_path: Path) -> None:
    script = tmp_path / "box"
    script.write_text("print()")
    script.chmod(0o444)
    message = box.update_message(script, "a-different-hash")
    assert "not writable by you" in message
    assert str(script) in message
    assert "self-update" not in message


def test_a_cache_without_a_check_time_reads_as_never_checked(tmp_path: Path) -> None:
    path = tmp_path / "update-check.json"
    path.write_text(json.dumps({"checked": 1000.0}))
    assert not box.checked_recently(path, 1000.0)


def test_a_cache_holding_a_check_time_that_is_not_a_number_reads_as_never_checked(tmp_path: Path) -> None:
    path = tmp_path / "update-check.json"
    path.write_text(json.dumps({"checked_at": "yesterday"}))
    assert not box.checked_recently(path, 1000.0)


class FakeStderr:
    """Stand in for sys.stderr, which is what decides whether a notice is coloured."""

    def __init__(self, *, terminal: bool) -> None:
        self.terminal = terminal

    def isatty(self) -> bool:
        """Say whether this stream is a terminal, which is the one thing in_red asks it."""
        return self.terminal


def use_a_terminal(monkeypatch: pytest.MonkeyPatch, terminal: bool) -> None:
    """Point stderr at a terminal or at a pipe, with no NO_COLOR in the environment either way."""
    monkeypatch.delenv(box.NO_COLOUR_ENV, raising=False)
    monkeypatch.setattr(sys, "stderr", FakeStderr(terminal=terminal))


def test_a_notice_is_red_on_a_terminal(monkeypatch: pytest.MonkeyPatch) -> None:
    use_a_terminal(monkeypatch, terminal=True)
    assert box.in_red("an update") == f"{box.RED}an update{box.RESET}"


def test_a_notice_is_plain_text_when_stderr_is_not_a_terminal(monkeypatch: pytest.MonkeyPatch) -> None:
    use_a_terminal(monkeypatch, terminal=False)
    assert box.in_red("an update") == "an update"


def test_a_notice_is_plain_text_when_no_color_is_set(monkeypatch: pytest.MonkeyPatch) -> None:
    use_a_terminal(monkeypatch, terminal=True)
    monkeypatch.setenv(box.NO_COLOUR_ENV, "1")
    assert box.in_red("an update") == "an update"


def test_the_update_message_is_red_on_a_terminal(monkeypatch: pytest.MonkeyPatch, tmp_path: Path) -> None:
    script = tmp_path / "box.py"
    script.write_text("print()")
    use_a_terminal(monkeypatch, terminal=True)
    message = box.update_message(script, "a-different-hash")
    assert message.startswith(box.RED)
    assert message.endswith(box.RESET)


def test_the_update_message_carries_no_escape_codes_into_a_pipe(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    script = tmp_path / "box.py"
    script.write_text("print()")
    use_a_terminal(monkeypatch, terminal=False)
    assert box.RED not in box.update_message(script, "a-different-hash")


def use_an_installed_copy(monkeypatch: pytest.MonkeyPatch, cache: Path) -> None:
    """Make the update check see the copy it exists for: an installed one, with its own cache."""
    monkeypatch.setattr(box, "is_tracked_by_git", lambda script_path: False)
    monkeypatch.setenv("XDG_CACHE_HOME", str(cache))
    monkeypatch.delenv(box.UPDATE_URL_ENV, raising=False)


def test_the_update_url_is_box_own_published_copy(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv(box.UPDATE_URL_ENV, raising=False)
    assert box.update_url() == box.UPDATE_URL


def test_a_fork_can_point_the_update_check_at_its_own_copy(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv(box.UPDATE_URL_ENV, "https://example.com/box.py")
    assert box.update_url() == "https://example.com/box.py"


def test_an_empty_update_url_checks_nothing(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str], tmp_path: Path
) -> None:
    def explode(url: str) -> str:
        raise AssertionError("a copy with no update URL must not be compared with anything")

    use_an_installed_copy(monkeypatch, tmp_path)
    monkeypatch.setenv(box.UPDATE_URL_ENV, "")
    monkeypatch.setattr(box, "fetch_remote_hash", explode)
    box.warn_when_outdated()
    assert capsys.readouterr().err == ""


def test_warn_when_outdated_stays_silent_when_the_check_fails(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str], tmp_path: Path
) -> None:
    def explode(url: str) -> str:
        raise OSError("no network")

    use_an_installed_copy(monkeypatch, tmp_path)
    monkeypatch.setattr(box, "fetch_remote_hash", explode)
    box.warn_when_outdated()
    captured = capsys.readouterr()
    assert captured.out == ""
    assert captured.err == ""


def test_warn_when_outdated_survives_a_cache_it_cannot_write(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str], tmp_path: Path
) -> None:
    unwritable = tmp_path / "cache"
    unwritable.write_text("not a directory")
    use_an_installed_copy(monkeypatch, unwritable)
    monkeypatch.setattr(box, "fetch_remote_hash", lambda url: "a-different-hash")
    box.warn_when_outdated()
    assert capsys.readouterr().err == ""


def test_warn_when_outdated_records_a_check_that_failed(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    fetches: list[str] = []

    def explode(url: str) -> str:
        fetches.append("tried")
        raise OSError("no network")

    use_an_installed_copy(monkeypatch, tmp_path)
    monkeypatch.setattr(box, "fetch_remote_hash", explode)
    box.warn_when_outdated()
    box.warn_when_outdated()
    assert fetches == ["tried"]


def test_warn_when_outdated_says_nothing_about_a_checked_out_box(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str], tmp_path: Path
) -> None:
    def explode(url: str) -> str:
        raise AssertionError("a checked out box.py must not be compared with the published one")

    monkeypatch.setattr(box, "is_tracked_by_git", lambda script_path: True)
    monkeypatch.setenv("XDG_CACHE_HOME", str(tmp_path))
    monkeypatch.setattr(box, "fetch_remote_hash", explode)
    box.warn_when_outdated()
    assert capsys.readouterr().err == ""


def test_box_py_in_this_repository_is_tracked_by_git() -> None:
    assert box.is_tracked_by_git(Path(box.__file__).resolve())


def test_an_installed_copy_is_not_tracked_by_git(tmp_path: Path) -> None:
    script = tmp_path / "box.py"
    script.write_text("print()")
    assert not box.is_tracked_by_git(script)


def test_warn_when_outdated_warns_at_most_once_an_hour(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str], tmp_path: Path
) -> None:
    fetches = []

    def fetch(url: str) -> str:
        fetches.append("a-different-hash")
        return "a-different-hash"

    use_an_installed_copy(monkeypatch, tmp_path)
    monkeypatch.setattr(box, "fetch_remote_hash", fetch)
    box.warn_when_outdated()
    assert "box self-update" in capsys.readouterr().err
    box.warn_when_outdated()
    assert capsys.readouterr().err == ""
    assert fetches == ["a-different-hash"]


def an_installed_script(directory: Path, monkeypatch: pytest.MonkeyPatch) -> Path:
    """Write a box.py the way an install has one: executable, and tracked by no repository."""
    script = directory / "box"
    script.write_bytes(b"print('old')\n")
    script.chmod(0o755)
    monkeypatch.setattr(box, "is_tracked_by_git", lambda script_path: False)
    monkeypatch.delenv(box.UPDATE_URL_ENV, raising=False)
    return script


def serve(monkeypatch: pytest.MonkeyPatch, published: bytes) -> None:
    """Answer the one download self-update makes with the bytes the URL is meant to hold."""
    monkeypatch.setattr(box, "download", lambda url: published)


def test_self_update_is_a_command() -> None:
    assert box.build_parser().parse_args(["self-update"]).command == "self-update"


def test_self_update_takes_no_flags() -> None:
    arguments = box.build_parser().parse_args(["--memory", "8g", "self-update"])
    with pytest.raises(box.ConfigError, match="self-update takes no flags"):
        box.require_no_flags(arguments)


def test_self_update_writes_the_published_copy(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    script = an_installed_script(tmp_path, monkeypatch)
    serve(monkeypatch, b"print('new')\n")
    assert box.self_update(script) == 0
    assert script.read_bytes() == b"print('new')\n"


def test_self_update_keeps_the_script_executable(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    script = an_installed_script(tmp_path, monkeypatch)
    serve(monkeypatch, b"print('new')\n")
    box.self_update(script)
    assert os.access(script, os.X_OK)


def test_self_update_leaves_nothing_beside_the_script(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    script = an_installed_script(tmp_path, monkeypatch)
    serve(monkeypatch, b"print('new')\n")
    box.self_update(script)
    assert [path.name for path in tmp_path.iterdir()] == [script.name]


def test_self_update_says_when_there_is_nothing_to_take(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    script = an_installed_script(tmp_path, monkeypatch)
    serve(monkeypatch, script.read_bytes())
    assert box.self_update(script) == 0
    assert "already the published copy" in capsys.readouterr().out


def test_self_update_reports_a_url_it_cannot_read(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    def explode(url: str) -> bytes:
        raise OSError("no network")

    script = an_installed_script(tmp_path, monkeypatch)
    monkeypatch.setattr(box, "download", explode)
    with pytest.raises(box.ConfigError, match="could not read"):
        box.self_update(script)
    assert script.read_bytes() == b"print('old')\n"


def test_self_update_rejects_an_empty_download(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    script = an_installed_script(tmp_path, monkeypatch)
    serve(monkeypatch, b"")
    with pytest.raises(box.ConfigError, match="no copy of box"):
        box.self_update(script)
    assert script.read_bytes() == b"print('old')\n"


def test_self_update_reports_a_script_it_cannot_write(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    directory = tmp_path / "bin"
    directory.mkdir()
    script = an_installed_script(directory, monkeypatch)
    serve(monkeypatch, b"print('new')\n")
    directory.chmod(0o555)
    try:
        with pytest.raises(box.ConfigError, match="could not write"):
            box.self_update(script)
    finally:
        directory.chmod(0o755)


def test_self_update_refuses_a_checkout(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    script = an_installed_script(tmp_path, monkeypatch)
    monkeypatch.setattr(box, "is_tracked_by_git", lambda script_path: True)
    serve(monkeypatch, b"print('new')\n")
    with pytest.raises(box.ConfigError, match="checkout rather than an install"):
        box.self_update(script)


def test_self_update_has_nowhere_to_update_from_without_a_url(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    script = an_installed_script(tmp_path, monkeypatch)
    monkeypatch.setenv(box.UPDATE_URL_ENV, "")
    with pytest.raises(box.ConfigError, match="nowhere to update from"):
        box.self_update(script)


def test_setup_command_updates_the_box_that_is_running(monkeypatch: pytest.MonkeyPatch) -> None:
    updated: list[Path] = []

    def self_update(script_path: Path) -> int:
        updated.append(script_path)
        return 0

    monkeypatch.setattr(box, "self_update", self_update)
    assert box.setup_command("self-update", Path("/tmp/demo")) == 0
    assert updated == [Path(box.__file__).resolve()]


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


def test_gen_rejects_a_mount_flag() -> None:
    arguments = box.build_parser().parse_args(["--mount", "/cache", "gen"])
    with pytest.raises(box.ConfigError, match=r"got --mount;"):
        box.require_no_flags(arguments)


def test_to_flag_names_the_repeatable_flag_the_way_the_user_typed_it() -> None:
    assert box.to_flag(box.MOUNT_DEST) == box.MOUNT_FLAG


def test_to_flag_hyphenates_a_config_key() -> None:
    assert box.to_flag("root_size") == "--root-size"


def test_gen_names_every_flag_it_was_given() -> None:
    arguments = box.build_parser().parse_args(["--memory", "8g", "--cpus", "8", "gen"])
    with pytest.raises(box.ConfigError, match="--cpus, --memory"):
        box.require_no_flags(arguments)


def test_gen_writes_both_files(tmp_path: Path) -> None:
    box.generate(tmp_path)
    assert json.loads((tmp_path / box.CONFIG_FILE).read_text()) == box.STARTER_CONFIG
    assert json.loads((tmp_path / box.MOUNTS_FILE).read_text()) == {}


def test_gen_writes_a_config_box_can_read_back(tmp_path: Path) -> None:
    box.generate(tmp_path)
    assert box.read_config_file(tmp_path / box.CONFIG_FILE) == box.STARTER_CONFIG


def test_the_starter_config_is_every_default_but_the_kit_gen_writes() -> None:
    assert box.STARTER_CONFIG == {**box.DEFAULTS, "kit": box.KIT_DIR}


def test_gen_writes_a_starter_kit(tmp_path: Path) -> None:
    box.generate(tmp_path)
    spec = (tmp_path / box.KIT_SPEC_FILE).read_text()
    assert "api.anthropic.com:443" in spec
    assert spec.endswith("\n")


def test_the_starter_kit_is_named_after_the_project() -> None:
    assert "name: my-repo-network-policy" in box.build_kit_spec("my-repo")


def test_the_starter_kit_uses_the_v2_block_names() -> None:
    spec = box.build_kit_spec("demo")
    # sbx v0.38.0 renamed caps to permissions, and its strict loader rejects the old name.
    assert "permissions:" in spec
    assert "caps:" not in spec


def test_the_starter_kit_says_it_is_a_starting_point() -> None:
    assert "starting point" in box.build_kit_spec("demo")


def test_gen_points_the_kit_setting_at_the_kit_it_writes(tmp_path: Path) -> None:
    box.generate(tmp_path)
    config = config_from_values(box.read_config_file(tmp_path / box.CONFIG_FILE), tmp_path)
    assert config.kit == box.KIT_DIR
    assert (tmp_path / config.kit).is_dir()


def test_gen_keeps_a_kit_the_project_already_has(tmp_path: Path) -> None:
    spec = tmp_path / box.KIT_SPEC_FILE
    spec.parent.mkdir(parents=True)
    spec.write_text("kind: mixin\n")
    box.generate(tmp_path)
    assert spec.read_text() == "kind: mixin\n"


def test_gen_says_what_it_wrote_and_what_it_kept(tmp_path: Path, capsys: pytest.CaptureFixture[str]) -> None:
    box.generate(tmp_path)
    box.generate(tmp_path)
    assert f"kept    {box.KIT_SPEC_FILE}" in capsys.readouterr().out


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
    prompt = box.build_mount_prompt(required, ["go", "cargo"], "darwin arm64")
    assert "go: the Go toolchain" in prompt
    assert "cargo: the cargo home" in prompt


def test_the_prompt_names_the_file_the_placeholder_and_the_host() -> None:
    prompt = box.build_mount_prompt({"go": "the Go toolchain"}, ["go"], "darwin arm64")
    assert box.MOUNTS_FILE in prompt
    assert box.MOUNT_PLACEHOLDER in prompt
    assert "darwin arm64" in prompt


def test_the_prompt_holds_the_rules_box_enforces() -> None:
    prompt = box.build_mount_prompt({"go": "the Go toolchain"}, ["go"], "linux x86_64")
    assert ":rw" in prompt
    assert "Never guess" in prompt


def test_the_prompt_covers_a_key_that_is_not_in_the_file_yet() -> None:
    prompt = box.build_mount_prompt({"go": "the Go toolchain"}, ["go"], "linux x86_64")
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
    box.require_ignored_local_paths(tmp_path)


def test_gen_creates_a_gitignore_holding_every_local_path(tmp_path: Path) -> None:
    subprocess.run(["git", "init", "-q", str(tmp_path)], check=True)
    box.generate(tmp_path)
    written = (tmp_path / box.GITIGNORE_FILE).read_text()
    assert written == f"{box.MOUNTS_FILE}\n{box.DEPS_DIR}/\n"


def test_gen_writes_no_gitignore_outside_a_repository(tmp_path: Path) -> None:
    box.generate(tmp_path)
    box.generate(tmp_path)
    assert not (tmp_path / box.GITIGNORE_FILE).exists()


def test_gen_says_it_skipped_the_gitignore_outside_a_repository(
    tmp_path: Path, capsys: pytest.CaptureFixture[str]
) -> None:
    box.generate(tmp_path)
    assert "not a git repository" in capsys.readouterr().out


def test_gen_keeps_what_the_gitignore_already_held(tmp_path: Path) -> None:
    subprocess.run(["git", "init", "-q", str(tmp_path)], check=True)
    (tmp_path / box.GITIGNORE_FILE).write_text("*.log\n")
    box.generate(tmp_path)
    written = (tmp_path / box.GITIGNORE_FILE).read_text()
    assert written == f"*.log\n{box.MOUNTS_FILE}\n{box.DEPS_DIR}/\n"


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


def test_prepare_launch_requires_the_token_environment_variable(tmp_path: Path) -> None:
    write_config(make_git_repository(tmp_path), {})
    with pytest.raises(box.ConfigError, match="CLAUDE_OAUTH_TOKEN_FILE is not set"):
        box.prepare_launch(make_config(), "", tmp_path)


def test_prepare_launch_requires_a_kit(tmp_path: Path) -> None:
    write_config(tmp_path, {})
    config = config_from_values({}, tmp_path)
    with pytest.raises(box.ConfigError, match="kit is not set"):
        box.prepare_launch(config, "/secrets/token", tmp_path)


def test_prepare_launch_requires_a_model(tmp_path: Path) -> None:
    write_config(tmp_path, {"kit": "registry/kit"})
    config = config_from_values({"kit": "registry/kit"}, tmp_path)
    with pytest.raises(box.ConfigError, match="model is not set"):
        box.prepare_launch(config, "/secrets/token", tmp_path)


def test_a_kit_naming_a_file_is_rejected(tmp_path: Path) -> None:
    spec = tmp_path / "spec.yaml"
    spec.write_text("kit: {}\n")
    config = config_from_values({"kit": str(spec), "model": "claude-opus-5"}, tmp_path)
    with pytest.raises(box.ConfigError, match="kit names a file"):
        box.require_settings(config)


def test_a_kit_naming_a_directory_is_accepted(tmp_path: Path) -> None:
    kit = tmp_path / "kit"
    kit.mkdir()
    (kit / "spec.yaml").write_text("kit: {}\n")
    config = config_from_values({"kit": str(kit), "model": "claude-opus-5"}, tmp_path)
    box.require_settings(config)


def test_a_kit_that_is_not_on_disk_is_left_to_sbx(tmp_path: Path) -> None:
    config = config_from_values({"kit": "some/registry/ref", "model": "claude-opus-5"}, tmp_path)
    box.require_settings(config)


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
    assert all(re.fullmatch(r"[a-z][a-z0-9_]*", key) for key in box.DEFAULTS)


def test_config_keys_match_the_config_fields() -> None:
    config = config_from_values({}, Path("/tmp/demo"))
    settings = set(box.DEFAULTS) - {"required_mounts"}
    assert set(vars(config)) == settings | {"mounts"}


def test_flags_use_the_config_keys_with_hyphens() -> None:
    arguments = box.build_parser().parse_args(["run", "--root-size", "20g", "--prompt-file", "p.md"])
    assert vars(arguments)["root_size"] == "20g"
    assert vars(arguments)["prompt_file"] == "p.md"


def test_plural_keeps_one_singular() -> None:
    assert box.plural("1", "commit") == "1 commit"


def test_plural_makes_every_other_count_plural() -> None:
    assert box.plural("2", "commit") == "2 commits"
    assert box.plural("0", "commit") == "0 commits"


def test_resolve_mounts_takes_the_extra_mounts_as_a_list(tmp_path: Path) -> None:
    write_box_file(tmp_path, box.MOUNTS_FILE, {"cache": "/cache"})
    required = {"cache": "the build cache"}
    assert box.resolve_mounts(["/other"], tmp_path, required) == ["/cache", "/other"]


def test_resolve_mounts_needs_no_extra_mounts(tmp_path: Path) -> None:
    assert box.resolve_mounts([], tmp_path, {}) == []


def test_read_required_mounts_reads_the_declaration(tmp_path: Path) -> None:
    write_config(tmp_path, {"required_mounts": {"go": "the Go toolchain"}})
    assert box.read_required_mounts(tmp_path) == {"go": "the Go toolchain"}


def test_read_required_mounts_is_empty_without_a_config(tmp_path: Path) -> None:
    assert box.read_required_mounts(tmp_path) == {}


def test_run_box_turns_a_ctrl_c_into_an_exit_code(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    def interrupt() -> int:
        raise KeyboardInterrupt

    monkeypatch.setattr(box, "main", interrupt)
    assert box.run_box() == 130
    assert "interrupted" in capsys.readouterr().err


def test_run_box_passes_the_exit_code_through(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(box, "main", lambda: 3)
    assert box.run_box() == 3


def test_the_python_running_the_tests_is_supported() -> None:
    assert box.unsupported_python(sys.version_info[:2]) == ""


def test_the_oldest_supported_python_is_supported() -> None:
    assert box.unsupported_python(box.PYTHON_MINIMUM) == ""


def test_an_older_python_names_both_versions() -> None:
    assert box.unsupported_python((3, 9)) == "box needs Python 3.11 or newer, but this python3 is 3.9."


def test_run_box_refuses_an_older_python_before_doing_anything(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    def refuse_to_run() -> int:
        raise AssertionError("box must not run on a Python it does not support")

    monkeypatch.setattr(sys, "version_info", (3, 9, 6, "final", 0))
    monkeypatch.setattr(box, "main", refuse_to_run)
    assert box.run_box() == 1
    assert "box: box needs Python 3.11 or newer" in capsys.readouterr().err


def test_format_value_joins_mounts() -> None:
    assert box.format_value(("/a:ro", "/b")) == "/a:ro /b"


def test_format_value_names_a_setting_nothing_was_given_for() -> None:
    assert box.format_value("") == box.UNSET


def test_format_value_names_an_empty_mount_list() -> None:
    assert box.format_value(()) == box.UNSET


def test_format_config_leaves_no_line_ending_in_whitespace(tmp_path: Path) -> None:
    rendered = box.format_config(config_from_values({}, tmp_path), "")
    assert box.UNSET in rendered
    assert [line for line in rendered.splitlines() if line != line.rstrip()] == []


def test_format_config_shows_the_token_path_with_the_settings() -> None:
    rendered = box.format_config(make_config(), "/secrets/token")
    assert box.TOKEN_FILE_ENV in rendered
    assert "/secrets/token" in rendered
    assert "8g" in rendered


def test_format_config_aligns_every_value_in_one_column() -> None:
    lines = box.format_config(make_config(), "/secrets/token").splitlines()[1:]
    columns = {len(line) - len(line.lstrip().split(" ", 1)[-1].lstrip()) for line in lines}
    assert len(columns) == 1


def test_resolve_path_expands_a_leading_tilde(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("HOME", "/home/someone")
    assert box.resolve_path("~/.cargo") == Path("/home/someone/.cargo")


def test_resolve_path_leaves_an_absolute_path_alone() -> None:
    assert box.resolve_path("/usr/local/go") == Path("/usr/local/go")


def test_build_environment_keeps_the_environment_box_was_run_with(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("BOX_TEST_MARKER", "kept")
    assert box.build_environment(make_config())["BOX_TEST_MARKER"] == "kept"


def test_warn_dirty_says_how_to_inspect_recover_and_remove(capsys: pytest.CaptureFixture[str]) -> None:
    box.warn_dirty("demo-1", " M box.py\n")
    printed = capsys.readouterr().err
    assert " M box.py" in printed
    assert f"Inspect:  sbx exec demo-1 git -C {Path.cwd()} diff" in printed
    assert f"Recover:  sbx cp demo-1:{Path.cwd()}/<file> ." in printed
    assert "sbx rm --force demo-1" in printed


def test_is_git_ignored_reads_the_gitignore(tmp_path: Path) -> None:
    make_repository(tmp_path, f"{box.MOUNTS_FILE}\n")
    assert box.is_git_ignored(tmp_path, box.MOUNTS_FILE)


def test_is_git_ignored_says_no_to_a_path_the_gitignore_misses(tmp_path: Path) -> None:
    make_repository(tmp_path, "*.log\n")
    assert not box.is_git_ignored(tmp_path, box.MOUNTS_FILE)


def test_setup_command_runs_gen(tmp_path: Path) -> None:
    assert box.setup_command("gen", tmp_path) == 0
    assert (tmp_path / box.CONFIG_FILE).is_file()


def test_setup_command_runs_mount_prompt(tmp_path: Path, capsys: pytest.CaptureFixture[str]) -> None:
    write_config(tmp_path, {"required_mounts": {"go": "the Go toolchain"}})
    assert box.setup_command("mount-prompt", tmp_path) == 0
    assert "go: the Go toolchain" in capsys.readouterr().out
    assert not (tmp_path / box.MOUNTS_FILE).exists()


def test_mount_prompt_names_the_platform_and_architecture_this_machine_runs(
    tmp_path: Path, capsys: pytest.CaptureFixture[str]
) -> None:
    write_config(tmp_path, {"required_mounts": {"go": "the Go toolchain"}})
    box.mount_prompt(tmp_path)
    assert box.host_description() in capsys.readouterr().out


def test_the_host_description_holds_the_platform_and_the_architecture() -> None:
    assert box.host_description() == f"{sys.platform} {platform.machine()}"


class FakeDownload:
    """Stand in for the response urlopen hands back, holding the published box.py's bytes."""

    def __init__(self, published: bytes) -> None:
        self.published = published

    def __enter__(self) -> FakeDownload:
        """Enter the with block fetch_remote_hash reads the response in."""
        return self

    def __exit__(self, *details: object) -> None:
        """Leave that block, letting any error through."""

    def read(self) -> bytes:
        """Answer with the published bytes."""
        return self.published


def test_fetch_remote_hash_hashes_what_the_update_url_serves(monkeypatch: pytest.MonkeyPatch) -> None:
    seen: dict[str, object] = {}

    def urlopen(url: str, timeout: float) -> FakeDownload:
        seen.update(url=url, timeout=timeout)
        return FakeDownload(b"print()")

    monkeypatch.setattr(urllib.request, "urlopen", urlopen)
    assert box.fetch_remote_hash(box.UPDATE_URL) == hashlib.sha256(b"print()").hexdigest()
    assert seen == {"url": box.UPDATE_URL, "timeout": box.UPDATE_TIMEOUT_SECONDS}


class FakeSession:
    """Record the order run_session does things in, and how far a failing step lets it get."""

    def __init__(self, *, create_fails: bool, agent_code: int) -> None:
        self.create_fails = create_fails
        self.agent_code = agent_code
        self.steps: list[str] = []
        self.commands: list[list[str]] = []

    def install(self, monkeypatch: pytest.MonkeyPatch) -> None:
        """Stand in for the secret, the cleanup and every sbx call run_session makes."""

        def drop_secret(sandbox_name: str) -> None:
            self.steps.append("drop-secret")

        def store_secret(sandbox_name: str, token: str) -> None:
            self.steps.append("store-secret")

        def cleanup(sandbox_name: str) -> None:
            self.steps.append("cleanup")

        monkeypatch.setattr(box, "drop_secret", drop_secret)
        monkeypatch.setattr(box, "store_secret", store_secret)
        monkeypatch.setattr(box, "cleanup", cleanup)
        monkeypatch.setattr(subprocess, "run", self.run)

    def run(self, command: list[str], **keywords: object) -> subprocess.CompletedProcess[str]:
        """Answer an sbx call, failing loudly when anything else was run instead."""
        assert command[0] == "sbx"
        self.steps.append(" ".join(command[:2]))
        self.commands.append(command)
        if command[1] == "create" and self.create_fails:
            return subprocess.CompletedProcess(args=command, returncode=1)
        if command[1] == "create":
            return subprocess.CompletedProcess(args=command, returncode=0)
        return subprocess.CompletedProcess(args=command, returncode=self.agent_code)


def make_launch() -> box.Launch:
    """Build what prepare_launch would have resolved for a run."""
    return box.Launch(sandbox_name="demo-1", token="sk-ant-secret", agent_args=["--model", "claude-opus-5"])


def test_run_session_stores_the_secret_before_creating_the_sandbox(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    session = FakeSession(create_fails=False, agent_code=0)
    session.install(monkeypatch)
    box.run_session(make_config(), make_launch())
    assert session.steps.index("store-secret") < session.steps.index("sbx create")


def test_run_session_creates_runs_and_cleans_up_in_that_order(monkeypatch: pytest.MonkeyPatch) -> None:
    session = FakeSession(create_fails=False, agent_code=0)
    session.install(monkeypatch)
    assert box.run_session(make_config(), make_launch()) == 0
    assert session.steps == ["drop-secret", "store-secret", "sbx create", "sbx run", "cleanup"]


def test_run_session_returns_the_agents_own_exit_code(monkeypatch: pytest.MonkeyPatch) -> None:
    session = FakeSession(create_fails=False, agent_code=3)
    session.install(monkeypatch)
    assert box.run_session(make_config(), make_launch()) == 3


def test_run_session_cleans_up_after_an_agent_that_failed(monkeypatch: pytest.MonkeyPatch) -> None:
    session = FakeSession(create_fails=False, agent_code=1)
    session.install(monkeypatch)
    box.run_session(make_config(), make_launch())
    assert session.steps[-1] == "cleanup"


def test_run_session_reports_a_failed_create_and_starts_nothing(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    session = FakeSession(create_fails=True, agent_code=0)
    session.install(monkeypatch)
    assert box.run_session(make_config(), make_launch()) == 1
    assert session.steps == ["drop-secret", "store-secret", "sbx create", "drop-secret"]
    assert "never started" in capsys.readouterr().err


def make_runnable_project(directory: Path, monkeypatch: pytest.MonkeyPatch) -> Path:
    """Build a project that passes every check box makes before it starts a sandbox."""
    make_git_repository(directory)
    (directory / box.GITIGNORE_FILE).write_text(f"{box.MOUNTS_FILE}\n{box.DEPS_DIR}/\n")
    write_config(directory, {"kit": "registry/kit", "model": "claude-opus-5"})
    token = directory / "token"
    token.write_text("sk-ant-secret\n")
    monkeypatch.setenv(box.TOKEN_FILE_ENV, str(token))
    return directory


def call_main(monkeypatch: pytest.MonkeyPatch, directory: Path, argument_list: list[str]) -> int:
    """Run main the way the command line would, with the update and version checks kept local."""
    monkeypatch.setattr(sys, "argv", ["box", *argument_list])
    monkeypatch.setattr(box, "warn_when_outdated", lambda: None)
    monkeypatch.setattr(box, "require_matching_versions", lambda: None)
    monkeypatch.chdir(directory)
    return box.main()


def refuse_to_run(config: box.Config, launch: box.Launch) -> int:
    """Stand in for run_session where reaching it at all is the failure."""
    raise AssertionError("no sandbox may be created here")


def test_main_writes_a_starter_project_with_gen(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    assert call_main(monkeypatch, tmp_path, ["gen"]) == 0
    assert (tmp_path / box.CONFIG_FILE).is_file()


def test_main_prints_the_config_without_creating_anything(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    make_runnable_project(tmp_path, monkeypatch)
    monkeypatch.setattr(box, "run_session", refuse_to_run)
    assert call_main(monkeypatch, tmp_path, ["config"]) == 0
    assert "claude-opus-5" in capsys.readouterr().out


def test_main_turns_a_rejected_project_into_one_line(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    write_config(tmp_path, {})
    monkeypatch.setattr(box, "run_session", refuse_to_run)
    assert call_main(monkeypatch, tmp_path, ["run"]) == 1
    printed = capsys.readouterr().err
    assert printed.startswith("box: kit is not set")
    assert "Traceback" not in printed


def test_main_sends_a_project_with_no_box_directory_to_gen(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    monkeypatch.setattr(box, "run_session", refuse_to_run)
    assert call_main(monkeypatch, tmp_path, ["run"]) == 1
    assert "Run box gen" in capsys.readouterr().err


def test_main_rejects_a_flag_on_a_setup_command(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    assert call_main(monkeypatch, tmp_path, ["--memory", "8g", "gen"]) == 1
    assert "gen takes no flags" in capsys.readouterr().err
    assert not (tmp_path / box.BOX_DIR).exists()


def test_main_hands_a_ready_project_to_run_session(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    launched: list[box.Launch] = []

    def run_session(config: box.Config, launch: box.Launch) -> int:
        launched.append(launch)
        return 7

    make_runnable_project(tmp_path, monkeypatch)
    monkeypatch.setattr(box, "taken_names", set)
    monkeypatch.setattr(box, "run_session", run_session)
    assert call_main(monkeypatch, tmp_path, ["run"]) == 7
    assert launched[0].token == "sk-ant-secret"
    assert launched[0].agent_args[-2:] == ["--model", "claude-opus-5"]


def test_main_checks_the_sbx_versions_before_any_command(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    def mismatch() -> None:
        raise box.ConfigError("the sbx client and its daemon are different versions")

    monkeypatch.setattr(sys, "argv", ["box", "gen"])
    monkeypatch.setattr(box, "warn_when_outdated", lambda: None)
    monkeypatch.setattr(box, "require_matching_versions", mismatch)
    monkeypatch.chdir(tmp_path)
    assert box.main() == 1
    assert "different versions" in capsys.readouterr().err
    # gen is the command that needs no settings at all, so it shows the check guards every command.
    assert not (tmp_path / box.BOX_DIR).exists()


def test_main_checks_for_an_update_even_when_the_command_fails(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    checks: list[str] = []

    monkeypatch.setattr(sys, "argv", ["box", "run"])
    monkeypatch.setattr(box, "warn_when_outdated", lambda: checks.append("checked"))
    monkeypatch.setattr(box, "run_session", refuse_to_run)
    monkeypatch.chdir(tmp_path)
    assert box.main() == 1
    assert checks == ["checked"]
