"""Synchronous and asynchronous HTTP clients for the RTVortex API."""

from __future__ import annotations

from collections.abc import AsyncIterator, Iterator
from typing import Any, Optional

import httpx

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
    Repo,
    RepoListResponse,
    Review,
    ReviewComment,
    ReviewListResponse,
    User,
    UserUpdateRequest,
)
from rtvortex_sdk.streaming import aiter_sse_events, iter_sse_events

_DEFAULT_BASE_URL = "https://api.rtvortex.dev"
_DEFAULT_TIMEOUT = 30.0
_USER_AGENT = "rtvortex-sdk-python/0.1.0"


# ── Helpers ──────────────────────────────────────────────────────────────────


def _raise_for_status(response: httpx.Response) -> None:
    """Map HTTP error codes to typed SDK exceptions."""
    if response.is_success:
        return
    code = response.status_code
    try:
        body = response.json()
    except Exception:
        body = response.text

    msg = body.get("error", response.text) if isinstance(body, dict) else str(body)

    if code == 401:
        raise AuthenticationError(msg, status_code=code, body=body)
    if code == 404:
        raise NotFoundError(msg, status_code=code, body=body)
    if code == 422:
        raise ValidationError(msg, status_code=code, body=body)
    if code in (403, 429):
        raise QuotaExceededError(msg, status_code=code, body=body)
    if code >= 500:
        raise ServerError(msg, status_code=code, body=body)
    raise RTVortexError(msg, status_code=code, body=body)


def _pagination_params(opts: PaginationOptions | None) -> dict[str, int]:
    if opts is None:
        return {}
    return {"limit": opts.limit, "offset": opts.offset}


# ── Synchronous Client ──────────────────────────────────────────────────────


