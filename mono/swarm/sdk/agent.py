"""Agent base class — registration, tool execution, and the agentic loop.

Every agent role (orchestrator, senior_dev, architect, …) extends this class
and overrides :meth:`build_system_prompt` and :meth:`parse_result`.

The agent registers with the Go server on first use, obtaining a short-lived
JWT.  All subsequent LLM and task-management calls use that token.
"""

from __future__ import annotations

import logging
import socket
from dataclasses import dataclass, field
from typing import Any

import httpx

from ..agents_config import get_config, _read_version
from .loop import agent_loop
from .tool import ToolDef

logger = logging.getLogger(__name__)


def _build_version() -> str:
    """Return the build version from ``mono/VERSION`` (same source as Makefile)."""
    return _read_version()


@dataclass
class AgentConfig:
    """Runtime configuration injected into every agent instance.

    Attributes:
        go_base_url: Base URL of the Go API server.
    """

    go_base_url: str = ""

    def __post_init__(self) -> None:
        if not self.go_base_url:
            self.go_base_url = get_config().go_server_url


@dataclass
class Task:
    """A task received from Go."""

    id: str
    repo_id: str
    description: str
    status: str = "submitted"
    plan_document: dict | None = None


@dataclass
class AgentResult:
    """Result returned by an agent after processing a task."""

    output: str = ""
    plan: dict | None = None
    diffs: list[dict] = field(default_factory=list)
    error: str | None = None


class Agent:
    """Base agent class. Each role overrides build_system_prompt and parse_result."""

    def __init__(self, agent_id: str, role: str, team_id: str, agent_config: AgentConfig | None = None):
        self.agent_id = agent_id
        self.role = role
        self.team_id = team_id
        self.config = agent_config or AgentConfig()
        self.token: str | None = None
        self.tools: list[ToolDef] = []

    async def register(self) -> None:
        """Register with Go and obtain a short-lived agent JWT.

        Sends the build version (from ``mono/VERSION``), hostname, and role
        to ``POST /internal/swarm/auth/register``.  The Go server validates
        the service secret (derived from the JWT signing key via the same
        SHA-256 algorithm both sides share) and returns a 3-hour JWT.

        The server may assign a canonical UUID for the agent, which replaces
        the original short-form ``agent_id``.
        """
        cfg = get_config()
        url = f"{self.config.go_base_url}/internal/swarm/auth/register"
        payload = {
            "agent_id": self.agent_id,
            "role": self.role,
            "team_id": self.team_id,
            "hostname": socket.gethostname(),
            "version": _build_version(),
        }

        async with httpx.AsyncClient(timeout=30.0) as client:
            resp = await client.post(
                url,
                headers={"X-Service-Secret": cfg.service_secret},
                json=payload,
            )
            resp.raise_for_status()
            data = resp.json()
            self.token = data["access_token"]
            # The server may assign a canonical UUID; always prefer it.
            if data.get("agent_id"):
                self.agent_id = data["agent_id"]
            logger.info("Agent %s registered (role=%s, team=%s)", self.agent_id, self.role, self.team_id)

    async def run(self, task: Task) -> AgentResult:
        """Execute the agentic loop for the given task.

        If the agent has not yet registered, it does so first. Then it builds
        a role-specific system prompt and hands off to :func:`agent_loop`,
        which iterates tool calls until the LLM signals completion.

        Args:
            task: The task to execute, received from the Go server.

        Returns:
            An :class:`AgentResult` with the role-specific output.
        """
        if not self.token:
            await self.register()

        system_prompt = self.build_system_prompt(task)
        initial_message = f"Task: {task.description}\nRepo: {task.repo_id}"

        messages = await agent_loop(
            system_prompt=system_prompt,
            tools=self.tools,
            tool_executor=self.execute_tool,
            agent_token=self.token or "",
            go_base_url=self.config.go_base_url,
            initial_message=initial_message,
        )

        return self.parse_result(messages)

    async def execute_tool(self, name: str, args: dict) -> Any:
        """Dispatch a tool call by name.

        Args:
            name: Tool function name (must match a registered :class:`ToolDef`).
            args: Keyword arguments parsed from the LLM's function call.

        Returns:
            The tool's return value (typically a JSON-serialisable string).

        Raises:
            ValueError: If no tool with the given name is registered.
        """
        for t in self.tools:
            if t.name == name:
                return await t.fn(**args)
        raise ValueError(f"Unknown tool: {name}")

    def build_system_prompt(self, task: Task) -> str:
        """Override in subclass to provide role-specific system prompt."""
        return (
            f"You are a {self.role} agent in the RTVortex Agent Swarm. "
            f"Your agent ID is {self.agent_id}. "
            f"You are working on repository {task.repo_id}. "
            "Use the provided tools to accomplish the task."
        )

    def parse_result(self, messages: list[dict]) -> AgentResult:
        """Override in subclass to extract role-specific results."""
        # Default: return the last assistant message content.
        for msg in reversed(messages):
            if msg.get("role") == "assistant" and msg.get("content"):
                return AgentResult(output=msg["content"])
        return AgentResult(output="No output produced.")
