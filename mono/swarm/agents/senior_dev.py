"""Senior Dev agent — primary code generation role.

The senior dev receives subtasks (or the full task in Phase 0), reads the
relevant code, and produces unified diffs implementing the changes.
"""

from __future__ import annotations

import json
import logging
from typing import Any

from ..sdk.agent import Agent, AgentResult, Task
from ..sdk.tool import ToolDef
from ..models import Diff
from ..tools.engine_tools import ENGINE_TOOLS
from ..tools.task_tools import report_diff, complete_task

logger = logging.getLogger(__name__)


class SeniorDevAgent(Agent):
    """Code generation agent — produces file diffs from approved plans.

    Phase 0 capabilities:
    - Read files from the codebase
    - Generate unified diffs for code changes
    - Submit diffs for human review

    Phase 1+ additions:
    - Work on delegated subtasks from orchestrator
    - Coordinate with QA for test generation
    - Self-review before submission
    """

    def __init__(self, agent_id: str, team_id: str, **kwargs):
        super().__init__(
            agent_id=agent_id,
            role="senior_dev",
            team_id=team_id,
            **kwargs,
        )
        # Senior dev gets engine tools for reading code + diff submission tools.
        self.tools: list[ToolDef] = list(ENGINE_TOOLS) + [report_diff, complete_task]

    def build_system_prompt(self, task: Task) -> str:
        plan_section = ""
        if task.plan_document:
            plan_section = f"""
## Approved Plan
The following plan has been approved by a human reviewer:
```json
{json.dumps(task.plan_document, indent=2)}
```

Follow this plan exactly. Implement each step in order.
"""

        return f"""You are a Senior Developer agent in the RTVortex Agent Swarm.
Your agent ID is {self.agent_id}. You are responsible for writing production-quality code.

## Your Role
You are responsible for:
1. Reading the existing code to understand the codebase
2. Implementing the changes described in the task or approved plan
3. Producing high-quality unified diffs for each modified file
4. Submitting diffs for human review

## Current Task
- Task ID: {task.id}
- Repository: {task.repo_id}
- Description: {task.description}
{plan_section}

## Instructions

### Step 1: Read Existing Code
Use `get_file_content` to read each file that needs to be modified.
Use `search_code` to find related code, patterns, or imports you need to understand.

### Step 2: Generate Changes
For each file that needs modification:
1. Read the full original file content
2. Write the complete proposed file content with your changes
3. Generate a standard git unified diff

### Step 3: Submit Diffs
For each changed file, use `report_diff` with:
- `file_path`: Path to the file
- `change_type`: "modified", "added", "deleted", or "renamed"
- `original`: The full original file content (empty for new files)
- `proposed`: The full proposed file content (empty for deletions)
- `unified_diff`: Standard git unified diff format

## Code Quality Rules
- Match the existing code style (indentation, naming conventions, patterns)
- Include proper error handling
- Add doc comments for new public functions/methods
- Do NOT remove existing functionality unless explicitly requested
- Keep changes minimal — only modify what's needed for the task
- If adding a new file, set change_type to "added" and original to empty string
- Generate valid unified diff format:
  ```
  --- a/path/to/file
  +++ b/path/to/file
  @@ -start,count +start,count @@
   context line
  -removed line
  +added line
   context line
  ```

## Diff Format
When creating unified diffs, use standard git format:
- 3 lines of context before and after each change
- Proper @@ hunk headers with line numbers
- Use 'a/' and 'b/' prefixes for file paths
"""

    def parse_result(self, messages: list[dict]) -> AgentResult:
        """Extract diffs from the conversation history.

        The senior dev's main outputs are file diffs submitted via report_diff.
        """
        output_parts: list[str] = []
        diffs: list[dict] = []

        for msg in messages:
            if msg.get("role") == "assistant" and msg.get("content"):
                output_parts.append(msg["content"])

            tool_calls = msg.get("tool_calls", [])
            for tc in tool_calls:
                fn = tc.get("function", {})
                if fn.get("name") == "report_diff":
                    try:
                        args = json.loads(fn.get("arguments", "{}"))
                        diffs.append({
                            "file_path": args.get("file_path", ""),
                            "change_type": args.get("change_type", "modified"),
                            "original": args.get("original", ""),
                            "proposed": args.get("proposed", ""),
                            "unified_diff": args.get("unified_diff", ""),
                        })
                    except (json.JSONDecodeError, TypeError) as e:
                        logger.warning("Failed to parse diff from tool call: %s", e)

        final_output = output_parts[-1] if output_parts else "Diffs submitted."

        return AgentResult(
            output=final_output,
            diffs=[
                Diff(
                    file_path=d["file_path"],
                    change_type=d["change_type"],
                    original=d["original"],
                    proposed=d["proposed"],
                    unified_diff=d["unified_diff"],
                )
                for d in diffs
            ],
        )
