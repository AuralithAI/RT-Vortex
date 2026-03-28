"""HTTP client for the Go swarm management endpoints.

All agent ↔ Go communication passes through :class:`GoClient`.  It handles
bearer-token authentication, request serialisation, and timeout management.
Endpoints correspond to the routes registered in ``internal/server/server.go``
under ``/internal/swarm/``.
"""

from __future__ import annotations

import json
import logging
from typing import Any

import httpx

from .agents_config import get_config

logger = logging.getLogger(__name__)


class GoClient:
    """Async HTTP client wrapping the Go swarm management API.

    A single instance is shared across all agents in a Python process.  The
    bearer token is set once after the initial :func:`~mono.swarm.auth.register_agent`
    call and reused for every subsequent request.
    """

    def __init__(self, token: str = ""):
        self.base_url = get_config().go_server_url
        self._token = token

    def set_token(self, token: str) -> None:
        self._token = token

    def _headers(self) -> dict[str, str]:
        headers = {"Content-Type": "application/json"}
        if self._token:
            headers["Authorization"] = f"Bearer {self._token}"
        return headers

    async def poll_next_task(self) -> dict | None:
        """Poll for the next assigned task.

        Returns:
            Parsed JSON task dict, or ``None`` when no work is available (204).

        Raises:
            httpx.HTTPStatusError: On non-2xx status (except 204).
        """
        async with httpx.AsyncClient(timeout=30.0) as client:
            resp = await client.get(
                f"{self.base_url}/internal/swarm/tasks/next",
                headers=self._headers(),
            )
            if resp.status_code == 204:
                return None
            resp.raise_for_status()
            return resp.json()

    async def get_task(self, task_id: str) -> dict:
        """Fetch the full task object by ID.

        Args:
            task_id: Task UUID.

        Returns:
            Parsed JSON task dict.

        Raises:
            httpx.HTTPStatusError: On non-2xx status.
        """
        async with httpx.AsyncClient(timeout=30.0) as client:
            resp = await client.get(
                f"{self.base_url}/internal/swarm/tasks/{task_id}",
                headers=self._headers(),
            )
            resp.raise_for_status()
            return resp.json()

    async def get_task_status(self, task_id: str) -> str:
        """Fetch current status of a task.

        Returns:
            The task's ``status`` string (e.g. ``implementing``, ``plan_review``).

        Raises:
            httpx.HTTPStatusError: On non-2xx status.
        """
        async with httpx.AsyncClient(timeout=15.0) as client:
            resp = await client.get(
                f"{self.base_url}/internal/swarm/tasks/{task_id}/status",
                headers=self._headers(),
            )
            resp.raise_for_status()
            data = resp.json()
            return data.get("status", "")

    async def report_plan(self, task_id: str, plan: dict[str, Any]) -> None:
        """Submit a structured plan document for human review.

        Args:
            task_id: Task UUID.
            plan: Plan dict with *summary*, *steps*, *affected_files*, and
                  *estimated_complexity*.
        """
        async with httpx.AsyncClient(timeout=30.0) as client:
            resp = await client.post(
                f"{self.base_url}/internal/swarm/tasks/{task_id}/plan",
                headers=self._headers(),
                json={"plan": plan},
            )
            resp.raise_for_status()

    async def report_diff(self, task_id: str, diff: dict[str, Any]) -> dict:
        """Submit a single-file diff for human review.

        Args:
            task_id: Task UUID.
            diff: Dict with *file_path*, *change_type*, *original*, *proposed*,
                  and *unified_diff*.

        Returns:
            Server-assigned diff metadata (includes the diff ID).
        """
        async with httpx.AsyncClient(timeout=30.0) as client:
            resp = await client.post(
                f"{self.base_url}/internal/swarm/tasks/{task_id}/diffs",
                headers=self._headers(),
                json=diff,
            )
            resp.raise_for_status()
            return resp.json()

    async def report_result(self, task_id: str) -> None:
        """Mark a task as completed after all diffs have been submitted."""
        async with httpx.AsyncClient(timeout=30.0) as client:
            resp = await client.post(
                f"{self.base_url}/internal/swarm/tasks/{task_id}/complete",
                headers=self._headers(),
            )
            resp.raise_for_status()

    async def fail_task(self, task_id: str, reason: str) -> None:
        """Mark a task as failed with an error reason.

        Args:
            task_id: Task UUID.
            reason: Human-readable failure reason.
        """
        async with httpx.AsyncClient(timeout=30.0) as client:
            resp = await client.post(
                f"{self.base_url}/internal/swarm/tasks/{task_id}/fail",
                headers=self._headers(),
                json={"reason": reason},
            )
            resp.raise_for_status()

    async def send_heartbeat(self, agent_id: str) -> None:
        """Send a keepalive heartbeat for *agent_id*.

        Go uses heartbeats to detect stale agents and reclaim their tokens.
        """
        async with httpx.AsyncClient(timeout=10.0) as client:
            resp = await client.post(
                f"{self.base_url}/internal/swarm/heartbeat/{agent_id}",
                headers=self._headers(),
            )
            resp.raise_for_status()

    async def revoke_agent(self, agent_id: str) -> None:
        """Revoke an agent's JWT token via Go.

        Called in the ``finally`` block of ``_run_full_pipeline`` to ensure
        tokens are cleaned up even if the completion/failure call failed.
        """
        async with httpx.AsyncClient(timeout=10.0) as client:
            try:
                resp = await client.delete(
                    f"{self.base_url}/internal/swarm/auth/revoke",
                    headers=self._headers(),
                )
                resp.raise_for_status()
            except Exception:
                pass  # Best-effort — Go heartbeat timeout is the backstop.

    async def declare_team_size(self, task_id: str, size: int) -> None:
        """Request a specific team size for *task_id*.

        The orchestrator calls this after analysing complexity.  Go uses the
        value to spin up or drain agents in the ``assignLoop``.
        """
        async with httpx.AsyncClient(timeout=10.0) as client:
            resp = await client.post(
                f"{self.base_url}/internal/swarm/tasks/{task_id}/declare-size",
                headers=self._headers(),
                json={"additional_agents": size},
            )
            resp.raise_for_status()

    # ── VCS proxy methods ────────────────────────────────────────────────

    async def vcs_read_file(self, repo_id: str, path: str, ref: str = "") -> str:
        """Read a file's content via the Go VCS proxy.

        Go resolves the repo's platform (GitHub/GitLab/Bitbucket) and token,
        fetches the file via the provider API, and returns the content.

        Args:
            repo_id: Repository UUID.
            path: File path relative to repo root.
            ref: Optional git ref (branch/tag/SHA). Empty = default branch.

        Returns:
            File content as a string.
        """
        async with httpx.AsyncClient(timeout=30.0) as client:
            resp = await client.post(
                f"{self.base_url}/internal/swarm/vcs/read-file",
                headers=self._headers(),
                json={"repo_id": repo_id, "path": path, "ref": ref},
            )
            resp.raise_for_status()
            data = resp.json()
            return data.get("content", "")

    async def vcs_list_dir(self, repo_id: str, path: str = "") -> list[dict]:
        """List directory contents via the Go VCS proxy.

        Args:
            repo_id: Repository UUID.
            path: Directory path relative to repo root. Empty for root.

        Returns:
            List of dicts with 'name' and 'type' keys.
        """
        async with httpx.AsyncClient(timeout=30.0) as client:
            resp = await client.post(
                f"{self.base_url}/internal/swarm/vcs/list-dir",
                headers=self._headers(),
                json={"repo_id": repo_id, "path": path},
            )
            resp.raise_for_status()
            data = resp.json()
            return data.get("entries", [])

    # ── Agent message (for live UI) ──────────────────────────────────────

    async def post_agent_message(self, task_id: str, message: dict) -> None:
        """Post an agent message for WebSocket broadcast to the browser UI.

        The Go server receives the message and broadcasts it via the swarm
        WebSocket hub so the frontend can display a live agent chat feed.

        Args:
            task_id: Task UUID.
            message: Dict with agent_id, agent_role, kind, content, metadata.
        """
        async with httpx.AsyncClient(timeout=10.0) as client:
            try:
                resp = await client.post(
                    f"{self.base_url}/internal/swarm/tasks/{task_id}/agent-message",
                    headers=self._headers(),
                    json=message,
                )
                resp.raise_for_status()
            except Exception:
                pass  # Best-effort — don't fail the agent if WS broadcast fails.
