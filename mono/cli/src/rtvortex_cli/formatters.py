"""Rich-based formatters for CLI output."""

from __future__ import annotations

import json
from typing import Any

from rich.console import Console
from rich.panel import Panel
from rich.table import Table


console = Console()


# ── Review Formatting ────────────────────────────────────────────────────────


def format_review_table(data: dict[str, Any]) -> Table:
    """Build a Rich table from a review API response."""
    table = Table(title="Review Summary", show_lines=True)
    table.add_column("Field", style="bold cyan", width=20)
    table.add_column("Value", style="white")

    table.add_row("Review ID", str(data.get("id", "—")))
    table.add_row("Repository", str(data.get("repo_id", "—")))
    table.add_row("PR Number", str(data.get("pr_number", "—")))
    table.add_row("Status", _status_style(str(data.get("status", "unknown"))))
    table.add_row("Comments", str(data.get("comments_count", 0)))
    table.add_row("Created", str(data.get("created_at", "—")))
    if data.get("completed_at"):
        table.add_row("Completed", str(data["completed_at"]))
    return table


def format_comments_table(comments: list[dict[str, Any]]) -> Table:
    """Build a Rich table of review comments, colour-coded by severity."""
    table = Table(title=f"Review Comments ({len(comments)})", show_lines=True)
    table.add_column("#", style="dim", width=4)
    table.add_column("File", style="blue", max_width=40)
    table.add_column("Line", style="cyan", width=6)
    table.add_column("Severity", width=10)
    table.add_column("Message", max_width=60)

    severity_styles = {
        "critical": "[bold red]critical[/bold red]",
        "error": "[red]error[/red]",
        "warning": "[yellow]warning[/yellow]",
        "suggestion": "[green]suggestion[/green]",
        "info": "[dim]info[/dim]",
    }

    for i, c in enumerate(comments, 1):
        sev = str(c.get("severity", "info")).lower()
        table.add_row(
            str(i),
            str(c.get("file_path", "—")),
            str(c.get("line_number", "—")),
            severity_styles.get(sev, sev),
            str(c.get("message", "")),
        )
    return table


def format_review_json(data: dict[str, Any]) -> str:
    """Pretty-print a review response as JSON."""
    return json.dumps(data, indent=2, default=str)


def format_review_markdown(data: dict[str, Any]) -> str:
    """Format a review response as Markdown."""
    lines = [
        f"# Review {data.get('id', '—')}",
        "",
        f"- **Repository:** {data.get('repo_id', '—')}",
        f"- **PR:** #{data.get('pr_number', '—')}",
        f"- **Status:** {data.get('status', 'unknown')}",
        f"- **Comments:** {data.get('comments_count', 0)}",
        "",
    ]
    comments = data.get("comments", [])
    if comments:
        lines.append("## Comments\n")
        for c in comments:
            sev = c.get("severity", "info")
            lines.append(
                f"- **[{sev}]** `{c.get('file_path', '?')}:{c.get('line_number', '?')}` — "
                f"{c.get('message', '')}"
            )
    return "\n".join(lines)


# ── Status Formatting ────────────────────────────────────────────────────────


def format_status_panel(
    health: dict[str, Any],
    stats: dict[str, Any] | None = None,
) -> Panel:
    """Build a Rich panel with server health and (optional) admin stats."""
    table = Table(show_header=False, box=None, padding=(0, 2))
    table.add_column("Key", style="bold")
    table.add_column("Value")

    # Health
    status_val = health.get("status", "unknown")
    table.add_row("Server Status", _status_style(status_val))
    for check, val in health.get("checks", {}).items():
        style = "[green]" if val == "ok" else "[red]"
        table.add_row(f"  {check}", f"{style}{val}[/]")

    if stats:
        table.add_row("", "")  # spacer
        table.add_row("[bold]Stats[/bold]", "")
        for key, val in stats.items():
            if isinstance(val, dict):
                continue
            table.add_row(f"  {key}", str(val))

    return Panel(table, title="[bold]RTVortex Status[/bold]", border_style="blue")


# ── User Formatting ──────────────────────────────────────────────────────────


def format_user_table(user: dict[str, Any]) -> Table:
    """Display user profile in a Rich table."""
    table = Table(title="Current User", show_lines=True)
    table.add_column("Field", style="bold cyan", width=16)
    table.add_column("Value", style="white")

    table.add_row("ID", str(user.get("id", "—")))
    table.add_row("Email", str(user.get("email", "—")))
    table.add_row("Display Name", str(user.get("display_name", "—")))
    table.add_row("Provider", str(user.get("provider", "—")))
    return table


# ── Helpers ──────────────────────────────────────────────────────────────────


def _status_style(status: str) -> str:
    """Return a Rich-markup-styled status string."""
    s = status.lower()
    if s in ("ok", "ready", "completed", "healthy"):
        return f"[bold green]{status}[/bold green]"
    if s in ("pending", "processing", "running"):
        return f"[bold yellow]{status}[/bold yellow]"
    if s in ("failed", "error", "dead", "unhealthy", "not_ready"):
        return f"[bold red]{status}[/bold red]"
    return status
