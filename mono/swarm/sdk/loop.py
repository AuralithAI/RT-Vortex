"""Provider-agnostic agentic loop.

Implements the standard tool-use loop used by every agent role:

1. Send the accumulated message history and tool schemas to the Go LLM proxy.
2. If the response contains ``tool_calls``, execute each one via
   *tool_executor*, append the results, and repeat.
3. If the response contains no ``tool_calls``, the LLM is finished — return
   the full message history.
4. Bail out after *max_turns* round-trips to prevent runaway conversations.

The loop is provider-agnostic because the Go server normalises every LLM
backend (OpenAI, Anthropic, Gemini, Ollama, …) into the OpenAI
chat-completion wire format before returning the response.
"""

from __future__ import annotations

import json
import logging
from typing import Any, Callable, Coroutine

from .go_llm_client import llm_complete
from .tool import ToolDef

logger = logging.getLogger(__name__)


async def agent_loop(
    system_prompt: str,
    tools: list[ToolDef],
    tool_executor: Callable[[str, dict], Coroutine[Any, Any, Any]],
    agent_token: str,
    go_base_url: str | None = None,
    max_turns: int = 25,
    initial_message: str = "",
    agent_role: str = "",
) -> list[dict]:
    """Run the provider-agnostic agentic loop.

    Args:
        system_prompt: System message for the LLM.
        tools: List of ToolDef objects (from @tool decorator).
        tool_executor: Async callable(name, args) → result.
        agent_token: JWT for authenticating with Go.
        go_base_url: Go server URL (defaults to config).
        max_turns: Maximum tool-call round trips.
        initial_message: Optional first user message to kick off the loop.
        agent_role: Agent role hint for smart model routing.

    Returns:
        Full message history.
    """
    messages: list[dict] = [{"role": "system", "content": system_prompt}]

    if initial_message:
        messages.append({"role": "user", "content": initial_message})

    tool_schemas = [t.schema for t in tools]

    for turn in range(max_turns):
        logger.debug("agent_loop turn %d/%d", turn + 1, max_turns)

        response = await llm_complete(
            messages=messages,
            tools=tool_schemas if tool_schemas else None,
            go_base_url=go_base_url,
            agent_token=agent_token,
            agent_role=agent_role,
        )

        # Extract the assistant message from OpenAI-compatible response.
        choices = response.get("choices", [])
        if not choices:
            logger.warning("agent_loop: empty choices in LLM response")
            break

        choice = choices[0]
        message = choice.get("message", {})
        finish_reason = choice.get("finish_reason", "")
        messages.append(message)

        # Handle truncated responses — finish_reason == "length" means the
        # model hit its output token limit before completing the answer.
        # The Go server's smart routing retries with another provider, but
        # if ALL providers truncated (or only one is healthy), the truncated
        # response lands here.  When truncation drops tool calls, we retry
        # the same turn with a nudge so the LLM can finish its work.
        if finish_reason == "length":
            model = response.get("model", "unknown")
            logger.warning(
                "agent_loop: response truncated (finish_reason=length, model=%s, turn=%d)",
                model, turn + 1,
            )

            # If the model was trying to produce tool calls but got cut off,
            # the tool_calls field will be empty/missing.  Nudge the LLM to
            # retry with a shorter output rather than silently dropping it.
            if not message.get("tool_calls"):
                messages.append({
                    "role": "user",
                    "content": (
                        "Your previous response was truncated before you could finish. "
                        "Please try again — be more concise and call the required tool "
                        "directly without lengthy preamble."
                    ),
                })
                logger.info(
                    "agent_loop: nudging LLM to retry after truncation (turn %d)",
                    turn + 1,
                )
                continue  # re-enter the loop for another turn

        # Check for tool calls.
        tool_calls = message.get("tool_calls")
        if not tool_calls:
            # LLM is done — no more tool calls.
            logger.debug("agent_loop: LLM finished (no tool_calls), turn %d", turn + 1)
            break

        # Execute each tool call and append results.
        for tc in tool_calls:
            fn_name = tc["function"]["name"]
            fn_args_str = tc["function"].get("arguments", "{}")
            try:
                fn_args = json.loads(fn_args_str) if isinstance(fn_args_str, str) else fn_args_str
            except json.JSONDecodeError:
                fn_args = {}

            logger.debug("agent_loop: calling tool %s(%s)", fn_name, fn_args)

            try:
                result = await tool_executor(fn_name, fn_args)
            except Exception as e:
                logger.error("agent_loop: tool %s raised %s", fn_name, e)
                result = {"error": str(e)}

            messages.append({
                "role": "tool",
                "tool_call_id": tc["id"],
                "content": json.dumps(result) if not isinstance(result, str) else result,
            })

    return messages
