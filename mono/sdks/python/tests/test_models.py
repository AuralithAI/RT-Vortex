"""Tests for Pydantic models."""

from __future__ import annotations

from datetime import datetime, timezone

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


class TestUser:
    def test_user_from_dict(self):
        u = User.model_validate(
            {"id": "u1", "email": "a@b.com", "display_name": "Alice"}
        )
        assert u.id == "u1"
        assert u.email == "a@b.com"
        assert u.display_name == "Alice"

    def test_user_defaults(self):
        u = User(id="u2", email="x@y.com")
        assert u.display_name == ""
        assert u.avatar_url == ""
        assert u.created_at is None

    def test_user_update_exclude_none(self):
        req = UserUpdateRequest(display_name="Bob")
        d = req.model_dump(exclude_none=True)
        assert d == {"display_name": "Bob"}
        assert "avatar_url" not in d


class TestOrg:
    def test_org_list_response(self):
        resp = OrgListResponse.model_validate(
            {
                "total": 2,
                "limit": 10,
                "offset": 0,
                "organizations": [
                    {"id": "o1", "name": "Acme", "slug": "acme"},
                    {"id": "o2", "name": "Beta", "slug": "beta"},
                ],
            }
        )
        assert resp.total == 2
        assert len(resp.organizations) == 2
        assert resp.organizations[0].name == "Acme"

    def test_member_list_response(self):
        resp = MemberListResponse.model_validate(
            {
                "total": 1,
                "members": [
                    {"user_id": "u1", "email": "a@b.com", "role": "admin"}
                ],
            }
        )
        assert resp.members[0].role == "admin"


class TestReview:
    def test_review_from_dict(self):
        r = Review.model_validate(
            {
                "id": "r1",
                "repo_id": "repo1",
                "pr_number": 42,
                "status": "completed",
                "comments_count": 3,
            }
        )
        assert r.status == "completed"
        assert r.comments_count == 3

    def test_review_comment(self):
        c = ReviewComment.model_validate(
            {
                "id": "c1",
                "review_id": "r1",
                "file_path": "main.py",
                "line_number": 10,
                "severity": "warning",
                "message": "Use f-string",
            }
        )
        assert c.severity == "warning"
        assert c.line_number == 10

    def test_review_list_response(self):
        resp = ReviewListResponse.model_validate(
            {"total": 0, "reviews": []}
        )
        assert resp.total == 0
        assert resp.reviews == []


class TestProgressEvent:
    def test_defaults(self):
        evt = ProgressEvent()
        assert evt.event == "progress"
        assert evt.step == ""

    def test_from_dict(self):
        evt = ProgressEvent.model_validate(
            {
                "event": "complete",
                "step": "analysis",
                "step_index": 2,
                "total_steps": 5,
                "status": "completed",
                "message": "Done",
            }
        )
        assert evt.event == "complete"
        assert evt.total_steps == 5


class TestRepo:
    def test_repo_defaults(self):
        r = Repo(id="r1")
        assert r.default_branch == "main"
        assert r.platform == ""

    def test_repo_list_response_empty(self):
        resp = RepoListResponse.model_validate(
            {"total": 0, "repositories": []}
        )
        assert len(resp.repositories) == 0


class TestIndexStatus:
    def test_defaults(self):
        s = IndexStatus()
        assert s.status == "idle"
        assert s.progress == 0


class TestAdminStats:
    def test_from_dict(self):
        stats = AdminStats.model_validate(
            {"total_users": 10, "total_repos": 5, "reviews_today": 3}
        )
        assert stats.total_users == 10
        assert stats.reviews_today == 3


class TestHealthStatus:
    def test_with_checks(self):
        h = HealthStatus.model_validate(
            {"status": "ok", "checks": {"db": "up", "cache": "up"}}
        )
        assert h.status == "ok"
        assert h.checks is not None
        assert h.checks["db"] == "up"


class TestPaginationOptions:
    def test_defaults(self):
        p = PaginationOptions()
        assert p.limit == 20
        assert p.offset == 0

    def test_custom(self):
        p = PaginationOptions(limit=50, offset=10)
        assert p.limit == 50
