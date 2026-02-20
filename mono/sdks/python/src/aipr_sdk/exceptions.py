"""Exceptions for AI-PR-Reviewer SDK."""


class AIPRError(Exception):
    """Base exception for AIPR SDK errors."""

    pass


class AIPRAPIError(AIPRError):
    """Exception raised when an API request fails."""

    def __init__(
        self,
        message: str,
        status_code: int | None = None,
        response_body: str | None = None,
    ) -> None:
        super().__init__(message)
        self.status_code = status_code
        self.response_body = response_body

    @property
    def is_client_error(self) -> bool:
        """Check if this is a client error (4xx status code)."""
        return self.status_code is not None and 400 <= self.status_code < 500

    @property
    def is_server_error(self) -> bool:
        """Check if this is a server error (5xx status code)."""
        return self.status_code is not None and 500 <= self.status_code < 600

    @property
    def is_rate_limit_error(self) -> bool:
        """Check if this is a rate limit error (429)."""
        return self.status_code == 429


class AIPRTimeoutError(AIPRError):
    """Exception raised when a request times out."""

    pass


class AIPRConnectionError(AIPRError):
    """Exception raised when a connection error occurs."""

    pass
