"""HTTP client for AI-PR-Reviewer API."""

from __future__ import annotations

import asyncio
from typing import TYPE_CHECKING, TypeVar

import httpx

from aipr_sdk.exceptions import AIPRAPIError, AIPRConnectionError, AIPRTimeoutError
from aipr_sdk.models import (
    IndexRequest,
    IndexResponse,
    ReviewRequest,
    ReviewResponse,
)

if TYPE_CHECKING:
    from types import TracebackType

T = TypeVar("T")


class AIPRClient:
    """Synchronous client for AI-PR-Reviewer API.

    Example:
        >>> client = AIPRClient(base_url="https://api.aipr.example.com", api_key="your-key")
        >>> response = client.review(ReviewRequest(
        ...     repository_url="https://github.com/owner/repo",
        ...     pull_request_id=123,
        ... ))
        >>> for comment in response.comments:
        ...     print(f"{comment.severity}: {comment.message}")
    """

    def __init__(
        self,
        base_url: str = "http://localhost:8080",
        api_key: str | None = None,
        timeout: float = 300.0,
        connect_timeout: float = 30.0,
    ) -> None:
        """Initialize the AIPR client.

        Args:
            base_url: The base URL of the AIPR API.
            api_key: API key for authentication.
            timeout: Request timeout in seconds.
            connect_timeout: Connection timeout in seconds.
        """
        self.base_url = base_url.rstrip("/")

        headers = {
            "Content-Type": "application/json",
            "Accept": "application/json",
            "User-Agent": "aipr-python-sdk/0.1.0",
        }
        if api_key:
            headers["Authorization"] = f"Bearer {api_key}"

        self._client = httpx.Client(
            base_url=self.base_url,
            headers=headers,
            timeout=httpx.Timeout(timeout, connect=connect_timeout),
        )

    def __enter__(self) -> AIPRClient:
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_val: BaseException | None,
        exc_tb: TracebackType | None,
    ) -> None:
        self.close()

    def close(self) -> None:
        """Close the HTTP client."""
        self._client.close()

    def review(self, request: ReviewRequest) -> ReviewResponse:
        """Submit a pull request for review.

        Args:
            request: The review request.

        Returns:
            The review response with comments and metadata.

        Raises:
            AIPRAPIError: If the API request fails.
            AIPRTimeoutError: If the request times out.
        """
        return self._post("/api/v1/reviews", request, ReviewResponse)

    def get_review(self, review_id: str) -> ReviewResponse:
        """Get the status of a review by ID.

        Args:
            review_id: The review ID.

        Returns:
            The review response.

        Raises:
            AIPRAPIError: If the API request fails.
        """
        return self._get(f"/api/v1/reviews/{review_id}", ReviewResponse)

    def index(self, request: IndexRequest) -> IndexResponse:
        """Start indexing a repository.

        Args:
            request: The index request.

        Returns:
            The index job response.

        Raises:
            AIPRAPIError: If the API request fails.
        """
        return self._post("/api/v1/index", request, IndexResponse)

    def get_index_status(self, job_id: str) -> IndexResponse:
        """Get the status of an indexing job.

        Args:
            job_id: The job ID.

        Returns:
            The index job response.

        Raises:
            AIPRAPIError: If the API request fails.
        """
        return self._get(f"/api/v1/index/{job_id}", IndexResponse)

    def is_healthy(self) -> bool:
        """Check if the API server is healthy.

        Returns:
            True if the server is healthy.
        """
        try:
            response = self._client.get("/actuator/health")
            return response.is_success
        except Exception:
            return False

    def _get(self, path: str, response_type: type[T]) -> T:
        """Execute a GET request."""
        try:
            response = self._client.get(path)
            self._check_response(response)
            return response_type.model_validate(response.json())
        except httpx.TimeoutException as e:
            raise AIPRTimeoutError(f"Request timed out: {e}") from e
        except httpx.ConnectError as e:
            raise AIPRConnectionError(f"Connection error: {e}") from e

    def _post(self, path: str, body: T, response_type: type[T]) -> T:
        """Execute a POST request."""
        try:
            response = self._client.post(path, json=body.model_dump(exclude_none=True))
            self._check_response(response)
            return response_type.model_validate(response.json())
        except httpx.TimeoutException as e:
            raise AIPRTimeoutError(f"Request timed out: {e}") from e
        except httpx.ConnectError as e:
            raise AIPRConnectionError(f"Connection error: {e}") from e

    def _check_response(self, response: httpx.Response) -> None:
        """Check response for errors."""
        if not response.is_success:
            raise AIPRAPIError(
                f"API request failed: {response.status_code} {response.reason_phrase}",
                status_code=response.status_code,
                response_body=response.text,
            )


