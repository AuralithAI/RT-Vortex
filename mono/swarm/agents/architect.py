"""Architect agent — impact analysis and risk assessment.

The architect is called by the orchestrator for medium-to-large tasks.
It analyses caller/callee graphs, identifies cross-cutting concerns,
and produces a structured impact-analysis section for the plan.

The architect does NOT generate code — only analysis.
"""

from __future__ import annotations

import json
import logging
from typing import Any

from ..sdk.agent import Agent, AgentResult, Task
from ..sdk.tool import ToolDef
from ..tools.engine_tools import ENGINE_TOOLS
from ..tools.task_tools import report_plan

logger = logging.getLogger(__name__)


class ArchitectAgent(Agent):
    """Impact analysis agent — traces dependencies and assesses risk.

    Responsibilities:
    - Trace caller/callee graphs via find_callers
    - Identify cross-cutting concerns (logging, auth, error handling)
    - Assess risk: breaking changes, performance impact, security
    - Produce a structured impact analysis for the orchestrator's plan
    """

    def __init__(self, agent_id: str, team_id: str, **kwargs):
        super().__init__(
            agent_id=agent_id,
            role="architect",
            team_id=team_id,
            **kwargs,
        )
        self.tools: list[ToolDef] = list(ENGINE_TOOLS) + [report_plan]

    def build_probe_system_prompt(self, task: Task) -> str:
        """Probe-phase prompt for the architect — impact analysis only.

        During the multi-LLM probe, LLMs don't have tool access. The architect's
        normal prompt references ``get_file_content``, ``find_callers``, and
        ``report_plan``. Without these tools, LLMs narrate hypothetical tool
        calls instead of providing useful analysis.

        This prompt tells the probe LLMs to produce structured impact analysis
        with dependency reasoning, risk assessment, and concrete file paths.
        """
        plan_section = ""
        if task.plan_document:
            plan_section = f"""
## Current Plan (from Orchestrator)
```json
{json.dumps(task.plan_document, indent=2)}
```
Analyse the impact of every change described in this plan.
"""

        return f"""You are the Architect agent in the RTVortex Agent Swarm.
Your agent ID is {self.agent_id}. You are responsible for impact analysis
and risk assessment. You do NOT generate code — only analysis.

## Current Task
- Task ID: {task.id}
- Repository: {task.repo_id}
- Description: {task.description}
{plan_section}

## IMPORTANT: This is an ANALYSIS-ONLY phase

You are in a planning probe phase where you do NOT have access to any tools.
You CANNOT search the codebase, read files, find callers, or call any functions.

Do NOT:
- Narrate tool calls (e.g. "I'll use find_callers to trace...")
- Pretend to read files or show file contents you haven't actually seen
- Simulate tool outputs or fabricate caller/callee data

Instead, provide your EXPERT IMPACT ANALYSIS:

### What You Must Produce:
1. **Dependency Trace** — For each changed file/function in the plan, reason
   about what other modules likely depend on it. Name EXACT file paths and
   function names where you can infer them from the project structure.
2. **Breaking Change Assessment** — Which changes could break callers?
   Signature changes, removed exports, changed return types, changed
   error semantics?
3. **Risk Matrix** — For each affected area, assess:
   - Risk level: low / medium / high / critical
   - Nature: breaking change / performance / security / concurrency
   - Recommendation: how to mitigate
4. **Cross-Cutting Concerns** — Does the change affect logging, auth,
   error handling, or other shared patterns?
5. **Transitive Dependencies** — Are there second-order effects? Files
   that import files that import the changed module?

### Quality Standards:
- Be SPECIFIC: "The `UserService.get_user()` method is called by
  `AuthMiddleware.validate()` in `middleware/auth.py`" is better than
  "other services may be affected."
- When you can't be certain, say what you EXPECT and why.
- Be conservative — when in doubt, flag a risk.

Your analysis will be used to inform the implementation phase and ensure
no breaking changes slip through.
"""

    def build_system_prompt(self, task: Task) -> str:
        plan_section = ""
        if task.plan_document:
            plan_section = f"""
## Current Plan (from Orchestrator)
```json
{json.dumps(task.plan_document, indent=2)}
```
Review this plan and provide impact analysis for it.
"""

        return f"""You are the Architect agent in the RTVortex Agent Swarm.
Your agent ID is {self.agent_id}. Your job is impact analysis, NOT code generation.

## Your Role
You are responsible for:
1. Tracing every function and type that will be modified
2. Finding all callers and dependents of modified code
3. Identifying breaking changes, performance implications, and security considerations
4. Producing a structured risk assessment

## Current Task
- Task ID: {task.id}
- Repository: {task.repo_id}
- Description: {task.description}
{plan_section}

## Instructions

### Step 1: Trace Dependencies
For each file listed in the plan's affected_files:
1. Use `get_file_content` to read the file
2. Identify every public function, class, or type that will change
3. Use `find_callers` to find all call sites for each modified symbol
4. Note any cross-module or cross-package dependencies

### Step 2: Assess Impact
For each dependency chain:
- Will the change break any callers? (signature changes, removed exports)
- Are there performance implications? (hot paths, N+1 queries, large allocations)
- Are there security implications? (auth checks, input validation, secrets handling)
- Are there concurrency concerns? (shared state, race conditions)

### Step 3: Submit Analysis
Use `report_plan` with a JSON impact analysis containing:
- `summary`: High-level impact summary
- `steps`: Array of impact items, each with:
  - `symbol`: The function/type affected
  - `callers`: Number of callers found
  - `risk_level`: "low", "medium", or "high"
  - `description`: What could go wrong
  - `recommendation`: How to mitigate the risk
- `affected_files`: List of ALL files that could be affected (including transitive deps)
- `estimated_complexity`: Updated complexity based on the impact analysis

## Important Rules
- Do NOT generate code — only analysis
- Trace callers at least 2 levels deep (callers of callers)
- Flag any file that imports or depends on a modified file
- Be conservative — when in doubt, flag a risk
- Focus on breaking changes first, then performance, then security
"""

    def parse_result(self, messages: list[dict]) -> AgentResult:
        """Extract impact analysis from the conversation history."""
        output_parts: list[str] = []
        plan: dict | None = None

        for msg in messages:
            if msg.get("role") == "assistant" and msg.get("content"):
                output_parts.append(msg["content"])

            tool_calls = msg.get("tool_calls", [])
            for tc in tool_calls:
                fn = tc.get("function", {})
                if fn.get("name") == "report_plan":
                    try:
                        args = json.loads(fn.get("arguments", "{}"))
                        steps = args.get("steps", "[]")
                        files = args.get("affected_files", "[]")
                        plan = {
                            "summary": args.get("summary", ""),
                            "steps": json.loads(steps) if isinstance(steps, str) else steps,
                            "affected_files": json.loads(files) if isinstance(files, str) else files,
                            "estimated_complexity": args.get("estimated_complexity", "medium"),
                        }
                    except (json.JSONDecodeError, TypeError) as e:
                        logger.warning("Failed to parse impact analysis from tool call: %s", e)

        final_output = output_parts[-1] if output_parts else "Impact analysis submitted."

        return AgentResult(
            output=final_output,
            plan=plan,
        )
