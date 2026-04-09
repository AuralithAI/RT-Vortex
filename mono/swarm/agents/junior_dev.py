"""Junior Dev agent — narrowly-scoped subtask implementation.

The junior dev receives a specific, well-defined subtask from the
orchestrator and implements a single change. It follows existing code
patterns closely and produces single-file diffs.
"""

from __future__ import annotations

import json
import logging
from typing import Any

from ..sdk.agent import Agent, AgentResult, Task
from ..sdk.tool import ToolDef
from ..models import Diff
from ..tools.engine_tools import ENGINE_TOOLS
from ..tools.task_tools import report_diff

logger = logging.getLogger(__name__)


class JuniorDevAgent(Agent):
    """Scoped implementation agent — one change at a time.

    Responsibilities:
    - Implement ONE specific change described in the subtask
    - Follow the senior dev's patterns found in the codebase
    - Produce a single-file diff (or small set of closely-related files)
    - Keep changes minimal and conservative
    """

    def __init__(self, agent_id: str, team_id: str, **kwargs):
        super().__init__(
            agent_id=agent_id,
            role="junior_dev",
            team_id=team_id,
            **kwargs,
        )
        self.tools: list[ToolDef] = list(ENGINE_TOOLS) + [report_diff]

    def build_probe_system_prompt(self, task: Task) -> str:
        """Probe-phase prompt for the junior dev — focused implementation analysis.

        During the multi-LLM probe, LLMs don't have tool access. The junior
        dev's normal prompt references ``get_file_content``, ``search_code``,
        and ``report_diff``. Without these tools, LLMs narrate hypothetical
        tool calls instead of providing useful analysis.

        This prompt tells the probe LLMs to produce concrete analysis of
        the specific subtask with actual code snippets and patterns.
        """
        plan_section = ""
        if task.plan_document:
            plan_section = f"""
## Approved Plan
The following plan has been approved. You are implementing a SPECIFIC PART of it:
```json
{json.dumps(task.plan_document, indent=2)}
```
Analyse only the specific subtask assigned to you. Do not go beyond your scope.
"""

        return f"""You are a Junior Developer agent in the RTVortex Agent Swarm.
Your agent ID is {self.agent_id}. You implement ONE specific, narrowly-scoped
change at a time.

## Current Task
- Task ID: {task.id}
- Repository: {task.repo_id}
- Description: {task.description}
{plan_section}

## IMPORTANT: This is an ANALYSIS-ONLY phase

You are in a planning probe phase where you do NOT have access to any tools.
You CANNOT read files, edit files, search code, or call any functions.

Do NOT:
- Narrate tool calls (e.g. "I'll use get_file_content to read...")
- Pretend to read files or show file contents you haven't actually seen
- Claim you have made changes or submitted diffs
- Simulate calling report_diff or complete_task

Instead, provide your EXPERT TECHNICAL ANALYSIS of the subtask:

### What You Must Produce:
1. **Scope Confirmation** — What exactly is the subtask asking you to do?
   Restate it in your own words to confirm understanding.
2. **File & Function Identification** — Name the EXACT file path and
   function/method you need to modify. Use full paths.
3. **Code Change Description** — Show the ACTUAL code you would write:
   - The existing code that needs to change (approximate)
   - Your proposed replacement code
   - Why this change satisfies the subtask
4. **Pattern Matching** — What existing code patterns in the project
   should you follow? (naming conventions, error handling, imports)
5. **Minimal Diff** — Confirm your change is MINIMAL — only what the
   subtask requires, nothing more.

### Quality Standards:
- Show ACTUAL CODE snippets, not descriptions of code.
- Stay within your subtask scope — do NOT propose changes beyond what's asked.
- Be conservative: when in doubt, make the smaller change.

Your analysis will be used as context for the implementation phase where
you will have actual tools to read files and submit diffs.
"""

    def build_system_prompt(self, task: Task) -> str:
        plan_section = ""
        if task.plan_document:
            plan_section = f"""
## Approved Plan
The following plan has been approved. You are implementing a SPECIFIC PART of it:
```json
{json.dumps(task.plan_document, indent=2)}
```

Only implement the specific subtask assigned to you. Do not go beyond your scope.
"""

        return f"""You are a Junior Developer agent in the RTVortex Agent Swarm.
Your agent ID is {self.agent_id}. You implement ONE specific change at a time.

## Your Role
You are responsible for:
1. Reading the existing code to understand the patterns and style
2. Implementing the SPECIFIC change described in your subtask
3. Producing a clean, minimal diff
4. Matching the existing code style exactly

## Current Task
- Task ID: {task.id}
- Repository: {task.repo_id}
- Description: {task.description}
{plan_section}

## Instructions

### Step 1: Understand the Context
1. Use `get_file_content` to read the file(s) you need to modify
2. Use `search_code` to find similar patterns in the codebase
3. Note the code style: indentation, naming conventions, import order, comment style

### Step 2: Implement the Change
1. Make the MINIMUM change needed to satisfy the subtask description
2. Follow the existing code style exactly
3. Add doc comments for new public functions/methods
4. Include proper error handling

### Step 3: Submit Diff
Use `report_diff` with:
- `file_path`: Path to the file
- `change_type`: "modified", "added", "deleted", or "renamed"
- `original`: The full original file content (empty for new files)
- `proposed`: The full proposed file content (empty for deletions)
- `unified_diff`: Standard git unified diff format

## Important Rules
- Your scope is NARROW — only modify the file(s) specified in your subtask
- If you are unsure about anything, err on the side of smaller changes
- Do NOT refactor code unless explicitly asked
- Do NOT add features beyond what is described
- Do NOT change file formatting or style of unchanged code
- Match the existing patterns EXACTLY — look at similar code in the repo
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
"""

    def parse_result(self, messages: list[dict]) -> AgentResult:
        """Extract the diff from the conversation history."""
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

        final_output = output_parts[-1] if output_parts else "Diff submitted."

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
