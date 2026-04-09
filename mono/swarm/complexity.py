"""Dynamic Team Formation — Complexity Scoring & Team Recommendation.

This module provides the Python-side entry point for Phase 9's dynamic team
formation pipeline.  It extracts complexity signals from an orchestrator plan,
optionally pre-computes a local complexity score, and calls the Go server's
``/internal/swarm/team-recommend`` endpoint for the authoritative ELO-aware
team composition.

Usage (inside redis_consumer.py after plan approval)::

    from mono.swarm.complexity import recommend_team_for_task

    formation = await recommend_team_for_task(go_client, task_id, repo_id, plan_dict)
    roles = formation["recommended_roles"]
    team_size = formation["team_size"]
"""

from __future__ import annotations

import logging
import math
import re
from dataclasses import dataclass, field
from typing import Any

logger = logging.getLogger(__name__)


# ── Local Signal Extraction ─────────────────────────────────────────────────

_SECURITY_KEYWORDS = frozenset({
    "auth", "security", "permission", "rbac", "jwt", "token",
    "encrypt", "credential", "secret", "vulnerability", "cve",
})

_TEST_PATTERNS = re.compile(
    r"(_test\.|\.test\.|\.spec\.|/test/|/tests/|__tests__/)", re.IGNORECASE
)

_EXTENSION_TO_LANG: dict[str, str] = {
    ".go": "go", ".py": "python", ".ts": "typescript", ".tsx": "typescript",
    ".js": "javascript", ".jsx": "javascript", ".java": "java", ".rs": "rust",
    ".c": "c", ".h": "c", ".cpp": "cpp", ".cc": "cpp", ".hpp": "cpp",
    ".rb": "ruby", ".sql": "sql", ".proto": "protobuf",
    ".yaml": "yaml", ".yml": "yaml", ".json": "json",
    ".md": "markdown", ".mdx": "markdown", ".css": "css", ".scss": "css",
    ".html": "html", ".sh": "shell", ".bash": "shell",
}


@dataclass
class ComplexitySignals:
    """Raw complexity signals extracted from a plan document."""

    file_count: int = 0
    step_count: int = 0
    description_length: int = 0
    language_count: int = 0
    test_files: int = 0
    has_migrations: bool = False
    cross_package: bool = False
    has_api_changes: bool = False
    has_security_impact: bool = False
    has_ui_changes: bool = False
    languages: list[str] = field(default_factory=list)

    def to_dict(self) -> dict[str, Any]:
        return {
            "file_count": self.file_count,
            "step_count": self.step_count,
            "description_length": self.description_length,
            "language_count": self.language_count,
            "test_files": self.test_files,
            "has_migrations": self.has_migrations,
            "cross_package": self.cross_package,
            "has_api_changes": self.has_api_changes,
            "has_security_impact": self.has_security_impact,
            "has_ui_changes": self.has_ui_changes,
            "languages": self.languages,
        }


def _file_ext(path: str) -> str:
    """Extract file extension from a path."""
    idx = path.rfind(".")
    if idx < 0 or "/" in path[idx:]:
        return ""
    return path[idx:]


def extract_signals(plan: dict[str, Any]) -> ComplexitySignals:
    """Extract complexity signals from a plan dict.

    This mirrors the Go ``ExtractSignalsFromPlan`` function so that the
    Python side can optionally pre-compute signals and pass them as
    overrides.
    """
    signals = ComplexitySignals()
    affected_files: list[str] = plan.get("affected_files") or []
    steps = plan.get("steps") or []
    summary = plan.get("summary") or ""

    signals.file_count = len(affected_files)
    signals.step_count = len(steps)
    signals.description_length = len(summary)

    lang_set: set[str] = set()
    top_dirs: set[str] = set()

    for f in affected_files:
        ext = _file_ext(f)
        lang = _EXTENSION_TO_LANG.get(ext.lower(), "")
        if lang:
            lang_set.add(lang)

        lower = f.lower()

        if _TEST_PATTERNS.search(lower):
            signals.test_files += 1

        if "migration" in lower or "migrate" in lower:
            signals.has_migrations = True

        if any(kw in lower for kw in ("handler", "route", "endpoint", "api", "proto", "schema")):
            signals.has_api_changes = True

        if any(kw in lower for kw in ("component", "page.tsx", ".css", ".scss", "/web/", "/ui/")):
            signals.has_ui_changes = True

        parts = f.split("/", 2)
        if len(parts) >= 2:
            top_dirs.add(f"{parts[0]}/{parts[1]}")

    signals.cross_package = len(top_dirs) >= 3

    summary_lower = summary.lower()
    for kw in _SECURITY_KEYWORDS:
        if kw in summary_lower:
            signals.has_security_impact = True
            break

    signals.languages = sorted(lang_set)
    signals.language_count = len(signals.languages)
    return signals


