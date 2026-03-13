"""Centralised swarm configuration — single source of truth.

Merges ``rtserverprops.xml`` values with hard-coded defaults.  Every module
in the swarm package reads settings through :func:`get_config` rather than
accessing environment variables directly.

Resolution order (highest priority first):

    1. Value from ``rtserverprops.xml``  (if present under RTVORTEX_HOME)
    2. Hard-coded default

Usage::

    from mono.swarm.agents_config import get_config

    cfg = get_config()
    print(cfg.engine_host, cfg.go_server_url, cfg.redis_url)
"""

from __future__ import annotations

import hashlib
import os
import sys
import xml.etree.ElementTree as ET
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any


# ── XML parser utility ──────────────────────────────────────────────────────

def _resolve_xml_var(raw: str, env: dict[str, str] | None = None) -> str:
    """Expand ``${ENV_VAR:default}`` and ``${rtvortex.home}`` placeholders.

    Mirrors the variable resolution that the Go config loader performs on
    ``rtserverprops.xml`` so Python reads the same effective values.
    """
    if not raw or "${" not in raw:
        return raw

    rt_home = _resolve_rt_home()
    result = raw.replace("${rtvortex.home}", rt_home)

    while "${" in result:
        start = result.index("${")
        end = result.index("}", start)
        token = result[start + 2 : end]

        if ":" in token:
            var_name, default = token.split(":", 1)
        else:
            var_name, default = token, ""

        value = os.getenv(var_name, default)
        result = result[:start] + value + result[end + 1 :]

    return result


def _resolve_rt_home() -> str:
    """Determine the ``rt_home`` directory.

    Resolution order:

    1. ``$RTVORTEX_HOME`` environment variable (absolute or relative).
    2. Auto-detect from the running executable path — the swarm venv lives at
       ``<rt_home>/swarm/venv/…``, so walking up three levels from
       ``sys.executable`` (or ``sys.prefix``) gives ``rt_home``.
    3. Auto-detect from this source file — during development, this file is at
       ``<repo>/mono/swarm/agents_config.py``; ``<repo>/rt_home`` is checked.

    Returns the resolved path (always absolute), or an empty string if nothing
    was found.
    """
    # 1. Explicit env var.
    env = os.getenv("RTVORTEX_HOME", "")
    if env:
        p = Path(env).resolve()
        if (p / "config" / "rtserverprops.xml").is_file():
            return str(p)

    # 2. Infer from venv location:  …/rt_home/swarm/venv/bin/python
    #    sys.prefix → …/rt_home/swarm/venv
    venv_prefix = Path(sys.prefix).resolve()
    candidate = venv_prefix.parent.parent          # …/rt_home
    if (candidate / "config" / "rtserverprops.xml").is_file():
        return str(candidate)

    # 3. Infer from source tree:  mono/swarm/agents_config.py → repo/rt_home
    repo_root = Path(__file__).resolve().parent.parent.parent
    candidate = repo_root / "rt_home"
    if (candidate / "config" / "rtserverprops.xml").is_file():
        return str(candidate)

    return ""


