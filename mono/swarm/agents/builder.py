"""Builder agent — sandbox build validation role.

The builder agent is a **pipeline stage**, not a regular team member.  It
runs *after* code agents produce diffs and *before* the PR is created.  Its
job is to:

1. Detect the build system from file patterns (Gradle, CMake, Python, etc.)
2. Scan for environment variable references via engine embeddings
3. Cross-reference discovered env vars with the user's repo-scoped secrets
4. Propose a build plan and request human confirmation for missing secrets
5. Trigger an ephemeral container build (Docker backend)
6. Analyse build failures and propose fixes (up to 2 retries)

The builder does **not** have workspace write tools — it validates, it does
not modify code.  Fix proposals flow through the team discussion → human
approval loop.

Security invariants:
- Secrets are resolved and injected **only at container runtime**.
- Secret values are **never persisted** to disk or logs.
- Secret values are zeroed from Go server memory after container creation.
"""

from __future__ import annotations

import json
import logging
from typing import Any

from ..sdk.agent import Agent, AgentResult, Task
from ..sdk.tool import ToolDef
from ..tools.engine_tools import ENGINE_TOOLS

logger = logging.getLogger(__name__)

# Build-related file patterns that trigger mandatory builder validation.
BUILD_FILE_PATTERNS = frozenset({
    "Makefile",
    "CMakeLists.txt",
    "build.gradle",
    "build.gradle.kts",
    "pom.xml",
    "pyproject.toml",
    "setup.py",
    "setup.cfg",
    "package.json",
    "Dockerfile",
    "docker-compose.yml",
    "docker-compose.yaml",
    "go.mod",
    "Cargo.toml",
    "meson.build",
    "BUILD.bazel",
    "WORKSPACE",
    "Gemfile",
    "requirements.txt",
    "SANDBOX.md",
    "BUILD.md",
    ".rtvortex/build.yml",
})

# Env-var patterns to search for per language ecosystem.
ENV_SCAN_QUERIES = [
    "os.getenv OR os.environ OR process.env",           # Python / Node
    "System.getenv OR System.getProperty",               # Java
    "std::getenv OR getenv",                             # C/C++
    "os.Getenv OR viper.Get",                            # Go
    "ENV OR ARG",                                        # Dockerfile
    "cmake -D OR set(CMAKE",                             # CMake
]

# Well-known env vars with safe defaults.
WELL_KNOWN_ENV_VARS: dict[str, str] = {
    "JAVA_HOME": "/usr/lib/jvm/java-17",
    "CMAKE_PREFIX_PATH": "/usr/local",
    "GOPATH": "/go",
    "GOROOT": "/usr/local/go",
    "PYTHONPATH": "",
    "NODE_PATH": "",
    "PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
}


def affects_build_system(affected_files: list[str]) -> bool:
    """Return True if any affected file matches a build-related pattern.

    Used by the pipeline to decide whether to run the builder in full
    validation mode vs. fast-scan mode.
    """
    for fpath in affected_files:
        # Check basename match.
        basename = fpath.rsplit("/", 1)[-1] if "/" in fpath else fpath
        if basename in BUILD_FILE_PATTERNS:
            return True
        # Check full-path match (e.g. .rtvortex/build.yml).
        if fpath in BUILD_FILE_PATTERNS:
            return True
    return False


class BuilderAgent(Agent):
    """Build validation agent — runs as a pipeline stage after diffs.

    Capabilities:
    - Search code semantically via the engine (for env-var discovery)
    - Read workspace files (build configs, SANDBOX.md)
    - Inspect the changeset to determine what build systems are affected
    - Post env-var discovery results to team discussion for HITL confirmation

    Does NOT have:
    - Workspace write tools (no edit_file, create_file, delete_file)
    - The ability to modify code directly
    """

    def __init__(self, agent_id: str, team_id: str, **kwargs: Any):
        super().__init__(
            agent_id=agent_id,
            role="builder",
            team_id=team_id,
            **kwargs,
        )
        # Builder gets read-only workspace tools + engine search tools.
        # Import here to avoid circular imports at module level.
        from ..tools.workspace_tools import (
            workspace_read_file,
            workspace_search,
            workspace_list_dir,
            workspace_status,
        )

        self.tools: list[ToolDef] = [
            workspace_read_file,
            workspace_search,
            workspace_list_dir,
            workspace_status,
        ] + list(ENGINE_TOOLS)

    def build_system_prompt(self, task: Task) -> str:
        plan_section = ""
        if task.plan_document:
            plan_section = f"""
## Approved Plan
```json
{json.dumps(task.plan_document, indent=2)}
```
"""

        return f"""You are the Builder Agent in the RTVortex Agent Swarm.
Your agent ID is {self.agent_id}. You are responsible for build validation.

## Your Role
You run AFTER code agents have produced diffs and BEFORE the PR is created.
Your job is to validate that the proposed changes will build successfully.

You are NOT a code author — you validate, you do not modify code.

## Current Task
- Task ID: {task.id}
- Repository: {task.repo_id}
- Description: {task.description}
{plan_section}

## What You Must Do

### Step 1: Detect Build System
Read the repository root to identify the build system:
- `Makefile` → Make
- `CMakeLists.txt` → CMake
- `build.gradle` / `build.gradle.kts` → Gradle
- `pom.xml` → Maven
- `pyproject.toml` / `setup.py` → Python (pip/poetry)
- `package.json` → Node.js (npm/yarn)
- `go.mod` → Go modules
- `Cargo.toml` → Rust (cargo)
- `SANDBOX.md` / `BUILD.md` → Custom build instructions

Also check for `.rtvortex/build.yml` which provides structured build config.

### Step 2: Scan for Environment Variables
Use `workspace_search` and `search_code` to find env-var references:
- Python: `os.getenv`, `os.environ`
- Java: `System.getenv`, `System.getProperty`
- C/C++: `std::getenv`, `getenv`
- Go: `os.Getenv`, `viper.Get`
- Node: `process.env`
- Docker: `ENV`, `ARG`
- CMake: `cmake -D`, `$ENV{{}}`

### Step 3: Report Findings
Summarise your analysis:
1. Build system detected (command, base image recommendation)
2. Environment variables found (name, file, line)
3. Which env vars are in repo secrets vs. missing
4. Recommended build command
5. Any potential build issues from the proposed diffs

### Step 4: Complete
Call `complete_task` with your analysis summary.

## CRITICAL RULES
- Do NOT attempt to edit or create files
- Do NOT hallucinate build results — you are analysing, not building
- Be specific: cite exact file paths and line numbers
- If SANDBOX.md or BUILD.md exists, use its instructions verbatim
"""

    def build_probe_system_prompt(self, task: Task) -> str:
        """Probe prompt for multi-LLM analysis (no tools available)."""
        return f"""You are a Build Validation specialist analysing a code change.

Task: {task.description}
Repository: {task.repo_id}

Analyse the task and provide:
1. What build system(s) does this repository likely use?
2. What environment variables might be needed?
3. What are potential build failure risks from the described changes?
4. What build command would you recommend?

Be specific and cite common patterns. Do NOT narrate tool calls.
"""

    def parse_result(self, messages: list[dict]) -> AgentResult:
        """Extract the build analysis from the agent's messages."""
        output_parts: list[str] = []
        for msg in messages:
            if msg.get("role") == "assistant" and msg.get("content"):
                content = msg["content"]
                if isinstance(content, str):
                    output_parts.append(content)
                elif isinstance(content, list):
                    for block in content:
                        if isinstance(block, dict) and block.get("type") == "text":
                            output_parts.append(block["text"])

        return AgentResult(output="\n\n".join(output_parts))
