"""Decorator that converts async Python functions into LLM tool schemas.

The ``@tool`` decorator inspects a function's type hints and docstring to
generate an `OpenAI function-calling schema
<https://platform.openai.com/docs/guides/function-calling>`_. The resulting
:class:`ToolDef` pairs the schema with the original callable so the agentic
loop (:mod:`~mono.swarm.sdk.loop`) can advertise available tools and dispatch
calls without manual JSON authoring.

Example::

    @tool(description="Search the codebase for relevant code")
    async def search_code(query: str, repo_id: str, top_k: int = 10) -> str:
        ...
"""

from __future__ import annotations

import inspect
import typing
from dataclasses import dataclass, field
from typing import Any, Callable, Coroutine


# ── Type mapping ─────────────────────────────────────────────────────────────

_TYPE_MAP: dict[type, str] = {
    str: "string",
    int: "integer",
    float: "number",
    bool: "boolean",
    list: "array",
    dict: "object",
}


def _python_type_to_json(annotation: Any) -> str:
    """Map a Python type hint to a JSON Schema type string.

    Generic origins (``list[str]``, ``dict[str, Any]``) are collapsed to their
    base JSON Schema type.  Unknown annotations fall back to ``"string"``.
    """
    origin = typing.get_origin(annotation)
    if origin is not None:
        # Handle generic types like list[str], dict[str, Any], etc.
        if origin in (list, typing.List):
            return "array"
        return "object"
    return _TYPE_MAP.get(annotation, "string")


# ── ToolDef ──────────────────────────────────────────────────────────────────

@dataclass
class ToolDef:
    """A registered tool: its JSON schema plus the underlying async callable.

    Instances are created by the :func:`tool` decorator and consumed by
    :func:`~mono.swarm.sdk.loop.agent_loop`, which serialises ``schema`` for
    the LLM and dispatches calls to ``fn``.
    """

    name: str
    description: str
    fn: Callable[..., Coroutine]
    schema: dict = field(default_factory=dict)


# ── @tool decorator ─────────────────────────────────────────────────────────

def tool(description: str = "") -> Callable:
    """Decorator that converts an async function into a ToolDef.

    Usage::

        @tool(description="Search the codebase for relevant code")
        async def search_code(query: str, repo_id: str) -> str:
            ...

    The decorator generates an OpenAI function-calling schema from the
    function's type hints and docstring.
    """
    def decorator(fn: Callable) -> ToolDef:
        sig = inspect.signature(fn)
        hints = typing.get_type_hints(fn)

        # Build JSON Schema for parameters.
        properties: dict[str, dict] = {}
        required: list[str] = []

        for param_name, param in sig.parameters.items():
            if param_name in ("self", "cls"):
                continue

            param_type = hints.get(param_name, str)
            json_type = _python_type_to_json(param_type)
            properties[param_name] = {"type": json_type}

            if param.default is inspect.Parameter.empty:
                required.append(param_name)

        desc = description or fn.__doc__ or f"Tool: {fn.__name__}"

        schema = {
            "type": "function",
            "function": {
                "name": fn.__name__,
                "description": desc,
                "parameters": {
                    "type": "object",
                    "properties": properties,
                    "required": required,
                },
            },
        }

        return ToolDef(
            name=fn.__name__,
            description=desc,
            fn=fn,
            schema=schema,
        )

    return decorator