class RTVortexClient:
    """Synchronous client for the RTVortex API.

    Usage::

        from rtvortex_sdk import RTVortexClient

        client = RTVortexClient(token="your-token")
        user = client.me()
        print(user.email)
    """

    def __init__(
        self,
        *,
        token: str,
        base_url: str = _DEFAULT_BASE_URL,
        timeout: float = _DEFAULT_TIMEOUT,
        http_client: httpx.Client | None = None,
    ) -> None:
        self._base = base_url.rstrip("/")
        self._token = token
        self._owns_client = http_client is None
        self._client = http_client or httpx.Client(
            base_url=self._base,
            timeout=timeout,
            headers={
                "Authorization": f"Bearer {token}",
                "User-Agent": _USER_AGENT,
                "Accept": "application/json",
            },
        )

    # -- lifecycle --

    def close(self) -> None:
        if self._owns_client:
            self._client.close()

    def __enter__(self) -> "RTVortexClient":
        return self

    def __exit__(self, *exc: object) -> None:
        self.close()

    # -- internal helpers --

    def _request(
        self,
        method: str,
        path: str,
        *,
        params: dict[str, Any] | None = None,
        json: Any = None,
    ) -> httpx.Response:
        resp = self._client.request(method, path, params=params, json=json)
        _raise_for_status(resp)
        return resp

    # ── User ──

    def me(self) -> User:
        """Get the authenticated user's profile."""
        resp = self._request("GET", "/user/me")
        return User.model_validate(resp.json())

    def update_me(self, update: UserUpdateRequest) -> User:
        """Update the authenticated user's profile."""
        resp = self._request(
            "PUT", "/user/me", json=update.model_dump(exclude_none=True)
        )
        return User.model_validate(resp.json())

    # ── Organizations ──

    def list_orgs(self, pagination: PaginationOptions | None = None) -> OrgListResponse:
        resp = self._request("GET", "/orgs", params=_pagination_params(pagination))
        return OrgListResponse.model_validate(resp.json())

    def create_org(self, *, name: str, slug: str, plan: str = "free") -> Org:
        resp = self._request(
            "POST", "/orgs", json={"name": name, "slug": slug, "plan": plan}
        )
        return Org.model_validate(resp.json())

    def get_org(self, org_id: str) -> Org:
        resp = self._request("GET", f"/orgs/{org_id}")
        return Org.model_validate(resp.json())

    def update_org(
        self,
        org_id: str,
        *,
        name: str | None = None,
        slug: str | None = None,
        plan: str | None = None,
    ) -> Org:
        payload = {k: v for k, v in {"name": name, "slug": slug, "plan": plan}.items() if v is not None}
        resp = self._request("PUT", f"/orgs/{org_id}", json=payload)
        return Org.model_validate(resp.json())

    # ── Org Members ──

    def list_members(
        self, org_id: str, pagination: PaginationOptions | None = None
    ) -> MemberListResponse:
        resp = self._request(
            "GET", f"/orgs/{org_id}/members", params=_pagination_params(pagination)
        )
        return MemberListResponse.model_validate(resp.json())

    def invite_member(
        self, org_id: str, *, email: str, role: str = "member"
    ) -> OrgMember:
        resp = self._request(
            "POST",
            f"/orgs/{org_id}/members",
            json={"email": email, "role": role},
        )
        return OrgMember.model_validate(resp.json())

    def remove_member(self, org_id: str, user_id: str) -> None:
        self._request("DELETE", f"/orgs/{org_id}/members/{user_id}")

    # ── Repositories ──

    def list_repos(
        self, pagination: PaginationOptions | None = None
    ) -> RepoListResponse:
        resp = self._request("GET", "/repos", params=_pagination_params(pagination))
        return RepoListResponse.model_validate(resp.json())

    def register_repo(
        self,
        *,
        org_id: str,
        platform: str,
        owner: str,
        name: str,
        clone_url: str = "",
    ) -> Repo:
        resp = self._request(
            "POST",
            "/repos",
            json={
                "org_id": org_id,
                "platform": platform,
                "owner": owner,
                "name": name,
                "clone_url": clone_url,
            },
        )
        return Repo.model_validate(resp.json())

    def get_repo(self, repo_id: str) -> Repo:
        resp = self._request("GET", f"/repos/{repo_id}")
        return Repo.model_validate(resp.json())

    def update_repo(self, repo_id: str, **fields: Any) -> Repo:
        resp = self._request("PUT", f"/repos/{repo_id}", json=fields)
        return Repo.model_validate(resp.json())

    def delete_repo(self, repo_id: str) -> None:
        self._request("DELETE", f"/repos/{repo_id}")

    # ── Reviews ──

    def list_reviews(
        self, pagination: PaginationOptions | None = None
    ) -> ReviewListResponse:
        resp = self._request("GET", "/reviews", params=_pagination_params(pagination))
        return ReviewListResponse.model_validate(resp.json())

    def trigger_review(
        self, *, repo_id: str, pr_number: int, **extra: Any
    ) -> Review:
        payload: dict[str, Any] = {"repo_id": repo_id, "pr_number": pr_number, **extra}
        resp = self._request("POST", "/reviews", json=payload)
        return Review.model_validate(resp.json())

    def get_review(self, review_id: str) -> Review:
        resp = self._request("GET", f"/reviews/{review_id}")
        return Review.model_validate(resp.json())

    def get_review_comments(self, review_id: str) -> list[ReviewComment]:
        resp = self._request("GET", f"/reviews/{review_id}/comments")
        data = resp.json()
        items = data if isinstance(data, list) else data.get("comments", [])
        return [ReviewComment.model_validate(c) for c in items]

    def stream_review(
        self, review_id: str
    ) -> Iterator[ProgressEvent]:
        """Stream review progress events via SSE (synchronous).

        Yields ``ProgressEvent`` instances until the stream closes.
        """
        with self._client.stream(
            "GET",
            f"/reviews/{review_id}/ws",
            headers={"Accept": "text/event-stream"},
        ) as resp:
            _raise_for_status(resp)
            yield from iter_sse_events(resp)

    # ── Indexing ──

    def trigger_index(self, repo_id: str) -> IndexStatus:
        resp = self._request("POST", f"/repos/{repo_id}/index")
        return IndexStatus.model_validate(resp.json())

    def get_index_status(self, repo_id: str) -> IndexStatus:
        resp = self._request("GET", f"/repos/{repo_id}/index/status")
        return IndexStatus.model_validate(resp.json())

    # ── Admin ──

    def get_stats(self) -> AdminStats:
        resp = self._request("GET", "/admin/stats")
        return AdminStats.model_validate(resp.json())

    def health(self) -> HealthStatus:
        resp = self._request("GET", "/health")
        return HealthStatus.model_validate(resp.json())

    def health_detailed(self) -> HealthStatus:
        resp = self._request("GET", "/admin/health/detailed")
        return HealthStatus.model_validate(resp.json())


