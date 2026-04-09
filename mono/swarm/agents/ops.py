"""Ops agent — CI/CD and infrastructure configuration changes.

The ops agent handles Dockerfile updates, GitHub Actions workflows,
CI/CD pipeline configurations, deployment manifests, and other
infrastructure-as-code changes related to the task.
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


class OpsAgent(Agent):
    """Ops/infrastructure agent — CI/CD and deployment config changes.

    Responsibilities:
    - Update Dockerfiles when dependencies change
    - Modify CI/CD workflows (GitHub Actions, GitLab CI, etc.)
    - Update deployment manifests (k8s, docker-compose, etc.)
    - Adjust build configurations (Makefile, CMakeLists, package.json scripts)
    - Update environment variable documentation
    """

    def __init__(self, agent_id: str, team_id: str, **kwargs):
        super().__init__(
            agent_id=agent_id,
            role="ops",
            team_id=team_id,
            **kwargs,
        )
        self.tools: list[ToolDef] = list(ENGINE_TOOLS) + [report_diff]

    def build_probe_system_prompt(self, task: Task) -> str:
        """Probe-phase prompt for the ops agent — infrastructure impact analysis.

        During the multi-LLM probe, LLMs don't have tool access. The ops
        agent's normal prompt references ``search_code``, ``get_file_content``,
        and ``report_diff``. Without these tools, LLMs narrate hypothetical
        tool calls instead of providing useful analysis.

        This prompt tells the probe LLMs to produce concrete infrastructure
        impact analysis with specific CI/CD, Docker, and deployment concerns.
        """
        plan_section = ""
        if task.plan_document:
            plan_section = f"""
## Approved Plan
The following plan describes the code changes being made:
```json
{json.dumps(task.plan_document, indent=2)}
```
Determine what CI/CD, build, or deployment configurations need updating.
"""

        return f"""You are the Ops agent in the RTVortex Agent Swarm.
Your agent ID is {self.agent_id}. You handle infrastructure and CI/CD
configuration changes. You do NOT modify application logic.

## Current Task
- Task ID: {task.id}
- Repository: {task.repo_id}
- Description: {task.description}
{plan_section}

## IMPORTANT: This is an ANALYSIS-ONLY phase

You are in a planning probe phase where you do NOT have access to any tools.
You CANNOT search the codebase, read files, or call any functions.

Do NOT:
- Narrate tool calls (e.g. "I'll use search_code to find Dockerfile...")
- Pretend to read infrastructure files
- Simulate tool outputs

Instead, provide your EXPERT INFRASTRUCTURE IMPACT ANALYSIS:

### What You Must Produce:
1. **Infrastructure File Inventory** — Based on the plan's code changes,
   identify what infrastructure files likely need updating:
   - Dockerfiles (new dependencies? changed base images?)
   - CI/CD workflows (new test steps? build changes?)
   - Deployment manifests (new services? ports? env vars?)
   - Build configs (Makefile, CMakeLists, package.json scripts)
   - Dependency manifests (go.mod, requirements.txt, package.json)
2. **Change Impact Assessment** — For each code change in the plan:
   - Does it add new dependencies? → update package manifests + Docker
   - Does it add new environment variables? → update .env.example + docs
   - Does it change the build process? → update Makefile/CMake + CI
   - Does it add new services? → update docker-compose + k8s manifests
   - Does it change ports or protocols? → update deploy configs
3. **Concrete Changes** — For each infrastructure file that needs updating,
   describe the SPECIFIC change (e.g. "Add `RUN pip install pydantic` to
   `Dockerfile` after line with `pip install flask`").
4. **No-Change Justification** — If NO infrastructure changes are needed,
   explain WHY the code changes don't affect build/deploy.

### Quality Standards:
- Be SPECIFIC about file paths and line locations.
- Show actual config snippets you would add/modify.
- Don't propose unnecessary upgrades — only changes required by the plan.
- Be conservative: only change what's necessary.

Your analysis will be used as context for the implementation phase where
you will have actual tools to read configs and generate diffs.
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

Determine if any CI/CD, build, or deployment configurations need updating.
"""

        return f"""You are the Ops agent in the RTVortex Agent Swarm.
Your agent ID is {self.agent_id}. You handle infrastructure and CI/CD configuration changes.

## Your Role
You are responsible for:
1. Updating build configurations when dependencies or project structure change
2. Modifying CI/CD workflows to test and deploy new features
3. Updating Dockerfiles when base images or dependencies change
4. Adjusting deployment manifests for new services or configuration
5. Ensuring environment variables and secrets are properly configured

## Current Task
- Task ID: {task.id}
- Repository: {task.repo_id}
- Description: {task.description}
{plan_section}

## Instructions

### Step 1: Discover Infrastructure Files
1. Use `search_code` to find infrastructure files:
   - Dockerfiles, docker-compose.yml
   - .github/workflows/*.yml, .gitlab-ci.yml
   - Makefile, CMakeLists.txt
   - k8s manifests, helm charts
   - package.json scripts, go.mod, requirements.txt
   - .env.example, config files
2. Use `get_file_content` to read the relevant files
3. Understand the current build/deploy pipeline

### Step 2: Identify Required Changes
Based on the plan's code changes, determine if:
1. New dependencies need to be added to package manifests
2. New environment variables need to be documented
3. CI/CD workflows need new test or build steps
4. Dockerfiles need updated dependencies or build stages
5. Deployment configs need new services, ports, or volumes
6. Build scripts need new targets or modified commands

### Step 3: Generate Infrastructure Diffs
For each infrastructure file that needs updating:
1. Read the original file content
2. Make minimal, targeted changes
3. Use `report_diff` with:
   - `file_path`: Path to the infrastructure file
   - `change_type`: "modified" or "added"
   - `original`: Full original file content (empty for new files)
   - `proposed`: Full proposed file content
   - `unified_diff`: Standard git unified diff format

## Infrastructure Best Practices
- Keep Dockerfiles minimal — use multi-stage builds
- Pin dependency versions in CI/CD workflows
- Use environment variables for configuration, not hardcoded values
- Add health checks for new services
- Include proper caching in CI/CD pipelines
- Follow the existing naming conventions for new targets/jobs

## Important Rules
- ONLY modify infrastructure/CI/CD files — no application logic
- Be CONSERVATIVE — only change what's necessary for the code changes
- Don't upgrade base images or dependencies unless directly related
- Maintain backwards compatibility in build scripts
- Test commands should be idempotent
- Generate valid unified diff format with proper hunk headers
- If NO infrastructure changes are needed, report that in your output
"""

    def parse_result(self, messages: list[dict]) -> AgentResult:
        """Extract infrastructure diffs from the conversation history."""
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
                        logger.warning("Failed to parse ops diff from tool call: %s", e)

        final_output = output_parts[-1] if output_parts else "Infrastructure review complete."

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
