"""Tests for CLI commands via Click's test runner."""

from __future__ import annotations

import json

from click.testing import CliRunner

from rtvortex_cli.config import Config
from rtvortex_cli.main import cli


class TestAuthCommands:
    """Test auth login/logout/whoami."""

    def test_logout(self) -> None:
        cfg = Config(token="to-remove")
        cfg.save()
        runner = CliRunner()
        result = runner.invoke(cli, ["auth", "logout"])
        assert result.exit_code == 0
        assert "removed" in result.output.lower() or "✓" in result.output

    def test_whoami_no_token(self) -> None:
        runner = CliRunner()
        result = runner.invoke(cli, ["auth", "whoami"])
        assert result.exit_code != 0


class TestConfigCommands:
    """Test config show/set."""

    def test_config_show_defaults(self) -> None:
        runner = CliRunner()
        result = runner.invoke(cli, ["config", "show"])
        assert result.exit_code == 0
        assert "localhost" in result.output

    def test_config_set(self) -> None:
        runner = CliRunner()
        result = runner.invoke(cli, ["config", "set", "output_format", "json"])
        assert result.exit_code == 0
        assert "json" in result.output

        # Verify it was persisted
        cfg = Config.load()
        assert cfg.output_format == "json"


class TestVersionFlag:
    """Test --version flag."""

    def test_version(self) -> None:
        from rtvortex_cli import __version__

        runner = CliRunner()
        result = runner.invoke(cli, ["--version"])
        assert result.exit_code == 0
        assert __version__ in result.output


class TestHelpText:
    """Ensure all commands have help text."""

    def test_root_help(self) -> None:
        runner = CliRunner()
        result = runner.invoke(cli, ["--help"])
        assert result.exit_code == 0
        assert "RTVortex" in result.output

    def test_auth_help(self) -> None:
        runner = CliRunner()
        result = runner.invoke(cli, ["auth", "--help"])
        assert result.exit_code == 0

    def test_review_help(self) -> None:
        runner = CliRunner()
        result = runner.invoke(cli, ["review", "--help"])
        assert result.exit_code == 0
        assert "--watch" in result.output

    def test_index_help(self) -> None:
        runner = CliRunner()
        result = runner.invoke(cli, ["index", "--help"])
        assert result.exit_code == 0
        assert "--follow" in result.output

    def test_status_help(self) -> None:
        runner = CliRunner()
        result = runner.invoke(cli, ["status", "--help"])
        assert result.exit_code == 0
