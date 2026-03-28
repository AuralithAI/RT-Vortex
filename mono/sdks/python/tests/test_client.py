"""Tests for the synchronous RTVortexClient."""

from __future__ import annotations

import httpx
import pytest
import respx

from rtvortex_sdk import RTVortexClient
from rtvortex_sdk.exceptions import (
    AuthenticationError,
    NotFoundError,
    QuotaExceededError,
    ServerError,
    ValidationError,
)
from rtvortex_sdk.models import PaginationOptions, UserUpdateRequest


BASE = "https://api.rtvortex.test"


@pytest.fixture()
def client() -> RTVortexClient:
    c = RTVortexClient(token="tok-123", base_url=BASE)
    yield c
    c.close()


# ── User ──


class TestUserEndpoints:
    @respx.mock
    def test_me(self, client: RTVortexClient):
        respx.get(f"{BASE}/user/me").respond(
            200, json={"id": "u1", "email": "a@b.com", "display_name": "Alice"}
        )
        user = client.me()
        assert user.id == "u1"
        assert user.email == "a@b.com"

    @respx.mock
    def test_update_me(self, client: RTVortexClient):
        respx.put(f"{BASE}/user/me").respond(
            200, json={"id": "u1", "email": "a@b.com", "display_name": "Bob"}
        )
        user = client.update_me(UserUpdateRequest(display_name="Bob"))
        assert user.display_name == "Bob"


# ── Organizations ──


class TestOrgEndpoints:
    @respx.mock
    def test_list_orgs(self, client: RTVortexClient):
        respx.get(f"{BASE}/orgs").respond(
            200,
            json={
                "total": 1, "limit": 20, "offset": 0,
                "organizations": [{"id": "o1", "name": "Acme", "slug": "acme"}],
            },
        )
        resp = client.list_orgs()
        assert resp.total == 1
        assert resp.organizations[0].slug == "acme"

    @respx.mock
    def test_create_org(self, client: RTVortexClient):
        respx.post(f"{BASE}/orgs").respond(
            200, json={"id": "o2", "name": "Beta", "slug": "beta", "plan": "pro"}
        )
        org = client.create_org(name="Beta", slug="beta", plan="pro")
        assert org.plan == "pro"

    @respx.mock
    def test_get_org(self, client: RTVortexClient):
        respx.get(f"{BASE}/orgs/o1").respond(
            200, json={"id": "o1", "name": "Acme", "slug": "acme"}
        )
        org = client.get_org("o1")
        assert org.id == "o1"

    @respx.mock
    def test_update_org(self, client: RTVortexClient):
        respx.put(f"{BASE}/orgs/o1").respond(
            200, json={"id": "o1", "name": "Acme2", "slug": "acme"}
        )
        org = client.update_org("o1", name="Acme2")
        assert org.name == "Acme2"


# ── Members ──


class TestMemberEndpoints:
    @respx.mock
    def test_list_members(self, client: RTVortexClient):
        respx.get(f"{BASE}/orgs/o1/members").respond(
            200,
            json={"total": 1, "members": [{"user_id": "u1", "email": "a@b.com", "role": "admin"}]},
        )
        resp = client.list_members("o1")
        assert resp.members[0].role == "admin"

    @respx.mock
    def test_invite_member(self, client: RTVortexClient):
        respx.post(f"{BASE}/orgs/o1/members").respond(
            200, json={"user_id": "u2", "email": "b@c.com", "role": "member"}
        )
        m = client.invite_member("o1", email="b@c.com")
        assert m.user_id == "u2"

    @respx.mock
    def test_remove_member(self, client: RTVortexClient):
        respx.delete(f"{BASE}/orgs/o1/members/u2").respond(204)
        client.remove_member("o1", "u2")


# ── Repositories ──


class TestRepoEndpoints:
    @respx.mock
    def test_list_repos_with_pagination(self, client: RTVortexClient):
        respx.get(f"{BASE}/repos").respond(200, json={"total": 0, "repositories": []})
        resp = client.list_repos(PaginationOptions(limit=5, offset=0))
        assert resp.total == 0

    @respx.mock
    def test_register_repo(self, client: RTVortexClient):
        respx.post(f"{BASE}/repos").respond(
            200, json={"id": "rp1", "org_id": "o1", "platform": "github", "owner": "acme", "name": "api"},
        )
        repo = client.register_repo(org_id="o1", platform="github", owner="acme", name="api")
        assert repo.platform == "github"

    @respx.mock
    def test_get_repo(self, client: RTVortexClient):
        respx.get(f"{BASE}/repos/rp1").respond(
            200, json={"id": "rp1", "platform": "github", "owner": "a", "name": "b"}
        )
        repo = client.get_repo("rp1")
        assert repo.id == "rp1"

    @respx.mock
    def test_delete_repo(self, client: RTVortexClient):
        respx.delete(f"{BASE}/repos/rp1").respond(204)
        client.delete_repo("rp1")


