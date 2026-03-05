"""Configuration management for RTVortex CLI.

Handles loading/saving config from ~/.config/rtvortex/config.yaml,
with fallback to environment variables.
"""

from __future__ import annotations

import os
import stat
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Optional

import yaml


_DEFAULT_SERVER = "http://localhost:8080"
_DEFAULT_OUTPUT = "table"

_CONFIG_DIR_NAME = "rtvortex"
_CONFIG_FILE_NAME = "config.yaml"


def _config_dir() -> Path:
    """Return the platform-appropriate config directory."""
    xdg = os.environ.get("XDG_CONFIG_HOME")
    if xdg:
        return Path(xdg) / _CONFIG_DIR_NAME
    return Path.home() / ".config" / _CONFIG_DIR_NAME


def _config_path() -> Path:
    return _config_dir() / _CONFIG_FILE_NAME


@dataclass
class Config:
    """RTVortex CLI configuration."""

    server_url: str = _DEFAULT_SERVER
    token: Optional[str] = None
    output_format: str = _DEFAULT_OUTPUT
    extra: dict[str, Any] = field(default_factory=dict)

    # ── Loaders ──────────────────────────────────────────────────────────

    @classmethod
    def load(cls) -> Config:
        """Load configuration from file, then overlay environment variables.

        Priority: env vars > config file > defaults.
        """
        cfg = cls._from_file()

        # Environment overrides
        env_token = os.environ.get("RTVORTEX_TOKEN")
        if env_token:
            cfg.token = env_token

        env_server = os.environ.get("RTVORTEX_SERVER")
        if env_server:
            cfg.server_url = env_server.rstrip("/")

        env_output = os.environ.get("RTVORTEX_OUTPUT")
        if env_output and env_output in ("table", "json", "markdown"):
            cfg.output_format = env_output

        return cfg

    @classmethod
    def _from_file(cls) -> Config:
        path = _config_path()
        if not path.exists():
            return cls()
        try:
            with open(path) as f:
                data = yaml.safe_load(f) or {}
            return cls(
                server_url=str(data.get("server_url", _DEFAULT_SERVER)).rstrip("/"),
                token=data.get("token"),
                output_format=str(data.get("output_format", _DEFAULT_OUTPUT)),
                extra={k: v for k, v in data.items()
                       if k not in ("server_url", "token", "output_format")},
            )
        except (yaml.YAMLError, OSError):
            return cls()

    # ── Persistence ──────────────────────────────────────────────────────

    def save(self) -> Path:
        """Write config to disk with 0600 permissions.

        Returns the path written to.
        """
        path = _config_path()
        path.parent.mkdir(parents=True, exist_ok=True)

        data: dict[str, Any] = {"server_url": self.server_url}
        if self.token:
            data["token"] = self.token
        if self.output_format != _DEFAULT_OUTPUT:
            data["output_format"] = self.output_format
        data.update(self.extra)

        with open(path, "w") as f:
            yaml.safe_dump(data, f, default_flow_style=False)

        # Secure: only owner can read/write the config (contains token)
        path.chmod(stat.S_IRUSR | stat.S_IWUSR)
        return path

    def clear_token(self) -> None:
        """Remove the stored token and re-save."""
        self.token = None
        self.save()

    def set_value(self, key: str, value: str) -> None:
        """Set an arbitrary config key."""
        known = {"server_url", "token", "output_format"}
        if key in known:
            setattr(self, key, value)
        else:
            self.extra[key] = value
        self.save()

    # ── Display ──────────────────────────────────────────────────────────

    def masked_token(self) -> str:
        """Return the token with all but the last 4 characters masked."""
        if not self.token:
            return "(not set)"
        if len(self.token) <= 4:
            return "****"
        return "*" * (len(self.token) - 4) + self.token[-4:]

    @property
    def is_authenticated(self) -> bool:
        return bool(self.token)
