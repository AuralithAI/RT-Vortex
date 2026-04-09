"""Security agent — security review of code changes.

The security agent analyses diffs for vulnerabilities, auth issues,
injection flaws, secrets exposure, and other security concerns.
It produces a structured security report with line-numbered findings.
"""

from __future__ import annotations

import json
import logging

from ..sdk.agent import Agent, AgentResult, Task
from ..sdk.tool import ToolDef
from ..tools.engine_tools import ENGINE_TOOLS

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
        self.tools: list[ToolDef] = list(ENGINE_TOOLS)

    def build_probe_system_prompt(self, task: Task) -> str:
        """Probe-phase prompt for the security agent — vulnerability analysis only.

        During the multi-LLM probe, LLMs don't have tool access. The security
        agent's normal prompt references ``get_file_content`` and ``search_code``.
        Without these tools, LLMs narrate hypothetical tool calls instead of
        providing useful analysis.

        This prompt tells the probe LLMs to produce concrete security analysis
        with specific vulnerability types, attack vectors, and risk assessments.
        """
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
Your agent ID is {self.agent_id}. You are responsible for security review.
You do NOT generate code — only security analysis.

## Current Task
- Task ID: {task.id}
- Repository: {task.repo_id}
- Description: {task.description}
{plan_section}

## IMPORTANT: This is an ANALYSIS-ONLY phase

You are in a planning probe phase where you do NOT have access to any tools.
You CANNOT search the codebase, read files, or call any functions.

Do NOT:
- Narrate tool calls (e.g. "I'll use search_code to find auth patterns...")
- Pretend to read source files
- Simulate tool outputs

Instead, provide your EXPERT SECURITY ANALYSIS:

### What You Must Produce:
1. **Attack Surface Assessment** — Based on the plan, what new attack surface
   is being introduced or modified? (new endpoints, data flows, auth changes)
2. **Vulnerability Checklist** — For each changed file/function:
   - Injection risks (SQL, XSS, command injection, SSRF, path traversal)
   - Auth/AuthZ gaps (missing checks, IDOR, privilege escalation)
   - Secrets handling (hardcoded keys, tokens in logs, insecure defaults)
   - Cryptographic concerns (weak hashing, predictable randomness)
   - Data protection (PII exposure, missing validation, insecure deser)
3. **Risk Rating** — For each finding:
   - Severity: info / low / medium / high / critical
   - Likelihood of exploitation
   - Recommended mitigation
4. **Overall Assessment** — "safe", "concerns", or "critical"
5. **Specific Recommendations** — Concrete actions to mitigate each risk,
   with code patterns where applicable.

### Quality Standards:
- Be SPECIFIC: "The `POST /api/users` endpoint in `handlers/user.go`
  concatenates `req.Name` into an SQL query" is better than "SQL injection
  risk exists."
- Show the VULNERABLE pattern and the SECURE alternative.
- Don't flag theoretical risks without reasoning — explain WHY the specific
  code change creates the vulnerability.
- Be conservative — flag concerns even when uncertain.

Your analysis will inform the implementation phase and may block merging
if critical issues are found.
"""

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
Write your security findings as structured text in your final response.
Include:
- **Overall Assessment**: "safe", "concerns", or "critical"
- **Findings**: Each with affected symbol/file, risk level (info/low/medium/high/critical),
  description of the vulnerability, recommendation, and line reference
- **Summary**: Files with security concerns and overall risk

## Important Rules
- Do NOT generate code — only security analysis
- Do NOT call report_plan — your text output IS your report
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

        for msg in messages:
            if msg.get("role") == "assistant" and msg.get("content"):
                output_parts.append(msg["content"])

        return AgentResult(
            output=output_parts[-1] if output_parts else "Security review completed.",
        )
