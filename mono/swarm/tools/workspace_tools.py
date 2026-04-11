"""Workspace tools — @tool decorated functions for virtual workspace operations.

These tools give agents the ability to read, edit, create, and search files
through the :class:`~mono.swarm.workspace.VirtualWorkspace`. Reads hit the
VCS API (via Go proxy); writes are held in memory and converted to a
changeset at task completion.

Unlike the old ``report_diff`` approach where the LLM had to hallucinate
entire files and unified diffs from memory, these tools let the LLM make
targeted edits (search-and-replace) and the system computes the diff.
"""

from __future__ import annotations

import json
import logging
from typing import Any

from ..sdk.tool import tool
from ..workspace import VirtualWorkspace

logger = logging.getLogger(__name__)

# Module-level workspace — set per-task via init_workspace_tools().
_workspace: VirtualWorkspace | None = None


def init_workspace_tools(workspace: VirtualWorkspace) -> None:
    """Set the workspace for all workspace tools. Call once per task."""
    global _workspace
    _workspace = workspace


def _get_ws() -> VirtualWorkspace:
    if _workspace is None:
        raise RuntimeError("Workspace tools not initialised — call init_workspace_tools() first")
    return _workspace


def _get_workspace() -> VirtualWorkspace | None:
    """Return the current workspace (or None). Used by extended_tools."""
    return _workspace


# ── Tools ────────────────────────────────────────────────────────────────────


@tool(description=(
    "Read the full content of a file from the repository. "
    "Returns the file content as a string. Use this to understand existing code "
    "before making edits, or to read configuration files."
))
async def workspace_read_file(path: str) -> str:
    """Read a file from the repository.

    Args:
        path: File path relative to the repository root (e.g. 'src/auth.py').

    Returns:
        The file content as a string.
    """
    ws = _get_ws()
    try:
        content = await ws.read_file(path)
        return content
    except Exception as e:
        return json.dumps({
            "error": f"File not found: {path}",
            "hint": "Use workspace_list_dir or workspace_search to find the correct path.",
        })


@tool(description=(
    "Edit an existing file by replacing a specific string with new content. "
    "The old_str must exactly match the existing content (including whitespace). "
    "Only the first occurrence is replaced. Use this for targeted, precise edits."
))
async def workspace_edit_file(path: str, old_str: str, new_str: str) -> str:
    """Edit a file by replacing old_str with new_str.

    Args:
        path: File path relative to the repository root.
        old_str: The exact string to find and replace (must match exactly).
        new_str: The replacement string.

    Returns:
        Confirmation message or error.
    """
    ws = _get_ws()
    try:
        result = await ws.edit_file(path, old_str, new_str)
        return result
    except ValueError as e:
        return json.dumps({"error": str(e)})
    except Exception as e:
        return json.dumps({"error": f"Edit failed: {e}"})


@tool(description=(
    "Create a new file in the repository. Use this when you need to add "
    "a completely new file (e.g. a new module, test file, config file). "
    "Provide the full file content."
))
async def workspace_create_file(path: str, content: str) -> str:
    """Create a new file with the given content.

    Args:
        path: File path relative to the repository root.
        content: The full content for the new file.

    Returns:
        Confirmation message.
    """
    ws = _get_ws()
    try:
        result = await ws.create_file(path, content)
        return result
    except Exception as e:
        return json.dumps({"error": f"Create failed: {e}"})


@tool(description=(
    "Delete a file from the repository. Use this when a file needs to be "
    "removed as part of the task."
))
async def workspace_delete_file(path: str) -> str:
    """Delete a file.

    Args:
        path: File path relative to the repository root.

    Returns:
        Confirmation message.
    """
    ws = _get_ws()
    try:
        result = await ws.delete_file(path)
        return result
    except Exception as e:
        return json.dumps({"error": f"Delete failed: {e}"})


