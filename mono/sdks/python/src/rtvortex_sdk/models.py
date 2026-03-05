"""Pydantic models for RTVortex API responses."""

from __future__ import annotations

from datetime import datetime
from typing import Any, Generic, Optional, TypeVar

from pydantic import BaseModel, Field


# ── Generic Pagination ───────────────────────────────────────────────────────

T = TypeVar("T")


class PaginationOptions(BaseModel):
    """Options for paginated list requests."""

    limit: int = 20
    offset: int = 0


class PaginatedResponse(BaseModel, Generic[T]):
    """Base class for paginated API responses."""

    total: int = 0
    limit: int = 20
    offset: int = 0


# ── User ─────────────────────────────────────────────────────────────────────


class User(BaseModel):
    id: str
    email: str
    display_name: str = ""
    avatar_url: str = ""
    provider: str = ""
    created_at: Optional[datetime] = None


class UserUpdateRequest(BaseModel):
    display_name: Optional[str] = None
    avatar_url: Optional[str] = None


# ── Organization ─────────────────────────────────────────────────────────────


class Org(BaseModel):
    id: str
    name: str
    slug: str
    plan: str = "free"
    settings: Optional[dict[str, Any]] = None
    created_at: Optional[datetime] = None
    updated_at: Optional[datetime] = None


class OrgListResponse(PaginatedResponse[Org]):
    organizations: list[Org] = Field(default_factory=list)


class OrgMember(BaseModel):
    user_id: str
    email: str = ""
    display_name: str = ""
    avatar_url: str = ""
    role: str = "member"
    joined_at: Optional[datetime] = None


class MemberListResponse(PaginatedResponse[OrgMember]):
    members: list[OrgMember] = Field(default_factory=list)


# ── Repository ───────────────────────────────────────────────────────────────


class Repo(BaseModel):
    id: str
    org_id: str = ""
    platform: str = ""
    owner: str = ""
    name: str = ""
    default_branch: str = "main"
    clone_url: str = ""
    external_id: str = ""
    webhook_secret: str = ""
    config: Optional[dict[str, Any]] = None
    created_at: Optional[datetime] = None
    updated_at: Optional[datetime] = None


class RepoListResponse(PaginatedResponse[Repo]):
    repositories: list[Repo] = Field(default_factory=list)


# ── Review ───────────────────────────────────────────────────────────────────


class ReviewComment(BaseModel):
    id: str = ""
    review_id: str = ""
    file_path: str = ""
    line_number: int = 0
    severity: str = "info"
    category: str = ""
    message: str = ""
    suggestion: str = ""
    created_at: Optional[datetime] = None


class Review(BaseModel):
    id: str
    repo_id: str = ""
    pr_number: int = 0
    status: str = "pending"
    comments_count: int = 0
    current_step: str = ""
    total_steps: Optional[int] = None
    steps_completed: int = 0
    created_at: Optional[datetime] = None
    completed_at: Optional[datetime] = None
    metadata: Optional[dict[str, Any]] = None


class ReviewListResponse(PaginatedResponse[Review]):
    reviews: list[Review] = Field(default_factory=list)


# ── Streaming / Progress ────────────────────────────────────────────────────


class ProgressEvent(BaseModel):
    """A single progress event from the SSE review stream."""

    event: str = "progress"  # progress | complete | error
    step: str = ""
    step_index: int = 0
    total_steps: int = 0
    status: str = ""  # running | completed | failed
    message: str = ""
    metadata: Optional[dict[str, Any]] = None


# ── Index ────────────────────────────────────────────────────────────────────


class IndexStatus(BaseModel):
    repo_id: str = ""
    status: str = "idle"
    progress: int = 0
    job_id: str = ""
    started_at: Optional[datetime] = None
    completed_at: Optional[datetime] = None


# ── Admin ────────────────────────────────────────────────────────────────────


class AdminStats(BaseModel):
    total_users: int = 0
    total_orgs: int = 0
    total_repos: int = 0
    total_reviews: int = 0
    reviews_today: int = 0
    active_jobs: int = 0


# ── Health ───────────────────────────────────────────────────────────────────


class HealthStatus(BaseModel):
    status: str = "unknown"
    checks: Optional[dict[str, str]] = None
    time: str = ""
