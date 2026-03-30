"""RTVortex Python SDK — official client for the RTVortex API."""

from rtvortex_sdk.client import AsyncRTVortexClient, RTVortexClient
from rtvortex_sdk.exceptions import (
    AuthenticationError,
    NotFoundError,
    QuotaExceededError,
    RTVortexError,
    ServerError,
    ValidationError,
)
from rtvortex_sdk.models import (
    AdminStats,
    HealthStatus,
    IndexStatus,
    MemberListResponse,
    Org,
    OrgListResponse,
    OrgMember,
    PaginationOptions,
    ProgressEvent,
    Review,
    ReviewComment,
    ReviewListResponse,
    Repo,
    RepoListResponse,
    SwarmAgent,
    SwarmDiff,
    SwarmDiffComment,
    SwarmOverview,
    SwarmPlan,
    SwarmPlanStep,
    SwarmTask,
    SwarmTaskListResponse,
    SwarmTeam,
    SwarmWsEvent,
    User,
)

__version__ = "0.0.0"

__all__ = [
    # Clients
    "RTVortexClient",
    "AsyncRTVortexClient",
    # Exceptions
    "RTVortexError",
    "AuthenticationError",
    "NotFoundError",
    "QuotaExceededError",
    "ServerError",
    "ValidationError",
    # Models
    "AdminStats",
    "HealthStatus",
    "IndexStatus",
    "MemberListResponse",
    "Org",
    "OrgListResponse",
    "OrgMember",
    "PaginationOptions",
    "ProgressEvent",
    "Review",
    "ReviewComment",
    "ReviewListResponse",
    "Repo",
    "RepoListResponse",
    "SwarmAgent",
    "SwarmDiff",
    "SwarmDiffComment",
    "SwarmOverview",
    "SwarmPlan",
    "SwarmPlanStep",
    "SwarmTask",
    "SwarmTaskListResponse",
    "SwarmTeam",
    "SwarmWsEvent",
    "User",
]
