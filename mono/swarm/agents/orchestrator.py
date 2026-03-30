"""Orchestrator agent — team lead responsible for planning and delegation.

The orchestrator receives a task, searches the codebase to understand scope,
produces a plan document, and delegates subtasks to other agents.
"""

from __future__ import annotations

import json
import logging
import re
from typing import Any

from ..sdk.agent import Agent, AgentResult, Task
from ..sdk.tool import ToolDef
from ..tools.engine_tools import ENGINE_TOOLS
from ..tools.task_tools import TASK_TOOLS

logger = logging.getLogger(__name__)


class OrchestratorAgent(Agent):
    """Team lead agent: planning, delegation, synthesis.

    Capabilities:
    - Search the codebase to understand context
    - Produce a structured plan document
    - Submit the plan for human review
    - Delegate implementation subtasks to SeniorDev/JuniorDev
    - Coordinate QA and Security reviews
    - Synthesise final output
    """

    def __init__(self, agent_id: str, team_id: str, **kwargs):
        super().__init__(
            agent_id=agent_id,
            role="orchestrator",
            team_id=team_id,
            **kwargs,
        )
        # Orchestrator gets both engine and task tools.
        self.tools: list[ToolDef] = list(ENGINE_TOOLS) + list(TASK_TOOLS)

    def build_system_prompt(self, task: Task) -> str:
        return f"""You are the Orchestrator agent in the RTVortex Agent Swarm.
Your agent ID is {self.agent_id}. You are the team lead.

## Your Role
You are responsible for:
1. Understanding the task by searching the codebase
2. Producing a structured plan document
3. Submitting the plan for human review

## Current Task
- Task ID: {task.id}
- Repository: {task.repo_id}
- Description: {task.description}

## Instructions

### Step 1: Understand the Codebase
Use the `search_code` tool to find relevant code in the repository.
Search for patterns, function names, and concepts mentioned in the task description.
Use `get_file_content` to read specific files you need to understand in full.
Use `get_index_status` first to verify the repository is indexed.

### Step 2: Analyse Scope
Based on your search results, determine:
- Which files need to be modified
- What the estimated complexity is (small: 1-3 files, medium: 4-15 files, large: 16+ files)
- What steps are needed to implement the changes

### Step 3: Submit Plan
Use `report_plan` to submit a structured plan with:
- A clear summary of what will be done
- Step-by-step implementation plan (JSON array of step objects)
- List of affected files
- Complexity estimate
- **agents_needed**: A JSON array of role strings specifying exactly which
  agent roles should be on the team. Choose from: "senior_dev", "junior_dev",
  "qa", "security", "architect", "devops", "ui_ux". Pick only the roles actually
  required — don't request a security agent for a typo fix.
  Example: ["senior_dev", "qa"] for a medium bug-fix that needs testing.
  Small task: ["senior_dev"]
  Medium task: ["senior_dev", "qa", "security"]
  Large task: ["architect", "senior_dev", "junior_dev", "qa", "security", "docs", "ui_ux"]

## Important Rules
- Always search the codebase BEFORE writing a plan
- Be specific about which files and functions will change
- Include test files in your affected_files if the task requires test changes
- Do NOT generate code — only produce the plan
- Keep the plan concise but thorough
- Choose **agents_needed** carefully — unnecessary agents waste resources
"""

    def parse_result(self, messages: list[dict]) -> AgentResult:
        """Extract the plan from the conversation history.

        The orchestrator's main output is a plan document submitted via
        the report_plan tool. We also capture the final assistant message.

        If the LLM wrote the plan in its text response instead of calling
        report_plan (truncation, model behaviour, etc.), we attempt to
        extract a structured plan from the text as a fallback.
        """
        output_parts: list[str] = []
        plan: dict | None = None

        for msg in messages:
            if msg.get("role") == "assistant" and msg.get("content"):
                output_parts.append(msg["content"])

            # Check if report_plan was called — extract the plan from tool args.
            tool_calls = msg.get("tool_calls", [])
            for tc in tool_calls:
                fn = tc.get("function", {})
                if fn.get("name") == "report_plan":
                    try:
                        args = json.loads(fn.get("arguments", "{}"))
                        steps = args.get("steps", "[]")
                        files = args.get("affected_files", "[]")
                        agents = args.get("agents_needed", "[]")
                        plan = {
                            "summary": args.get("summary", ""),
                            "steps": json.loads(steps) if isinstance(steps, str) else steps,
                            "affected_files": json.loads(files) if isinstance(files, str) else files,
                            "estimated_complexity": args.get("estimated_complexity", "medium"),
                            "agents_needed": json.loads(agents) if isinstance(agents, str) else agents,
                        }
                    except (json.JSONDecodeError, TypeError) as e:
                        logger.warning("Failed to parse plan from tool call: %s", e)

        final_output = output_parts[-1] if output_parts else "Plan submitted."

        # Fallback: if the LLM never called report_plan (e.g. truncation lost
        # the tool call, or the model wrote the plan as text), try to extract
        # a structured plan from the last assistant message.
        if plan is None and output_parts:
            plan = self._extract_plan_from_text(final_output)
            if plan:
                logger.info("Extracted plan from assistant text (report_plan tool was not called)")

        return AgentResult(
            output=final_output,
            plan=plan,
        )

    @staticmethod
    def _extract_plan_from_text(text: str) -> dict | None:
        """Best-effort extraction of a plan from free-form assistant text.

        Looks for JSON blocks first, then falls back to treating the entire
        text as a summary with a single step.

        Returns:
            A plan dict, or ``None`` if the text is too short to be useful.
        """
        if not text or len(text) < 50:
            return None

        # Try to find an embedded JSON plan object.
        json_blocks = re.findall(r'```(?:json)?\s*(\{.*?\})\s*```', text, re.DOTALL)
        for block in json_blocks:
            try:
                candidate = json.loads(block)
                if "summary" in candidate or "steps" in candidate:
                    return {
                        "summary": candidate.get("summary", text[:200]),
                        "steps": candidate.get("steps", [{"description": text[:500]}]),
                        "affected_files": candidate.get("affected_files", []),
                        "estimated_complexity": candidate.get("estimated_complexity", "medium"),
                        "agents_needed": candidate.get("agents_needed", []),
                    }
            except (json.JSONDecodeError, TypeError):
                continue

        # Fallback: use the text itself as the plan summary.  The LLM
        # clearly produced planning output; losing it because it skipped a
        # tool call shouldn't fail the entire task.
        return {
            "summary": text[:500],
            "steps": [{"description": text}],
            "affected_files": [],
            "estimated_complexity": "medium",
            "agents_needed": [],
        }
