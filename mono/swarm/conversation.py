"""Shared conversation — inter-agent communication log.

All agents on a task share a single :class:`SharedConversation`.  Each agent
appends its thinking, tool calls, and results.  Other agents can read the
conversation to see what their teammates have done.

Every message is also forwarded to the Go server (``POST
/internal/swarm/tasks/{id}/agent-message``), which broadcasts it via
WebSocket so the browser UI can show a live "agents talking" feed.

Multi-LLM Discussion Protocol:
    When an agent probes multiple LLMs, the responses are organised into a
    :class:`DiscussionThread`.  Each thread groups per-provider responses
    under a named topic, tracks which providers participated, and records
    a final synthesis / selected answer.  The Go server broadcasts
    ``swarm_discussion`` events so the UI can render multi-model panels.
"""

from __future__ import annotations

import asyncio
import logging
import time
import uuid as _uuid
from dataclasses import dataclass, field
from enum import Enum
from typing import Any

logger = logging.getLogger(__name__)


@dataclass
class AgentMessage:
    """A single message in the shared agent conversation."""

    agent_id: str
    agent_role: str
    kind: str  # "thinking", "tool_call", "tool_result", "edit", "error"
    content: str
    metadata: dict[str, Any] = field(default_factory=dict)
    timestamp: float = field(default_factory=time.time)

    def to_dict(self) -> dict[str, Any]:
        return {
            "agent_id": self.agent_id,
            "agent_role": self.agent_role,
            "kind": self.kind,
            "content": self.content,
            "metadata": self.metadata,
            "timestamp": self.timestamp,
        }


# ── Multi-LLM Discussion Protocol ──────────────────────────────────────────


class DiscussionStatus(str, Enum):
    """Lifecycle states for a discussion thread."""

    OPEN = "open"               # Providers are still responding
    COMPLETE = "complete"       # All providers responded, no synthesis yet
    SYNTHESISED = "synthesised" # Final answer selected/merged


@dataclass
class ProviderResponse:
    """A single LLM provider's response within a discussion thread.

    Attributes:
        provider: Provider name (e.g. ``"grok"``, ``"anthropic"``).
        model: Model identifier returned by the provider.
        content: The assistant's text response.
        latency_ms: Time taken for this provider's response in milliseconds.
        finish_reason: Completion finish reason (``"stop"``, ``"length"``).
        token_usage: Token usage dict ``{prompt_tokens, completion_tokens, total_tokens}``.
        error: Non-empty string if this provider failed.
        timestamp: When this response was recorded.
    """

    provider: str
    model: str = ""
    content: str = ""
    latency_ms: int = 0
    finish_reason: str = ""
    token_usage: dict[str, int] = field(default_factory=dict)
    error: str = ""
    timestamp: float = field(default_factory=time.time)

    @property
    def succeeded(self) -> bool:
        return not self.error

    def to_dict(self) -> dict[str, Any]:
        return {
            "provider": self.provider,
            "model": self.model,
            "content": self.content,
            "latency_ms": self.latency_ms,
            "finish_reason": self.finish_reason,
            "token_usage": self.token_usage,
            "error": self.error,
            "timestamp": self.timestamp,
        }