def _parse_server_props(xml_path: str) -> dict[str, Any]:
    """Extract relevant settings from ``rtserverprops.xml``.

    Reads host, port, and TLS/connection-type information for the Go server,
    C++ engine, and Redis.  The TLS flags determine whether ``https://`` or
    ``http://`` (and ``rediss://`` or ``redis://``) schemes are used when
    constructing connection URLs.

    Returns a flat dict of resolved values.  Missing or unparseable files
    return an empty dict — callers fall back to defaults.
    """
    if not xml_path or not Path(xml_path).is_file():
        return {}

    try:
        tree = ET.parse(xml_path)
    except ET.ParseError:
        return {}

    root = tree.getroot()
    props: dict[str, Any] = {}

    # <server host="..." port="...">
    #   <tls enabled="true|false" .../>
    server_el = root.find("server")
    if server_el is not None:
        props["server_host"] = _resolve_xml_var(server_el.get("host", ""))
        props["server_port"] = _resolve_xml_var(server_el.get("port", ""))
        tls_el = server_el.find("tls")
        if tls_el is not None:
            props["server_tls"] = _resolve_xml_var(
                tls_el.get("enabled", "false")
            ).lower() == "true"
        else:
            props["server_tls"] = False

    # <engine host="..." port="..." negotiation-type="TLS|PLAINTEXT" ...>
    engine_el = root.find("engine")
    if engine_el is not None:
        props["engine_host"] = _resolve_xml_var(engine_el.get("host", ""))
        props["engine_port"] = _resolve_xml_var(engine_el.get("port", ""))
        props["engine_timeout_ms"] = _resolve_xml_var(
            engine_el.get("timeout-ms", "")
        )
        negotiation = _resolve_xml_var(
            engine_el.get("negotiation-type", "PLAINTEXT")
        ).upper()
        props["engine_tls"] = negotiation in ("TLS", "MTLS")

    # <redis host="..." port="..." password="..." database="...">
    redis_el = root.find("redis")
    if redis_el is not None:
        host = _resolve_xml_var(redis_el.get("host", "localhost"))
        port = _resolve_xml_var(redis_el.get("port", "6379"))
        password = _resolve_xml_var(redis_el.get("password", ""))
        db = _resolve_xml_var(redis_el.get("database", "0"))

        # Redis TLS: check for <tls enabled="true"/> child or REDIS_TLS env
        redis_tls = False
        redis_tls_el = redis_el.find("tls")
        if redis_tls_el is not None:
            redis_tls = _resolve_xml_var(
                redis_tls_el.get("enabled", "false")
            ).lower() == "true"
        props["redis_tls"] = redis_tls

        scheme = "rediss" if redis_tls else "redis"
        if password:
            props["redis_url"] = f"{scheme}://:{password}@{host}:{port}/{db}"
        else:
            props["redis_url"] = f"{scheme}://{host}:{port}/{db}"

    # <llm max-tokens="..." timeout-ms="...">
    llm_el = root.find("llm")
    if llm_el is not None:
        mt = _resolve_xml_var(llm_el.get("max-tokens", ""))
        if mt:
            props["llm_max_tokens"] = mt
        tm = _resolve_xml_var(llm_el.get("timeout-ms", ""))
        if tm:
            props["llm_timeout_ms"] = tm

    # <security jwt-secret="..." ...>
    security_el = root.find("security")
    if security_el is not None:
        jwt_secret = _resolve_xml_var(security_el.get("jwt-secret", ""))
        if jwt_secret:
            props["jwt_secret"] = jwt_secret

    return props


# ── AgentsConfig ─────────────────────────────────────────────────────────────

