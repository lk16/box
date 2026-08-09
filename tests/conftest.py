"""Fixtures every test in this directory gets, asked for or not."""

from __future__ import annotations

import os

import pytest


@pytest.fixture(autouse=True)
def isolated_git_config(monkeypatch: pytest.MonkeyPatch) -> None:
    """Keep a developer's own git configuration out of the repositories these tests create."""
    # A global core.excludesFile or init.templateDir would otherwise decide what check-ignore says.
    monkeypatch.setenv("GIT_CONFIG_GLOBAL", os.devnull)
    monkeypatch.setenv("GIT_CONFIG_SYSTEM", os.devnull)
    monkeypatch.setenv("GIT_CONFIG_NOSYSTEM", "1")
