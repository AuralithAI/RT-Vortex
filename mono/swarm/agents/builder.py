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


# ── Build failure analysis ───────────────────────────────────────────────────

# Patterns that indicate a transient (retryable) failure vs. a code error.
_TRANSIENT_PATTERNS = [
    "connection refused",
    "connection reset",
    "timeout",
    "timed out",
    "network unreachable",
    "temporary failure",
    "could not resolve host",
    "no space left on device",
    "cannot allocate memory",
    "oom",
    "killed",
    "signal: killed",
    "429 too many requests",
    "502 bad gateway",
    "503 service unavailable",
]

_COMPILE_ERROR_PATTERNS = [
    "error:",
    "undefined reference",
    "cannot find symbol",
    "syntax error",
    "compilation failed",
    "build failed",
    "type mismatch",
    "undeclared identifier",
    "no such module",
    "import error",
    "modulenotfounderror",
]

_DEPENDENCY_PATTERNS = [
    "could not resolve dependencies",
    "no matching version",
    "package not found",
    "module not found",
    "404 not found",
    "failed to fetch",
    "unable to locate package",
    "no matching distribution",
    "unresolved dependency",
]

_SECRET_PATTERNS = [
    "authentication failed",
    "unauthorized",
    "401",
    "403 forbidden",
    "permission denied",
    "access denied",
    "invalid token",
    "bad credentials",
]