class AsyncAIPRClient:
    """Asynchronous client for AI-PR-Reviewer API.

    Example:
        >>> async with AsyncAIPRClient(base_url="https://api.aipr.example.com") as client:
        ...     response = await client.review(ReviewRequest(
        ...         repository_url="https://github.com/owner/repo",
        ...         pull_request_id=123,
        ...     ))
    """

    def __init__(
        self,
        base_url: str = "http://localhost:8080",
        api_key: str | None = None,
        timeout: float = 300.0,
        connect_timeout: float = 30.0,
    ) -> None:
        """Initialize the async AIPR client.

        Args:
            base_url: The base URL of the AIPR API.
            api_key: API key for authentication.
            timeout: Request timeout in seconds.
            connect_timeout: Connection timeout in seconds.
        """
        self.base_url = base_url.rstrip("/")

        headers = {
            "Content-Type": "application/json",
            "Accept": "application/json",
            "User-Agent": "aipr-python-sdk/0.1.0",
        }
        if api_key:
            headers["Authorization"] = f"Bearer {api_key}"

        self._client = httpx.AsyncClient(
            base_url=self.base_url,
            headers=headers,
            timeout=httpx.Timeout(timeout, connect=connect_timeout),
        )

    async def __aenter__(self) -> AsyncAIPRClient:
        return self

    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc_val: BaseException | None,
        exc_tb: TracebackType | None,
    ) -> None:
        await self.close()

    async def close(self) -> None:
        """Close the HTTP client."""
        await self._client.aclose()

    async def review(self, request: ReviewRequest) -> ReviewResponse:
        """Submit a pull request for review asynchronously.

        Args:
            request: The review request.

        Returns:
            The review response with comments and metadata.
        """
        return await self._post("/api/v1/reviews", request, ReviewResponse)

    async def get_review(self, review_id: str) -> ReviewResponse:
        """Get the status of a review by ID.

        Args:
            review_id: The review ID.

        Returns:
            The review response.
        """
        return await self._get(f"/api/v1/reviews/{review_id}", ReviewResponse)

    async def index(self, request: IndexRequest) -> IndexResponse:
        """Start indexing a repository.

        Args:
            request: The index request.

        Returns:
            The index job response.
        """
        return await self._post("/api/v1/index", request, IndexResponse)

    async def get_index_status(self, job_id: str) -> IndexResponse:
        """Get the status of an indexing job.

        Args:
            job_id: The job ID.

        Returns:
            The index job response.
        """
        return await self._get(f"/api/v1/index/{job_id}", IndexResponse)

    async def wait_for_review(
        self,
        review_id: str,
        poll_interval: float = 5.0,
        timeout: float | None = None,
    ) -> ReviewResponse:
        """Wait for a review to complete.

        Args:
            review_id: The review ID.
            poll_interval: Time between status checks in seconds.
            timeout: Maximum time to wait in seconds. None for no timeout.

        Returns:
            The completed review response.

        Raises:
            AIPRTimeoutError: If the timeout is exceeded.
        """
        elapsed = 0.0
        while True:
            response = await self.get_review(review_id)
            if response.is_complete:
                return response

            if timeout is not None and elapsed >= timeout:
                raise AIPRTimeoutError(f"Review {review_id} did not complete within {timeout}s")

            await asyncio.sleep(poll_interval)
            elapsed += poll_interval

    async def is_healthy(self) -> bool:
        """Check if the API server is healthy.

        Returns:
            True if the server is healthy.
        """
        try:
            response = await self._client.get("/actuator/health")
            return response.is_success
        except Exception:
            return False

    async def _get(self, path: str, response_type: type[T]) -> T:
        """Execute a GET request."""
        try:
            response = await self._client.get(path)
            self._check_response(response)
            return response_type.model_validate(response.json())
        except httpx.TimeoutException as e:
            raise AIPRTimeoutError(f"Request timed out: {e}") from e
        except httpx.ConnectError as e:
            raise AIPRConnectionError(f"Connection error: {e}") from e

    async def _post(self, path: str, body: T, response_type: type[T]) -> T:
        """Execute a POST request."""
        try:
            response = await self._client.post(path, json=body.model_dump(exclude_none=True))
            self._check_response(response)
            return response_type.model_validate(response.json())
        except httpx.TimeoutException as e:
            raise AIPRTimeoutError(f"Request timed out: {e}") from e
        except httpx.ConnectError as e:
            raise AIPRConnectionError(f"Connection error: {e}") from e

    def _check_response(self, response: httpx.Response) -> None:
        """Check response for errors."""
        if not response.is_success:
            raise AIPRAPIError(
                f"API request failed: {response.status_code} {response.reason_phrase}",
                status_code=response.status_code,
                response_body=response.text,
            )
