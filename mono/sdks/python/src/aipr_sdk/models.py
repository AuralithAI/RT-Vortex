"""Pydantic models for AI-PR-Reviewer API."""

from datetime import datetime
from typing import Any

from pydantic import BaseModel, Field


class FileChange(BaseModel):
    """Represents a file change in a pull request."""

    path: str
    status: str = "modified"
    additions: int | None = None
    deletions: int | None = None
    patch: str | None = None


class ReviewContext(BaseModel):
    """Additional context for the review."""

    pr_title: str | None = None
    pr_description: str | None = None
    author: str | None = None
    labels: list[str] = Field(default_factory=list)


class ReviewRequest(BaseModel):
    """Request to submit a pull request for review."""

    repository_url: str
    pull_request_id: int | None = None
    base_sha: str | None = None
    head_sha: str | None = None
    diff_content: str | None = None
    files: list[FileChange] = Field(default_factory=list)
    context: ReviewContext | None = None
    config: dict[str, Any] = Field(default_factory=dict)


class ReviewComment(BaseModel):
    """A single review comment."""

    file: str
    line: int | None = None
    end_line: int | None = None
    severity: str = "info"
    category: str = "general"
    message: str
    suggestion: str | None = None
    confidence: float | None = None


class ReviewMetrics(BaseModel):
    """Metrics from the review."""

    files_reviewed: int = 0
    lines_reviewed: int = 0
    total_comments: int = 0
    critical_issues: int = 0
    processing_time_ms: int | None = None
    llm_tokens_used: int | None = None


class ReviewResponse(BaseModel):
    """Response from a review request."""

    review_id: str
    status: str
    overall_assessment: str | None = None
    summary: str | None = None
    comments: list[ReviewComment] = Field(default_factory=list)
    metrics: ReviewMetrics | None = None
    created_at: datetime | None = None
    completed_at: datetime | None = None
    error: str | None = None

    @property
    def is_complete(self) -> bool:
        """Check if the review is complete."""
        return self.status.lower() in ("completed", "failed")

    @property
    def is_success(self) -> bool:
        """Check if the review was successful."""
        return self.status.lower() == "completed"


class IndexRequest(BaseModel):
    """Request to index a repository."""

    repository_url: str
    branch: str = "main"
    commit_sha: str | None = None
    include_patterns: list[str] = Field(default_factory=list)
    exclude_patterns: list[str] = Field(default_factory=list)
    force_reindex: bool = False


class IndexResponse(BaseModel):
    """Response from an index request."""

    job_id: str
    status: str
    repository_url: str | None = None
    branch: str | None = None
    commit_sha: str | None = None
    files_indexed: int | None = None
    chunks_created: int | None = None
    progress_percent: float | None = None
    created_at: datetime | None = None
    completed_at: datetime | None = None
    error: str | None = None

    @property
    def is_complete(self) -> bool:
        """Check if the indexing job is complete."""
        return self.status.lower() in ("completed", "failed")

    @property
    def is_success(self) -> bool:
        """Check if the indexing job was successful."""
        return self.status.lower() == "completed"
