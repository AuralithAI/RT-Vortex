"""Go LLM proxy client — sends completion requests to the Go API server.

All LLM calls flow through Go's ``/internal/swarm/llm/complete`` endpoint.
Python never imports any provider SDK (no ``openai``, ``anthropic``, etc.).

The Go server's ``llm.Registry`` selects the configured provider (OpenAI,
Anthropic, Gemini, Grok, Ollama …), calls it with provider-specific logic,
and returns the result normalised to the **OpenAI chat-completion wire format**
— the de facto industry standard that every major provider supports or that
the Go adapter translates to.  This lets us parse one consistent JSON shape
on the Python side regardless of which backend is active.
"""

from __future__ import annotations

import httpx

from ..agents_config import get_config


async def llm_complete(
    messages: list[dict],
    tools: list[dict] | None = None,
    max_tokens: int | None = None,
    *,
    go_base_url: str | None = None,
    agent_token: str = "",
) -> dict:
    """Send a chat-completion request via the Go LLM proxy.

    The Go server selects the active LLM provider, calls it, and normalises
    the response to the standard chat-completion shape::

        {
            "id": "...",
            "choices": [{"index": 0, "message": {"role": "assistant", ...}}],
            "usage": {"prompt_tokens": ..., "completion_tokens": ..., ...}
        }

    Args:
        messages: Conversation history (system, user, assistant, tool roles).
        tools: Optional list of tool schemas in function-calling format.
        max_tokens: Maximum tokens for the completion.
        go_base_url: Go server base URL. Falls back to config.
        agent_token: JWT obtained during agent registration.

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

    async with httpx.AsyncClient(timeout=cfg.llm_timeout) as client:
        resp = await client.post(
            url,
            headers={"Authorization": f"Bearer {agent_token}"},
            json=payload,
        )
        resp.raise_for_status()
        return resp.json()
