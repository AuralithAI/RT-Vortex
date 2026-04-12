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

# File extensions worth scanning for env-var references.
_SCANNABLE_EXTENSIONS = frozenset({
    ".py", ".js", ".ts", ".java", ".go", ".c", ".cpp", ".h",
    ".rs", ".rb", ".env.example",
})

# Basenames that are always worth scanning regardless of extension.
_SCANNABLE_BASENAMES = frozenset({
    "Dockerfile", "docker-compose.yml", "docker-compose.yaml",
    "CMakeLists.txt", ".env.example",
})

ENV_SCAN_QUERIES = [
    "os.getenv OR os.environ OR process.env",
    "System.getenv OR System.getProperty",
    "std::getenv OR getenv",
    "os.Getenv OR viper.Get",
    "ENV OR ARG",
    "cmake -D OR set(CMAKE",
]

WELL_KNOWN_ENV_VARS: dict[str, str] = {
    "JAVA_HOME": "/usr/lib/jvm/java-17",
    "CMAKE_PREFIX_PATH": "/usr/local",
    "GOPATH": "/go",
    "GOROOT": "/usr/local/go",
    "PYTHONPATH": "",
    "NODE_PATH": "",
    "PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
}

# Max bytes of file content to send to the probe endpoint per file.
_MAX_FILE_SIZE_FOR_PROBE = 64 * 1024

# Max total files to scan for env vars.
_MAX_SCAN_FILES = 50


def affects_build_system(affected_files: list[str]) -> bool:
    """Return True if any affected file matches a build-related pattern."""
    for fpath in affected_files:
        basename = fpath.rsplit("/", 1)[-1] if "/" in fpath else fpath
        if basename in BUILD_FILE_PATTERNS:
            return True
        if fpath in BUILD_FILE_PATTERNS:
            return True
    return False


def _is_scannable(filepath: str) -> bool:
    """Return True if a file is worth scanning for env-var references."""
    basename = filepath.rsplit("/", 1)[-1] if "/" in filepath else filepath
    if basename in _SCANNABLE_BASENAMES:
        return True
    dot_idx = basename.rfind(".")
    if dot_idx >= 0:
        ext = basename[dot_idx:]
        return ext in _SCANNABLE_EXTENSIONS
    return False


class BuilderAgent(Agent):
    """Build validation agent — runs as a pipeline stage after diffs."""

    def __init__(self, agent_id: str, team_id: str, **kwargs: Any):
        super().__init__(
            agent_id=agent_id,
            role="builder",
            team_id=team_id,
            **kwargs,
        )
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

        self._probe_result: dict | None = None
        self._user_id: str = ""

    async def run_probe(
        self,
        task: Task,
        user_id: str,
        repo_files: list[str],
        changed_files: list[str],
        workspace: Any | None = None,
    ) -> dict:
        """Run the pre-build environment probe via the Go sandbox service.

        Collects scannable file contents from the workspace cache and sends
        them to the Go probe endpoint for env-var detection and secret
        cross-referencing.
        """
        from ..go_client import GoClient

        go_client: GoClient | None = getattr(self, "_go_client", None)
        if go_client is None and hasattr(self, "config"):
            from ..agents_config import get_config
            go_client = GoClient(self.token or "")

        if go_client is None:
            logger.warning("builder: no go_client available, skipping probe")
            return {}

        file_contents: dict[str, str] = {}
        scan_count = 0

        if workspace is not None:
            # Collect from workspace cache — files already fetched by agents.
            for fpath, content in workspace._file_cache.items():
                if scan_count >= _MAX_SCAN_FILES:
                    break
                if _is_scannable(fpath) and len(content) <= _MAX_FILE_SIZE_FOR_PROBE:
                    file_contents[fpath] = content
                    scan_count += 1

            # Also try to read build config files and Dockerfiles if not cached.
            for fpath in repo_files:
                if scan_count >= _MAX_SCAN_FILES:
                    break
                basename = fpath.rsplit("/", 1)[-1] if "/" in fpath else fpath
                if basename in BUILD_FILE_PATTERNS and fpath not in file_contents:
                    try:
                        content = await workspace.read_file(fpath)
                        if len(content) <= _MAX_FILE_SIZE_FOR_PROBE:
                            file_contents[fpath] = content
                            scan_count += 1
                    except Exception:
                        pass

        self._user_id = user_id

        try:
            probe = await go_client.sandbox_probe(
                task_id=task.id,
                repo_id=task.repo_id,
                user_id=user_id,
                repo_files=repo_files,
                changed_files=changed_files,
                file_contents=file_contents,
            )
            self._probe_result = probe
            logger.info(
                "builder: probe complete — build_system=%s, detected_envs=%d, "
                "matched=%d, missing=%d, ready=%s",
                probe.get("build_system", "?"),
                len(probe.get("detected_envs", [])),
                len(probe.get("matched_secrets", [])),
                len(probe.get("missing_secrets", [])),
                probe.get("ready", False),
            )
            return probe
        except Exception as e:
            logger.warning("builder: probe failed: %s", e)
            return {}

    def build_system_prompt(self, task: Task) -> str:
        plan_section = ""
        if task.plan_document:
            plan_section = f"""
## Approved Plan
```json
{json.dumps(task.plan_document, indent=2)}
```
"""

        probe_section = ""
        if self._probe_result:
            probe_section = f"""
## Pre-Build Probe Results
```json
{json.dumps(self._probe_result, indent=2)}
```

Use these probe results in your analysis. The probe has already:
- Detected the build system and recommended command
- Scanned source files for environment variable references
- Cross-referenced detected env vars with the user's repo secrets
- Identified missing secrets that may need to be added

Focus on validating the probe findings and identifying any additional
build risks not covered by the automated scan.
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
{plan_section}{probe_section}

## What You Must Do

### Step 1: Review Probe Results
If probe results are available above, review them for accuracy.
Otherwise detect the build system by reading repository root files.

### Step 2: Validate Environment Variables
Confirm the probe's env-var findings by searching the codebase:
- Are there env vars the probe missed?
- Are any "missing" secrets actually optional or have defaults in code?

### Step 3: Assess Build Readiness
Based on the probe and your analysis:
1. Build system (command, base image)
2. Environment variables found (name, file)
3. Which env vars are covered by repo secrets vs. missing
4. Recommended build command
5. Build risk assessment for the proposed diffs

### Step 4: Complete
Call `complete_task` with your analysis summary.

## CRITICAL RULES
- Do NOT attempt to edit or create files
- Do NOT hallucinate build results — you are analysing, not building
- Be specific: cite exact file paths and line numbers
- If SANDBOX.md or BUILD.md exists, use its instructions verbatim
"""

    def build_probe_system_prompt(self, task: Task) -> str:
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

        combined = "\n\n".join(output_parts)

        # Attach probe result as structured metadata in the output.
        if self._probe_result:
            combined += f"\n\n---\n## Probe Result (structured)\n```json\n{json.dumps(self._probe_result, indent=2)}\n```"

        return AgentResult(output=combined)