def _analyse_build_failure(logs: str, exit_code: int) -> dict:
    """Classify a build failure from log output.

    Returns a dict with: category, root_cause, suggestion, retryable.
    """
    lower = logs.lower() if logs else ""

    # Check secret / auth failures first — not retryable without human action.
    for pat in _SECRET_PATTERNS:
        if pat in lower:
            return {
                "category": "authentication",
                "root_cause": f"Authentication or permission error detected (matched: '{pat}')",
                "suggestion": "Check that all required secrets are added and valid in the Build Secrets UI.",
                "retryable": False,
            }

    # Transient infrastructure failures — retryable.
    for pat in _TRANSIENT_PATTERNS:
        if pat in lower:
            return {
                "category": "transient",
                "root_cause": f"Transient infrastructure error (matched: '{pat}')",
                "suggestion": "This looks like a temporary failure. Retrying may resolve it.",
                "retryable": True,
            }

    # Dependency resolution — sometimes retryable (registry flake).
    for pat in _DEPENDENCY_PATTERNS:
        if pat in lower:
            return {
                "category": "dependency",
                "root_cause": f"Dependency resolution failed (matched: '{pat}')",
                "suggestion": "A required dependency could not be resolved. Check version constraints or registry availability.",
                "retryable": True,
            }

    # Compile / syntax errors — not retryable without code changes.
    for pat in _COMPILE_ERROR_PATTERNS:
        if pat in lower:
            return {
                "category": "compilation",
                "root_cause": f"Compilation or type error detected (matched: '{pat}')",
                "suggestion": "The code has compilation errors that require code changes to fix.",
                "retryable": False,
            }

    # Timeout (exit code -1 is our convention from sandbox.go).
    if exit_code == -1:
        return {
            "category": "timeout",
            "root_cause": "Build exceeded the timeout limit",
            "suggestion": "Consider increasing the timeout or optimising the build.",
            "retryable": True,
        }

    # Unknown failure.
    return {
        "category": "unknown",
        "root_cause": f"Build failed with exit code {exit_code}",
        "suggestion": "Review the build logs for details.",
        "retryable": True,
    }


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
        self._build_result: dict | None = None

    async def confirm_and_execute(
        self,
        task: Task,
        user_id: str,
        probe_result: dict,
        changed_files: list[str] | None = None,
        workspace_files: dict[str, str] | None = None,
    ) -> dict:
        """Request HITL confirmation of the build plan and execute if approved.

        Sends a structured question to the human with the probe summary.
        If missing secrets exist the human can approve (proceed without them),
        reject (abort the build), or add the secrets first and re-probe.
        On approval (or timeout) the build executes via resolve-execute.
        """
        from ..go_client import GoClient

        go_client: GoClient | None = getattr(self, "_go_client", None)
        if go_client is None:
            logger.warning("builder: no go_client, skipping confirm_and_execute")
            return {"status": "skipped", "reason": "no_go_client"}

        build_system = probe_result.get("build_system", "unknown")
        build_command = probe_result.get("build_command", "")
        base_image = probe_result.get("base_image", "")
        matched = probe_result.get("matched_secrets", [])
        missing = probe_result.get("missing_secrets", [])
        ready = probe_result.get("ready", False)
        recommendations = probe_result.get("recommendations", [])

        # Build a human-readable plan summary for HITL.
        lines = [
            f"**Build System:** {build_system}",
            f"**Command:** `{build_command}`",
            f"**Base Image:** `{base_image}`",
        ]
        if matched:
            lines.append(f"**Secrets Available:** {', '.join(matched)}")
        if missing:
            lines.append(f"**⚠ Missing Secrets:** {', '.join(missing)}")
        if recommendations:
            lines.append("**Recommendations:**")
            for rec in recommendations:
                lines.append(f"  - {rec}")

        if ready:
            lines.append("\n✅ Build is ready to execute.")
        else:
            lines.append(
                "\n⚠ Build may fail — missing secrets or unknown build system."
            )

        plan_summary = "\n".join(lines)

        # Determine HITL question urgency and timeout.
        if missing:
            urgency = "high"
            question = (
                "The sandbox build plan has **missing secrets**. "
                "Should I proceed with the build anyway?\n\n"
                + plan_summary
                + "\n\nReply **yes** to proceed, **no** to abort, "
                "or **add secrets** if you want to add them first."
            )
        else:
            urgency = "normal"
            question = (
                "Ready to run a sandbox build. Please confirm:\n\n"
                + plan_summary
                + "\n\nReply **yes** to proceed or **no** to skip the build."
            )

        # Post the plan summary as an agent message for the UI.
        try:
            await go_client.post_agent_message(
                task_id=task.id,
                message={
                    "agent_id": self.agent_id,
                    "agent_role": "builder",
                    "kind": "build_plan",
                    "content": plan_summary,
                    "metadata": {
                        "build_system": build_system,
                        "ready": ready,
                        "missing_secrets": missing,
                        "matched_secrets": matched,
                    },
                },
            )
        except Exception:
            pass

        # Ask the human for confirmation.
        try:
            hitl_resp = await go_client.ask_human(
                question=question,
                context=f"Sandbox build for task {task.id}",
                urgency=urgency,
                timeout=120,
            )
        except Exception as e:
            logger.warning("builder: HITL ask failed: %s — auto-approving", e)
            hitl_resp = {"response": "yes", "timed_out": "true"}

        response_text = hitl_resp.get("response", "").strip().lower()
        timed_out = hitl_resp.get("timed_out", "false") == "true"

        # Parse the human response.
        approved = response_text.startswith("yes") or timed_out
        rejected = response_text.startswith("no")
        wants_secrets = "add secret" in response_text

        if wants_secrets:
            logger.info("builder: human wants to add secrets first — aborting build")
            return {
                "status": "pending_secrets",
                "reason": "human requested adding secrets before build",
                "missing_secrets": missing,
            }

        if rejected:
            logger.info("builder: human rejected the build plan")
            return {"status": "rejected", "reason": "human rejected build plan"}

        # Approved — execute the build.
        logger.info(
            "builder: build approved (timed_out=%s) — executing", timed_out
        )

        secret_refs = matched + missing  # attempt all — Go resolves what it can
        try:
            result = await go_client.sandbox_resolve_execute(
                task_id=task.id,
                repo_id=task.repo_id,
                user_id=user_id,
                build_system=build_system,
                command=build_command,
                base_image=base_image,
                secret_refs=secret_refs,
                sandbox_mode=True,
                changed_files=changed_files or [],
                workspace_files=workspace_files or {},
                collect_artifacts=True,
            )
            self._build_result = result

            # Handle skip response from Go.
            if result.get("skipped"):
                logger.info(
                    "builder: build skipped — %s", result.get("reason", "")
                )
                try:
                    await go_client.post_agent_message(
                        task_id=task.id,
                        message={
                            "agent_id": self.agent_id,
                            "agent_role": "builder",
                            "kind": "build_skipped",
                            "content": result.get("reason", "build skipped"),
                            "metadata": {"build_id": result.get("build_id", "")},
                        },
                    )
                except Exception:
                    pass
                return {
                    "status": "skipped",
                    "build_id": result.get("build_id", ""),
                    "reason": result.get("reason", ""),
                    "exit_code": 0,
                }

            exit_code = result.get("exit_code", -1)
            build_id = result.get("build_id", "")
            complexity = result.get("complexity")
            fast_path = result.get("fast_path", False)
            fingerprint = result.get("fingerprint")
            image_tag = result.get("image_tag", "")
            logger.info(
                "builder: build finished — exit_code=%d, build_id=%s, "
                "complexity=%s, fast_path=%s, resolved=%s, failed=%s",
                exit_code,
                build_id,
                complexity.get("label", "?") if complexity else "n/a",
                fast_path,
                result.get("resolved_secrets", []),
                result.get("failed_secrets", []),
            )

            # Post build result as agent message.
            build_status = "success" if exit_code == 0 else "failed"
            try:
                await go_client.post_agent_message(
                    task_id=task.id,
                    message={
                        "agent_id": self.agent_id,
                        "agent_role": "builder",
                        "kind": "build_result",
                        "content": f"Build {build_status} (exit code {exit_code})",
                        "metadata": {
                            "build_id": build_id,
                            "exit_code": exit_code,
                            "duration": result.get("duration", ""),
                            "resolved_secrets": result.get("resolved_secrets", []),
                            "failed_secrets": result.get("failed_secrets", []),
                            "artifacts": result.get("artifacts", []),
                            "workspace_injected": result.get("workspace_injected", False),
                            "complexity": complexity,
                            "fast_path": fast_path,
                            "fingerprint": fingerprint,
                            "image_tag": image_tag,
                        },
                    },
                )
            except Exception:
                pass

            initial = {
                "status": build_status,
                "build_id": build_id,
                "exit_code": exit_code,
                "duration": result.get("duration", ""),
                "resolved_secrets": result.get("resolved_secrets", []),
                "failed_secrets": result.get("failed_secrets", []),
                "logs_truncated": len(result.get("logs", "")) > 1024,
                "artifacts": result.get("artifacts", []),
                "workspace_injected": result.get("workspace_injected", False),
                "complexity": complexity,
                "fast_path": fast_path,
                "fingerprint": fingerprint,
                "image_tag": image_tag,
            }

            if build_status == "failed":
                return await self.analyse_and_retry(task, initial)

            return initial

        except Exception as e:
            logger.error("builder: build execution failed: %s", e)
            return {"status": "error", "reason": str(e)}

    async def analyse_and_retry(
        self,
        task: Task,
        build_result: dict,
        max_retries: int = 2,
    ) -> dict:
        """Analyse a failed build and retry up to max_retries times.

        For each retry:
        1. Fetch full logs from the Go server
        2. Analyse the failure (extract error category + root cause)
        3. Post the analysis as an agent message for the UI
        4. Trigger a retry via the Go retry endpoint
        5. If the retry succeeds, return the result

        Returns the final build result dict (success or last failure).
        """
        from ..go_client import GoClient

        go_client: GoClient | None = getattr(self, "_go_client", None)
        if go_client is None:
            return build_result

        current = build_result
        build_id = current.get("build_id", "")
        if not build_id:
            logger.warning("builder: no build_id for retry — skipping")
            return current

        for attempt in range(1, max_retries + 1):
            exit_code = current.get("exit_code", -1)
            if exit_code == 0:
                return current

            # Fetch full logs for analysis.
            logs = current.get("logs", "")
            if not logs and build_id:
                try:
                    log_data = await go_client.sandbox_build_logs(build_id)
                    logs = log_data.get("logs", "")
                except Exception:
                    pass

            # Analyse the failure.
            analysis = _analyse_build_failure(logs, exit_code)

            logger.info(
                "builder: retry %d/%d — category=%s, build_id=%s",
                attempt, max_retries, analysis["category"], build_id,
            )

            # Post analysis as agent message.
            try:
                await go_client.post_agent_message(
                    task_id=task.id,
                    message={
                        "agent_id": self.agent_id,
                        "agent_role": "builder",
                        "kind": "build_failure_analysis",
                        "content": (
                            f"**Build failed** (exit code {exit_code}, "
                            f"attempt {attempt}/{max_retries})\n\n"
                            f"**Category:** {analysis['category']}\n"
                            f"**Root cause:** {analysis['root_cause']}\n"
                            f"**Suggestion:** {analysis['suggestion']}"
                        ),
                        "metadata": {
                            "build_id": build_id,
                            "attempt": attempt,
                            "max_retries": max_retries,
                            "category": analysis["category"],
                        },
                    },
                )
            except Exception:
                pass

            # If the failure is not retryable, stop.
            if not analysis["retryable"]:
                logger.info(
                    "builder: failure not retryable (category=%s) — stopping",
                    analysis["category"],
                )
                return current

            # Trigger retry via Go.
            try:
                retry_result = await go_client.sandbox_retry_build(build_id)
                build_id = retry_result.get("build_id", build_id)
                self._build_result = retry_result

                retry_exit = retry_result.get("exit_code", -1)
                retry_status = "success" if retry_exit == 0 else "failed"

                logger.info(
                    "builder: retry %d result — exit_code=%d, build_id=%s",
                    attempt, retry_exit, build_id,
                )

                try:
                    await go_client.post_agent_message(
                        task_id=task.id,
                        message={
                            "agent_id": self.agent_id,
                            "agent_role": "builder",
                            "kind": "build_retry_result",
                            "content": (
                                f"Retry {attempt}/{max_retries}: "
                                f"**{retry_status}** (exit code {retry_exit})"
                            ),
                            "metadata": {
                                "build_id": build_id,
                                "attempt": attempt,
                                "exit_code": retry_exit,
                            },
                        },
                    )
                except Exception:
                    pass

                current = {
                    "status": retry_status,
                    "build_id": build_id,
                    "exit_code": retry_exit,
                    "duration": retry_result.get("duration", ""),
                    "logs": retry_result.get("logs", ""),
                    "resolved_secrets": retry_result.get("resolved_secrets", []),
                    "failed_secrets": retry_result.get("failed_secrets", []),
                    "retry_count": retry_result.get("retry_count", attempt),
                }

                if retry_exit == 0:
                    return current

            except Exception as e:
                logger.error("builder: retry %d failed: %s", attempt, e)
                return current

        return current

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

        # Attach build result if a build was executed.
        if self._build_result:
            combined += f"\n\n---\n## Build Result (structured)\n```json\n{json.dumps(self._build_result, indent=2)}\n```"

        return AgentResult(output=combined)
