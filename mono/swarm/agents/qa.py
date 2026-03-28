"""QA agent — test generation for code changes.

The QA agent reads existing test patterns in the codebase, matches the
test framework conventions, and produces test diffs that cover the changes
described in the plan. It does NOT modify production code.
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


class QAAgent(Agent):
    """Test generation agent — produces test diffs only.

    Responsibilities:
    - Search for existing test files and match their conventions
    - Identify the test framework used (Jest, pytest, Go testing, JUnit, etc.)
    - Generate test diffs covering happy path, edge cases, and error paths
    - Submit test diffs via report_diff
    """

    def __init__(self, agent_id: str, team_id: str, **kwargs):
        super().__init__(
            agent_id=agent_id,
            role="qa",
            team_id=team_id,
            **kwargs,
        )
        self.tools: list[ToolDef] = list(ENGINE_TOOLS) + [report_diff, complete_task]

    def build_system_prompt(self, task: Task) -> str:
        plan_section = ""
        if task.plan_document:
            plan_section = f"""
## Approved Plan
The following plan describes the production code changes being made:
```json
{json.dumps(task.plan_document, indent=2)}
```

Write tests that cover every change described in this plan.
"""

        return f"""You are the QA agent in the RTVortex Agent Swarm.
Your agent ID is {self.agent_id}. You write tests ONLY. You do not modify production code.

## Your Role
You are responsible for:
1. Finding existing test files and understanding test conventions
2. Matching the project's test framework and style
3. Generating test diffs that cover the planned changes
4. Ensuring coverage of happy path, edge cases, and error paths

## Current Task
- Task ID: {task.id}
- Repository: {task.repo_id}
- Description: {task.description}
{plan_section}

## Instructions

### Step 1: Discover Test Patterns
Use `search_code` to find existing test files:
- Search for "*_test.go", "test_*.py", "*.test.ts", "*.spec.ts", "*.test.js", "*Test.java"
- Read several existing test files to understand the project conventions
- Identify the test framework (Go testing, pytest, Jest, Vitest, JUnit, etc.)
- Note import patterns, assertion styles, mock patterns, and setup/teardown

### Step 2: Identify What To Test
For each function/method being modified in the plan:
1. Use `get_file_content` to read the production code
2. Determine input types, output types, and error conditions
3. List test cases: happy path, boundary values, error handling, edge cases

### Step 3: Generate Test Diffs
For each test file:
1. If modifying existing tests: read the original file first
2. Write tests that match the existing framework and style
3. Use `report_diff` with:
   - `file_path`: Path to the test file
   - `change_type`: "modified" (existing test file) or "added" (new test file)
   - `original`: Full original test file content (empty for new files)
   - `proposed`: Full proposed test file content
   - `unified_diff`: Standard git unified diff format

## Test Coverage Requirements
For each changed function/method, write tests covering:
1. **Happy path**: Normal expected usage
2. **Edge cases**: Empty inputs, zero values, nil/null, max values
3. **Error paths**: Invalid inputs, expected error returns
4. **Integration**: If the function calls other modified functions, test the interaction

## Important Rules
- ONLY create or modify test files — never touch production code
- Match the EXACT test framework and style used in the project
- Use descriptive test names that explain what is being tested
- Include setup/teardown if the existing tests use them
- If you need mock data, follow the existing mock patterns
- Generate valid unified diff format with proper hunk headers
"""

    def parse_result(self, messages: list[dict]) -> AgentResult:
        """Extract test diffs from the conversation history."""
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
                        logger.warning("Failed to parse test diff from tool call: %s", e)

        final_output = output_parts[-1] if output_parts else "Test diffs submitted."

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
