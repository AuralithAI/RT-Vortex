"""HTTP client wrapper with retry and error handling."""

from __future__ import annotations

import time
from typing import Any, Optional

import httpx
from rich.console import Console

from rtvortex_cli.config import Config

console = Console(stderr=True)

# Retryable HTTP status codes
_RETRY_STATUSES = {429, 500, 502, 503, 504}
_DEFAULT_MAX_RETRIES = 3
_BASE_BACKOFF = 1.0  # seconds


class APIError(Exception):
    """Raised when the API returns a non-success response."""

    def __init__(self, status: int, message: str, body: Optional[dict[str, Any]] = None):
        self.status = status
        self.message = message
        self.body = body or {}
        super().__init__(f"HTTP {status}: {message}")


class APIClient:
    """HTTP client for the RTVortex API with automatic retry."""

    def __init__(self, config: Config, *, timeout: float = 30.0):
        self._config = config
        headers: dict[str, str] = {
            "User-Agent": "rtvortex-cli/0.1.0",
            "Accept": "application/json",
        }
        if config.token:
            headers["Authorization"] = f"Bearer {config.token}"

        self._client = httpx.Client(
            base_url=config.server_url,
            headers=headers,
            timeout=timeout,
            follow_redirects=True,
        )

    # ── Public helpers ───────────────────────────────────────────────────

    def get(self, path: str, **kwargs: Any) -> dict[str, Any]:
        return self._request("GET", path, **kwargs)

    def post(self, path: str, **kwargs: Any) -> dict[str, Any]:
        return self._request("POST", path, **kwargs)

    def put(self, path: str, **kwargs: Any) -> dict[str, Any]:
        return self._request("PUT", path, **kwargs)

    def delete(self, path: str, **kwargs: Any) -> dict[str, Any]:
        return self._request("DELETE", path, **kwargs)

    def close(self) -> None:
        self._client.close()

    # ── Retry logic ──────────────────────────────────────────────────────

    def _request(
        self,
        method: str,
        path: str,
        *,
        max_retries: int = _DEFAULT_MAX_RETRIES,
        **kwargs: Any,
    ) -> dict[str, Any]:
        last_exc: Optional[Exception] = None

        for attempt in range(max_retries + 1):
            try:
                resp = self._client.request(method, path, **kwargs)

                if resp.status_code < 400:
                    # Some endpoints return empty bodies (204, etc.)
                    if resp.status_code == 204 or not resp.content:
                        return {}
                    return resp.json()  # type: ignore[no-any-return]

                # Retryable?
                if resp.status_code in _RETRY_STATUSES and attempt < max_retries:
                    wait = _BASE_BACKOFF * (2**attempt)
                    # Honour Retry-After header if present
                    retry_after = resp.headers.get("Retry-After")
                    if retry_after and retry_after.isdigit():
                        wait = max(wait, float(retry_after))
                    console.print(
                        f"[yellow]⟳ Retrying ({attempt + 1}/{max_retries})… "
                        f"waiting {wait:.0f}s[/yellow]"
                    )
                    time.sleep(wait)
                    continue

                # Non-retryable error
                body = _safe_json(resp)
                msg = body.get("message", resp.text[:200])
                raise APIError(resp.status_code, msg, body)

            except httpx.TransportError as exc:
                last_exc = exc
                if attempt < max_retries:
                    wait = _BASE_BACKOFF * (2**attempt)
                    console.print(
                        f"[yellow]⟳ Connection error, retrying ({attempt + 1}/{max_retries})… "
                        f"waiting {wait:.0f}s[/yellow]"
                    )
                    time.sleep(wait)
                    continue
                raise APIError(0, f"Connection failed: {exc}") from exc

        # Should not reach here, but just in case
        raise APIError(0, f"Max retries exceeded: {last_exc}")


def _safe_json(resp: httpx.Response) -> dict[str, Any]:
    try:
        return resp.json()  # type: ignore[no-any-return]
    except Exception:
        return {}