# ── Asynchronous Client ─────────────────────────────────────────────────────


class AsyncRTVortexClient:
    """Asynchronous client for the RTVortex API.

    Usage::

        import asyncio
        from rtvortex_sdk import AsyncRTVortexClient

        async def main():
            async with AsyncRTVortexClient(token="your-token") as client:
                user = await client.me()
                print(user.email)

        asyncio.run(main())
    """

    def __init__(
        self,
        *,
        token: str,
        base_url: str = _DEFAULT_BASE_URL,
        timeout: float = _DEFAULT_TIMEOUT,
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        self._base = base_url.rstrip("/")
        self._token = token
        self._owns_client = http_client is None
        self._client = http_client or httpx.AsyncClient(
            base_url=self._base,
            timeout=timeout,
            headers={
                "Authorization": f"Bearer {token}",
                "User-Agent": _USER_AGENT,
                "Accept": "application/json",
            },
        )

    # -- lifecycle --

    async def aclose(self) -> None:
        if self._owns_client:
            await self._client.aclose()

    async def __aenter__(self) -> "AsyncRTVortexClient":
        return self

    async def __aexit__(self, *exc: object) -> None:
        await self.aclose()

    # -- internal helpers --

    async def _request(
        self,
        method: str,
        path: str,
        *,
        params: dict[str, Any] | None = None,
        json: Any = None,
    ) -> httpx.Response:
        resp = await self._client.request(method, path, params=params, json=json)
        _raise_for_status(resp)
        return resp

    # ── User ──

    async def me(self) -> User:
        resp = await self._request("GET", "/user/me")
        return User.model_validate(resp.json())

    async def update_me(self, update: UserUpdateRequest) -> User:
        resp = await self._request(
            "PUT", "/user/me", json=update.model_dump(exclude_none=True)
        )
        return User.model_validate(resp.json())

    # ── Organizations ──

    async def list_orgs(
        self, pagination: PaginationOptions | None = None
    ) -> OrgListResponse:
        resp = await self._request(
            "GET", "/orgs", params=_pagination_params(pagination)
        )
        return OrgListResponse.model_validate(resp.json())

    async def create_org(self, *, name: str, slug: str, plan: str = "free") -> Org:
        resp = await self._request(
            "POST", "/orgs", json={"name": name, "slug": slug, "plan": plan}
        )
        return Org.model_validate(resp.json())

    async def get_org(self, org_id: str) -> Org:
        resp = await self._request("GET", f"/orgs/{org_id}")
        return Org.model_validate(resp.json())

    async def update_org(
        self,
        org_id: str,
        *,
        name: str | None = None,
        slug: str | None = None,
        plan: str | None = None,
    ) -> Org:
        payload = {k: v for k, v in {"name": name, "slug": slug, "plan": plan}.items() if v is not None}
        resp = await self._request("PUT", f"/orgs/{org_id}", json=payload)
        return Org.model_validate(resp.json())

    # ── Org Members ──

    async def list_members(
        self, org_id: str, pagination: PaginationOptions | None = None
    ) -> MemberListResponse:
        resp = await self._request(
            "GET", f"/orgs/{org_id}/members", params=_pagination_params(pagination)
        )
        return MemberListResponse.model_validate(resp.json())

    async def invite_member(
        self, org_id: str, *, email: str, role: str = "member"
    ) -> OrgMember:
        resp = await self._request(
            "POST",
            f"/orgs/{org_id}/members",
            json={"email": email, "role": role},
        )
        return OrgMember.model_validate(resp.json())

    async def remove_member(self, org_id: str, user_id: str) -> None:
        await self._request("DELETE", f"/orgs/{org_id}/members/{user_id}")

    # ── Repositories ──

    async def list_repos(
        self, pagination: PaginationOptions | None = None
    ) -> RepoListResponse:
        resp = await self._request(
            "GET", "/repos", params=_pagination_params(pagination)
        )
        return RepoListResponse.model_validate(resp.json())

    async def register_repo(
        self,
        *,
        org_id: str,
        platform: str,
        owner: str,
        name: str,
        clone_url: str = "",
    ) -> Repo:
        resp = await self._request(
            "POST",
            "/repos",
            json={
                "org_id": org_id,
                "platform": platform,
                "owner": owner,
                "name": name,
                "clone_url": clone_url,
            },
        )
        return Repo.model_validate(resp.json())

    async def get_repo(self, repo_id: str) -> Repo:
        resp = await self._request("GET", f"/repos/{repo_id}")
        return Repo.model_validate(resp.json())

    async def update_repo(self, repo_id: str, **fields: Any) -> Repo:
        resp = await self._request("PUT", f"/repos/{repo_id}", json=fields)
        return Repo.model_validate(resp.json())

    async def delete_repo(self, repo_id: str) -> None:
        await self._request("DELETE", f"/repos/{repo_id}")

    # ── Reviews ──

    async def list_reviews(
        self, pagination: PaginationOptions | None = None
    ) -> ReviewListResponse:
        resp = await self._request(
            "GET", "/reviews", params=_pagination_params(pagination)
        )
        return ReviewListResponse.model_validate(resp.json())

    async def trigger_review(
        self, *, repo_id: str, pr_number: int, **extra: Any
    ) -> Review:
        payload: dict[str, Any] = {"repo_id": repo_id, "pr_number": pr_number, **extra}
        resp = await self._request("POST", "/reviews", json=payload)
        return Review.model_validate(resp.json())

    async def get_review(self, review_id: str) -> Review:
        resp = await self._request("GET", f"/reviews/{review_id}")
        return Review.model_validate(resp.json())

    async def get_review_comments(self, review_id: str) -> list[ReviewComment]:
        resp = await self._request("GET", f"/reviews/{review_id}/comments")
        data = resp.json()
        items = data if isinstance(data, list) else data.get("comments", [])
        return [ReviewComment.model_validate(c) for c in items]

    async def stream_review(
        self, review_id: str
    ) -> AsyncIterator[ProgressEvent]:
        """Stream review progress events via SSE (async).

        Yields ``ProgressEvent`` instances until the stream closes.
        """
        async with self._client.stream(
            "GET",
            f"/reviews/{review_id}/ws",
            headers={"Accept": "text/event-stream"},
        ) as resp:
            _raise_for_status(resp)
            async for evt in aiter_sse_events(resp):
                yield evt

    # ── Indexing ──

    async def trigger_index(self, repo_id: str) -> IndexStatus:
        resp = await self._request("POST", f"/repos/{repo_id}/index")
        return IndexStatus.model_validate(resp.json())

    async def get_index_status(self, repo_id: str) -> IndexStatus:
        resp = await self._request("GET", f"/repos/{repo_id}/index/status")
        return IndexStatus.model_validate(resp.json())

    # ── Admin ──

    async def get_stats(self) -> AdminStats:
        resp = await self._request("GET", "/admin/stats")
        return AdminStats.model_validate(resp.json())

    async def health(self) -> HealthStatus:
        resp = await self._request("GET", "/health")
        return HealthStatus.model_validate(resp.json())

    async def health_detailed(self) -> HealthStatus:
        resp = await self._request("GET", "/admin/health/detailed")
        return HealthStatus.model_validate(resp.json())
