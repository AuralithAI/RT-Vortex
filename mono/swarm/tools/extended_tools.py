"""Extended tool suite for swarm agents."""

from __future__ import annotations

import asyncio
import json
import logging
from typing import Any

from ..sdk.tool import tool

logger = logging.getLogger(__name__)

# ── Module-level clients (set via init_extended_tools) ───────────────────────

_go_client = None
_engine_client = None
_agent_memory = None
_redis_url: str = ""


def init_extended_tools(
    go_client: Any = None,
    engine_client: Any = None,
    agent_memory: Any = None,
    redis_url: str = "",
) -> None:
    global _go_client, _engine_client, _agent_memory, _redis_url
    _go_client = go_client
    _engine_client = engine_client
    _agent_memory = agent_memory
    _redis_url = redis_url


@tool()
async def run_tests(
    test_command: str = "",
    test_files: str = "",
    timeout_seconds: int = 120,
) -> str:
    """Run the repository's test suite or specific test files."""
    if not _go_client:
        return "Error: Go client not available — cannot run tests."

    try:
        result = await _go_client.run_ci_command(
            command_type="test",
            command=test_command,
            files=test_files.split(",") if test_files else [],
            timeout=timeout_seconds,
        )
        return result.get("output", "No output")
    except Exception as e:
        return f"Test execution failed: {e}"


@tool()
async def run_build(
    build_command: str = "",
    timeout_seconds: int = 180,
) -> str:
    """Run the repository's build or lint command."""
    if not _go_client:
        return "Error: Go client not available — cannot run build."

    try:
        result = await _go_client.run_ci_command(
            command_type="build",
            command=build_command,
            files=[],
            timeout=timeout_seconds,
        )
        return result.get("output", "No output")
    except Exception as e:
        return f"Build failed: {e}"


@tool()
async def git_diff() -> str:
    """Get the current workspace diff as unified diff text."""
    from .workspace_tools import _get_workspace
    ws = _get_workspace()
    if ws is None:
        return "No workspace available."

    changeset = ws.get_changeset()
    if not changeset:
        return "No changes in workspace."

    diffs = []
    for change in changeset:
        diffs.append(f"--- a/{change['file_path']}")
        diffs.append(f"+++ b/{change['file_path']}")
        diffs.append(change.get("unified_diff", "(no diff available)"))
        diffs.append("")

    return "\n".join(diffs)


@tool()
async def search_knowledge_graph(
    query: str,
    relationship_type: str = "",
    max_hops: int = 2,
    top_k: int = 10,
) -> str:
    """Search the repository's knowledge graph for relationship-aware results."""
    if not _engine_client:
        return "Error: Engine client not available — cannot search knowledge graph."

    try:
        # Use the engine's GraphRAG search
        result = await _engine_client.search(query, _get_repo_id(), top_k=top_k)
        chunks = result.get("chunks", [])

        # Format with relationship info if available
        output = []
        for i, chunk in enumerate(chunks):
            entry = {
                "rank": i + 1,
                "file": chunk.get("file_path", "?"),
                "score": round(chunk.get("score", 0), 3),
                "lines": f"{chunk.get('start_line', '?')}-{chunk.get('end_line', '?')}",
                "preview": chunk.get("content", "")[:300],
            }
            output.append(entry)

        return json.dumps(output, indent=2)
    except Exception as e:
        return f"Knowledge graph search failed: {e}"


@tool()
async def ask_human(
    question: str,
    context: str = "",
    urgency: str = "normal",
    timeout_seconds: int = 300,
) -> str:
    """Ask the human a question and wait for their response (HITL gate)."""
    if not _go_client:
        return "Error: Go client not available — cannot contact human."

    try:
        result = await _go_client.ask_human(
            question=question,
            context=context,
            urgency=urgency,
            timeout=timeout_seconds,
        )
        return result.get("response", "No response received.")
    except asyncio.TimeoutError:
        return (
            f"Human did not respond within {timeout_seconds}s. "
            f"Proceeding with best judgment based on available context."
        )
    except Exception as e:
        return f"Failed to reach human: {e}. Proceeding with best judgment."


@tool()
async def web_search_and_fetch(
    url: str = "",
    query: str = "",
    max_results: int = 5,
    extract_pdf: bool = False,
) -> str:
    """Fetch a URL or search the web. Supports HTML, PDFs, GitHub issues, docs."""
    if not _go_client:
        return "Error: Go client not available."

    try:
        result = await _go_client.web_fetch(
            url=url,
            query=query,
            max_results=max_results,
            extract_pdf=extract_pdf,
        )
        text = result.get("text", "")
        if not text:
            return json.dumps({
                "note": "No content retrieved.",
                "url": url,
                "query": query,
            })
        # Auto-embed if we got usable content and engine is available.
        if _engine_client and text and url:
            try:
                await _engine_client.ingest_asset(
                    repo_id=_get_repo_id(),
                    source_url=url,
                    content=text[:50000],
                    asset_type="document",
                )
            except Exception:
                pass  # Best-effort embedding.
        return text[:30000]
    except Exception as e:
        return f"Web fetch failed: {e}"