@tool(description=(
    "Edit a file if it exists, or create it if it doesn't. "
    "Use this instead of workspace_edit_file when you're not sure whether "
    "the file already exists. If the file exists, old_str must match exactly "
    "and will be replaced with new_str. If the file doesn't exist, a new file "
    "is created with new_str as its full content."
))
async def workspace_edit_or_create(path: str, old_str: str, new_str: str) -> str:
    """Edit a file if it exists, or create it with new_str if it doesn't.

    Args:
        path: File path relative to the repository root.
        old_str: The exact string to find and replace (used only if file exists).
        new_str: The replacement string, or full content for a new file.

    Returns:
        Confirmation message describing the action taken.
    """
    ws = _get_ws()
    try:
        result = await ws.edit_or_create_file(path, old_str, new_str)
        return result
    except ValueError as e:
        return json.dumps({"error": str(e)})
    except Exception as e:
        return json.dumps({"error": f"Edit-or-create failed: {e}"})


@tool(description=(
    "Create multiple files at once as a single module. Use this when you need "
    "to create an entire package or module with several files (e.g. a Go "
    "package with 5-6 files). Provide a JSON array of objects, each with "
    "'path' and 'content' keys. All files are created atomically so the "
    "module is never half-written."
))
async def workspace_create_module(files: str) -> str:
    """Create multiple files as a single module.

    Args:
        files: JSON string — array of {"path": "...", "content": "..."} objects.

    Returns:
        Summary of all created files.
    """
    ws = _get_ws()
    try:
        parsed = json.loads(files)
        if not isinstance(parsed, list):
            return json.dumps({"error": "files must be a JSON array"})
        for entry in parsed:
            if not isinstance(entry, dict) or "path" not in entry or "content" not in entry:
                return json.dumps({
                    "error": "Each entry must be an object with 'path' and 'content' keys"
                })
        result = await ws.create_module(parsed)
        return result
    except json.JSONDecodeError as e:
        return json.dumps({"error": f"Invalid JSON: {e}"})
    except Exception as e:
        return json.dumps({"error": f"Create module failed: {e}"})


@tool(description=(
    "List the contents of a directory in the repository. "
    "Returns file and directory names. Use this to explore the repository structure."
))
async def workspace_list_dir(path: str = "") -> str:
    """List directory contents.

    Args:
        path: Directory path relative to the repository root. Empty for root.

    Returns:
        JSON array of entries with 'name' and 'type' (file or dir).
    """
    ws = _get_ws()
    try:
        entries = await ws.list_dir(path)
        return json.dumps(entries, indent=2)
    except Exception as e:
        return json.dumps({"error": str(e)})


@tool(description=(
    "Search the codebase using semantic search. Returns ranked code snippets "
    "with file paths and scores. Use this to find relevant code, patterns, "
    "or understand how something is implemented."
))
async def workspace_search(query: str, top_k: int = 10) -> str:
    """Search the codebase semantically.

    Args:
        query: Natural language search query.
        top_k: Maximum number of results (default 10).

    Returns:
        JSON string of search results with code chunks.
    """
    ws = _get_ws()
    try:
        return await ws.search(query, top_k=top_k)
    except Exception as e:
        return json.dumps({"error": str(e)})


@tool(description=(
    "Get a summary of all changes made so far in the workspace. "
    "Returns a list of changed files with their change types. "
    "Use this to review what you've done before finishing."
))
async def workspace_status() -> str:
    """Show workspace status — which files have been changed.

    Returns:
        JSON summary of all pending changes.
    """
    ws = _get_ws()
    changes = ws.get_changeset()
    if not changes:
        return "No changes yet."
    summary = []
    for ch in changes:
        summary.append({
            "file": ch["file_path"],
            "type": ch["change_type"],
        })
    return json.dumps(summary, indent=2)


# ── Collect all workspace tools ──────────────────────────────────────────────

WORKSPACE_TOOLS = [
    workspace_read_file,
    workspace_edit_file,
    workspace_create_file,
    workspace_edit_or_create,
    workspace_create_module,
    workspace_delete_file,
    workspace_list_dir,
    workspace_search,
    workspace_status,
]
