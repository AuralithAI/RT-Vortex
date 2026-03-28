"""RTVortex CLI — main entry point.

Commands:
    rtvortex auth login|logout|whoami
    rtvortex review [--repo-id ID --pr N] [--watch] [--output table|json|markdown]
    rtvortex index  [--repo-id ID] [--follow]
    rtvortex status
    rtvortex config show|set
"""

from __future__ import annotations

import sys
import time
from typing import Optional

import click
from rich.console import Console
from rich.progress import (
    BarColumn,
    MofNCompleteColumn,
    Progress,
    SpinnerColumn,
    TextColumn,
    TimeElapsedColumn,
)

from rtvortex_cli import __version__
from rtvortex_cli.client import APIClient, APIError
from rtvortex_cli.config import Config
from rtvortex_cli.formatters import (
    format_comments_table,
    format_review_json,
    format_review_markdown,
    format_review_table,
    format_status_panel,
    format_user_table,
)

console = Console()
err_console = Console(stderr=True)

# Poll intervals
_REVIEW_POLL_INTERVAL = 3.0  # seconds
_INDEX_POLL_INTERVAL = 5.0


def _get_client(cfg: Config | None = None) -> APIClient:
    """Build an APIClient from the current config, exiting on auth failure."""
    cfg = cfg or Config.load()
    return APIClient(cfg)


def _require_auth(cfg: Config) -> None:
    """Exit with a helpful message if no token is configured."""
    if not cfg.is_authenticated:
        err_console.print(
            "[red]✗ Not authenticated.[/red]\n"
            "  Run [bold]rtvortex auth login[/bold] or set RTVORTEX_TOKEN."
        )
        raise SystemExit(1)


# ═════════════════════════════════════════════════════════════════════════════
# Root group
# ═════════════════════════════════════════════════════════════════════════════


@click.group()
@click.version_option(__version__, prog_name="rtvortex")
def cli() -> None:
    """RTVortex — AI-powered code review from your terminal."""


# ═════════════════════════════════════════════════════════════════════════════
# auth
# ═════════════════════════════════════════════════════════════════════════════


@cli.group()
def auth() -> None:
    """Authenticate with the RTVortex server."""


@auth.command()
@click.option("--token", prompt="API token", hide_input=True, help="API token from Web UI.")
@click.option("--server", default=None, help="Server URL (default: http://localhost:8080).")
def login(token: str, server: Optional[str]) -> None:
    """Save an API token and validate it against the server."""
    cfg = Config.load()
    cfg.token = token.strip()
    if server:
        cfg.server_url = server.rstrip("/")

    # Validate the token by hitting /ready
    client = APIClient(cfg)
    try:
        client.get("/ready")
    except APIError as exc:
        err_console.print(f"[red]✗ Server validation failed:[/red] {exc.message}")
        raise SystemExit(1) from exc
    finally:
        client.close()

    path = cfg.save()
    console.print(f"[green]✓ Authenticated.[/green] Config saved to {path}")


@auth.command()
def logout() -> None:
    """Remove the stored API token."""
    cfg = Config.load()
    cfg.clear_token()
    console.print("[green]✓ Token removed.[/green]")


@auth.command()
def whoami() -> None:
    """Show the currently authenticated user."""
    cfg = Config.load()
    _require_auth(cfg)
    client = _get_client(cfg)
    try:
        user = client.get("/api/v1/user/me")
        console.print(format_user_table(user))
    except APIError as exc:
        err_console.print(f"[red]✗ {exc.message}[/red]")
        raise SystemExit(1) from exc
    finally:
        client.close()


# ═════════════════════════════════════════════════════════════════════════════
# review
# ═════════════════════════════════════════════════════════════════════════════


@cli.command()
@click.option("--repo-id", required=True, help="Repository UUID.")
@click.option("--pr", "pr_number", required=True, type=int, help="Pull request number.")
@click.option("--watch", is_flag=True, help="Poll until the review completes.")
@click.option(
    "--output",
    "output_fmt",
    type=click.Choice(["table", "json", "markdown"]),
    default=None,
    help="Output format (default: from config).",
)
def review(repo_id: str, pr_number: int, watch: bool, output_fmt: Optional[str]) -> None:
    """Trigger an AI code review for a pull request."""
    cfg = Config.load()
    _require_auth(cfg)
    fmt = output_fmt or cfg.output_format
    client = _get_client(cfg)

    try:
        # Trigger the review
        with console.status("[bold cyan]Triggering review…[/bold cyan]"):
            result = client.post(
                "/api/v1/reviews",
                json={"repo_id": repo_id, "pr_number": pr_number},
            )

        review_id = result.get("review_id") or result.get("id")
        if not review_id:
            _print_result(result, fmt)
            return

        console.print(f"[green]✓ Review triggered:[/green] {review_id}")

        if watch:
            result = _watch_review(client, str(review_id))

        _print_result(result, fmt)

        # Fetch and display comments if available
        if review_id:
            try:
                comments_resp = client.get(f"/api/v1/reviews/{review_id}/comments")
                comments = comments_resp.get("comments", [])
                if comments and fmt == "table":
                    console.print()
                    console.print(format_comments_table(comments))
            except APIError:
                pass  # Comments endpoint may not be available yet

    except APIError as exc:
        err_console.print(f"[red]✗ Review failed:[/red] {exc.message}")
        raise SystemExit(1) from exc
    finally:
        client.close()


