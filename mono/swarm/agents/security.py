"""Security agent — security review of code changes.

The security agent analyses diffs for vulnerabilities, auth issues,
injection flaws, secrets exposure, and other security concerns.
It produces a structured security report with line-numbered findings.
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


class SecurityAgent(Agent):
    """Security review agent — finds vulnerabilities in proposed changes.

    Responsibilities:
    - Review all diffs for security vulnerabilities
    - Check for authentication/authorization gaps
    - Identify injection risks (SQL, XSS, command injection, SSRF)
    - Detect hardcoded secrets, API keys, tokens
    - Flag insecure cryptographic patterns
    - Assess IDOR and access control issues
    - Produce a structured security report
    """

    def __init__(self, agent_id: str, team_id: str, **kwargs):
        super().__init__(
            agent_id=agent_id,
            role="security",
            team_id=team_id,
            **kwargs,
        )
        self.tools: list[ToolDef] = list(ENGINE_TOOLS) + [report_plan]

    def build_system_prompt(self, task: Task) -> str:
        plan_section = ""
        if task.plan_document:
            plan_section = f"""
## Approved Plan
The following plan describes the changes being made:
```json
{json.dumps(task.plan_document, indent=2)}
```

Review every change in this plan for security implications.
"""

        return f"""You are the Security agent in the RTVortex Agent Swarm.
Your agent ID is {self.agent_id}. Your job is security review, NOT code generation.

## Your Role
You are responsible for reviewing ALL proposed code changes for security
vulnerabilities before they are merged. You must be thorough and conservative —
when in doubt, flag a concern.

## Current Task
- Task ID: {task.id}
- Repository: {task.repo_id}
- Description: {task.description}
{plan_section}

## Instructions

### Step 1: Review All Changed Files
For each file in the plan's affected_files:
1. Use `get_file_content` to read the full file
2. Use `search_code` to find related authentication, authorization, and validation patterns
3. Understand the data flow: where does user input enter? Where does it exit?

### Step 2: Check for Vulnerabilities
For each changed function/method, check for:

**Injection Flaws**
- SQL injection: string concatenation in queries, missing parameterized queries
- XSS: unescaped user input in HTML/templates
- Command injection: shell commands with user input
- SSRF: user-controlled URLs in HTTP requests
- Path traversal: user input in file paths

**Authentication & Authorization**
- Missing auth checks on new endpoints
- Broken access control (IDOR — can user A access user B's data?)
- JWT/session handling issues
- Privilege escalation paths

**Secrets & Configuration**
- Hardcoded API keys, tokens, passwords
- Secrets in logs or error messages
- Insecure default configurations
- Missing TLS/encryption

**Cryptography**
- Weak hashing (MD5, SHA1 for passwords)
- Missing salt/pepper for password hashing
- Predictable random values for security-sensitive operations
- Insecure key management

**Data Protection**
- PII exposure in logs or responses
- Missing input validation
- Missing rate limiting on sensitive endpoints
- Insecure deserialization

### Step 3: Submit Security Report
Use `report_plan` with a structured JSON security report containing:
- `summary`: Overall security assessment ("safe", "concerns", "critical")
- `steps`: Array of findings, each with:
  - `symbol`: The function/endpoint/file affected
  - `risk_level`: "info", "low", "medium", "high", "critical"
  - `description`: What the vulnerability is and how it could be exploited
  - `recommendation`: Specific fix recommendation
  - `line_reference`: File and line number where the issue exists
- `affected_files`: List of files with security concerns
- `estimated_complexity`: "small" if no issues, "medium" if concerns, "large" if critical

## Important Rules
- Do NOT generate code — only security analysis
- Be CONSERVATIVE — flag potential issues even if you're not 100% sure
- Check EVERY new endpoint for auth/authz
- Check EVERY database query for injection
- Check EVERY user input for validation
- Look for IDOR in every data access path
- Check for secrets in EVERY string literal and configuration
- Rate severity honestly — don't over-inflate or under-report
"""

    def parse_result(self, messages: list[dict]) -> AgentResult:
        """Extract security findings from the conversation history."""
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
                            "estimated_complexity": args.get("estimated_complexity", "small"),
                        }
                    except (json.JSONDecodeError, TypeError) as e:
                        logger.warning("Failed to parse security report from tool call: %s", e)

        final_output = output_parts[-1] if output_parts else "Security review submitted."

        return AgentResult(
            output=final_output,
            plan=plan,
        )
