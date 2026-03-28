"""Tests for rtvortex_cli.config module."""

from __future__ import annotations

import stat
from pathlib import Path

import pytest

from rtvortex_cli.config import Config


class TestConfigLoad:
    """Test Config.load() with various source combinations."""

    def test_defaults(self) -> None:
        cfg = Config.load()
        assert cfg.server_url == "http://localhost:8080"
        assert cfg.token is None
        assert cfg.output_format == "table"

    def test_from_file(self, saved_config: Config) -> None:
        cfg = Config.load()
        assert cfg.server_url == "http://test:8080"
        assert cfg.token == "test-token-1234"

    def test_env_overrides_file(
        self, saved_config: Config, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        monkeypatch.setenv("RTVORTEX_TOKEN", "env-override-token")
        monkeypatch.setenv("RTVORTEX_SERVER", "https://env.example.com/")
        cfg = Config.load()
        assert cfg.token == "env-override-token"
        assert cfg.server_url == "https://env.example.com"  # trailing slash stripped

    def test_env_output_format(self, monkeypatch: pytest.MonkeyPatch) -> None:
        monkeypatch.setenv("RTVORTEX_OUTPUT", "json")
        cfg = Config.load()
        assert cfg.output_format == "json"

    def test_invalid_env_output_ignored(self, monkeypatch: pytest.MonkeyPatch) -> None:
        monkeypatch.setenv("RTVORTEX_OUTPUT", "xml")
        cfg = Config.load()
        assert cfg.output_format == "table"


class TestConfigSave:
    """Test Config.save() persistence and permissions."""

    def test_save_creates_file(self, config: Config) -> None:
        path = config.save()
        assert path.exists()
        assert path.name == "config.yaml"

    def test_save_permissions(self, config: Config) -> None:
        path = config.save()
        mode = path.stat().st_mode
        assert mode & stat.S_IRUSR  # owner can read
        assert mode & stat.S_IWUSR  # owner can write
        assert not (mode & stat.S_IRGRP)  # group cannot read
        assert not (mode & stat.S_IROTH)  # others cannot read

    def test_roundtrip(self, config: Config) -> None:
        config.save()
        loaded = Config.load()
        assert loaded.server_url == config.server_url
        assert loaded.token == config.token

    def test_extra_keys_preserved(self) -> None:
        cfg = Config(extra={"custom_key": "custom_value"})
        cfg.save()
        loaded = Config.load()
        assert loaded.extra.get("custom_key") == "custom_value"


class TestConfigToken:
    """Test token management."""

    def test_clear_token(self, saved_config: Config) -> None:
        saved_config.clear_token()
        loaded = Config.load()
        assert loaded.token is None

    def test_masked_token(self) -> None:
        cfg = Config(token="abcdefghijklmnop")
        assert cfg.masked_token().endswith("mnop")
        assert cfg.masked_token().startswith("*")

    def test_masked_no_token(self) -> None:
        cfg = Config()
        assert cfg.masked_token() == "(not set)"

    def test_masked_short_token(self) -> None:
        cfg = Config(token="abc")
        assert cfg.masked_token() == "****"

    def test_is_authenticated(self) -> None:
        assert not Config().is_authenticated
        assert Config(token="x").is_authenticated


class TestConfigSetValue:
    """Test set_value for known and extra keys."""

    def test_set_known_key(self) -> None:
        cfg = Config()
        cfg.set_value("output_format", "json")
        assert cfg.output_format == "json"

    def test_set_extra_key(self) -> None:
        cfg = Config()
        cfg.set_value("my_custom", "val")
        assert cfg.extra["my_custom"] == "val"
        # Verify it was persisted
        loaded = Config.load()
        assert loaded.extra.get("my_custom") == "val"
