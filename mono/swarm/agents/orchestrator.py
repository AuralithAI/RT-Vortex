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

        final_output = self._strip_preamble(final_output)

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
    def _strip_preamble(text: str) -> str:
        """Remove conversational preamble from LLM output.

        Some models (especially Grok) prefix their responses with lines like
        ``"Ok, I am the orchestrator agent and here is my plan..."`` or similar
        self-introductions before the actual content.  Strip those.
        """
        lines = text.split("\n")
        stripped: list[str] = []
        past_preamble = False
        for line in lines:
            if not past_preamble:
                lower = line.strip().lower()
                # Skip blank lines at the very top.
                if not lower:
                    continue
                # Detect preamble patterns — conversational self-intro lines.
                if re.match(
                    r"^(ok[,.]?\s+)?(i am|i'm|as)\s+(the\s+)?(an?\s+)?orchestrator",
                    lower,
                ):
                    continue
                if re.match(
                    r"^(ok[,.]?\s+)?(let me|i'll|i will)\s+(start|begin|create|produce|analyze|analyse)",
                    lower,
                ):
                    continue
                if re.match(r"^(sure|alright|certainly|understood)[,!.]", lower):
                    continue
                # If we get here, the line is real content.
                past_preamble = True
            stripped.append(line)
        return "\n".join(stripped).strip() if stripped else text

    @staticmethod
    def _extract_plan_from_text(text: str) -> dict | None:
        """Best-effort extraction of a plan from free-form assistant text.

        The function tries, in order:
        1. Embedded JSON code blocks that look like a plan.
        2. Embedded JSON code blocks that look like a *steps* array.
        3. Markdown-section parsing (### Summary, ### Steps, etc.).
        4. Numbered/bulleted list extraction as steps.
        5. Final fallback — split on paragraphs so we never stuff the entire
           response into a single step.

        Returns:
            A plan dict, or ``None`` if the text is too short to be useful.
        """
        if not text or len(text) < 50:
            return None

        # ── 1. Try to find an embedded JSON plan object ──────────────────
        json_blocks = re.findall(r'```(?:json)?\s*(\{.*?\})\s*```', text, re.DOTALL)
        for block in json_blocks:
            try:
                candidate = json.loads(block)
                if "summary" in candidate or "steps" in candidate:
                    raw_steps = candidate.get("steps", [])
                    from ..tools.task_tools import _normalize_step
                    steps = [_normalize_step(s) for s in raw_steps] if raw_steps else []
                    return {
                        "summary": candidate.get("summary", text[:200]),
                        "steps": steps or [{"description": text[:500]}],
                        "affected_files": candidate.get("affected_files", []),
                        "estimated_complexity": candidate.get("estimated_complexity", "medium"),
                        "agents_needed": candidate.get("agents_needed", []),
                    }
            except (json.JSONDecodeError, TypeError):
                continue

        # ── 2. Try to find a JSON array of steps ─────────────────────────
        json_arrays = re.findall(r'```(?:json)?\s*(\[.*?\])\s*```', text, re.DOTALL)
        for block in json_arrays:
            try:
                candidate = json.loads(block)
                if isinstance(candidate, list) and len(candidate) > 0:
                    from ..tools.task_tools import _normalize_step
                    steps = [_normalize_step(s) for s in candidate]
                    # Use text before the code block as summary.
                    pre = text[:text.find("```")].strip()
                    summary = pre[:500] if pre else "Implementation plan"
                    return {
                        "summary": summary,
                        "steps": steps,
                        "affected_files": [],
                        "estimated_complexity": "medium",
                        "agents_needed": [],
                    }
            except (json.JSONDecodeError, TypeError):
                continue

        # ── 3. Parse Markdown sections ───────────────────────────────────
        plan = OrchestratorAgent._extract_from_markdown_sections(text)
        if plan:
            return plan

        # ── 4. Extract numbered/bulleted steps from the text ─────────────
        plan = OrchestratorAgent._extract_numbered_steps(text)
        if plan:
            return plan

        # ── 5. Final fallback — split into paragraph-sized steps ─────────
        paragraphs = [p.strip() for p in re.split(r'\n{2,}', text) if p.strip()]
        if len(paragraphs) > 1:
            summary = paragraphs[0][:500]
            steps = [{"description": p[:1000]} for p in paragraphs[1:]]
            return {
                "summary": summary,
                "steps": steps or [{"description": text[:500]}],
                "affected_files": [],
                "estimated_complexity": "medium",
                "agents_needed": [],
            }

        # Single paragraph fallback.
        return {
            "summary": text[:500],
            "steps": [{"description": text[:1000]}],
            "affected_files": [],
            "estimated_complexity": "medium",
            "agents_needed": [],
        }

    @staticmethod
    def _extract_from_markdown_sections(text: str) -> dict | None:
        """Parse markdown-structured plan text into a plan dict.

        Recognises headings like:
        - ``### Summary`` / ``## Summary``
        - ``### Implementation Plan`` / ``### Steps`` / ``### Plan``
        - ``### Affected Files``
        - ``### Estimated Complexity``
        - ``### Agents Needed``
        """
        # Split on markdown headings (##, ###, ####) preserving the heading text.
        section_pattern = re.compile(r'^#{2,4}\s+(.+)$', re.MULTILINE)
        headings = list(section_pattern.finditer(text))

        if len(headings) < 2:
            return None  # Not enough structure to parse.

        sections: dict[str, str] = {}
        for i, match in enumerate(headings):
            title = match.group(1).strip().lower()
            # Normalise common heading variants.
            for key in ("summary", "implementation plan", "plan", "steps",
                        "affected files", "estimated complexity",
                        "agents needed", "agents", "complexity"):
                if key in title:
                    start = match.end()
                    end = headings[i + 1].start() if i + 1 < len(headings) else len(text)
                    sections[key] = text[start:end].strip()
                    break

        if not sections:
            return None

        # ── Summary ──────────────────────────────────────────────────────
        summary = sections.get("summary", "")
        if not summary:
            # Use text before the first heading as summary.
            summary = text[:headings[0].start()].strip()
        summary = summary[:500]

        # ── Steps ────────────────────────────────────────────────────────
        steps_text = (
            sections.get("implementation plan")
            or sections.get("plan")
            or sections.get("steps")
            or ""
        )
        steps = OrchestratorAgent._parse_steps_text(steps_text)

        # ── Affected files ───────────────────────────────────────────────
        files_text = sections.get("affected files", "")
        affected_files = OrchestratorAgent._extract_file_paths(files_text)

        # ── Complexity ───────────────────────────────────────────────────
        complexity_text = (sections.get("estimated complexity") or sections.get("complexity") or "").lower()
        if "small" in complexity_text:
            complexity = "small"
        elif "large" in complexity_text:
            complexity = "large"
        else:
            complexity = "medium"

        # ── Agents needed ────────────────────────────────────────────────
        agents_text = sections.get("agents needed") or sections.get("agents") or ""
        agents_needed = OrchestratorAgent._extract_agents(agents_text)

        if not steps and not summary:
            return None

        return {
            "summary": summary or "Implementation plan",
            "steps": steps or [{"description": summary or "See plan details"}],
            "affected_files": affected_files,
            "estimated_complexity": complexity,
            "agents_needed": agents_needed,
        }

    @staticmethod
    def _extract_numbered_steps(text: str) -> dict | None:
        """Extract numbered or bulleted list items as plan steps."""
        # Match lines starting with "1.", "2.", "- ", "* ", etc.
        step_pattern = re.compile(
            r'^(?:\d+[\.\)]\s+|\-\s+|\*\s+)(.+?)(?=\n(?:\d+[\.\)]\s+|\-\s+|\*\s+)|\Z)',
            re.MULTILINE | re.DOTALL,
        )
        matches = step_pattern.findall(text)
        if len(matches) < 2:
            return None  # Need at least 2 items to call it a list.

        steps = [{"description": m.strip()[:1000]} for m in matches if m.strip()]

        # Use text before the first list item as the summary.
        first_match = step_pattern.search(text)
        summary = text[:first_match.start()].strip()[:500] if first_match else text[:200]

        return {
            "summary": summary or "Implementation plan",
            "steps": steps,
            "affected_files": [],
            "estimated_complexity": "medium",
            "agents_needed": [],
        }

    @staticmethod
    def _parse_steps_text(text: str) -> list[dict]:
        """Parse a steps section into individual step dicts.

        Handles:
        - Numbered lists (``1. Do X``, ``2. Do Y``)
        - Bulleted lists (``- Do X``, ``* Do Y``)
        - JSON arrays embedded in the section
        """
        if not text:
            return []

        # Try JSON array first.
        json_match = re.search(r'```(?:json)?\s*(\[.*?\])\s*```', text, re.DOTALL)
        if json_match:
            try:
                arr = json.loads(json_match.group(1))
                if isinstance(arr, list):
                    from ..tools.task_tools import _normalize_step
                    return [_normalize_step(s) for s in arr]
            except (json.JSONDecodeError, TypeError):
                pass

        # Try bare JSON array (no code fence).
        bare_match = re.search(r'\[[\s\S]*\]', text)
        if bare_match:
            try:
                arr = json.loads(bare_match.group(0))
                if isinstance(arr, list) and len(arr) > 0 and isinstance(arr[0], (dict, str)):
                    from ..tools.task_tools import _normalize_step
                    return [_normalize_step(s) for s in arr]
            except (json.JSONDecodeError, TypeError):
                pass

        # Parse numbered/bulleted list items, each potentially multi-line.
        lines = text.split("\n")
        steps: list[dict] = []
        current: list[str] = []

        for line in lines:
            stripped = line.strip()
            # New numbered/bulleted item?
            if re.match(r'^(\d+[\.\)]\s+|\-\s+|\*\s+)', stripped):
                if current:
                    desc = " ".join(current).strip()
                    if desc:
                        steps.append({"description": desc[:1000]})
                # Remove the bullet/number prefix.
                cleaned = re.sub(r'^(\d+[\.\)]\s+|\-\s+|\*\s+)', '', stripped)
                current = [cleaned]
            elif stripped and current:
                current.append(stripped)
            elif not stripped and current:
                # Blank line ends the current item.
                desc = " ".join(current).strip()
                if desc:
                    steps.append({"description": desc[:1000]})
                current = []

        # Flush last item.
        if current:
            desc = " ".join(current).strip()
            if desc:
                steps.append({"description": desc[:1000]})

        return steps

    @staticmethod
    def _extract_file_paths(text: str) -> list[str]:
        """Extract file paths from an 'Affected Files' section."""
        if not text:
            return []
        files: list[str] = []
        for line in text.split("\n"):
            stripped = line.strip()
            # Remove bullets/numbers.
            cleaned = re.sub(r'^(\d+[\.\)]\s+|\-\s+|\*\s+|`)', '', stripped).rstrip('`').strip()
            # Looks like a file path?
            if cleaned and "/" in cleaned and " " not in cleaned:
                files.append(cleaned)
            elif cleaned and re.match(r'^[\w\-\.]+\.\w+$', cleaned):
                # Bare filename like "server.go".
                files.append(cleaned)
        return files

    @staticmethod
    def _extract_agents(text: str) -> list[str]:
        """Extract agent role names from an 'Agents Needed' section."""
        if not text:
            return []
        known_roles = {
            "senior_dev", "junior_dev", "qa", "security",
            "architect", "devops", "ui_ux", "docs", "ops",
        }
        agents: list[str] = []
        # Try JSON array.
        json_match = re.search(r'\[.*?\]', text, re.DOTALL)
        if json_match:
            try:
                arr = json.loads(json_match.group(0))
                if isinstance(arr, list):
                    return [str(a) for a in arr]
            except (json.JSONDecodeError, TypeError):
                pass
        # Scan for known roles.
        lower = text.lower()
        for role in known_roles:
            if role in lower or role.replace("_", " ") in lower:
                agents.append(role)
        return agents
