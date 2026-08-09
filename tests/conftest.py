"""Fixtures every test in this directory gets, asked for or not."""

from __future__ import annotations

import os
import shutil

import pytest

import box


@pytest.fixture(autouse=True)
def isolated_git_config(monkeypatch: pytest.MonkeyPatch) -> None:
    """Keep a developer's own git configuration out of the repositories these tests create."""
    # A global core.excludesFile or init.templateDir would otherwise decide what check-ignore says.
    monkeypatch.setenv("GIT_CONFIG_GLOBAL", os.devnull)
    monkeypatch.setenv("GIT_CONFIG_SYSTEM", os.devnull)
    monkeypatch.setenv("GIT_CONFIG_NOSYSTEM", "1")


@pytest.fixture(autouse=True)
def binaries_on_path(monkeypatch: pytest.MonkeyPatch, tmp_path_factory: pytest.TempPathFactory) -> None:
    """Put a stand-in on PATH for every command box shells out to that this machine lacks."""
    # The tests fake sbx and claude rather than calling them, so box has to find them anyway.
    directory = tmp_path_factory.mktemp("bin")
    for name in box.REQUIRED_BINARIES:
        if shutil.which(name):
            continue
        stub = directory / name
        stub.write_text("#!/bin/sh\nexit 1\n")
        stub.chmod(0o755)
    monkeypatch.setenv("PATH", f"{directory}{os.pathsep}{os.environ['PATH']}")