@dataclass
class DiscussionThread:
    """A multi-LLM discussion thread within a shared conversation.

    When an agent probes multiple LLMs (via :meth:`Agent.probe_and_gather`),
    the responses are grouped into a DiscussionThread.  This gives the UI a
    structured way to render "Model A said X, Model B said Y" panels, and
    gives the consensus engine (Phase 5) a clean input.

    Attributes:
        thread_id: Unique identifier for this discussion.
        agent_id: The agent that initiated the probe.
        agent_role: The role of the initiating agent.
        topic: A short description of what was being asked (e.g. first user message).
        action_type: Optional action type filter used in the probe.
        responses: Ordered list of per-provider responses (priority order).
        status: Current lifecycle state.
        synthesis: The final selected/merged answer (set after consensus).
        synthesis_provider: Which provider's answer was chosen (if pick-best).
        created_at: When the thread was created.
        completed_at: When the thread reached ``COMPLETE`` or ``SYNTHESISED``.
    """

    thread_id: str = field(default_factory=lambda: _uuid.uuid4().hex[:16])
    agent_id: str = ""
    agent_role: str = ""
    topic: str = ""
    action_type: str = ""
    responses: list[ProviderResponse] = field(default_factory=list)
    status: DiscussionStatus = DiscussionStatus.OPEN
    synthesis: str = ""
    synthesis_provider: str = ""
    created_at: float = field(default_factory=time.time)
    completed_at: float = 0.0

    @property
    def successful_responses(self) -> list[ProviderResponse]:
        """Return only the responses that succeeded (no error)."""
        return [r for r in self.responses if r.succeeded]

    @property
    def provider_count(self) -> int:
        return len(self.responses)

    @property
    def success_count(self) -> int:
        return len(self.successful_responses)

    @property
    def all_contents(self) -> list[str]:
        """Return content strings from all successful providers."""
        return [r.content for r in self.responses if r.succeeded and r.content]

    def add_response(self, resp: ProviderResponse) -> None:
        """Add a provider's response to the thread."""
        self.responses.append(resp)

    def complete(self) -> None:
        """Mark the thread as complete (all providers have responded)."""
        self.status = DiscussionStatus.COMPLETE
        self.completed_at = time.time()

    def synthesise(self, content: str, provider: str = "") -> None:
        """Record the final synthesis / selected answer.

        Args:
            content: The synthesised or selected answer text.
            provider: If a specific provider was chosen, its name.
        """
        self.synthesis = content
        self.synthesis_provider = provider
        self.status = DiscussionStatus.SYNTHESISED
        self.completed_at = time.time()

    def to_dict(self) -> dict[str, Any]:
        return {
            "thread_id": self.thread_id,
            "agent_id": self.agent_id,
            "agent_role": self.agent_role,
            "topic": self.topic,
            "action_type": self.action_type,
            "responses": [r.to_dict() for r in self.responses],
            "status": self.status.value,
            "synthesis": self.synthesis,
            "synthesis_provider": self.synthesis_provider,
            "provider_count": self.provider_count,
            "success_count": self.success_count,
            "created_at": self.created_at,
            "completed_at": self.completed_at,
        }

    def summary_for_prompt(self, max_content_len: int = 500) -> str:
        """Build a text summary suitable for injection into an LLM prompt.

        Shows each provider's answer (truncated) so an agent or consensus
        engine can compare them.
        """
        lines = [f"### Multi-LLM Discussion: {self.topic}"]
        for r in self.responses:
            status = "✅" if r.succeeded else "❌"
            label = f"{r.provider}/{r.model}" if r.model else r.provider
            if r.succeeded:
                content = r.content[:max_content_len]
                if len(r.content) > max_content_len:
                    content += "…"
                lines.append(f"{status} **{label}** ({r.latency_ms}ms): {content}")
            else:
                lines.append(f"{status} **{label}**: {r.error}")

        if self.synthesis:
            lines.append(f"\n**Selected answer** ({self.synthesis_provider or 'consensus'}): {self.synthesis[:max_content_len]}")

        return "\n".join(lines)


