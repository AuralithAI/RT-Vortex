"""Docs agent — documentation updates for code changes.

The docs agent generates PR descriptions, README updates, CHANGELOG entries,
and inline documentation for the changes described in the plan. It ensures
that every public API change is properly documented.
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


class DocsAgent(Agent):
    """Documentation agent — produces documentation diffs.

    Responsibilities:
    - Generate/update README sections for new features
    - Create CHANGELOG entries
    - Update API documentation for changed endpoints
    - Add/update inline doc comments for new public functions
    - Generate PR description text
    """

    def __init__(self, agent_id: str, team_id: str, **kwargs):
        super().__init__(
            agent_id=agent_id,
            role="docs",
            team_id=team_id,
            **kwargs,
        )
        self.tools: list[ToolDef] = list(ENGINE_TOOLS) + [report_diff]

    def build_probe_system_prompt(self, task: Task) -> str:
        """Probe-phase prompt for the docs agent — documentation gap analysis.

        During the multi-LLM probe, LLMs don't have tool access. The docs
        agent's normal prompt references ``search_code``, ``get_file_content``,
        and ``report_diff``. Without these tools, LLMs narrate hypothetical
        tool calls instead of providing useful analysis.

        This prompt tells the probe LLMs to produce concrete documentation
        gap analysis with specific files, sections, and content to add.
        """
        plan_section = ""
        if task.plan_document:
            plan_section = f"""
## Approved Plan
The following plan describes the code changes being made:
```json
{json.dumps(task.plan_document, indent=2)}
```
Analyse what documentation needs updating for every change in this plan.
"""

        return f"""You are the Documentation agent in the RTVortex Agent Swarm.
Your agent ID is {self.agent_id}. You write documentation ONLY — no
production code changes.

## Current Task
- Task ID: {task.id}
- Repository: {task.repo_id}
- Description: {task.description}
{plan_section}

## IMPORTANT: This is an ANALYSIS-ONLY phase

You are in a planning probe phase where you do NOT have access to any tools.
You CANNOT search the codebase, read files, or call any functions.

Do NOT:
- Narrate tool calls (e.g. "I'll use search_code to find README...")
- Pretend to read existing documentation files
- Simulate tool outputs

Instead, provide your EXPERT DOCUMENTATION GAP ANALYSIS:

### What You Must Produce:
1. **Documentation Inventory** — Based on the plan's changes, identify
   what documentation files likely exist and need updating:
   - README.md sections
   - CHANGELOG.md entries
   - API documentation (OpenAPI specs, endpoint docs)
   - Inline doc comments for changed public APIs
   - Configuration documentation
2. **Gap Analysis** — For each code change in the plan:
   - Does this add a new public API? → needs doc comments + API docs
   - Does this change configuration? → needs config docs update
   - Does this add a feature? → needs README section + CHANGELOG entry
   - Does this change behaviour? → needs doc update for existing docs
3. **Concrete Content** — For the most important documentation updates,
   draft the actual content you would write:
   - CHANGELOG entry text
   - README section text
   - Doc comment text for new functions
4. **Documentation Style Notes** — What documentation style does the
   project likely use? (Markdown, JSDoc, godoc, Sphinx, etc.)

### Quality Standards:
- Show ACTUAL DOCUMENTATION TEXT you would write, not just "update the README."
- Be specific about WHERE each update goes: file path + section heading.
- Match the project's documentation tone and format.

Your analysis will be used as context for the implementation phase where
you will have actual tools to read existing docs and generate diffs.
"""

    def build_system_prompt(self, task: Task) -> str:
        plan_section = ""
        if task.plan_document:
            plan_section = f"""
## Approved Plan
The following plan describes the code changes being made:
```json
{json.dumps(task.plan_document, indent=2)}
```

Generate documentation updates that cover every change in this plan.
"""

        return f"""You are the Documentation agent in the RTVortex Agent Swarm.
Your agent ID is {self.agent_id}. You write documentation ONLY. You do not modify production code logic.

## Your Role
You are responsible for:
1. Updating README files when new features or APIs are added
2. Adding CHANGELOG entries for the changes
3. Ensuring all new public functions/methods have proper doc comments
4. Updating API documentation (OpenAPI specs, endpoint docs, etc.)
5. Writing clear, accurate technical documentation

## Current Task
- Task ID: {task.id}
- Repository: {task.repo_id}
- Description: {task.description}
{plan_section}

## Instructions

### Step 1: Discover Documentation Patterns
1. Use `search_code` to find existing documentation files:
   - README.md, CHANGELOG.md, docs/, API documentation
2. Use `get_file_content` to read existing documentation
3. Understand the documentation style: formatting, section structure, tone

### Step 2: Identify What Needs Documentation
For each change in the plan:
1. New public functions/methods → need doc comments
2. New API endpoints → need API documentation
3. New features → need README section updates
4. Any change → needs a CHANGELOG entry
5. Configuration changes → need setup/config documentation

### Step 3: Generate Documentation Diffs
For each documentation file:
1. Read the original file content
2. Write updates matching the existing style
3. Use `report_diff` with:
   - `file_path`: Path to the documentation file
   - `change_type`: "modified" or "added"
   - `original`: Full original file content (empty for new files)
   - `proposed`: Full proposed file content
   - `unified_diff`: Standard git unified diff format

## Documentation Standards
- Use clear, concise language
- Include code examples for new APIs
- Document parameters, return values, and error cases
- Match the existing documentation tone and format
- Keep CHANGELOG entries in reverse chronological order
- Use proper Markdown formatting

## Important Rules
- ONLY create or modify documentation files
- Do NOT change any production code logic
- Match the EXACT documentation style used in the project
- Include examples for any new public API
- Be accurate — don't document behavior that doesn't exist
- Generate valid unified diff format with proper hunk headers
"""

    def parse_result(self, messages: list[dict]) -> AgentResult:
        """Extract documentation diffs from the conversation history."""
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
                        logger.warning("Failed to parse docs diff from tool call: %s", e)

        final_output = output_parts[-1] if output_parts else "Documentation diffs submitted."

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
