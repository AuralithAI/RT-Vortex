"""Shared test fixtures for RTVortex CLI tests."""

from __future__ import annotations

import os
from pathlib import Path
from typing import Generator

import pytest

from rtvortex_cli.config import Config


@pytest.fixture(autouse=True)
def _isolate_config(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> Generator[None, None, None]:
    """Ensure tests never touch the real config file."""
    config_dir = tmp_path / "rtvortex"
    config_dir.mkdir()
    monkeypatch.setenv("XDG_CONFIG_HOME", str(tmp_path))
    # Clear any env-based auth so tests are deterministic
    monkeypatch.delenv("RTVORTEX_TOKEN", raising=False)
    monkeypatch.delenv("RTVORTEX_SERVER", raising=False)
    monkeypatch.delenv("RTVORTEX_OUTPUT", raising=False)
    yield


@pytest.fixture
def config() -> Config:
    return Config(server_url="http://test:8080", token="test-token-1234")


@pytest.fixture
def saved_config(config: Config) -> Config:
    config.save()
    return config