class SharedConversation:
    """Thread-safe conversation shared by all agents on a task.

    Messages are stored in order and also forwarded to Go for WebSocket
    broadcast to the browser.

    Usage::

        conv = SharedConversation(task_id="...", go_client=go_client)
        await conv.append(AgentMessage(
            agent_id="sr-abc",
            agent_role="senior_dev",
            kind="thinking",
            content="Reading the auth module to understand the login flow...",
        ))

        # Another agent reads what happened:
        context = conv.get_summary_for("qa")
    """

    def __init__(self, task_id: str, go_client: Any):
        self.task_id = task_id
        self._go = go_client
        self._messages: list[AgentMessage] = []
        self._discussions: dict[str, DiscussionThread] = {}  # thread_id → thread
        self._lock = asyncio.Lock()

    async def append(self, msg: AgentMessage) -> None:
        """Append a message and broadcast it to the UI."""
        async with self._lock:
            self._messages.append(msg)

        # Fire-and-forget broadcast to Go → WebSocket → browser.
        try:
            await self._go.post_agent_message(self.task_id, msg.to_dict())
        except Exception as e:
            logger.debug("Failed to broadcast agent message: %s", e)

    async def append_thinking(
        self, agent_id: str, agent_role: str, content: str
    ) -> None:
        """Shorthand for appending a 'thinking' message."""
        await self.append(AgentMessage(
            agent_id=agent_id,
            agent_role=agent_role,
            kind="thinking",
            content=content,
        ))

    async def append_tool_call(
        self,
        agent_id: str,
        agent_role: str,
        tool_name: str,
        tool_args: dict[str, Any],
    ) -> None:
        """Shorthand for appending a 'tool_call' message."""
        # Build a concise description.
        if tool_name in ("read_file", "workspace_read_file"):
            desc = f"Reading {tool_args.get('path', '?')}"
        elif tool_name in ("edit_file", "workspace_edit_file"):
            desc = f"Editing {tool_args.get('path', '?')}"
        elif tool_name in ("create_file", "workspace_create_file"):
            desc = f"Creating {tool_args.get('path', '?')}"
        elif tool_name in ("search_code", "workspace_search"):
            desc = f"Searching: {tool_args.get('query', '?')}"
        elif tool_name == "workspace_list_dir":
            desc = f"Listing {tool_args.get('path', '/')}"
        elif tool_name == "workspace_grep":
            desc = f"Grep: {tool_args.get('pattern', '?')}"
        elif tool_name == "mcp_call":
            desc = f"MCP: {tool_args.get('provider', '?')}/{tool_args.get('action', '?')}"
        elif tool_name == "mcp_list_tools":
            desc = "Listing MCP providers"
        elif tool_name == "mcp_list_connections":
            desc = "Listing MCP connections"
        elif tool_name == "mcp_describe_action":
            desc = f"Describing {tool_args.get('provider', '?')}/{tool_args.get('action', '?')}"
        else:
            desc = f"Calling {tool_name}"

        await self.append(AgentMessage(
            agent_id=agent_id,
            agent_role=agent_role,
            kind="tool_call",
            content=desc,
            metadata={"tool": tool_name, "args": _truncate_args(tool_args)},
        ))

    async def append_edit(
        self,
        agent_id: str,
        agent_role: str,
        path: str,
        change_type: str,
    ) -> None:
        """Shorthand for appending an 'edit' message."""
        action_map = {
            "modified": "Edited",
            "added": "Created",
            "deleted": "Deleted",
        }
        action = action_map.get(change_type, "Changed")
        await self.append(AgentMessage(
            agent_id=agent_id,
            agent_role=agent_role,
            kind="edit",
            content=f"{action} {path}",
            metadata={"path": path, "change_type": change_type},
        ))

    def get_messages(self) -> list[AgentMessage]:
        """Return all messages (snapshot — safe to iterate without lock)."""
        return list(self._messages)

    def get_summary_for(self, agent_role: str, max_messages: int = 50) -> str:
        """Build a text summary of the conversation for injection into a system prompt.

        Returns the most recent *max_messages* messages formatted as a
        readable transcript.
        """
        msgs = self._messages[-max_messages:]
        if not msgs:
            return ""

        lines: list[str] = ["## Team Conversation So Far"]
        for m in msgs:
            role_label = m.agent_role.replace("_", " ").title()
            if m.kind == "thinking":
                lines.append(f"**{role_label}** ({m.agent_id[:8]}): {m.content}")
            elif m.kind == "tool_call":
                lines.append(f"**{role_label}** ({m.agent_id[:8]}): 🔧 {m.content}")
            elif m.kind == "edit":
                lines.append(f"**{role_label}** ({m.agent_id[:8]}): ✏️ {m.content}")
            elif m.kind == "error":
                lines.append(f"**{role_label}** ({m.agent_id[:8]}): ❌ {m.content}")
            elif m.kind == "tool_result":
                # Keep tool results brief in the summary.
                short = m.content[:200] + "…" if len(m.content) > 200 else m.content
                lines.append(f"  → {short}")
            elif m.kind == "probe_response":
                # Multi-LLM probe — show provider and truncated content.
                provider = m.metadata.get("provider", "?")
                model = m.metadata.get("model", "")
                label = f"{provider}/{model}" if model else provider
                short = m.content[:200] + "…" if len(m.content) > 200 else m.content
                lines.append(f"**{role_label}** ({m.agent_id[:8]}): 🔀 [{label}] {short}")
            elif m.kind == "discussion_synthesis":
                lines.append(f"**{role_label}** ({m.agent_id[:8]}): 🏆 {m.content}")

        return "\n".join(lines)

    # ── Discussion Thread Protocol ──────────────────────────────────────

    async def open_discussion(
        self,
        agent_id: str,
        agent_role: str,
        topic: str,
        action_type: str = "",
    ) -> DiscussionThread:
        """Create a new multi-LLM discussion thread.

        Call this before probing multiple LLMs.  Each provider's response
        is then added via :meth:`add_discussion_response`.

        Args:
            agent_id: The agent initiating the probe.
            agent_role: The agent's role.
            topic: Short description of what's being asked.
            action_type: Optional action type filter.

        Returns:
            A new :class:`DiscussionThread` tracked by this conversation.
        """
        thread = DiscussionThread(
            agent_id=agent_id,
            agent_role=agent_role,
            topic=topic,
            action_type=action_type,
        )

        async with self._lock:
            self._discussions[thread.thread_id] = thread

        # Broadcast thread creation to Go → WebSocket → browser.
        try:
            await self._go.post_discussion_event(self.task_id, {
                "event": "thread_opened",
                "thread": thread.to_dict(),
            })
        except Exception as e:
            logger.debug("Failed to broadcast discussion open: %s", e)

        logger.info(
            "Discussion thread %s opened by %s/%s: %s",
            thread.thread_id, agent_role, agent_id[:8], topic,
        )
        return thread

    async def add_discussion_response(
        self,
        thread_id: str,
        provider: str,
        model: str,
        content: str,
        latency_ms: int = 0,
        finish_reason: str = "",
        token_usage: dict[str, int] | None = None,
        error: str = "",
    ) -> ProviderResponse | None:
        """Add a provider's response to an existing discussion thread.

        Also appends a ``probe_response`` message to the flat message log
        so ``get_summary_for`` includes it.

        Args:
            thread_id: The discussion thread to add to.
            provider: Provider name.
            model: Model identifier.
            content: The assistant's response text.
            latency_ms: Response latency in milliseconds.
            finish_reason: Completion finish reason.
            token_usage: Token usage dict.
            error: Non-empty if this provider failed.

        Returns:
            The :class:`ProviderResponse` that was added, or ``None`` if
            the thread_id was not found.
        """
        resp = ProviderResponse(
            provider=provider,
            model=model,
            content=content,
            latency_ms=latency_ms,
            finish_reason=finish_reason,
            token_usage=token_usage or {},
            error=error,
        )

        async with self._lock:
            thread = self._discussions.get(thread_id)
            if thread is None:
                logger.warning("Discussion thread %s not found", thread_id)
                return None
            thread.add_response(resp)

        # Also append to the flat message log for summary inclusion.
        if not error:
            await self.append(AgentMessage(
                agent_id=thread.agent_id,
                agent_role=thread.agent_role,
                kind="probe_response",
                content=content,
                metadata={
                    "thread_id": thread_id,
                    "provider": provider,
                    "model": model,
                    "latency_ms": latency_ms,
                },
            ))

        # Broadcast to UI.
        try:
            await self._go.post_discussion_event(self.task_id, {
                "event": "provider_response",
                "thread_id": thread_id,
                "response": resp.to_dict(),
            })
        except Exception as e:
            logger.debug("Failed to broadcast discussion response: %s", e)

        return resp

    async def complete_discussion(self, thread_id: str) -> DiscussionThread | None:
        """Mark a discussion thread as complete (all providers responded).

        Returns:
            The completed :class:`DiscussionThread`, or ``None`` if not found.
        """
        async with self._lock:
            thread = self._discussions.get(thread_id)
            if thread is None:
                return None
            thread.complete()

        try:
            await self._go.post_discussion_event(self.task_id, {
                "event": "thread_completed",
                "thread_id": thread_id,
                "provider_count": thread.provider_count,
                "success_count": thread.success_count,
            })
        except Exception as e:
            logger.debug("Failed to broadcast discussion complete: %s", e)

        return thread

    async def synthesise_discussion(
        self,
        thread_id: str,
        content: str,
        provider: str = "",
    ) -> DiscussionThread | None:
        """Record the final synthesis / selected answer for a discussion.

        Args:
            thread_id: The discussion thread.
            content: The synthesised or selected answer.
            provider: If a specific provider was chosen, its name.

        Returns:
            The synthesised :class:`DiscussionThread`, or ``None`` if not found.
        """
        async with self._lock:
            thread = self._discussions.get(thread_id)
            if thread is None:
                return None
            thread.synthesise(content, provider)

        # Append a synthesis message to the flat log.
        await self.append(AgentMessage(
            agent_id=thread.agent_id,
            agent_role=thread.agent_role,
            kind="discussion_synthesis",
            content=content,
            metadata={
                "thread_id": thread_id,
                "synthesis_provider": provider,
            },
        ))

        try:
            await self._go.post_discussion_event(self.task_id, {
                "event": "thread_synthesised",
                "thread_id": thread_id,
                "synthesis": content,
                "synthesis_provider": provider,
            })
        except Exception as e:
            logger.debug("Failed to broadcast discussion synthesis: %s", e)

        return thread

    def get_discussion(self, thread_id: str) -> DiscussionThread | None:
        """Get a discussion thread by ID."""
        return self._discussions.get(thread_id)

    def get_discussions(self) -> list[DiscussionThread]:
        """Return all discussion threads (snapshot)."""
        return list(self._discussions.values())

    def get_discussion_summary(self, thread_id: str) -> str:
        """Get a prompt-ready summary of a specific discussion thread.

        Returns an empty string if the thread is not found.
        """
        thread = self._discussions.get(thread_id)
        if thread is None:
            return ""
        return thread.summary_for_prompt()

    @property
    def message_count(self) -> int:
        return len(self._messages)

    @property
    def discussion_count(self) -> int:
        """Number of discussion threads in this conversation."""
        return len(self._discussions)


def _truncate_args(args: dict[str, Any], max_val_len: int = 100) -> dict[str, Any]:
    """Truncate long argument values for metadata (avoid bloating WS messages)."""
    result: dict[str, Any] = {}
    for k, v in args.items():
        if isinstance(v, str) and len(v) > max_val_len:
            result[k] = v[:max_val_len] + "…"
        else:
            result[k] = v
    return result
