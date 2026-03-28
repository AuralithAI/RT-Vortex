"""Shared conversation — inter-agent communication log.

All agents on a task share a single :class:`SharedConversation`.  Each agent
appends its thinking, tool calls, and results.  Other agents can read the
conversation to see what their teammates have done.

Every message is also forwarded to the Go server (``POST
/internal/swarm/tasks/{id}/agent-message``), which broadcasts it via
WebSocket so the browser UI can show a live "agents talking" feed.
"""

from __future__ import annotations

import asyncio
import logging
import time
from dataclasses import dataclass, field
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

        return "\n".join(lines)

    @property
    def message_count(self) -> int:
        return len(self._messages)


def _truncate_args(args: dict[str, Any], max_val_len: int = 100) -> dict[str, Any]:
    """Truncate long argument values for metadata (avoid bloating WS messages)."""
    result: dict[str, Any] = {}
    for k, v in args.items():
        if isinstance(v, str) and len(v) > max_val_len:
            result[k] = v[:max_val_len] + "…"
        else:
            result[k] = v
    return result
