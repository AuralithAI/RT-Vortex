"""Pydantic domain models shared across the swarm subsystem.

These models mirror the Go-side structs and are used for request/response
serialisation, internal hand-offs, and typed access to task, agent, team,
diff, and plan data throughout the Python codebase.
"""

from __future__ import annotations

from datetime import datetime
from typing import Any

from pydantic import BaseModel, Field


class Task(BaseModel):
    """A code-modification task assigned to an agent team.

    Tasks flow through the statuses ``submitted → planning → in_progress →
    review → completed`` (or ``failed``).  The ``plan_document`` is populated
    by the orchestrator and must be approved by a human before the task moves
    to ``in_progress``.
    """

    id: str
    repo_id: str
    description: str
    status: str = "submitted"
    plan_document: dict[str, Any] | None = None
    assigned_team_id: str | None = None
    assigned_agents: list[str] = Field(default_factory=list)
    pr_url: str | None = None
    pr_number: int | None = None
    human_rating: int | None = None
    human_comment: str | None = None
    submitted_by: str | None = None
    created_at: datetime | None = None
    completed_at: datetime | None = None
    timeout_at: datetime | None = None


class SwarmAgent(BaseModel):
    """Runtime metadata for a registered agent instance.

    Each agent registers with the Go server on startup and receives a JWT.
    The ELO score is updated after every human-rated task completion.
    """

    id: str
    role: str
    team_id: str | None = None
    status: str = "offline"
    elo_score: float = 1200.0
    tasks_done: int = 0
    tasks_rated: int = 0
    avg_rating: float = 0.0
    hostname: str = ""
    version: str = ""


class Team(BaseModel):
    """A transient group of agents working on a single task.

    Teams are created dynamically when a task is assigned and dissolved on
    completion.  The orchestrator acts as lead and may request more agents via
    ``declare_team_size``.
    """

    id: str
    name: str = ""
    lead_agent_id: str | None = None
    status: str = "idle"
    agent_ids: list[str] = Field(default_factory=list)


class Diff(BaseModel):
    """A single-file change produced by an agent.

    The ``unified_diff`` field contains a standard ``git diff`` so the
    reviewer UI can render it inline.  ``status`` tracks the human review
    outcome (``pending → approved | rejected``).
    """

    id: str = ""
    task_id: str = ""
    file_path: str
    change_type: str = "modified"  # modified, added, deleted, renamed
    original: str = ""
    proposed: str = ""
    unified_diff: str = ""
    agent_id: str | None = None
    status: str = "pending"


class DiffComment(BaseModel):
    """An inline comment on a diff, authored by an agent or a human reviewer."""

    id: str = ""
    diff_id: str = ""
    author_type: str = ""  # agent | user
    author_id: str = ""
    line_number: int = 0
    content: str = ""


class EngineResult(BaseModel):
    """Typed wrapper for results returned by the C++ engine gRPC service.

    ``chunks`` contains the ranked code snippets.  ``fused_context`` is the
    engine's pre-built RAG context string suitable for direct injection into
    an LLM prompt.
    """

    chunks: list[dict[str, Any]] = Field(default_factory=list)
    requires_llm: bool = False
    confidence: float = 0.0
    fused_context: str = ""


class AgentResult(BaseModel):
    """Structured output returned by an agent after processing a task.

    The orchestrator populates ``plan``; the senior-dev populates ``diffs``.
    If the agent fails, ``error`` contains the reason and other fields may be
    empty.
    """

    output: str = ""
    plan: dict[str, Any] | None = None
    diffs: list[Diff] = Field(default_factory=list)
    error: str | None = None


class PlanDocument(BaseModel):
    """A structured implementation plan produced by the orchestrator.

    Plans must be approved by a human reviewer before the task moves to
    ``in_progress``.  ``agents_needed`` is advisory — the orchestrator uses
    ``declare_team_size`` to make the actual request.
    """

    summary: str = ""
    steps: list[dict[str, Any]] = Field(default_factory=list)
    affected_files: list[str] = Field(default_factory=list)
    estimated_complexity: str = "medium"  # small, medium, large
    agents_needed: int = 2
