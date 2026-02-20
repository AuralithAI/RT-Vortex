#!/usr/bin/env python3
"""
AI PR Reviewer - Command Line Interface

Production-grade CLI for reviewing PRs, managing indices, and configuring the reviewer.
"""

import argparse
import json
import os
import sys
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Optional, List, Dict, Any

import requests
from rich.console import Console
from rich.table import Table
from rich.progress import Progress, SpinnerColumn, TextColumn
from rich.syntax import Syntax
from rich.panel import Panel
from rich.markdown import Markdown

# Version
__version__ = "0.1.0"

console = Console()


@dataclass
class Config:
    """CLI configuration."""
    server_url: str = "http://localhost:8080"
    api_key: Optional[str] = None
    llm_provider: str = "openai-compatible"
    llm_api_key: Optional[str] = None
    llm_model: str = "gpt-4-turbo-preview"
    llm_base_url: str = "https://api.openai.com/v1"
    output_format: str = "rich"
    
    @classmethod
    def from_file(cls, path: Path) -> "Config":
        """Load config from YAML file."""
        if not path.exists():
            return cls()
        
        import yaml
        with open(path) as f:
            data = yaml.safe_load(f)
        
        return cls(
            server_url=data.get("server_url", cls.server_url),
            api_key=data.get("api_key"),
            llm_provider=data.get("llm_provider", cls.llm_provider),
            llm_api_key=data.get("llm_api_key"),
            llm_model=data.get("llm_model", cls.llm_model),
            llm_base_url=data.get("llm_base_url", cls.llm_base_url),
            output_format=data.get("output_format", cls.output_format),
        )
    
    @classmethod
    def from_env(cls) -> "Config":
        """Load config from environment variables."""
        return cls(
            server_url=os.getenv("AIPR_SERVER_URL", cls.server_url),
            api_key=os.getenv("AIPR_API_KEY"),
            llm_provider=os.getenv("AIPR_LLM_PROVIDER", cls.llm_provider),
            llm_api_key=os.getenv("AIPR_LLM_API_KEY") or os.getenv("OPENAI_API_KEY"),
            llm_model=os.getenv("AIPR_LLM_MODEL", cls.llm_model),
            llm_base_url=os.getenv("AIPR_LLM_BASE_URL", cls.llm_base_url),
            output_format=os.getenv("AIPR_OUTPUT_FORMAT", cls.output_format),
        )


class APIClient:
    """Client for AI PR Reviewer API."""
    
    def __init__(self, config: Config):
        self.config = config
        self.session = requests.Session()
        if config.api_key:
            self.session.headers["Authorization"] = f"Bearer {config.api_key}"
    
    def review(self, repo_id: str, pr_number: int, diff: str, **kwargs) -> Dict[str, Any]:
        """Submit a PR for review."""
        response = self.session.post(
            f"{self.config.server_url}/api/v1/reviews",
            json={
                "repoId": repo_id,
                "prNumber": pr_number,
                "diff": diff,
                "prTitle": kwargs.get("title"),
                "prDescription": kwargs.get("description"),
            },
            timeout=300,
        )
        response.raise_for_status()
        return response.json()
    
    def get_review(self, review_id: str) -> Dict[str, Any]:
        """Get a review by ID."""
        response = self.session.get(
            f"{self.config.server_url}/api/v1/reviews/{review_id}",
            timeout=30,
        )
        response.raise_for_status()
        return response.json()
    
    def index(self, repo_id: str, path: str, incremental: bool = True) -> Dict[str, Any]:
        """Index a repository."""
        endpoint = "incremental" if incremental else "full"
        response = self.session.post(
            f"{self.config.server_url}/api/v1/index/{endpoint}",
            json={"repoId": repo_id, "path": path},
            timeout=60,
        )
        response.raise_for_status()
        return response.json()
    
    def index_status(self, job_id: str) -> Dict[str, Any]:
        """Get indexing job status."""
        response = self.session.get(
            f"{self.config.server_url}/api/v1/index/status/{job_id}",
            timeout=30,
        )
        response.raise_for_status()
        return response.json()


