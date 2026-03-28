"""Exception hierarchy for RTVortex SDK."""

from __future__ import annotations

from typing import Any, Optional


class RTVortexError(Exception):
    """Base exception for all RTVortex SDK errors."""

    def __init__(
        self,
        message: str,
        *,
        status_code: Optional[int] = None,
        body: Optional[Any] = None,
    ) -> None:
        super().__init__(message)
        self.message = message
        self.status_code = status_code
        self.body = body

    def __repr__(self) -> str:
        parts = [f"message={self.message!r}"]
        if self.status_code is not None:
            parts.append(f"status_code={self.status_code}")
        return f"{type(self).__name__}({', '.join(parts)})"


class AuthenticationError(RTVortexError):
    """Raised on HTTP 401 – invalid or expired token."""


class NotFoundError(RTVortexError):
    """Raised on HTTP 404 – resource does not exist."""


class ValidationError(RTVortexError):
    """Raised on HTTP 422 – invalid request payload."""


class QuotaExceededError(RTVortexError):
    """Raised on HTTP 403/429 – quota or rate limit exceeded."""


class ServerError(RTVortexError):
    """Raised on HTTP 5xx – server-side failure."""
