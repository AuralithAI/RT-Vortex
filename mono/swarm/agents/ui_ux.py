"""UI/UX agent — user-interface and user-experience review for code changes.

The UI/UX agent reviews frontend code for design consistency, accessibility
compliance, responsive layout correctness, component composition, and general
UX best-practices.  It produces diffs that improve the user-facing quality of
the application.
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


class UIUXAgent(Agent):
    """UI/UX design agent — produces frontend improvement diffs.

    Responsibilities:
    - Review component structure and composition patterns
    - Validate accessibility (ARIA attributes, keyboard navigation, contrast)
    - Ensure responsive design across breakpoints
    - Improve CSS/Tailwind usage and consistency
    - Suggest UX flow improvements (loading states, error handling, transitions)
    - Verify design-system / token usage
    """

    def __init__(self, agent_id: str, team_id: str, **kwargs):
        super().__init__(
            agent_id=agent_id,
            role="ui_ux",
            team_id=team_id,
            **kwargs,
        )
        self.tools: list[ToolDef] = list(ENGINE_TOOLS) + [report_diff]

    def build_probe_system_prompt(self, task: Task) -> str:
        """Probe-phase prompt for the UI/UX agent — design review analysis only.

        During the multi-LLM probe, LLMs don't have tool access. The UI/UX
        agent's normal prompt references ``search_code``, ``get_file_content``,
        and ``report_diff``. Without these tools, LLMs narrate hypothetical
        tool calls instead of providing useful analysis.

        This prompt tells the probe LLMs to produce concrete UI/UX analysis
        with accessibility findings, responsive design issues, and component
        quality assessments.
        """
        plan_section = ""
        if task.plan_document:
            plan_section = f"""
## Approved Plan
The following plan describes the code changes being made:
```json
{json.dumps(task.plan_document, indent=2)}
```
Review the UI/UX aspects of every change in this plan.
"""

        return f"""You are the UI/UX Designer agent in the RTVortex Agent Swarm.
Your agent ID is {self.agent_id}. You focus exclusively on user-interface
quality and user-experience improvements.

## Current Task
- Task ID: {task.id}
- Repository: {task.repo_id}
- Description: {task.description}
{plan_section}

## IMPORTANT: This is an ANALYSIS-ONLY phase

You are in a planning probe phase where you do NOT have access to any tools.
You CANNOT search the codebase, read component files, or call any functions.

Do NOT:
- Narrate tool calls (e.g. "I'll use search_code to find components...")
- Pretend to read component or stylesheet files
- Simulate tool outputs

Instead, provide your EXPERT UI/UX REVIEW ANALYSIS:

### What You Must Produce:
1. **Accessibility Audit** — Based on the plan's UI changes:
   - Missing ARIA attributes (labels, roles, live regions)
   - Keyboard navigation gaps
   - Colour contrast concerns
   - Focus management issues
   - Screen reader compatibility
2. **Responsive Design Review** — For each UI change:
   - Does it work across mobile, tablet, and desktop?
   - Are breakpoints handled correctly?
   - Is touch target sizing adequate (min 44x44px)?
3. **Component Quality** — For each changed component:
   - Is it following the project's design system/tokens?
   - Is it using semantic HTML?
   - Does it have proper loading, error, and empty states?
   - Are animations respecting prefers-reduced-motion?
4. **UX Pattern Analysis** — Are there UX improvements needed?
   - Loading/skeleton states for async operations
   - Error boundaries and user-friendly error messages
   - Transitions and micro-interactions
   - Form validation patterns
5. **Concrete Recommendations** — For each finding, show the ACTUAL
   markup/CSS changes you would make.

### Quality Standards:
- Show ACTUAL CODE (JSX/HTML/CSS) for your recommendations, not just
  descriptions like "add an aria-label."
- Reference specific components and file paths from the plan.
- Prioritise accessibility (WCAG 2.1 AA) over aesthetics.
- Be specific about which breakpoints and which states.

Your analysis will be used as context for the implementation phase where
you will have actual tools to read components and generate diffs.
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

Review and improve the UI/UX aspects of every change in this plan.
"""

        return f"""You are the UI/UX Designer agent in the RTVortex Agent Swarm.
Your agent ID is {self.agent_id}. You focus exclusively on user-interface
quality and user-experience improvements.

## Your Role
You are responsible for:
1. Reviewing component structure for reusability and composition best-practices
2. Ensuring accessibility compliance (WCAG 2.1 AA): ARIA roles, labels,
   keyboard navigation, colour contrast, focus management
3. Validating responsive design across mobile, tablet, and desktop breakpoints
4. Improving CSS / Tailwind utility usage — removing duplication, enforcing
   design-token consistency, and simplifying class lists
5. Enhancing UX patterns: loading/skeleton states, error boundaries, toast
   notifications, transitions/animations, empty states
6. Checking consistent use of the project's design system (colours, spacing,
   typography, iconography)

## Current Task
- Task ID: {task.id}
- Repository: {task.repo_id}
- Description: {task.description}
{plan_section}

## Instructions

### Step 1: Discover Frontend Patterns
1. Use `search_code` to locate UI-related files:
   - Components (`*.tsx`, `*.jsx`, `*.vue`, `*.svelte`)
   - Stylesheets (`*.css`, `*.scss`, `tailwind.config.*`)
   - Design tokens / theme files
2. Use `get_file_content` to read component implementations
3. Understand the existing design language: colour palette, spacing scale,
   typography, component library

### Step 2: Identify UI/UX Improvements
For each change in the plan:
1. Missing or incorrect ARIA attributes → add proper accessibility markup
2. Hard-coded colours / spacing → replace with design tokens
3. Non-responsive layouts → add responsive breakpoint handling
4. Missing loading / error / empty states → add appropriate UX patterns
5. Inconsistent component patterns → refactor toward project conventions
6. Animation / transition gaps → add smooth transitions where beneficial

### Step 3: Generate UI/UX Diffs
For each file you want to improve:
1. Read the original file content
2. Write your proposed improvements matching the existing code style
3. Use `report_diff` with:
   - `file_path`: Path to the UI file
   - `change_type`: "modified" or "added"
   - `original`: Full original file content (empty for new files)
   - `proposed`: Full proposed file content
   - `unified_diff`: Standard git unified diff format

## UI/UX Standards
- Follow WCAG 2.1 AA guidelines for accessibility
- Use semantic HTML elements (`<nav>`, `<main>`, `<section>`, `<article>`, etc.)
- Prefer design-system tokens over hard-coded values
- Ensure every interactive element is keyboard-accessible
- Add `aria-label` or `aria-labelledby` to all icon-only buttons
- Use `prefers-reduced-motion` media query for animations
- Keep component files focused — one primary component per file
- Use proper loading and error states for async operations

## Important Rules
- ONLY modify frontend / UI files
- Do NOT change backend logic, API routes, or database code
- Match the EXACT coding style used in the project
- Prefer small, incremental improvements over large rewrites
- Be accurate — don't propose markup that would break functionality
- Generate valid unified diff format with proper hunk headers
"""

    def parse_result(self, messages: list[dict]) -> AgentResult:
        """Extract UI/UX improvement diffs from the conversation history."""
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
                        logger.warning("Failed to parse ui_ux diff from tool call: %s", e)

        final_output = output_parts[-1] if output_parts else "UI/UX improvement diffs submitted."

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