def _watch_review(client: APIClient, review_id: str) -> dict:
    """Poll the review endpoint until completion, showing progress."""
    terminal_statuses = {"completed", "failed", "error"}

    with Progress(
        SpinnerColumn(),
        TextColumn("[progress.description]{task.description}"),
        BarColumn(),
        MofNCompleteColumn(),
        TimeElapsedColumn(),
        console=console,
    ) as progress:
        task = progress.add_task("Reviewing…", total=None)

        while True:
            data = client.get(f"/api/v1/reviews/{review_id}")
            status = str(data.get("status", "unknown")).lower()

            # Update description with current step info
            step = data.get("current_step", "")
            steps_total = data.get("total_steps")
            steps_done = data.get("steps_completed", 0)

            desc = f"[{status}]"
            if step:
                desc += f" {step}"

            if steps_total:
                progress.update(task, total=steps_total, completed=steps_done, description=desc)
            else:
                progress.update(task, description=desc)

            if status in terminal_statuses:
                break

            time.sleep(_REVIEW_POLL_INTERVAL)

    status = str(data.get("status", "unknown")).lower()
    if status == "completed":
        console.print("[bold green]✓ Review complete.[/bold green]")
    else:
        console.print(f"[bold red]✗ Review {status}.[/bold red]")

    return data  # type: ignore[return-value]


def _print_result(data: dict, fmt: str) -> None:
    """Print review/index result in the requested format."""
    if fmt == "json":
        console.print(format_review_json(data))
    elif fmt == "markdown":
        console.print(format_review_markdown(data))
    else:
        console.print(format_review_table(data))


# ═════════════════════════════════════════════════════════════════════════════
# index
# ═════════════════════════════════════════════════════════════════════════════


@cli.command()
@click.option("--repo-id", required=True, help="Repository UUID.")
@click.option("--follow", is_flag=True, help="Poll index status with a progress bar.")
def index(repo_id: str, follow: bool) -> None:
    """Trigger codebase indexing for a repository."""
    cfg = Config.load()
    _require_auth(cfg)
    client = _get_client(cfg)

    try:
        with console.status("[bold cyan]Starting indexing…[/bold cyan]"):
            result = client.post(f"/api/v1/repos/{repo_id}/index")

        console.print(f"[green]✓ Indexing started.[/green] Job: {result.get('job_id', '—')}")

        if follow:
            _follow_index(client, repo_id)

    except APIError as exc:
        err_console.print(f"[red]✗ Indexing failed:[/red] {exc.message}")
        raise SystemExit(1) from exc
    finally:
        client.close()


def _follow_index(client: APIClient, repo_id: str) -> None:
    """Poll index status until complete."""
    terminal_statuses = {"completed", "failed", "error", "idle"}

    with Progress(
        SpinnerColumn(),
        TextColumn("[progress.description]{task.description}"),
        BarColumn(),
        TextColumn("{task.fields[pct]}"),
        TimeElapsedColumn(),
        console=console,
    ) as progress:
        task = progress.add_task("Indexing…", total=100, pct="")

        while True:
            data = client.get(f"/api/v1/repos/{repo_id}/index/status")
            status = str(data.get("status", "unknown")).lower()
            pct = data.get("progress", 0)

            progress.update(
                task,
                completed=pct,
                description=f"Indexing [{status}]",
                pct=f"{pct}%",
            )

            if status in terminal_statuses:
                break

            time.sleep(_INDEX_POLL_INTERVAL)

    if status == "completed":
        console.print("[bold green]✓ Indexing complete.[/bold green]")
    elif status == "idle":
        console.print("[dim]Index is idle (already up to date).[/dim]")
    else:
        console.print(f"[bold red]✗ Indexing {status}.[/bold red]")


# ═════════════════════════════════════════════════════════════════════════════
# status
# ═════════════════════════════════════════════════════════════════════════════


@cli.command()
def status() -> None:
    """Show server health and system statistics."""
    cfg = Config.load()
    client = _get_client(cfg)

    try:
        health = client.get("/ready")

        stats = None
        if cfg.is_authenticated:
            try:
                stats = client.get("/api/v1/admin/stats")
            except APIError:
                pass  # Non-admin users won't have access

        console.print(format_status_panel(health, stats))

    except APIError as exc:
        err_console.print(f"[red]✗ Cannot reach server:[/red] {exc.message}")
        raise SystemExit(1) from exc
    finally:
        client.close()


# ═════════════════════════════════════════════════════════════════════════════
# config
# ═════════════════════════════════════════════════════════════════════════════


@cli.group("config")
def config_group() -> None:
    """View and manage CLI configuration."""


@config_group.command("show")
def config_show() -> None:
    """Display current configuration (token is masked)."""
    cfg = Config.load()

    from rich.table import Table

    table = Table(title="RTVortex Configuration", show_lines=True)
    table.add_column("Key", style="bold cyan", width=16)
    table.add_column("Value", style="white")

    table.add_row("server_url", cfg.server_url)
    table.add_row("token", cfg.masked_token())
    table.add_row("output_format", cfg.output_format)
    for k, v in cfg.extra.items():
        table.add_row(k, str(v))

    console.print(table)


@config_group.command("set")
@click.argument("key")
@click.argument("value")
def config_set(key: str, value: str) -> None:
    """Set a configuration value (e.g. server_url, output_format)."""
    cfg = Config.load()
    cfg.set_value(key, value)
    console.print(f"[green]✓ {key} = {value}[/green]")


# ═════════════════════════════════════════════════════════════════════════════
# Entry point
# ═════════════════════════════════════════════════════════════════════════════

if __name__ == "__main__":
    cli()
