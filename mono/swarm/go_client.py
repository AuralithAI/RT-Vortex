"""HTTP client for the Go swarm management endpoints."""

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

    async def declare_team_size(self, task_id: str, size: int, team_id: str = "") -> None:
        """Request a specific team size for *task_id*.

        The consumer calls this after the orchestrator plan is approved.
        Go uses the value to spin up or drain agents in the ``assignLoop``.
        """
        payload: dict = {"additional_agents": size}
        if team_id:
            payload["team_id"] = team_id
        async with httpx.AsyncClient(timeout=10.0) as client:
            resp = await client.post(
                f"{self.base_url}/internal/swarm/tasks/{task_id}/declare-size",
                headers=self._headers(),
                json=payload,
            )
            resp.raise_for_status()

    # ── VCS proxy methods ────────────────────────────────────────────────

    # ── Sandbox builder methods ──────────────────────────────────────────

    async def sandbox_probe(
        self,
        task_id: str,
        repo_id: str,
        user_id: str,
        repo_files: list[str],
        changed_files: list[str],
        file_contents: dict[str, str] | None = None,
    ) -> dict:
        """Run the pre-build environment probe via the Go sandbox service.

        Detects the build system, scans file contents for env-var references,
        cross-references with the user's repo-scoped secrets, and returns a
        readiness assessment.
        """
        async with httpx.AsyncClient(timeout=30.0) as client:
            resp = await client.post(
                f"{self.base_url}/internal/swarm/sandbox/probe",
                headers=self._headers(),
                json={
                    "task_id": task_id,
                    "repo_id": repo_id,
                    "user_id": user_id,
                    "repo_files": repo_files,
                    "changed_files": changed_files,
                    "file_contents": file_contents or {},
                },
            )
            resp.raise_for_status()
            return resp.json()

    async def sandbox_generate_plan(
        self,
        task_id: str,
        repo_id: str,
        repo_files: list[str],
        changed_files: list[str],
        secret_names: list[str] | None = None,
    ) -> dict:
        """Generate a build plan via the Go sandbox service."""
        async with httpx.AsyncClient(timeout=30.0) as client:
            resp = await client.post(
                f"{self.base_url}/internal/swarm/sandbox/plan",
                headers=self._headers(),
                json={
                    "task_id": task_id,
                    "repo_id": repo_id,
                    "repo_files": repo_files,
                    "changed_files": changed_files,
                    "secret_names": secret_names or [],
                },
            )
            resp.raise_for_status()
            return resp.json()

    async def sandbox_list_secrets(
        self,
        repo_id: str,
        user_id: str,
    ) -> list[str]:
        """List build secret names (never values) for a repo+user."""
        async with httpx.AsyncClient(timeout=15.0) as client:
            resp = await client.get(
                f"{self.base_url}/internal/swarm/sandbox/secrets",
                headers=self._headers(),
                params={"repo_id": repo_id, "user_id": user_id},
            )
            resp.raise_for_status()
            data = resp.json()
            return data.get("secrets", [])

    async def sandbox_resolve_execute(
        self,
        task_id: str,
        repo_id: str,
        user_id: str,
        build_system: str,
        command: str,
        base_image: str,
        secret_refs: list[str] | None = None,
        pre_commands: list[str] | None = None,
        sandbox_mode: bool = True,
        timeout_sec: int = 600,
        memory_limit: str = "2g",
        cpu_limit: str = "2",
    ) -> dict:
        """Resolve secrets and execute a sandboxed build in one call.

        The Go server resolves secret values from the keychain, injects
        them as container env vars, runs the build, and zeroes memory.
        Secret values never leave the Go process boundary.
        """
        async with httpx.AsyncClient(timeout=float(timeout_sec + 30)) as client:
            resp = await client.post(
                f"{self.base_url}/internal/swarm/sandbox/resolve-execute",
                headers=self._headers(),
                json={
                    "task_id": task_id,
                    "repo_id": repo_id,
                    "user_id": user_id,
                    "build_system": build_system,
                    "command": command,
                    "base_image": base_image,
                    "secret_refs": secret_refs or [],
                    "pre_commands": pre_commands or [],
                    "sandbox_mode": sandbox_mode,
                    "timeout_sec": timeout_sec,
                    "memory_limit": memory_limit,
                    "cpu_limit": cpu_limit,
                },
            )
            resp.raise_for_status()
            return resp.json()

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

    async def post_discussion_event(self, task_id: str, event: dict) -> None:
        """Post a discussion thread lifecycle event for WebSocket broadcast.

        The Go server receives the event and broadcasts it as a
        ``swarm_discussion`` WebSocket event so the frontend can render
        multi-model comparison panels in real time.

        Args:
            task_id: Task UUID.
            event: Dict with ``event`` key (``"thread_opened"``,
                ``"provider_response"``, ``"thread_completed"``,
                ``"thread_synthesised"``) and event-specific payload fields.
        """
        async with httpx.AsyncClient(timeout=10.0) as client:
            try:
                resp = await client.post(
                    f"{self.base_url}/internal/swarm/tasks/{task_id}/discussion",
                    headers=self._headers(),
                    json=event,
                )
                resp.raise_for_status()
            except Exception:
                pass  # Best-effort — don't fail the agent if WS broadcast fails.

    async def post_consensus_event(self, task_id: str, event: dict) -> None:
        """Post a consensus engine result for WebSocket broadcast.

        The Go server records Prometheus metrics (strategy, winner, confidence)
        and broadcasts the consensus outcome as a ``swarm_discussion``
        WebSocket event (sub-type ``consensus_result``).

        Args:
            task_id: Task UUID.
            event: Dict with ``strategy``, ``provider``, ``confidence``,
                ``reasoning``, ``scores``, etc.
        """
        async with httpx.AsyncClient(timeout=10.0) as client:
            try:
                resp = await client.post(
                    f"{self.base_url}/internal/swarm/tasks/{task_id}/consensus",
                    headers=self._headers(),
                    json=event,
                )
                resp.raise_for_status()
            except Exception:
                pass  # Best-effort — don't fail the agent if WS broadcast fails.

    # ── MTM (Medium-Term Memory) endpoints ───────────────────────────────

    async def mtm_store(
        self,
        repo_id: str,
        agent_role: str,
        key: str,
        insight: str,
        confidence: float = 0.8,
    ) -> None:
        async with httpx.AsyncClient(timeout=15.0) as client:
            resp = await client.post(
                f"{self.base_url}/internal/swarm/memory/mtm",
                headers=self._headers(),
                json={
                    "repo_id": repo_id,
                    "agent_role": agent_role,
                    "key": key,
                    "insight": insight,
                    "confidence": confidence,
                },
            )
            resp.raise_for_status()

    async def mtm_recall(
        self,
        repo_id: str,
        agent_role: str,
        limit: int = 10,
    ) -> list[dict]:
        async with httpx.AsyncClient(timeout=15.0) as client:
            resp = await client.get(
                f"{self.base_url}/internal/swarm/memory/mtm",
                headers=self._headers(),
                params={
                    "repo_id": repo_id,
                    "agent_role": agent_role,
                    "limit": str(limit),
                },
            )
            resp.raise_for_status()
            data = resp.json()
            return data.get("insights", [])

    # ── Consensus Insights (Cross-Task Learning) endpoints ───────────────

    async def insight_store(
        self,
        repo_id: str,
        task_id: str,
        thread_id: str,
        category: str,
        key: str,
        insight: str,
        confidence: float = 0.8,
        strategy: str = "",
        provider: str = "",
        metadata: dict | None = None,
    ) -> None:
        """Store a cross-task consensus insight via the Go API."""
        async with httpx.AsyncClient(timeout=15.0) as client:
            resp = await client.post(
                f"{self.base_url}/internal/swarm/memory/insights",
                headers=self._headers(),
                json={
                    "repo_id": repo_id,
                    "task_id": task_id,
                    "thread_id": thread_id,
                    "category": category,
                    "key": key,
                    "insight": insight,
                    "confidence": confidence,
                    "strategy": strategy,
                    "provider": provider,
                    "metadata": metadata or {},
                },
            )
            resp.raise_for_status()

    async def insight_recall(
        self,
        repo_id: str,
        category: str = "",
        limit: int = 20,
    ) -> list[dict]:
        """Recall cross-task consensus insights for a repository."""
        async with httpx.AsyncClient(timeout=15.0) as client:
            resp = await client.get(
                f"{self.base_url}/internal/swarm/memory/insights",
                headers=self._headers(),
                params={
                    "repo_id": repo_id,
                    "category": category,
                    "limit": str(limit),
                },
            )
            resp.raise_for_status()
            data = resp.json()
            return data.get("insights", [])

    async def provider_stats(
        self,
        repo_id: str,
    ) -> list[dict]:
        """Get provider reliability stats for a repository."""
        async with httpx.AsyncClient(timeout=15.0) as client:
            resp = await client.get(
                f"{self.base_url}/internal/swarm/memory/provider-stats",
                headers=self._headers(),
                params={"repo_id": repo_id},
            )
            resp.raise_for_status()
            data = resp.json()
            return data.get("stats", [])

    # ── Role-Based ELO ─────────────────────────────────────────

    async def report_role_outcome(
        self,
        role: str,
        repo_id: str,
        task_id: str = "",
        human_rating: int = 0,
        consensus_confidence: float = 0.0,
        consensus_strategy: str = "",
        consensus_win: bool = False,
        tests_passed: bool = False,
        pr_accepted: bool = False,
        build_success: bool = False,
    ) -> dict:
        """Report a task outcome for role-based ELO scoring.

        The Go server computes a composite reward from human rating,
        consensus quality, and automatic metrics (tests/PR/build).
        The result updates the persistent (role, repo_id) ELO record.

        Args:
            role: Agent role (e.g. ``senior_dev``, ``qa``, ``security``).
            repo_id: Repository UUID.
            task_id: Task UUID (for history tracking).
            human_rating: User rating 1-5 (0 = not rated).
            consensus_confidence: Consensus engine confidence 0.0-1.0.
            consensus_strategy: Strategy used (e.g. ``multi_judge_panel``).
            consensus_win: Whether this role's response won consensus.
            tests_passed: Automatic signal: tests passed.
            pr_accepted: Automatic signal: PR accepted by user.
            build_success: Automatic signal: build succeeded.

        Returns:
            Updated RoleELO record from the server.
        """
        async with httpx.AsyncClient(timeout=15.0) as client:
            resp = await client.post(
                f"{self.base_url}/internal/swarm/role-elo/outcome",
                headers=self._headers(),
                json={
                    "role": role,
                    "repo_id": repo_id,
                    "task_id": task_id,
                    "human_rating": human_rating,
                    "consensus_confidence": consensus_confidence,
                    "consensus_strategy": consensus_strategy,
                    "consensus_win": consensus_win,
                    "tests_passed": tests_passed,
                    "pr_accepted": pr_accepted,
                    "build_success": build_success,
                },
            )
            resp.raise_for_status()
            return resp.json()

    async def get_role_elo(self, role: str, repo_id: str) -> dict:
        """Get the ELO record for a (role, repo_id) pair.

        Returns:
            RoleELO record with ``elo_score``, ``tier``, ``tasks_done``,
            ``wins``, ``losses``, ``consensus_avg``, etc.
        """
        async with httpx.AsyncClient(timeout=15.0) as client:
            resp = await client.get(
                f"{self.base_url}/internal/swarm/role-elo/{role}",
                headers=self._headers(),
                params={"repo_id": repo_id},
            )
            resp.raise_for_status()
            return resp.json()

    # ── CI Signal Ingestion ──────────────────────────────────────────────

    async def report_ci_signal(
        self,
        task_id: str,
        build_success: bool = False,
        tests_passed: bool = False,
        pr_accepted: bool = False,
        details: str = "",
    ) -> dict:
        """Report a CI signal (build/test/PR result) for a task.

        This feeds automatic signals into the role-based ELO system,
        closing the loop between agent-produced PRs and their CI outcomes.

        Args:
            task_id: The swarm task ID.
            build_success: Whether the build succeeded.
            tests_passed: Whether tests passed.
            pr_accepted: Whether the PR was merged/accepted.
            details: Optional human-readable details.

        Returns:
            Server acknowledgment with task_id.
        """
        async with httpx.AsyncClient(timeout=15.0) as client:
            resp = await client.post(
                f"{self.base_url}/internal/swarm/ci-signal/report",
                headers=self._headers(),
                json={
                    "task_id": task_id,
                    "build_success": build_success,
                    "tests_passed": tests_passed,
                    "pr_accepted": pr_accepted,
                    "details": details,
                },
            )
            resp.raise_for_status()
            return resp.json()

    async def get_ci_signal(self, task_id: str) -> dict:
        """Get the CI signal status for a task.

        Returns:
            CI signal record with pr_state, ci_state, etc.
        """
        async with httpx.AsyncClient(timeout=15.0) as client:
            resp = await client.get(
                f"{self.base_url}/api/v1/swarm/tasks/{task_id}/ci-signal",
                headers=self._headers(),
            )
            resp.raise_for_status()
            return resp.json()

    # ── HITL (Human-in-the-Loop) endpoints ───────────────────────────────

    async def ask_human(
        self,
        question: str,
        context: str = "",
        urgency: str = "normal",
        timeout: int = 300,
    ) -> dict:
        async with httpx.AsyncClient(timeout=float(timeout + 10)) as client:
            resp = await client.post(
                f"{self.base_url}/internal/swarm/hitl/ask",
                headers=self._headers(),
                json={
                    "question": question,
                    "context": context,
                    "urgency": urgency,
                    "timeout_seconds": timeout,
                },
            )
            resp.raise_for_status()
            return resp.json()

    # ── CI/CD proxy endpoints ────────────────────────────────────────────

    async def run_ci_command(
        self,
        command_type: str,
        command: str = "",
        files: list[str] | None = None,
        timeout: int = 120,
    ) -> dict:
        async with httpx.AsyncClient(timeout=float(timeout + 10)) as client:
            resp = await client.post(
                f"{self.base_url}/internal/swarm/ci/run",
                headers=self._headers(),
                json={
                    "command_type": command_type,
                    "command": command,
                    "files": files or [],
                    "timeout_seconds": timeout,
                },
            )
            resp.raise_for_status()
            return resp.json()

    # ── Web fetch proxy ──────────────────────────────────────────────────

    async def web_fetch(
        self,
        url: str = "",
        query: str = "",
        max_results: int = 5,
        extract_pdf: bool = False,
    ) -> dict:
        async with httpx.AsyncClient(timeout=45.0) as client:
            resp = await client.post(
                f"{self.base_url}/internal/swarm/web/fetch",
                headers=self._headers(),
                json={
                    "url": url,
                    "query": query,
                    "max_results": max_results,
                    "extract_pdf": extract_pdf,
                },
            )
            resp.raise_for_status()
            return resp.json()

    # ── Inter-agent communication bus ────────────────────────────────────

    async def publish_agent_bus(
        self,
        target_role: str,
        message: str,
        include_embeddings_ref: bool = False,
        confidence: float = 0.8,
        task_id: str = "",
        from_role: str = "",
    ) -> dict:
        async with httpx.AsyncClient(timeout=15.0) as client:
            resp = await client.post(
                f"{self.base_url}/internal/swarm/agent-bus/publish",
                headers=self._headers(),
                json={
                    "target_role": target_role,
                    "message": message,
                    "include_embeddings_ref": include_embeddings_ref,
                    "confidence": confidence,
                    "task_id": task_id,
                    "from_role": from_role,
                },
            )
            resp.raise_for_status()
            return resp.json()

    async def read_agent_bus(
        self,
        role: str = "",
        limit: int = 10,
    ) -> dict:
        async with httpx.AsyncClient(timeout=15.0) as client:
            resp = await client.get(
                f"{self.base_url}/internal/swarm/agent-bus/read",
                headers=self._headers(),
                params={"role": role, "limit": str(limit)},
            )
            resp.raise_for_status()
            return resp.json()

    # ── Asset ingestion ──────────────────────────────────────────────────

    async def ingest_asset(
        self,
        repo_id: str,
        source_url: str = "",
        content: str = "",
        asset_type: str = "document",
    ) -> dict:
        async with httpx.AsyncClient(timeout=30.0) as client:
            resp = await client.post(
                f"{self.base_url}/internal/swarm/ingest-asset",
                headers=self._headers(),
                json={
                    "repo_id": repo_id,
                    "source_url": source_url,
                    "content": content,
                    "asset_type": asset_type,
                },
            )
            resp.raise_for_status()
            return resp.json()

    # ── MCP integrations ─────────────────────────────────────────────────

    async def mcp_call(
        self,
        provider: str,
        action: str,
        params: dict | None = None,
        user_id: str = "",
        org_id: str = "",
        agent_id: str = "",
        task_id: str = "",
    ) -> dict:
        async with httpx.AsyncClient(timeout=45.0) as client:
            resp = await client.post(
                f"{self.base_url}/internal/swarm/mcp/call",
                headers=self._headers(),
                json={
                    "provider": provider,
                    "action": action,
                    "params": params or {},
                    "user_id": user_id,
                    "org_id": org_id,
                    "agent_id": agent_id,
                    "task_id": task_id,
                },
            )
            resp.raise_for_status()
            return resp.json()

    async def mcp_list_providers(self) -> list:
        async with httpx.AsyncClient(timeout=15.0) as client:
            resp = await client.get(
                f"{self.base_url}/internal/swarm/mcp/providers",
                headers=self._headers(),
            )
            resp.raise_for_status()
            return resp.json()

    async def mcp_list_connections(self) -> list:
        async with httpx.AsyncClient(timeout=15.0) as client:
            resp = await client.get(
                f"{self.base_url}/api/v1/integrations/connections",
                headers=self._headers(),
            )
            resp.raise_for_status()
            return resp.json()

    async def mcp_describe_action(self, provider: str, action: str) -> dict:
        async with httpx.AsyncClient(timeout=15.0) as client:
            resp = await client.get(
                f"{self.base_url}/internal/swarm/mcp/describe",
                headers=self._headers(),
                params={"provider": provider, "action": action},
            )
            resp.raise_for_status()
            return resp.json()

    # ── Team Formation ───────────────────────────────────────────────────

    async def recommend_team(
        self,
        task_id: str,
        repo_id: str,
        plan: dict | None = None,
        signals: dict | None = None,
    ) -> dict:
        """Request an optimal team composition from the Go server.

        The Go ``TeamFormationService`` analyses the plan document, computes
        a multi-dimensional complexity score, queries role-based ELO tiers,
        and returns an ``TeamFormation`` struct with recommended roles, team
        size, reasoning, and strategy.

        Args:
            task_id: The swarm task ID.
            repo_id: Repository ID (for ELO lookups).
            plan: Optional plan dict (if None, Go loads from DB).
            signals: Optional pre-computed complexity signals override.

        Returns:
            TeamFormation dict with ``complexity_score``, ``complexity_label``,
            ``recommended_roles``, ``role_elos``, ``team_size``, ``reasoning``,
            ``strategy``, ``input_signals``.
        """
        payload: dict = {"task_id": task_id, "repo_id": repo_id}
        if plan is not None:
            payload["plan"] = plan
        if signals is not None:
            payload["signals"] = signals
        async with httpx.AsyncClient(timeout=30.0) as client:
            resp = await client.post(
                f"{self.base_url}/internal/swarm/team-recommend",
                headers=self._headers(),
                json=payload,
            )
            resp.raise_for_status()
            return resp.json()

    # ── Adaptive Probe Tuning ────────────────────────────────────────────

    async def get_probe_config(
        self,
        role: str,
        repo_id: str = "",
        action_type: str = "",
        complexity_label: str = "",
        tier: str = "",
    ) -> dict:
        """Fetch adaptive probe configuration from Go.

        Args:
            role: Agent role (e.g. "senior_dev").
            repo_id: Repository ID for repo-specific configs.
            action_type: Optional action type filter.
            complexity_label: Task complexity for per-probe enhancement.
            tier: Agent's ELO tier for per-probe enhancement.

        Returns:
            ProbeConfig dict with tuned parameters.
        """
        params: dict[str, str] = {"role": role}
        if repo_id:
            params["repo_id"] = repo_id
        if action_type:
            params["action_type"] = action_type
        if complexity_label:
            params["complexity_label"] = complexity_label
        if tier:
            params["tier"] = tier
        async with httpx.AsyncClient(timeout=15.0) as client:
            resp = await client.get(
                f"{self.base_url}/internal/swarm/probe-config",
                headers=self._headers(),
                params=params,
            )
            resp.raise_for_status()
            return resp.json()

    async def record_probe_history(self, outcome: dict) -> None:
        """Record a probe outcome in the adaptive tuning history.

        Args:
            outcome: ProbeOutcome dict with all probe details.
        """
        async with httpx.AsyncClient(timeout=15.0) as client:
            resp = await client.post(
                f"{self.base_url}/internal/swarm/probe-history",
                headers=self._headers(),
                json=outcome,
            )
            resp.raise_for_status()

    # ── Self-Healing Pipeline ────────────────────────────────────────────

    async def report_provider_outcome(
        self,
        provider: str,
        success: bool,
        latency_ms: float = 0.0,
        error_msg: str = "",
        task_id: str = "",
        agent_id: str = "",
    ) -> None:
        """Report a provider call outcome for circuit-breaker tracking.

        Fire-and-forget — errors are logged but not raised.

        Args:
            provider: LLM provider name (e.g. "openai", "anthropic").
            success: Whether the call succeeded.
            latency_ms: Call latency in milliseconds.
            error_msg: Error message on failure.
            task_id: Optional task context.
            agent_id: Optional agent context.
        """
        payload = {
            "provider": provider,
            "success": success,
            "latency_ms": latency_ms,
            "error_msg": error_msg,
            "task_id": task_id,
            "agent_id": agent_id,
        }
        try:
            async with httpx.AsyncClient(timeout=10.0) as client:
                resp = await client.post(
                    f"{self.base_url}/internal/swarm/self-heal/provider-outcome",
                    headers=self._headers(),
                    json=payload,
                )
                if resp.status_code >= 400:
                    logger.warning(
                        "self-heal: provider-outcome report failed status=%d",
                        resp.status_code,
                    )
        except Exception:
            logger.debug("self-heal: failed to report provider outcome", exc_info=True)

    async def check_provider_status(self, provider: str) -> dict:
        """Check whether a provider is available (circuit breaker check).

        Args:
            provider: LLM provider name.

        Returns:
            Dict with ``available``, ``state``, ``consecutive_failures``, ``open_until``.
            Returns a healthy-default on failure.
        """
        try:
            async with httpx.AsyncClient(timeout=5.0) as client:
                resp = await client.get(
                    f"{self.base_url}/internal/swarm/self-heal/provider-status",
                    headers=self._headers(),
                    params={"provider": provider},
                )
                if resp.status_code == 200:
                    return resp.json()
        except Exception:
            logger.debug("self-heal: failed to check provider status", exc_info=True)
        return {"provider": provider, "available": True, "state": "closed"}

    # ── Observability Dashboard ──────────────────────────────────────────

    async def get_observability_dashboard(self, hours: int = 24) -> dict:
        """Fetch the full observability dashboard.

        Args:
            hours: Time range for time-series data (default 24h).

        Returns:
            Dashboard dict with current, time_series, provider_perf, health_breakdown, cost_summary.
        """
        try:
            async with httpx.AsyncClient(timeout=30.0) as client:
                resp = await client.get(
                    f"{self.base_url}/api/v1/swarm/observability/dashboard",
                    headers=self._headers(),
                    params={"hours": hours},
                )
                if resp.status_code == 200:
                    return resp.json()
        except Exception:
            logger.debug("observability: failed to fetch dashboard", exc_info=True)
        return {}

    async def get_health_score(self) -> dict:
        """Fetch the current system health score breakdown.

        Returns:
            Dict with ``score``, ``task_health_pct``, ``agent_health_pct``, etc.
        """
        try:
            async with httpx.AsyncClient(timeout=10.0) as client:
                resp = await client.get(
                    f"{self.base_url}/api/v1/swarm/observability/health",
                    headers=self._headers(),
                )
                if resp.status_code == 200:
                    return resp.json()
        except Exception:
            logger.debug("observability: failed to fetch health", exc_info=True)
        return {"score": 100, "details": "unknown"}

    async def get_cost_summary(self) -> dict:
        """Fetch the current cost summary.

        Returns:
            Dict with ``today_usd``, ``this_week_usd``, ``this_month_usd``, ``by_provider``.
        """
        try:
            async with httpx.AsyncClient(timeout=10.0) as client:
                resp = await client.get(
                    f"{self.base_url}/api/v1/swarm/observability/cost",
                    headers=self._headers(),
                )
                if resp.status_code == 200:
                    return resp.json()
        except Exception:
            logger.debug("observability: failed to fetch cost", exc_info=True)
        return {"today_usd": 0, "this_week_usd": 0, "this_month_usd": 0}