@dataclass(frozen=True)
class AgentsConfig:
    """Immutable configuration for the agent swarm.

    Attributes:
        engine_host: Hostname of the C++ engine gRPC server.
        engine_port: Port of the C++ engine gRPC server.
        engine_tls: Whether the engine gRPC channel uses TLS (from
            ``negotiation-type`` in the XML: ``TLS``/``mTLS`` → True,
            ``PLAINTEXT`` → False).
        go_server_url: Base URL of the Go API server (``https://`` when
            server TLS is enabled, ``http://`` otherwise).
        redis_url: Redis connection URL (``rediss://`` for TLS,
            ``redis://`` for plaintext).
        service_secret: Derived service secret for agent registration
            (``SHA-256("rtvortex-swarm:" + jwt_secret)``).
        max_teams: Maximum concurrent agent teams.
        max_agents_per_team: Upper bound of agents in a single team.
        min_agents_per_team: Minimum agents required to form a team.
        llm_max_tokens: Default max tokens for LLM completions.
        llm_timeout: HTTP timeout (seconds) for LLM proxy calls.
        heartbeat_interval: Seconds between agent heartbeats.
        task_poll_interval: Seconds between task poll attempts (fallback mode).
        version: Build version string from ``mono/VERSION``.
    """

    engine_host: str = "localhost"
    engine_port: int = 50051
    engine_tls: bool = False
    go_server_url: str = "http://localhost:8080"
    redis_url: str = "redis://localhost:6379/0"
    service_secret: str = ""
    max_teams: int = 5
    max_agents_per_team: int = 10
    min_agents_per_team: int = 2
    llm_max_tokens: int = 4096
    llm_timeout: float = 120.0
    heartbeat_interval: int = 30
    task_poll_interval: float = 1.0
    version: str = "0.0.0"

    @classmethod
    def load(cls, xml_path: str = "") -> "AgentsConfig":
        """Build configuration from ``rtserverprops.xml``.

        The XML file is the single source of truth — the same file the Go
        server reads.  Defaults are used only for fields absent from the XML.

        Args:
            xml_path: Explicit path to ``rtserverprops.xml``.  When empty the
                method looks under ``$RTVORTEX_HOME/config/rtserverprops.xml``.

        Returns:
            Fully resolved ``AgentsConfig`` instance.
        """
        if not xml_path:
            rt_home = _resolve_rt_home()
            if rt_home:
                xml_path = os.path.join(rt_home, "config", "rtserverprops.xml")

        xml = _parse_server_props(xml_path)

        engine_host = xml.get("engine_host", "localhost")
        engine_port = int(xml.get("engine_port", 50051))
        engine_tls = xml.get("engine_tls", False)

        # Build Go server URL — scheme depends on server TLS setting.
        server_host = xml.get("server_host", "localhost")
        server_port = xml.get("server_port", "8080")
        server_tls = xml.get("server_tls", False)
        scheme = "https" if server_tls else "http"
        go_server_url = f"{scheme}://{server_host}:{server_port}"

        redis_url = xml.get("redis_url", "redis://localhost:6379/0")

        # Derive the service secret deterministically from the JWT secret,
        # exactly the same way Go's deriveSwarmSecret() does:
        #   SHA-256("rtvortex-swarm:" + jwt_secret)
        jwt_secret = xml.get("jwt_secret", "")
        service_secret = ""
        if jwt_secret:
            service_secret = hashlib.sha256(
                f"rtvortex-swarm:{jwt_secret}".encode()
            ).hexdigest()

        # LLM timeout: XML gives milliseconds, we store seconds.
        llm_timeout_ms = xml.get("llm_timeout_ms", "")
        llm_timeout = float(llm_timeout_ms) / 1000 if llm_timeout_ms else 120.0

        llm_max_tokens = int(xml.get("llm_max_tokens", 4096))

        # Read build version from mono/VERSION (same as Makefile).
        version = _read_version()

        return cls(
            engine_host=engine_host,
            engine_port=engine_port,
            engine_tls=engine_tls,
            go_server_url=go_server_url,
            redis_url=redis_url,
            service_secret=service_secret,
            max_teams=5,
            max_agents_per_team=10,
            min_agents_per_team=2,
            llm_max_tokens=llm_max_tokens,
            llm_timeout=llm_timeout,
            heartbeat_interval=30,
            task_poll_interval=1.0,
            version=version,
        )


def _read_version() -> str:
    """Read the build version from ``mono/VERSION`` or the installed package.

    Checks (in order):
    1. ``$RTVORTEX_HOME/../mono/VERSION`` (source tree during development)
    2. ``mono/VERSION`` relative to this file (if running from source)
    3. Package metadata (if installed via pip)
    4. Falls back to ``"0.0.0"``
    """
    # Try RTVORTEX_HOME first (production layout).
    rt_home = _resolve_rt_home()
    if rt_home:
        # In production the VERSION file is copied alongside the swarm venv.
        candidates = [
            Path(rt_home) / "VERSION",
            Path(rt_home).parent / "mono" / "VERSION",
        ]
        for p in candidates:
            if p.is_file():
                return p.read_text().strip()

    # Try relative to this source file (development).
    src_version = Path(__file__).resolve().parent.parent / "VERSION"
    if src_version.is_file():
        return src_version.read_text().strip()

    # Last resort: package metadata.
    try:
        from importlib.metadata import version as pkg_version
        return pkg_version("rtvortex-swarm")
    except Exception:
        return "0.0.0"


# ── Module-level singleton ──────────────────────────────────────────────────

_cfg: AgentsConfig | None = None


def get_config() -> AgentsConfig:
    """Return the process-wide :class:`AgentsConfig` singleton.

    The config is loaded lazily on first call and cached for the lifetime of
    the process.  All swarm modules should use this instead of constructing
    their own ``AgentsConfig`` or reading environment variables directly.
    """
    global _cfg
    if _cfg is None:
        _cfg = AgentsConfig.load()
    return _cfg