def cmd_review(args, config: Config):
    """Review a PR or diff."""
    console.print("[bold]AI PR Reviewer[/bold] - Reviewing changes...\n")
    
    if args.diff_file:
        diff = Path(args.diff_file).read_text()
    elif args.stdin:
        diff = sys.stdin.read()
    elif args.pr:
        diff = fetch_pr_diff(args.repo, args.pr, config)
    else:
        import subprocess
        result = subprocess.run(
            ["git", "diff", args.base or "HEAD~1"],
            capture_output=True,
            text=True,
        )
        diff = result.stdout
    
    if not diff.strip():
        console.print("[yellow]No changes to review[/yellow]")
        return 0
    
    client = APIClient(config)
    
    with Progress(
        SpinnerColumn(),
        TextColumn("[progress.description]{task.description}"),
        console=console,
    ) as progress:
        task = progress.add_task("Analyzing code...", total=None)
        
        try:
            result = client.review(
                repo_id=args.repo or get_repo_from_git(),
                pr_number=args.pr or 0,
                diff=diff,
                title=args.title,
                description=args.description,
            )
        except Exception as e:
            console.print(f"[red]Error: {e}[/red]")
            return 1
        
        progress.update(task, description="Review complete!")
    
    # Output results
    if args.output == "json":
        print(json.dumps(result, indent=2))
    else:
        display_review(result)
    
    # Exit code based on findings
    critical = sum(1 for c in result.get("comments", []) if c.get("severity") == "critical")
    return 1 if critical > 0 and args.fail_on_critical else 0


def cmd_index(args, config: Config):
    """Index a repository."""
    console.print("[bold]AI PR Reviewer[/bold] - Indexing repository...\n")
    
    client = APIClient(config)
    
    with Progress(
        SpinnerColumn(),
        TextColumn("[progress.description]{task.description}"),
        console=console,
    ) as progress:
        task = progress.add_task("Starting indexing...", total=None)
        
        try:
            result = client.index(
                repo_id=args.repo or get_repo_from_git(),
                path=args.path or os.getcwd(),
                incremental=not args.full,
            )
            job_id = result.get("jobId")
            
            while True:
                status = client.index_status(job_id)
                state = status.get("state")
                progress_pct = status.get("progress", 0)
                message = status.get("message", "Processing...")
                
                progress.update(task, description=f"[{progress_pct}%] {message}")
                
                if state == "COMPLETED":
                    break
                elif state == "FAILED":
                    console.print(f"[red]Indexing failed: {status.get('error')}[/red]")
                    return 1
                
                time.sleep(2)
            
        except Exception as e:
            console.print(f"[red]Error: {e}[/red]")
            return 1
    
    console.print("[green]✓ Indexing complete![/green]")
    console.print(f"  Files: {status.get('filesProcessed', 0)}")
    console.print(f"  Chunks: {status.get('chunksCreated', 0)}")
    
    return 0


def cmd_config(args, config: Config):
    """Show or set configuration."""
    if args.show:
        table = Table(title="Configuration")
        table.add_column("Key")
        table.add_column("Value")
        
        table.add_row("Server URL", config.server_url)
        table.add_row("API Key", "***" if config.api_key else "(not set)")
        table.add_row("LLM Provider", config.llm_provider)
        table.add_row("LLM Model", config.llm_model)
        table.add_row("LLM Base URL", config.llm_base_url)
        table.add_row("Output Format", config.output_format)
        
        console.print(table)
    
    return 0


