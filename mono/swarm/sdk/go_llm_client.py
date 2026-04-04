"""Go LLM proxy client — sends completion requests to the Go API server.

All LLM calls flow through Go's ``/internal/swarm/llm/complete`` endpoint.
Python never imports any provider SDK (no ``openai``, ``anthropic``, etc.).

The Go server's ``llm.Registry`` selects the configured provider (OpenAI,
Anthropic, Gemini, Grok, Ollama …), calls it with provider-specific logic,
and returns the result normalised to the **OpenAI chat-completion wire format**
— the de facto industry standard that every major provider supports or that
the Go adapter translates to.  This lets us parse one consistent JSON shape
on the Python side regardless of which backend is active.

Multi-LLM probe support:
    :func:`llm_probe` sends the same messages to multiple LLM providers in
    parallel via ``/internal/swarm/llm/probe``.  Each provider's response is
    returned separately so the agent (or consensus engine) can compare,
    synthesise, or pick the best answer.
"""

from __future__ import annotations

import logging
from dataclasses import dataclass, field
from typing import Any

import httpx

from ..agents_config import get_config

logger = logging.getLogger(__name__)


async def llm_complete(
    messages: list[dict],
    tools: list[dict] | None = None,
    max_tokens: int | None = None,
    *,
    go_base_url: str | None = None,
    agent_token: str = "",
    agent_role: str = "",
) -> dict:
    """Send a chat-completion request via the Go LLM proxy.

    The Go server selects the active LLM provider, calls it, and normalises
    the response to the standard chat-completion shape::

        {
            "id": "...",
            "choices": [{"index": 0, "message": {"role": "assistant", ...}}],
            "usage": {"prompt_tokens": ..., "completion_tokens": ..., ...}
        }

    When ``agent_role`` is provided, the Go server uses role-based model
    routing to pick the best provider for the task (e.g. orchestrator gets the
    strongest model, junior_dev gets a fast/cheap one).  If the chosen model
    truncates the response, the server automatically retries with the next
    provider in the fallback chain.

    Args:
        messages: Conversation history (system, user, assistant, tool roles).
        tools: Optional list of tool schemas in function-calling format.
        max_tokens: Maximum tokens for the completion.
        go_base_url: Go server base URL. Falls back to config.
        agent_token: JWT obtained during agent registration.
        agent_role: Agent role hint for smart model routing (e.g. "orchestrator").

    Returns:
        Parsed JSON response dict.

    Raises:
        httpx.HTTPStatusError: On non-2xx response from the Go proxy.
    """
    cfg = get_config()
    url = (go_base_url or cfg.go_server_url) + "/internal/swarm/llm/complete"
    payload: dict = {
        "messages": messages,
        "max_tokens": max_tokens or cfg.llm_max_tokens,
    }
    if tools:
        payload["tools"] = tools
    if agent_role:
        payload["agent_role"] = agent_role

    async with httpx.AsyncClient(timeout=cfg.llm_timeout) as client:
        resp = await client.post(
            url,
            headers={"Authorization": f"Bearer {agent_token}"},
            json=payload,
        )
        resp.raise_for_status()
        return resp.json()


# ── Multi-LLM Probe Types ───────────────────────────────────────────────────


@dataclass
class ProbeResultItem:
    """A single provider's response from a multi-LLM probe.

    Attributes:
        provider: Provider name (e.g. ``"grok"``, ``"anthropic"``).
        model: Model identifier returned by the provider.
        content: The assistant's text response.
        finish_reason: Completion finish reason (``"stop"``, ``"length"``).
        tool_calls: Tool call requests from the provider, if any.
        usage: Token usage dict ``{prompt_tokens, completion_tokens, total_tokens}``.
        latency_ms: Time taken for this provider's response in milliseconds.
        error: Non-empty string if this provider failed.
    """

    provider: str = ""
    model: str = ""
    content: str = ""
    finish_reason: str = ""
    tool_calls: list[dict] = field(default_factory=list)
    usage: dict[str, int] = field(default_factory=dict)
    latency_ms: int = 0
    error: str = ""

    @property
    def succeeded(self) -> bool:
        """True if this provider returned a successful response."""
        return not self.error

    @classmethod
    def from_dict(cls, d: dict[str, Any]) -> "ProbeResultItem":
        """Construct from the JSON dict returned by the Go probe endpoint."""
        return cls(
            provider=d.get("provider", ""),
            model=d.get("model", ""),
            content=d.get("content", ""),
            finish_reason=d.get("finish_reason", ""),
            tool_calls=d.get("tool_calls") or [],
            usage=d.get("usage", {}),
            latency_ms=d.get("latency_ms", 0),
            error=d.get("error", ""),
        )


