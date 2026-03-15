"""Engine tools — @tool decorated functions for C++ engine gRPC calls.

These tools are injected into agents so the LLM can search code, read files,
and check index status through the agentic loop.
"""

from __future__ import annotations

import hashlib
import json
import logging
from typing import Any

import redis.asyncio as aioredis

from ..engine_client import EngineClient
from ..sdk.tool import tool

logger = logging.getLogger(__name__)

# Module-level engine client — set via init_engine_tools().
_engine: EngineClient | None = None
_redis: aioredis.Redis | None = None
_SEARCH_CACHE_TTL = 300  # 5 minutes


def init_engine_tools(
    engine_client: EngineClient,
    redis_url: str | None = None,
) -> None:
    """Set the engine client for all engine tools. Call once at startup."""
    global _engine, _redis
    _engine = engine_client
    if redis_url and _redis is None:
        _redis = aioredis.from_url(redis_url, decode_responses=True)


def _get_engine() -> EngineClient:
    if _engine is None:
        raise RuntimeError("Engine tools not initialised — call init_engine_tools() first")
    return _engine


# ── Tools ────────────────────────────────────────────────────────────────────


@tool(description=(
    "Search the codebase for relevant code chunks using semantic search. "
    "Returns ranked code snippets with file paths, line ranges, and scores. "
    "Use this when you need to understand existing code, find patterns, "
    "or locate relevant implementations."
))
async def search_code(query: str, repo_id: str, top_k: int = 10) -> str:
    """Search the codebase for relevant code chunks.

    Args:
        query: Natural language search query describing what you're looking for.
        repo_id: Repository identifier.
        top_k: Maximum number of results to return (default 10).

    Returns:
        JSON string of search results with code chunks, file paths, and scores.
    """
    engine = _get_engine()
    result = await _cached_search(engine, query=query, repo_id=repo_id, top_k=top_k)
    return json.dumps(result, indent=2)


async def _cached_search(
    engine: EngineClient,
    *,
    query: str,
    repo_id: str,
    top_k: int,
) -> Any:
    """Search with optional Redis caching."""
    if _redis is None:
        return await engine.search(query=query, repo_id=repo_id, top_k=top_k)

    key_hash = hashlib.sha256(f"{repo_id}:{query}:{top_k}".encode()).hexdigest()[:16]
    cache_key = f"search:{repo_id}:{key_hash}"

    try:
        cached = await _redis.get(cache_key)
        if cached:
            return json.loads(cached)
    except Exception:
        pass

    result = await engine.search(query=query, repo_id=repo_id, top_k=top_k)

    try:
        await _redis.setex(cache_key, _SEARCH_CACHE_TTL, json.dumps(result))
    except Exception:
        pass

    return result


@tool(description=(
    "Get the full content of a specific file from the repository. "
    "Use this when you need to read an entire file to understand its structure, "
    "or when you need the original content to produce a diff."
))
async def get_file_content(repo_id: str, file_path: str, ref: str = "") -> str:
    """Read a file's full content from the engine's local clone.

    Args:
        repo_id: Repository identifier.
        file_path: Path to the file within the repository.
        ref: Optional git ref (branch, tag, commit). Empty = HEAD.

    Returns:
        JSON string with file content, encoding, and is_binary flag.
    """
    engine = _get_engine()
    result = await engine.get_file_content(repo_id=repo_id, file_path=file_path, ref=ref)
    return json.dumps(result, indent=2)


@tool(description=(
    "Check the indexing status for a repository in the engine. "
    "Returns whether the repo is indexed and how many chunks/files are available. "
    "Use this before searching to verify the repo is ready."
))
async def get_index_status(repo_id: str) -> str:
    """Check whether a repository has been indexed by the engine.

    Args:
        repo_id: Repository identifier.

    Returns:
        JSON string with indexed flag, total_chunks, and total_files.
    """
    engine = _get_engine()
    result = await engine.get_index_status(repo_id=repo_id)
    return json.dumps(result, indent=2)


@tool(description=(
    "Search for specific function or class callers/usages in the codebase. "
    "Use this when you need to find how a function is called, where a class "
    "is instantiated, or trace data flow through the codebase."
))
async def find_callers(query: str, repo_id: str, symbol_name: str) -> str:
    """Search for callers and usages of a symbol in the codebase.

    This uses the engine's semantic search with a caller-focused query.

    Args:
        query: Context about what callers you're looking for.
        repo_id: Repository identifier.
        symbol_name: The function, class, or variable name to find usages of.

    Returns:
        JSON string of search results focused on call sites.
    """
    engine = _get_engine()
    caller_query = f"calls to {symbol_name}: {query}"
    result = await _cached_search(engine, query=caller_query, repo_id=repo_id, top_k=15)
    return json.dumps(result, indent=2)


# ── Collect all tools ────────────────────────────────────────────────────────

ENGINE_TOOLS = [search_code, get_file_content, get_index_status, find_callers]
