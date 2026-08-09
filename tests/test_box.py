"""Tests for the pure configuration and command-building helpers in box.py."""

from __future__ import annotations

import json
import subprocess
import sys
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


def test_read_config_file_rejects_a_null_setting(tmp_path: Path) -> None:
    path = write_config(tmp_path, {"model": None})
    with pytest.raises(box.ConfigError, match="gives model null, and it must be a string"):
        box.read_config_file(path)


def test_read_config_file_rejects_a_setting_that_is_a_list(tmp_path: Path) -> None:
    path = write_config(tmp_path, {"memory": [1, 2]})
    with pytest.raises(box.ConfigError, match="gives memory a list, and it must be a string"):
        box.read_config_file(path)


def test_read_config_file_rejects_a_numeric_setting(tmp_path: Path) -> None:
    path = write_config(tmp_path, {"cpus": 4})
    with pytest.raises(box.ConfigError, match="gives cpus a number, and it must be a string"):
        box.read_config_file(path)


def test_read_config_file_keeps_required_mounts_an_object(tmp_path: Path) -> None:
    declared = {"go": "the Go toolchain"}
    path = write_config(tmp_path, {"required_mounts": declared})
    assert box.read_config_file(path) == {"required_mounts": declared}


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


def test_build_config_rejects_a_setting_that_is_not_a_string() -> None:
    with pytest.raises(box.ConfigError, match="cpus is a number, and it must be a string"):
        build_config({"cpus": 6}, Path("/tmp/demo"))


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
    with pytest.raises(box.ConfigError, match="gives cache null, and it must be a string"):
        box.read_mounts_file(path)


def test_as_descriptions_rejects_a_description_that_is_not_a_string() -> None:
    with pytest.raises(box.ConfigError, match="gives go a number, and it must be a string"):
        box.as_descriptions({"go": 1})


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

    def __init__(self, count: str, suggestion: str, branches: set[str]) -> None:
        self.count = count
        self.suggestion = suggestion
        self.branches = branches
        self.refuse_branch = False
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
    repository = FakeRepository("3", "add-retry-logic", {"main"})
    repository.install(monkeypatch)
    box.settle_ref(SANDBOX_REF)
    assert repository.created == [("add-retry-logic", "abc123")]
    assert repository.deleted == [SANDBOX_REF.ref_name]


def test_settle_ref_names_the_branch_after_the_commits(monkeypatch: pytest.MonkeyPatch) -> None:
    repository = FakeRepository("3", "add-retry-logic", set())
    repository.install(monkeypatch)
    box.settle_ref(SANDBOX_REF)
    assert repository.named_after == "Add retry logic\n"


def test_settle_ref_numbers_a_branch_the_repository_already_has(monkeypatch: pytest.MonkeyPatch) -> None:
    repository = FakeRepository("1", "add-retry-logic", {"add-retry-logic"})
    repository.install(monkeypatch)
    box.settle_ref(SANDBOX_REF)
    assert repository.created == [("add-retry-logic-2", "abc123")]


