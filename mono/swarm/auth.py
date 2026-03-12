"""Agent registration and JWT lifecycle management.

The Python swarm authenticates with the Go server by sending the service
secret to ``POST /internal/swarm/auth/register``.  The secret is derived
deterministically from the JWT signing key in ``rtserverprops.xml`` via
``SHA-256("rtvortex-swarm:" + jwt_secret)`` — the exact same algorithm Go
uses in ``deriveSwarmSecret()``.  No environment variable is needed.

Go returns a 3-hour agent JWT which is used for all subsequent API calls.
"""

from __future__ import annotations

import logging

import httpx

from .agents_config import get_config, _read_version

logger = logging.getLogger(__name__)


async def register_agent(
    agent_id: str,
    role: str,
    team_id: str,
    hostname: str = "",
    version: str = "",
) -> str:
    """Register an agent with the Go server and return a JWT access token.

    Calls ``POST /internal/swarm/auth/register`` with the service secret in
    the ``X-Service-Secret`` header.  The Go server validates the secret,
    creates the agent record, and returns a signed JWT with a 3-hour TTL.

    Args:
        agent_id: Unique identifier for this agent instance.
        role: Agent role (``orchestrator``, ``senior_dev``, etc.).
        team_id: UUID of the team this agent belongs to.
        hostname: Machine hostname (informational).
        version: Build version string.  Defaults to ``mono/VERSION``.

    Returns:
        The JWT access token string.

    Raises:
        httpx.HTTPStatusError: If registration fails (invalid secret, etc.).
    """
    cfg = get_config()

    if not version:
        version = cfg.version

    url = f"{cfg.go_server_url}/internal/swarm/auth/register"
    payload = {
        "agent_id": agent_id,
        "role": role,
        "team_id": team_id,
        "hostname": hostname,
        "version": version,
    }

    async with httpx.AsyncClient(timeout=30.0) as client:
        resp = await client.post(
            url,
            headers={"X-Service-Secret": cfg.service_secret},
            json=payload,
        )
        resp.raise_for_status()
        data = resp.json()
        token = data["access_token"]
        logger.info("Agent %s registered (role=%s, team=%s, expires_in=%ds)",
                     agent_id, role, team_id, data.get("expires_in", 0))
        return token


async def check_and_reregister(
    agent_id: str,
    role: str,
    team_id: str,
    current_token: str,
) -> str:
    """Verify the current token is still valid; re-register if expired.

    Sends a lightweight heartbeat call.  If it returns a non-4xx status the
    token is still good.  Otherwise the agent is re-registered (~5 ms).

    Args:
        agent_id: Agent identifier.
        role: Agent role.
        team_id: Team UUID.
        current_token: The JWT to test.

    Returns:
        A valid JWT (either the original or a freshly issued one).
    """
    cfg = get_config()
    url = f"{cfg.go_server_url}/internal/swarm/heartbeat/{agent_id}"
    try:
        async with httpx.AsyncClient(timeout=5.0) as client:
            resp = await client.post(
                url,
                headers={"Authorization": f"Bearer {current_token}"},
            )
            if resp.status_code < 400:
                return current_token
    except httpx.HTTPError:
        pass

    logger.info("Agent %s token expired, re-registering", agent_id)
    return await register_agent(agent_id, role, team_id)
