"""Agent base class — registration, tool execution, and the agentic loop.

Every agent role (orchestrator, senior_dev, architect, …) extends this class
and overrides :meth:`build_system_prompt` and :meth:`parse_result`.

The agent registers with the Go server on first use, obtaining a short-lived
JWT.  All subsequent LLM and task-management calls use that token.

If a :class:`~mono.swarm.conversation.SharedConversation` is attached, all
LLM responses and tool calls are broadcast to the shared conversation for
live UI display.

* **Memory hierarchy** — Every agent gets an :class:`AgentMemory` instance
  with STM/MTM/LTM tiers.  Memory context is injected into the system prompt
  and reflections run after each tool call.
"""

from __future__ import annotations

import logging
import socket
from dataclasses import dataclass, field
from typing import TYPE_CHECKING, Any

import httpx

from ..agents_config import get_config, _read_version
from .go_llm_client import llm_complete, llm_probe, ProbeResponse, ProbeResultItem
from .loop import agent_loop
from .tool import ToolDef

if TYPE_CHECKING:
    from ..conversation import SharedConversation
    from ..memory import AgentMemory
    from ..workspace import VirtualWorkspace

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
        self.conversation: SharedConversation | None = None
        self.workspace: VirtualWorkspace | None = None
        self.memory: AgentMemory | None = None

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

        If a :attr:`conversation` is attached, the system prompt is augmented
        with a summary of what other agents have said so far, and every LLM
        response / tool call is broadcast to the conversation.

        If a :attr:`memory` is attached, memory context (STM scratchpad,
        MTM insights, LTM search) is injected into the system prompt.

        Args:
            task: The task to execute, received from the Go server.

        Returns:
            An :class:`AgentResult` with the role-specific output.
        """
        if not self.token:
            await self.register()

        system_prompt = self.build_system_prompt(task)

        # Inject conversation context so this agent sees what others did.
        if self.conversation and self.conversation.message_count > 0:
            summary = self.conversation.get_summary_for(self.role)
            if summary:
                system_prompt += f"\n\n{summary}\n"

        # Inject memory context (STM observations + MTM insights + LTM search).
        if self.memory:
            try:
                mem_context = await self.memory.build_memory_context(
                    task_description=task.description
                )
                if mem_context:
                    system_prompt += (
                        f"\n\n=== Memory Context ===\n{mem_context}\n"
                    )
            except Exception as e:
                logger.warning("Agent %s: memory context build failed: %s",
                               self.agent_id, e)

        initial_message = f"Task: {task.description}\nRepo: {task.repo_id}"

        messages = await agent_loop(
            system_prompt=system_prompt,
            tools=self.tools,
            tool_executor=self.execute_tool,
            agent_token=self.token or "",
            go_base_url=self.config.go_base_url,
            initial_message=initial_message,
            agent_role=self.role,
            agent_id=self.agent_id,
            conversation=self.conversation,
            memory=self.memory,
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

    # ── Multi-LLM Probe ─────────────────────────────────────────────────

    async def probe_and_gather(
        self,
        messages: list[dict],
        *,
        action_type: str = "",
        num_models: int = 0,
        tools: list[dict] | None = None,
        max_tokens: int | None = None,
    ) -> ProbeResponse:
        """Probe multiple LLMs in parallel and return all responses.

        This is the agent-level entry point for Perplexity-style multi-LLM
        querying.  The agent sends the same messages to all providers in its
        role's priority matrix, and each provider's response is returned
        separately.

        The agent (or a future consensus engine) can then:
        - Pick the best single response
        - Synthesise a combined answer from multiple perspectives
        - Use GPT (always last) as the final judge

        If a :attr:`conversation` is attached, each provider's response is
        broadcast so the UI can show the multi-model comparison.

        Args:
            messages: Full message history to send to all providers.
            action_type: Optional filter (e.g. "reasoning", "code_gen").
            num_models: Max providers to query. 0 = all in priority matrix.
            tools: Optional tool schemas (same tools sent to all providers).
            max_tokens: Max tokens per provider completion.

        Returns:
            A :class:`ProbeResponse` with per-provider results in priority order.

        Raises:
            RuntimeError: If the agent has not been registered yet.
            httpx.HTTPStatusError: On non-2xx response from the Go server.
        """
        if not self.token:
            raise RuntimeError(
                f"Agent {self.agent_id} must be registered before probing. "
                "Call await agent.register() first."
            )

        logger.info(
            "Agent %s probing %d models (role=%s, action=%s)",
            self.agent_id, num_models or -1, self.role, action_type or "all",
        )

        probe_resp = await llm_probe(
            messages=messages,
            tools=tools,
            max_tokens=max_tokens,
            go_base_url=self.config.go_base_url,
            agent_token=self.token,
            agent_role=self.role,
            action_type=action_type,
            num_models=num_models,
        )

        # Broadcast each provider's response to the shared conversation.
        if self.conversation:
            for result in probe_resp.successful_results:
                await self.conversation.append_thinking(
                    agent_id=self.agent_id,
                    agent_role=self.role,
                    content=(
                        f"[{result.provider}/{result.model}] "
                        f"({result.latency_ms}ms): {result.content}"
                    ),
                )

        logger.info(
            "Agent %s probe complete: %d/%d providers succeeded in %dms",
            self.agent_id,
            probe_resp.successes,
            probe_resp.providers,
            probe_resp.total_ms,
        )

        return probe_resp

    def pick_best_probe_result(self, probe_resp: ProbeResponse) -> str:
        """Select the best content from a multi-LLM probe response.

        Default strategy: return the first successful result (highest-priority
        provider).  Subclasses can override this for more sophisticated
        selection (e.g. longest response, consensus voting, GPT-as-judge).

        Args:
            probe_resp: The response from :meth:`probe_and_gather`.

        Returns:
            The content string from the best provider, or an empty string
            if all providers failed.
        """
        best = probe_resp.best_result
        if best:
            logger.debug(
                "Agent %s picked probe result from %s/%s (%dms)",
                self.agent_id, best.provider, best.model, best.latency_ms,
            )
            return best.content
        return ""

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
