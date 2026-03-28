"""Tests for rtvortex_cli.formatters module."""

from __future__ import annotations

from rtvortex_cli.formatters import (
    format_comments_table,
    format_review_json,
    format_review_markdown,
    format_review_table,
    format_status_panel,
    format_user_table,
)


class TestReviewFormatters:
    """Tests for review output formatting."""

    _REVIEW = {
        "id": "abc-123",
        "repo_id": "repo-456",
        "pr_number": 42,
        "status": "completed",
        "comments_count": 5,
        "created_at": "2026-03-05T12:00:00Z",
        "completed_at": "2026-03-05T12:01:30Z",
    }

    def test_table_renders(self) -> None:
        table = format_review_table(self._REVIEW)
        assert table.title == "Review Summary"
        assert table.row_count >= 5

    def test_json_output(self) -> None:
        out = format_review_json(self._REVIEW)
        assert '"abc-123"' in out
        assert '"pr_number": 42' in out

    def test_markdown_output(self) -> None:
        out = format_review_markdown(self._REVIEW)
        assert "# Review abc-123" in out
        assert "**Status:** completed" in out

    def test_markdown_with_comments(self) -> None:
        review = {**self._REVIEW, "comments": [
            {"severity": "critical", "file_path": "main.go", "line_number": 10, "message": "bug"},
        ]}
        out = format_review_markdown(review)
        assert "[critical]" in out
        assert "main.go" in out


class TestCommentsFormatter:
    """Tests for comment table rendering."""

    def test_empty_comments(self) -> None:
        table = format_comments_table([])
        assert table.row_count == 0

    def test_comments_with_severity(self) -> None:
        comments = [
            {"severity": "critical", "file_path": "a.go", "line_number": 1, "message": "bad"},
            {"severity": "warning", "file_path": "b.go", "line_number": 2, "message": "meh"},
            {"severity": "suggestion", "file_path": "c.go", "line_number": 3, "message": "nice"},
        ]
        table = format_comments_table(comments)
        assert table.row_count == 3


class TestStatusFormatter:
    """Tests for status panel rendering."""

    def test_health_only(self) -> None:
        panel = format_status_panel({"status": "ready", "checks": {"postgres": "ok"}})
        assert panel is not None
        assert panel.title is not None

    def test_health_with_stats(self) -> None:
        panel = format_status_panel(
            {"status": "ready", "checks": {}},
            {"total_reviews": 100, "total_repos": 5},
        )
        assert panel is not None


class TestUserFormatter:
    """Tests for user table rendering."""

    def test_user_table(self) -> None:
        table = format_user_table({
            "id": "u-1",
            "email": "alice@example.com",
            "display_name": "Alice",
            "provider": "github",
        })
        assert table.row_count == 4