@tool()
async def search_web(
    query: str,
    max_results: int = 5,
) -> str:
    """Search the web for documentation, API references, or error solutions."""
    return await web_search_and_fetch(query=query, max_results=max_results)


@tool()
async def self_critique(
    work_summary: str,
    critique_focus: str = "correctness",
) -> str:
    """Critique your own work before submitting it."""
    critique_prompts = {
        "correctness": (
            "Review your changes for correctness:\n"
            "1. Does the implementation match the task requirements?\n"
            "2. Are there any logical errors or off-by-one mistakes?\n"
            "3. Do all code paths handle errors properly?\n"
            "4. Are return types and function signatures correct?"
        ),
        "edge_cases": (
            "Review your changes for edge cases:\n"
            "1. What happens with empty inputs?\n"
            "2. What about nil/null/None values?\n"
            "3. Concurrent access — is it thread-safe?\n"
            "4. What if the network is unavailable?"
        ),
        "style": (
            "Review your changes for style consistency:\n"
            "1. Does the code follow the repo's naming conventions?\n"
            "2. Are imports organized the same way as existing files?\n"
            "3. Is error handling consistent with the codebase?\n"
            "4. Are comments and docstrings in the right format?"
        ),
        "security": (
            "Review your changes for security:\n"
            "1. Is user input validated and sanitized?\n"
            "2. Are there any SQL injection or XSS risks?\n"
            "3. Are secrets/tokens handled securely?\n"
            "4. Are permissions checked before operations?"
        ),
        "all": (
            "Comprehensive review of your changes:\n"
            "1. CORRECTNESS: Does it match requirements? Logical errors?\n"
            "2. EDGE CASES: Empty inputs, nil values, concurrency?\n"
            "3. STYLE: Naming, imports, error handling consistency?\n"
            "4. SECURITY: Input validation, injection risks, auth checks?\n"
            "5. PERFORMANCE: Any unnecessary loops or allocations?"
        ),
    }

    prompt = critique_prompts.get(critique_focus, critique_prompts["all"])
    return (
        f"Self-critique requested ({critique_focus}):\n\n"
        f"Your work: {work_summary}\n\n"
        f"{prompt}\n\n"
        f"List any issues found and suggest fixes. If everything looks good, "
        f"confirm that the implementation is ready for submission."
    )


@tool()
async def recall_memory(
    query: str = "",
    memory_tier: str = "all",
    limit: int = 10,
) -> str:
    """Recall information from the agent memory hierarchy (STM/MTM/LTM)."""
    if not _agent_memory:
        return "Memory system not initialized."

    sections: list[str] = []

    if memory_tier in ("stm", "all"):
        scratchpad = await _agent_memory.stm.get_scratchpad()
        if scratchpad:
            sections.append(f"=== Short-Term Memory ===\n{scratchpad}")

    if memory_tier in ("mtm", "all") and _agent_memory.mtm:
        mtm_text = await _agent_memory.mtm.recall_as_text(limit=limit)
        if mtm_text:
            sections.append(f"=== Medium-Term Memory ===\n{mtm_text}")

    if memory_tier in ("ltm", "all") and query:
        ltm_text = await _agent_memory.ltm.search_as_text(query, top_k=limit)
        if ltm_text:
            sections.append(f"=== Long-Term Memory ===\n{ltm_text}")

    if not sections:
        return "No memories found for the given query and tier."

    return "\n\n".join(sections)


@tool()
async def send_agent_message(
    target_role: str,
    message: str,
    include_embeddings_ref: bool = False,
    confidence: float = 0.8,
) -> str:
    """Send a message to another agent via the inter-agent bus (Redis Streams)."""
    if not _go_client:
        return "Error: Go client not available."
    try:
        result = await _go_client.publish_agent_bus(
            target_role=target_role,
            message=message,
            include_embeddings_ref=include_embeddings_ref,
            confidence=confidence,
        )
        return result.get("status", "sent")
    except Exception as e:
        return f"Agent message failed: {e}"


@tool()
async def read_agent_messages(
    limit: int = 10,
) -> str:
    """Read messages sent to this agent from other agents on the bus."""
    if not _go_client:
        return "Error: Go client not available."
    try:
        result = await _go_client.read_agent_bus(limit=limit)
        messages = result.get("messages", [])
        if not messages:
            return "No messages."
        lines = []
        for m in messages:
            lines.append(f"[{m.get('from_role', '?')}] {m.get('message', '')}")
        return "\n".join(lines)
    except Exception as e:
        return f"Read agent messages failed: {e}"


# Tool collections by role.
CODE_TOOLS = [run_tests, run_build, git_diff, self_critique, recall_memory]
RESEARCH_TOOLS = [search_knowledge_graph, web_search_and_fetch, search_web, recall_memory]
HITL_TOOLS = [ask_human]
COMM_TOOLS = [send_agent_message, read_agent_messages]

ALL_EXTENDED_TOOLS = [
    run_tests, run_build, git_diff, search_knowledge_graph,
    ask_human, web_search_and_fetch, search_web, self_critique,
    recall_memory, send_agent_message, read_agent_messages,
]


def _get_repo_id() -> str:
    from .workspace_tools import _get_workspace
    ws = _get_workspace()
    if ws:
        return ws.repo_id
    return ""
