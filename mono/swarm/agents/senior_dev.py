"""Senior Dev agent — primary code generation role.

The senior dev receives subtasks or the full task, reads the
relevant code, and produces changes via workspace edit tools.

Instead of hallucinating unified diffs, the senior dev makes targeted
edits (search-and-replace) through the VirtualWorkspace. The system
computes the final diff from the changeset automatically.
"""

from __future__ import annotations

import json
import logging
from typing import Any

from ..sdk.agent import Agent, AgentResult, Task
from ..sdk.tool import ToolDef
from ..models import Diff
from ..tools.engine_tools import ENGINE_TOOLS
from ..tools.task_tools import complete_task
from ..tools.workspace_tools import WORKSPACE_TOOLS

logger = logging.getLogger(__name__)


class SeniorDevAgent(Agent):
    """Code generation agent — edits files via workspace tools.

    Capabilities:
    - Search code semantically via the engine
    - Read files from the repository via VCS API
    - Edit files with targeted search-and-replace operations
    - Create new files
    - Delete files
    - Review workspace status before completing
    """

    def __init__(self, agent_id: str, team_id: str, **kwargs):
        super().__init__(
            agent_id=agent_id,
            role="senior_dev",
            team_id=team_id,
            **kwargs,
        )
        # Workspace tools for reading/editing files + complete_task to signal done.
        self.tools: list[ToolDef] = list(WORKSPACE_TOOLS) + list(ENGINE_TOOLS) + [complete_task]

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
3. Making precise, targeted edits to files
4. Creating new files when needed

## Current Task
- Task ID: {task.id}
- Repository: {task.repo_id}
- Description: {task.description}
{plan_section}

## Available Tools

### Reading Code
- `workspace_read_file(path)` — Read a file's full content
- `workspace_search(query)` — Semantic search across the codebase
- `workspace_list_dir(path)` — List directory contents
- `search_code(query, repo_id)` — Search via the engine (alternative)
- `get_file_content(repo_id, file_path)` — Read via engine (alternative)

### Making Changes
- `workspace_edit_file(path, old_str, new_str)` — Edit a file by replacing exact text
- `workspace_create_file(path, content)` — Create a new file
- `workspace_delete_file(path)` — Delete a file
- `workspace_status()` — See what files you've changed

### Completing
- `complete_task(task_id)` — Mark the task as done

## Instructions

### Step 1: Read & Understand
Use `workspace_read_file` and `workspace_search` to understand the existing code.
Read each file you plan to modify BEFORE editing it.

### Step 2: Make Changes
For each change needed:
1. Read the file first with `workspace_read_file`
2. Use `workspace_edit_file` with the EXACT text to find and its replacement
3. For new files, use `workspace_create_file` with the full content

### Step 3: Verify & Complete
1. Use `workspace_status` to review all your changes
2. Call `complete_task` when all changes are done

## Code Quality Rules
- Match the existing code style (indentation, naming conventions, patterns)
- Include proper error handling
- Add doc comments for new public functions/methods
- Do NOT remove existing functionality unless explicitly requested
- Keep changes minimal — only modify what's needed
- The `old_str` in `workspace_edit_file` MUST exactly match the file content
  (including whitespace and indentation)

## CRITICAL RULES
- Always READ a file before editing it
- The `old_str` must be an EXACT match of existing text in the file
- Include enough context in `old_str` to be unambiguous (3-5 lines)
- Do NOT call `complete_task` until all changes are made
- Use `workspace_status` to verify your changes before completing
- If you received a "Multi-LLM Consensus" or "Initial Analysis" section,
  that is ONLY analysis context — NO actual file changes were made.
  You MUST still use workspace_edit_file / workspace_create_file to make
  all required edits yourself.
- Do NOT assume that any tool calls mentioned in the analysis have been
  executed. You are the one who must execute them.
"""

    def parse_result(self, messages: list[dict]) -> AgentResult:
        """Extract results from the conversation history.

        The senior dev's main outputs are workspace edits. The actual diffs
        are computed by the VirtualWorkspace, not extracted from tool calls.
        We still return a summary output from the conversation.
        """
        output_parts: list[str] = []

        for msg in messages:
            if msg.get("role") == "assistant" and msg.get("content"):
                output_parts.append(msg["content"])

        final_output = output_parts[-1] if output_parts else "Changes applied via workspace."

        # Diffs are now extracted from the VirtualWorkspace by the pipeline,
        # not from tool calls. Return empty diffs here.
        return AgentResult(output=final_output, diffs=[])