# ── Reviews ──


class TestReviewEndpoints:
    @respx.mock
    def test_list_reviews(self, client: RTVortexClient):
        respx.get(f"{BASE}/reviews").respond(
            200, json={"total": 1, "reviews": [{"id": "rv1", "repo_id": "rp1", "pr_number": 10}]}
        )
        resp = client.list_reviews()
        assert resp.total == 1
        assert resp.reviews[0].pr_number == 10

    @respx.mock
    def test_trigger_review(self, client: RTVortexClient):
        respx.post(f"{BASE}/reviews").respond(
            200, json={"id": "rv2", "repo_id": "rp1", "pr_number": 11, "status": "pending"}
        )
        rev = client.trigger_review(repo_id="rp1", pr_number=11)
        assert rev.status == "pending"

    @respx.mock
    def test_get_review(self, client: RTVortexClient):
        respx.get(f"{BASE}/reviews/rv1").respond(
            200, json={"id": "rv1", "status": "completed", "comments_count": 5}
        )
        rev = client.get_review("rv1")
        assert rev.comments_count == 5

    @respx.mock
    def test_get_review_comments_list(self, client: RTVortexClient):
        respx.get(f"{BASE}/reviews/rv1/comments").respond(
            200,
            json=[
                {"id": "c1", "severity": "warning", "message": "Fix this"},
                {"id": "c2", "severity": "error", "message": "Bug"},
            ],
        )
        comments = client.get_review_comments("rv1")
        assert len(comments) == 2
        assert comments[1].severity == "error"

    @respx.mock
    def test_get_review_comments_object(self, client: RTVortexClient):
        respx.get(f"{BASE}/reviews/rv1/comments").respond(
            200, json={"comments": [{"id": "c1", "severity": "info", "message": "OK"}]},
        )
        comments = client.get_review_comments("rv1")
        assert len(comments) == 1


# ── Indexing ──


class TestIndexEndpoints:
    @respx.mock
    def test_trigger_index(self, client: RTVortexClient):
        respx.post(f"{BASE}/repos/rp1/index").respond(
            200, json={"repo_id": "rp1", "status": "running", "job_id": "j1"}
        )
        status = client.trigger_index("rp1")
        assert status.status == "running"

    @respx.mock
    def test_get_index_status(self, client: RTVortexClient):
        respx.get(f"{BASE}/repos/rp1/index/status").respond(
            200, json={"repo_id": "rp1", "status": "completed", "progress": 100}
        )
        status = client.get_index_status("rp1")
        assert status.progress == 100


# ── Admin / Health ──


class TestAdminEndpoints:
    @respx.mock
    def test_get_stats(self, client: RTVortexClient):
        respx.get(f"{BASE}/admin/stats").respond(
            200, json={"total_users": 42, "total_repos": 7, "reviews_today": 3}
        )
        stats = client.get_stats()
        assert stats.total_users == 42

    @respx.mock
    def test_health(self, client: RTVortexClient):
        respx.get(f"{BASE}/health").respond(200, json={"status": "ok"})
        h = client.health()
        assert h.status == "ok"

    @respx.mock
    def test_health_detailed(self, client: RTVortexClient):
        respx.get(f"{BASE}/admin/health/detailed").respond(
            200, json={"status": "ok", "checks": {"db": "up"}}
        )
        h = client.health_detailed()
        assert h.checks is not None
        assert h.checks["db"] == "up"


# ── Error mapping ──


class TestErrorMapping:
    @respx.mock
    def test_401(self, client: RTVortexClient):
        respx.get(f"{BASE}/user/me").respond(401, json={"error": "unauthorized"})
        with pytest.raises(AuthenticationError) as exc:
            client.me()
        assert exc.value.status_code == 401

    @respx.mock
    def test_404(self, client: RTVortexClient):
        respx.get(f"{BASE}/repos/missing").respond(404, json={"error": "not found"})
        with pytest.raises(NotFoundError):
            client.get_repo("missing")

    @respx.mock
    def test_422(self, client: RTVortexClient):
        respx.post(f"{BASE}/orgs").respond(422, json={"error": "invalid slug"})
        with pytest.raises(ValidationError):
            client.create_org(name="x", slug="")

    @respx.mock
    def test_429(self, client: RTVortexClient):
        respx.get(f"{BASE}/reviews").respond(429, json={"error": "rate limited"})
        with pytest.raises(QuotaExceededError):
            client.list_reviews()

    @respx.mock
    def test_500(self, client: RTVortexClient):
        respx.get(f"{BASE}/admin/stats").respond(500, json={"error": "boom"})
        with pytest.raises(ServerError):
            client.get_stats()