def test_settle_ref_says_where_the_work_ended_up(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    repository = FakeRepository("3", "add-retry-logic", set())
    repository.install(monkeypatch)
    box.settle_ref(SANDBOX_REF)
    printed = capsys.readouterr().err
    assert "branch add-retry-logic holds 3 commits" in printed
    assert SANDBOX_REF.ref_name in printed


def test_settle_ref_counts_a_single_commit_in_the_singular(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    repository = FakeRepository("1", "add-retry-logic", set())
    repository.install(monkeypatch)
    box.settle_ref(SANDBOX_REF)
    assert "holds 1 commit from" in capsys.readouterr().err


def test_settle_ref_drops_a_ref_holding_no_commits(monkeypatch: pytest.MonkeyPatch) -> None:
    repository = FakeRepository("0", "add-retry-logic", set())
    repository.install(monkeypatch)
    box.settle_ref(SANDBOX_REF)
    assert repository.created == []
    assert repository.deleted == [SANDBOX_REF.ref_name]


def test_settle_ref_keeps_the_ref_when_naming_fails(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    repository = FakeRepository("2", "", set())
    repository.install(monkeypatch)
    box.settle_ref(SANDBOX_REF)
    assert repository.created == []
    assert repository.deleted == []
    assert SANDBOX_REF.ref_name in capsys.readouterr().err


def test_settle_ref_keeps_the_ref_when_git_refuses_the_branch(monkeypatch: pytest.MonkeyPatch) -> None:
    repository = FakeRepository("2", "add-retry-logic", set())
    repository.refuse_branch = True
    repository.install(monkeypatch)
    box.settle_ref(SANDBOX_REF)
    assert repository.deleted == []


def test_settle_ref_keeps_the_ref_when_git_cannot_count_the_commits(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    repository = FakeRepository("", "add-retry-logic", set())
    repository.install(monkeypatch)
    box.settle_ref(SANDBOX_REF)
    assert repository.created == []
    assert repository.deleted == []


def completed(returncode: int, stdout: str) -> subprocess.CompletedProcess[str]:
    """Build the result a finished claude run would hand back."""
    return subprocess.CompletedProcess(args=["claude"], returncode=returncode, stdout=stdout, stderr="")


def test_suggest_branch_name_kebab_cases_what_claude_printed(monkeypatch: pytest.MonkeyPatch) -> None:
    def run(command: list[str], **keywords: object) -> subprocess.CompletedProcess[str]:
        return completed(0, "Add Retry Logic\n")

    monkeypatch.setattr(subprocess, "run", run)
    assert box.suggest_branch_name("Add retry logic") == "add-retry-logic"


def test_suggest_branch_name_gives_claude_one_turn_and_no_more(monkeypatch: pytest.MonkeyPatch) -> None:
    seen: dict[str, object] = {}

    def run(command: list[str], **keywords: object) -> subprocess.CompletedProcess[str]:
        seen.update(keywords)
        return completed(0, "add-retry-logic\n")

    monkeypatch.setattr(subprocess, "run", run)
    box.suggest_branch_name("Add retry logic")
    assert seen["timeout"] == box.BRANCH_NAME_TIMEOUT_SECONDS


def test_suggest_branch_name_is_empty_when_claude_times_out(monkeypatch: pytest.MonkeyPatch) -> None:
    def run(command: list[str], **keywords: object) -> subprocess.CompletedProcess[str]:
        raise subprocess.TimeoutExpired(cmd="claude", timeout=box.BRANCH_NAME_TIMEOUT_SECONDS)

    monkeypatch.setattr(subprocess, "run", run)
    assert box.suggest_branch_name("Add retry logic") == ""


def test_suggest_branch_name_is_empty_when_claude_fails(monkeypatch: pytest.MonkeyPatch) -> None:
    def run(command: list[str], **keywords: object) -> subprocess.CompletedProcess[str]:
        return completed(1, "")

    monkeypatch.setattr(subprocess, "run", run)
    assert box.suggest_branch_name("Add retry logic") == ""


def test_suggest_branch_name_is_empty_when_claude_is_not_installed(monkeypatch: pytest.MonkeyPatch) -> None:
    def run(command: list[str], **keywords: object) -> subprocess.CompletedProcess[str]:
        raise FileNotFoundError("claude")

    monkeypatch.setattr(subprocess, "run", run)
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
        ".sbx/kit",
    ]


def test_build_create_command_omits_empty_kit() -> None:
    command = box.build_create_command(make_config(), "demo-1")
    assert "--kit" in command
    without_kit = box.build_create_command(build_config({}, Path("/tmp/demo")), "demo-1")
    assert "--kit" not in without_kit


def test_build_agent_args_includes_prompt_and_model() -> None:
    assert box.build_agent_args(make_config(), "be careful") == [
        "--append-system-prompt",
        "be careful",
        "--model",
        "claude-opus-5",
    ]


def test_build_agent_args_is_empty_without_settings(tmp_path: Path) -> None:
    assert box.build_agent_args(build_config({}, tmp_path), "") == []


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


def git_init(directory: Path) -> Path:
    """Create a git repository with no commits in it."""
    subprocess.run(["git", "init", "-q", str(directory)], check=True)
    return directory


def make_git_repository(directory: Path) -> Path:
    """Create a git repository with the one commit box needs to have something to clone."""
    git_init(directory)
    author = ["-c", "user.name=box", "-c", "user.email=box@example.com"]
    commit = ["commit", "-q", "--allow-empty", "-m", "first"]
    subprocess.run(["git", "-C", str(directory), *author, *commit], check=True)
    return directory


def make_repository(directory: Path, gitignore: str) -> Path:
    """Create a git repository holding a mounts file and the given .gitignore."""
    make_git_repository(directory)
    (directory / ".gitignore").write_text(gitignore)
    write_box_file(directory, box.MOUNTS_FILE, {"cache": "/cache"})
    return directory


def test_a_directory_that_is_not_a_repository_is_rejected(tmp_path: Path) -> None:
    with pytest.raises(box.ConfigError, match="not a git repository"):
        box.require_git_repository(tmp_path)


def test_a_repository_with_no_commits_is_rejected(tmp_path: Path) -> None:
    with pytest.raises(box.ConfigError, match="no commits"):
        box.require_git_repository(git_init(tmp_path))


def test_a_repository_with_a_commit_is_accepted(tmp_path: Path) -> None:
    box.require_git_repository(make_git_repository(tmp_path))


def test_prepare_launch_rejects_a_directory_that_is_not_a_repository(tmp_path: Path) -> None:
    with pytest.raises(box.ConfigError, match="not a git repository"):
        box.prepare_launch(make_config(), "/secrets/token", tmp_path)


def test_prepare_launch_rejects_a_repository_with_no_commits(tmp_path: Path) -> None:
    with pytest.raises(box.ConfigError, match="no commits"):
        box.prepare_launch(make_config(), "/secrets/token", git_init(tmp_path))


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


def test_gen_leaves_a_project_whose_deps_dir_box_accepts(tmp_path: Path) -> None:
    subprocess.run(["git", "init", "-q", str(tmp_path)], check=True)
    box.generate(tmp_path)
    box.require_ignored_local_paths(tmp_path)


def test_the_prompt_offers_the_deps_dir_when_nothing_here_fits() -> None:
    prompt = box.build_mount_prompt({"go": "the Go toolchain"}, ["go"], "darwin")
    assert f"{box.DEPS_DIR}/" in prompt
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
    arguments = box.build_parser().parse_args(["config", "--memory", "8g"])
    assert arguments.memory == "8g"


def test_config_is_not_a_setup_command() -> None:
    assert "config" not in box.SETUP_COMMANDS


def test_show_config_prints_the_settings_and_returns_zero(
    capsys: pytest.CaptureFixture[str], tmp_path: Path
) -> None:
    assert box.show_config(make_config(), "/secrets/token", make_git_repository(tmp_path)) == 0
    printed = capsys.readouterr().out
    assert "claude-opus-5" in printed
    assert "/secrets/token" in printed


def test_show_config_makes_the_checks_a_run_would(tmp_path: Path) -> None:
    config = build_config({"model": "claude-opus-5"}, tmp_path)
    with pytest.raises(box.ConfigError, match="kit is not set"):
        box.show_config(config, "/secrets/token", make_git_repository(tmp_path))


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


def test_the_update_message_names_this_script_and_the_url(tmp_path: Path) -> None:
    script = tmp_path / "box.py"
    script.write_text("print()")
    message = box.update_message(script, "a-different-hash")
    assert str(script) in message
    assert box.UPDATE_URL in message
    assert "curl" in message


def test_the_update_message_quotes_a_path_holding_a_space(tmp_path: Path) -> None:
    script = tmp_path / "my box.py"
    script.write_text("print()")
    message = box.update_message(script, "a-different-hash")
    assert f"'{script}'" in message


def test_the_update_message_says_when_the_script_cannot_be_written(tmp_path: Path) -> None:
    script = tmp_path / "box.py"
    script.write_text("print()")
    script.chmod(0o444)
    message = box.update_message(script, "a-different-hash")
    assert "not writable by you" in message
    assert "curl" not in message


def test_a_cache_without_a_check_time_reads_as_never_checked(tmp_path: Path) -> None:
    path = tmp_path / "update-check.json"
    path.write_text(json.dumps({"checked": 1000.0}))
    assert not box.checked_recently(path, 1000.0)


def test_a_cache_holding_a_check_time_that_is_not_a_number_reads_as_never_checked(tmp_path: Path) -> None:
    path = tmp_path / "update-check.json"
    path.write_text(json.dumps({"checked_at": "yesterday"}))
    assert not box.checked_recently(path, 1000.0)


def test_the_update_message_is_red(tmp_path: Path) -> None:
    script = tmp_path / "box.py"
    script.write_text("print()")
    message = box.update_message(script, "a-different-hash")
    assert message.startswith(box.RED)
    assert message.endswith(box.RESET)


def use_an_installed_copy(monkeypatch: pytest.MonkeyPatch, cache: Path) -> None:
    """Make the update check see the copy it exists for: an installed one, with its own cache."""
    monkeypatch.setattr(box, "is_tracked_by_git", lambda script_path: False)
    monkeypatch.setenv("XDG_CACHE_HOME", str(cache))


def test_warn_when_outdated_stays_silent_when_the_check_fails(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str], tmp_path: Path
) -> None:
    def explode() -> str:
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
    monkeypatch.setattr(box, "fetch_remote_hash", lambda: "a-different-hash")
    box.warn_when_outdated()
    assert capsys.readouterr().err == ""


def test_warn_when_outdated_records_a_check_that_failed(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    fetches: list[str] = []

    def explode() -> str:
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
    def explode() -> str:
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

    def fetch() -> str:
        fetches.append("a-different-hash")
        return "a-different-hash"

    use_an_installed_copy(monkeypatch, tmp_path)
    monkeypatch.setattr(box, "fetch_remote_hash", fetch)
    box.warn_when_outdated()
    assert box.UPDATE_URL in capsys.readouterr().err
    box.warn_when_outdated()
    assert capsys.readouterr().err == ""
    assert fetches == ["a-different-hash"]


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
    with pytest.raises(box.ConfigError, match="CLAUDE_OAUTH_TOKEN_FILE is not set"):
        box.prepare_launch(make_config(), "", make_git_repository(tmp_path))


def test_prepare_launch_requires_a_kit() -> None:
    config = build_config({}, Path("/tmp/demo"))
    with pytest.raises(box.ConfigError, match="kit is not set"):
        box.prepare_launch(config, "/secrets/token", Path("/tmp/demo"))


def test_prepare_launch_requires_a_model() -> None:
    config = build_config({"kit": ".sbx/kit"}, Path("/tmp/demo"))
    with pytest.raises(box.ConfigError, match="model is not set"):
        box.prepare_launch(config, "/secrets/token", Path("/tmp/demo"))


def test_a_kit_naming_a_file_is_rejected(tmp_path: Path) -> None:
    spec = tmp_path / "spec.yaml"
    spec.write_text("kit: {}\n")
    config = build_config({"kit": str(spec), "model": "claude-opus-5"}, tmp_path)
    with pytest.raises(box.ConfigError, match="kit names a file"):
        box.require_settings(config)


def test_a_kit_naming_a_directory_is_accepted(tmp_path: Path) -> None:
    kit = tmp_path / "kit"
    kit.mkdir()
    (kit / "spec.yaml").write_text("kit: {}\n")
    config = build_config({"kit": str(kit), "model": "claude-opus-5"}, tmp_path)
    box.require_settings(config)


def test_a_kit_that_is_not_on_disk_is_left_to_sbx(tmp_path: Path) -> None:
    config = build_config({"kit": "some/registry/ref", "model": "claude-opus-5"}, tmp_path)
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
    assert all(key == key.lower() for key in box.DEFAULTS)


def test_config_keys_match_the_config_fields() -> None:
    config = build_config({}, Path("/tmp/demo"))
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


def test_format_value_joins_mounts() -> None:
    assert box.format_value(("/a:ro", "/b")) == "/a:ro /b"


def test_format_config_shows_the_token_path_with_the_settings() -> None:
    rendered = box.format_config(make_config(), "/secrets/token")
    assert box.TOKEN_FILE_ENV in rendered
    assert "/secrets/token" in rendered
    assert "8g" in rendered
