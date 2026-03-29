"""Task tools — @tool decorated functions for reporting back to Go.

These tools are injected into agents so the LLM can submit plans, diffs,
and team size declarations through the agentic loop.
"""

from __future__ import annotations

import asyncio
import json
import logging
from typing import Any

from ..go_client import GoClient
from ..sdk.tool import tool

logger = logging.getLogger(__name__)

# Module-level Go client — set via init_task_tools().
_go_client: GoClient | None = None


def init_task_tools(go_client: GoClient) -> None:
    """Set the Go client for all task tools. Call once at startup."""
    global _go_client
    _go_client = go_client


def _get_client() -> GoClient:
    if _go_client is None:
        raise RuntimeError("Task tools not initialised — call init_task_tools() first")
    return _go_client


def _normalize_step(step: Any) -> dict:
    """Normalize a plan step to ``{description, files?}``.

    LLMs produce steps with varying keys (title/details, action, step/description,
    etc.).  The UI expects ``{description: str, files?: str[]}``.
    """
    if isinstance(step, str):
        return {"description": step}
    if not isinstance(step, dict):
        return {"description": str(step)}

    # Already has the expected key.
    if "description" in step:
        desc = step["description"]
    else:
        # Common LLM variants: title+details, action, summary, name, etc.
        title = step.get("title", "")
        details = step.get("details", "") or step.get("action", "") or step.get("summary", "")
        if title and details:
            desc = f"{title}: {details}"
        else:
            desc = title or details or str(step)

    result: dict = {"description": desc}
    files = step.get("files")
    if isinstance(files, list):
        result["files"] = files
    return result


# ── Tools ────────────────────────────────────────────────────────────────────


@tool(description=(
    "Submit a plan document for a task. The plan describes what changes will be "
    "made and why. The plan will be reviewed by a human before implementation "
    "proceeds. Include a summary, list of steps, affected files, and complexity."
))
async def report_plan(
    task_id: str,
    summary: str,
    steps: str,
    affected_files: str,
    estimated_complexity: str,
    agents_needed: str = "[]",
) -> str:
    """Submit a plan for human review.

    Args:
        task_id: The task ID this plan is for.
        summary: A concise summary of the planned changes.
        steps: JSON array string of step objects, each with 'description' and 'files'.
        affected_files: JSON array string of file paths that will be modified.
        estimated_complexity: One of 'small', 'medium', 'large'.
        agents_needed: JSON array string of role strings needed for the task,
            e.g. '["senior_dev", "qa"]'. Optional — defaults to empty list.

    Returns:
        Confirmation string.
    """
    client = _get_client()

    try:
        steps_list = json.loads(steps)
    except (json.JSONDecodeError, TypeError):
        steps_list = [{"description": steps}]

    # Normalize steps to the schema the UI expects: {description, files?}.
    # LLMs may use varying key names (title/details/step/action/etc.).
    steps_list = [_normalize_step(s) for s in steps_list]

    try:
        files_list = json.loads(affected_files)
    except (json.JSONDecodeError, TypeError):
        files_list = [affected_files] if isinstance(affected_files, str) else []

    try:
        agents_list = json.loads(agents_needed) if isinstance(agents_needed, str) else agents_needed
    except (json.JSONDecodeError, TypeError):
        agents_list = []

    plan = {
        "summary": summary,
        "steps": steps_list,
        "affected_files": files_list,
        "estimated_complexity": estimated_complexity,
        "agents_needed": agents_list,
    }

    await client.report_plan(task_id=task_id, plan=plan)
    logger.info("Plan submitted for task %s", task_id)
    return f"Plan submitted for task {task_id}. Awaiting human review."


@tool(description=(
    "Submit a file diff for a task. Each diff represents changes to a single file. "
    "Include the file path, change type, original content, proposed content, "
    "and a unified diff in standard git format."
))
async def report_diff(
    task_id: str,
    file_path: str,
    change_type: str,
    original: str,
    proposed: str,
    unified_diff: str,
) -> str:
    """Submit a single file diff for human review.

    Args:
        task_id: The task ID this diff is for.
        file_path: Path to the file being modified.
        change_type: One of 'modified', 'added', 'deleted', 'renamed'.
        original: The original file content (empty string for new files).
        proposed: The proposed file content (empty string for deleted files).
        unified_diff: Standard git unified diff format.

    Returns:
        JSON string with the created diff's ID.
    """
    client = _get_client()

    diff_payload = {
        "file_path": file_path,
        "change_type": change_type,
        "original": original,
        "proposed": proposed,
        "unified_diff": unified_diff,
    }

    result = await client.report_diff(task_id=task_id, diff=diff_payload)
    logger.info("Diff submitted for task %s file %s", task_id, file_path)
    return json.dumps(result)


@tool(description=(
    "Declare the team size needed for the current task. The orchestrator uses "
    "this after analysing task complexity to request the right number of agents. "
    "Valid sizes: 2 (small), 4 (medium), 6-10 (large)."
))
async def declare_team_size(task_id: str, size: int) -> str:
    """Declare how many agents are needed for this task.

    Args:
        task_id: The task ID.
        size: Number of agents needed (2-10).

    Returns:
        Confirmation string.
    """
    client = _get_client()
    size = max(2, min(10, size))
    # The team may not be assigned yet — retry a few times.
    for attempt in range(4):
        try:
            await client.declare_team_size(task_id=task_id, size=size)
            logger.info("Team size declared: %d for task %s", size, task_id)
            return f"Team size set to {size} for task {task_id}."
        except Exception as exc:
            if "409" in str(exc) or "Conflict" in str(exc) or "no assigned team" in str(exc):
                if attempt < 3:
                    await asyncio.sleep(3)
                    continue
            raise
    return f"Team size set to {size} for task {task_id}."


@tool(description=(
    "Mark a task as completed. Only the orchestrator should call this after "
    "all diffs have been submitted and the team's work is finished."
))
async def complete_task(task_id: str) -> str:
    """Mark a task as completed.

    Args:
        task_id: The task ID to complete.

    Returns:
        Confirmation string.
    """
    client = _get_client()
    await client.report_result(task_id=task_id)
    logger.info("Task %s marked complete", task_id)
    return f"Task {task_id} marked as completed."


# ── Collect all tools ────────────────────────────────────────────────────────

TASK_TOOLS = [report_plan, report_diff, complete_task]