# ── Local Complexity Score ──────────────────────────────────────────────────

def _sigmoid(value: float, midpoint: float) -> float:
    if value <= 0:
        return 0.0
    return value / (value + midpoint)


def compute_complexity_score(s: ComplexitySignals) -> float:
    """Compute a normalised 0.0–1.0 complexity score locally.

    This mirrors the Go ``ComputeComplexityScore`` function.
    """
    file_dim = _sigmoid(s.file_count, 8)
    step_dim = _sigmoid(s.step_count, 6)
    desc_dim = _sigmoid(s.description_length, 300)
    lang_dim = _sigmoid(s.language_count, 2)

    flag_score = 0.0
    if s.has_migrations:
        flag_score += 0.15
    if s.cross_package:
        flag_score += 0.12
    if s.has_api_changes:
        flag_score += 0.10
    if s.has_security_impact:
        flag_score += 0.10
    if s.has_ui_changes:
        flag_score += 0.05
    if s.test_files > 0:
        flag_score += 0.03 * min(s.test_files, 3)

    raw = (
        0.30 * file_dim
        + 0.25 * step_dim
        + 0.10 * desc_dim
        + 0.10 * lang_dim
        + 0.25 * min(flag_score, 0.50)
    )
    return max(0.0, min(1.0, raw))


_THRESHOLD_TRIVIAL = 0.15
_THRESHOLD_SMALL = 0.35
_THRESHOLD_MEDIUM = 0.60
_THRESHOLD_LARGE = 0.80


def complexity_label(score: float) -> str:
    """Map a score to a human-readable label (mirrors Go)."""
    if score < _THRESHOLD_TRIVIAL:
        return "trivial"
    if score < _THRESHOLD_SMALL:
        return "small"
    if score < _THRESHOLD_MEDIUM:
        return "medium"
    if score < _THRESHOLD_LARGE:
        return "large"
    return "critical"


# ── Recommend Team (Go call) ───────────────────────────────────────────────

async def recommend_team_for_task(
    go_client: Any,
    task_id: str,
    repo_id: str,
    plan: dict[str, Any] | None = None,
    *,
    send_signals: bool = True,
) -> dict[str, Any]:
    """Call the Go team-recommend endpoint with optional local signal override.

    1. Locally extract complexity signals from the plan (if provided).
    2. Call Go ``POST /internal/swarm/team-recommend`` which computes the
       authoritative ELO-aware team composition.
    3. Return the full ``TeamFormation`` dict.

    If the Go call fails, a sensible fallback is returned.
    """
    signals_dict: dict[str, Any] | None = None
    local_label = "unknown"

    if plan and send_signals:
        signals = extract_signals(plan)
        score = compute_complexity_score(signals)
        local_label = complexity_label(score)
        signals_dict = signals.to_dict()
        logger.info(
            "team-formation: local complexity score=%.3f label=%s task=%s",
            score, local_label, task_id,
        )

    try:
        formation = await go_client.recommend_team(
            task_id=task_id,
            repo_id=repo_id,
            plan=plan,
            signals=signals_dict,
        )
        logger.info(
            "team-formation: Go recommendation received task=%s complexity=%s team_size=%d roles=%s",
            task_id,
            formation.get("complexity_label", "?"),
            formation.get("team_size", 0),
            formation.get("recommended_roles", []),
        )
        return formation
    except Exception:
        logger.warning(
            "team-formation: Go call failed, using local fallback task=%s",
            task_id,
            exc_info=True,
        )
        return _local_fallback(plan, local_label)


def _local_fallback(
    plan: dict[str, Any] | None,
    label: str,
) -> dict[str, Any]:
    """Produce a reasonable team formation without the Go server."""
    role_map: dict[str, list[str]] = {
        "trivial": ["senior_dev"],
        "small": ["senior_dev", "qa"],
        "medium": ["senior_dev", "qa"],
        "large": ["architect", "senior_dev", "junior_dev", "qa", "security"],
        "critical": ["architect", "senior_dev", "senior_dev", "junior_dev", "qa", "security"],
    }
    roles = role_map.get(label, ["senior_dev", "qa"])
    return {
        "complexity_score": 0.0,
        "complexity_label": label,
        "recommended_roles": roles,
        "role_elos": {},
        "team_size": len(roles) + 1,
        "reasoning": f"Local fallback: {label} complexity",
        "strategy": "static",
        "input_signals": {},
    }