def display_review(result: Dict[str, Any]):
    """Display review results in rich format."""
    assessment = result.get("overallAssessment", "comment")
    assessment_colors = {
        "approve": "green",
        "request_changes": "red",
        "comment": "yellow",
    }
    color = assessment_colors.get(assessment, "white")
    
    console.print(Panel(
        f"[bold {color}]{assessment.upper()}[/bold {color}]\n\n{result.get('summary', 'No summary')}",
        title="Review Summary",
    ))
    
    # Comments
    comments = result.get("comments", [])
    if comments:
        console.print(f"\n[bold]Found {len(comments)} issue(s):[/bold]\n")
        
        severity_colors = {
            "critical": "red",
            "error": "red",
            "warning": "yellow",
            "info": "blue",
            "suggestion": "cyan",
        }
        
        for comment in comments:
            severity = comment.get("severity", "info")
            color = severity_colors.get(severity, "white")
            
            console.print(f"[{color}]● {severity.upper()}[/{color}] - {comment.get('category', '')}")
            console.print(f"  [dim]{comment.get('filePath', '')}:{comment.get('line', 0)}[/dim]")
            console.print(f"  {comment.get('message', '')}")
            
            if comment.get("suggestion"):
                console.print(f"  [dim]💡 {comment.get('suggestion')}[/dim]")
            
            console.print()
    else:
        console.print("\n[green]✓ No issues found![/green]")


def fetch_pr_diff(repo: str, pr_number: int, config: Config) -> str:
    """Fetch PR diff from GitHub."""
    token = os.getenv("GITHUB_TOKEN")
    if not token:
        raise ValueError("GITHUB_TOKEN environment variable required")
    
    response = requests.get(
        f"https://api.github.com/repos/{repo}/pulls/{pr_number}",
        headers={
            "Authorization": f"Bearer {token}",
            "Accept": "application/vnd.github.v3.diff",
        },
    )
    response.raise_for_status()
    return response.text


def get_repo_from_git() -> str:
    """Get repository name from git remote."""
    import subprocess
    result = subprocess.run(
        ["git", "remote", "get-url", "origin"],
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        return "unknown/unknown"
    
    url = result.stdout.strip()
    # Parse GitHub URL
    if "github.com" in url:
        parts = url.replace(".git", "").split("github.com")[-1]
        return parts.strip("/:")
    
    return "unknown/unknown"


def main():
    """Main entry point."""
    parser = argparse.ArgumentParser(
        description="AI PR Reviewer - Automated code review powered by AI",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument("--version", action="version", version=f"%(prog)s {__version__}")
    parser.add_argument("-c", "--config", help="Path to config file")
    parser.add_argument("-o", "--output", choices=["rich", "json", "github"], default="rich")
    
    subparsers = parser.add_subparsers(dest="command", help="Commands")
    
    # Review command
    review_parser = subparsers.add_parser("review", help="Review code changes")
    review_parser.add_argument("--repo", "-r", help="Repository (owner/name)")
    review_parser.add_argument("--pr", "-p", type=int, help="PR number to review")
    review_parser.add_argument("--diff-file", "-f", help="Path to diff file")
    review_parser.add_argument("--stdin", action="store_true", help="Read diff from stdin")
    review_parser.add_argument("--base", "-b", help="Base ref for diff")
    review_parser.add_argument("--title", "-t", help="PR title")
    review_parser.add_argument("--description", "-d", help="PR description")
    review_parser.add_argument("--fail-on-critical", action="store_true")
    
    # Index command
    index_parser = subparsers.add_parser("index", help="Index a repository")
    index_parser.add_argument("--repo", "-r", help="Repository ID")
    index_parser.add_argument("--path", "-p", help="Path to repository")
    index_parser.add_argument("--full", action="store_true", help="Full reindex")
    
    # Config command
    config_parser = subparsers.add_parser("config", help="Show configuration")
    config_parser.add_argument("--show", action="store_true", default=True)
    
    args = parser.parse_args()
    
    if not args.command:
        parser.print_help()
        return 1
    
    config_path = Path(args.config) if args.config else Path.home() / ".aipr" / "config.yml"
    config = Config.from_file(config_path)
    
    env_config = Config.from_env()
    for field in ["api_key", "llm_api_key", "server_url"]:
        if getattr(env_config, field):
            setattr(config, field, getattr(env_config, field))
    
    if args.output:
        config.output_format = args.output
    
    commands = {
        "review": cmd_review,
        "index": cmd_index,
        "config": cmd_config,
    }
    
    return commands[args.command](args, config)


if __name__ == "__main__":
    sys.exit(main())