@dataclass
class ProbeResponse:
    """Aggregated response from a multi-LLM probe.

    Attributes:
        results: Ordered list of per-provider results (priority order).
        total_ms: Wall-clock time for the entire probe.
        providers: Number of providers probed.
        successes: Number of providers that responded successfully.
        agent_role: The agent role that was probed.
    """

    results: list[ProbeResultItem] = field(default_factory=list)
    total_ms: int = 0
    providers: int = 0
    successes: int = 0
    agent_role: str = ""

    @classmethod
    def from_dict(cls, d: dict[str, Any]) -> "ProbeResponse":
        """Construct from the JSON dict returned by the Go probe endpoint."""
        results = [ProbeResultItem.from_dict(r) for r in d.get("results", [])]
        return cls(
            results=results,
            total_ms=d.get("total_ms", 0),
            providers=d.get("providers", 0),
            successes=d.get("successes", 0),
            agent_role=d.get("agent_role", ""),
        )

    @property
    def successful_results(self) -> list[ProbeResultItem]:
        """Return only the results that succeeded (no error)."""
        return [r for r in self.results if r.succeeded]

    @property
    def best_result(self) -> ProbeResultItem | None:
        """Return the first successful result (highest priority provider).

        The Go server returns results in priority-matrix order, so the first
        successful result is from the highest-priority provider.
        """
        for r in self.results:
            if r.succeeded:
                return r
        return None

    @property
    def all_contents(self) -> list[str]:
        """Return content strings from all successful providers."""
        return [r.content for r in self.results if r.succeeded and r.content]


# ── Probe Function ───────────────────────────────────────────────────────────


async def llm_probe(
    messages: list[dict],
    tools: list[dict] | None = None,
    max_tokens: int | None = None,
    *,
    go_base_url: str | None = None,
    agent_token: str = "",
    agent_role: str = "",
    action_type: str = "",
    num_models: int = 0,
) -> ProbeResponse:
    """Send a multi-LLM probe via the Go probe endpoint.

    Queries multiple LLM providers in parallel for the same messages.
    Each provider's response is returned separately so the agent or
    consensus engine can compare, synthesise, or pick the best answer.

    The Go server uses the priority matrix (Phase 1) to determine which
    providers to query and in what order.  GPT/OpenAI is always last.

    Args:
        messages: Conversation history (system, user, assistant, tool roles).
        tools: Optional list of tool schemas in function-calling format.
        max_tokens: Maximum tokens for each provider's completion.
        go_base_url: Go server base URL. Falls back to config.
        agent_token: JWT obtained during agent registration.
        agent_role: Agent role for priority matrix lookup (e.g. "orchestrator").
        action_type: Optional action type filter (e.g. "reasoning", "code_gen").
        num_models: Max providers to probe. 0 means all configured providers.

    Returns:
        A :class:`ProbeResponse` with all provider results in priority order.

    Raises:
        httpx.HTTPStatusError: On non-2xx response from the Go probe endpoint.
    """
    cfg = get_config()
    url = (go_base_url or cfg.go_server_url) + "/internal/swarm/llm/probe"

    payload: dict[str, Any] = {
        "messages": messages,
        "max_tokens": max_tokens or cfg.llm_max_tokens,
    }
    if tools:
        payload["tools"] = tools
    if agent_role:
        payload["agent_role"] = agent_role
    if action_type:
        payload["action_type"] = action_type
    if num_models > 0:
        payload["num_models"] = num_models

    logger.info(
        "llm_probe: requesting multi-LLM probe (role=%s, action=%s, num_models=%d, messages=%d)",
        agent_role, action_type or "all", num_models, len(messages),
    )

    # Probe can take a while — use a generous timeout (5 min matches Go side).
    timeout = max(cfg.llm_timeout, 300.0)

    async with httpx.AsyncClient(timeout=timeout) as client:
        resp = await client.post(
            url,
            headers={"Authorization": f"Bearer {agent_token}"},
            json=payload,
        )
        resp.raise_for_status()
        data = resp.json()

    probe_resp = ProbeResponse.from_dict(data)

    logger.info(
        "llm_probe: completed (providers=%d, successes=%d, total_ms=%d)",
        probe_resp.providers, probe_resp.successes, probe_resp.total_ms,
    )

    return probe_resp
