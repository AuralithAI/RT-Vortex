#!/usr/bin/env python3
"""Main evaluation runner for AI-PR-Reviewer."""

from __future__ import annotations

import asyncio
import json
import logging
import os
import time
from dataclasses import dataclass, field
from datetime import datetime
from pathlib import Path
from typing import Any

import click
import httpx
import yaml
from rich.console import Console
from rich.progress import Progress, TaskID

from .metrics import EvaluationMetrics, compute_metrics

console = Console()
logger = logging.getLogger(__name__)


@dataclass
class EvalConfig:
    """Evaluation configuration."""

    api_base_url: str = "http://localhost:8080"
    api_key: str | None = None
    timeout: int = 300
    workers: int = 4
    max_retries: int = 3
    categories: list[str] = field(default_factory=lambda: ["security", "performance", "testing"])


@dataclass
class TestCase:
    """A single test case for evaluation."""

    id: str
    repository: str
    files: list[dict[str, Any]]
    expected_comments: list[dict[str, Any]]
    context: dict[str, Any] = field(default_factory=dict)


@dataclass
class EvalResult:
    """Result of evaluating a single test case."""

    test_case_id: str
    success: bool
    actual_comments: list[dict[str, Any]]
    expected_comments: list[dict[str, Any]]
    latency_ms: float
    tokens_used: int | None = None
    error: str | None = None


class EvaluationRunner:
    """Runs evaluation against AI-PR-Reviewer API."""

    def __init__(self, config: EvalConfig) -> None:
        self.config = config
        self.client = httpx.AsyncClient(
            base_url=config.api_base_url,
            timeout=config.timeout,
            headers={
                "Content-Type": "application/json",
                "Authorization": f"Bearer {config.api_key}" if config.api_key else "",
            },
        )

    async def close(self) -> None:
        """Close the HTTP client."""
        await self.client.aclose()

    async def run_test_case(self, test_case: TestCase) -> EvalResult:
        """Run a single test case."""
        start_time = time.time()

        try:
            # Build review request
            request = {
                "repository_url": test_case.repository,
                "files": test_case.files,
                "context": test_case.context,
            }

            # Submit review
            response = await self.client.post("/api/v1/reviews", json=request)
            response.raise_for_status()
            result = response.json()

            # Wait for completion if needed
            review_id = result.get("review_id")
            status = result.get("status", "completed")

            while status in ("pending", "processing"):
                await asyncio.sleep(2)
                status_response = await self.client.get(f"/api/v1/reviews/{review_id}")
                status_response.raise_for_status()
                result = status_response.json()
                status = result.get("status", "completed")

            latency_ms = (time.time() - start_time) * 1000

            return EvalResult(
                test_case_id=test_case.id,
                success=True,
                actual_comments=result.get("comments", []),
                expected_comments=test_case.expected_comments,
                latency_ms=latency_ms,
                tokens_used=result.get("metrics", {}).get("llm_tokens_used"),
            )

        except Exception as e:
            latency_ms = (time.time() - start_time) * 1000
            return EvalResult(
                test_case_id=test_case.id,
                success=False,
                actual_comments=[],
                expected_comments=test_case.expected_comments,
                latency_ms=latency_ms,
                error=str(e),
            )

    async def run_all(
        self, test_cases: list[TestCase], progress: Progress, task: TaskID
    ) -> list[EvalResult]:
        """Run all test cases with limited concurrency."""
        semaphore = asyncio.Semaphore(self.config.workers)
        results: list[EvalResult] = []

        async def run_with_semaphore(tc: TestCase) -> EvalResult:
            async with semaphore:
                result = await self.run_test_case(tc)
                progress.advance(task)
                return result

        tasks = [run_with_semaphore(tc) for tc in test_cases]
        results = await asyncio.gather(*tasks)
        return results


def load_dataset(dataset_path: Path) -> list[TestCase]:
    """Load test cases from a dataset directory."""
    test_cases: list[TestCase] = []

    for json_file in dataset_path.glob("**/*.json"):
        with open(json_file) as f:
            data = json.load(f)

        # Handle both single and multiple test cases per file
        if isinstance(data, list):
            for item in data:
                test_cases.append(parse_test_case(item))
        else:
            test_cases.append(parse_test_case(data))

    return test_cases


def parse_test_case(data: dict[str, Any]) -> TestCase:
    """Parse a test case from JSON data."""
    expected_comments: list[dict[str, Any]] = []

    for file_data in data.get("files", []):
        for comment in file_data.get("expected_comments", []):
            expected_comments.append({
                "file": file_data.get("path"),
                **comment,
            })

    return TestCase(
        id=data.get("pr_id", data.get("id", "unknown")),
        repository=data.get("repository", "unknown/repo"),
        files=data.get("files", []),
        expected_comments=expected_comments,
        context=data.get("context", {}),
    )


def load_config(config_path: Path) -> EvalConfig:
    """Load evaluation configuration."""
    with open(config_path) as f:
        data = yaml.safe_load(f)

    api_config = data.get("api", {})
    eval_config = data.get("evaluation", {})

    return EvalConfig(
        api_base_url=api_config.get("base_url", "http://localhost:8080"),
        api_key=os.environ.get("AIPR_API_KEY", api_config.get("api_key")),
        timeout=api_config.get("timeout", 300),
        workers=eval_config.get("workers", 4),
        max_retries=eval_config.get("max_retries", 3),
        categories=eval_config.get("categories", ["security", "performance", "testing"]),
    )


def save_results(results: list[EvalResult], metrics: EvaluationMetrics, output_path: Path) -> None:
    """Save evaluation results to JSON."""
    output_path.parent.mkdir(parents=True, exist_ok=True)

    output = {
        "timestamp": datetime.utcnow().isoformat(),
        "summary": {
            "total_tests": len(results),
            "successful": sum(1 for r in results if r.success),
            "failed": sum(1 for r in results if not r.success),
            "precision": metrics.precision,
            "recall": metrics.recall,
            "f1": metrics.f1,
            "latency_p50_ms": metrics.latency_p50,
            "latency_p95_ms": metrics.latency_p95,
            "latency_p99_ms": metrics.latency_p99,
        },
        "results": [
            {
                "test_case_id": r.test_case_id,
                "success": r.success,
                "latency_ms": r.latency_ms,
                "actual_comments": r.actual_comments,
                "expected_comments": r.expected_comments,
                "error": r.error,
            }
            for r in results
        ],
    }

    with open(output_path, "w") as f:
        json.dump(output, f, indent=2)


@click.command()
@click.option(
    "--dataset",
    "-d",
    type=click.Path(exists=True, path_type=Path),
    required=True,
    help="Path to dataset directory",
)
@click.option(
    "--config",
    "-c",
    type=click.Path(exists=True, path_type=Path),
    default="configs/default.yaml",
    help="Path to config file",
)
@click.option(
    "--output",
    "-o",
    type=click.Path(path_type=Path),
    default="results/latest.json",
    help="Output path for results",
)
@click.option("--verbose", "-v", is_flag=True, help="Enable verbose output")
def main(dataset: Path, config: Path, output: Path, verbose: bool) -> None:
    """Run AI-PR-Reviewer evaluation."""
    if verbose:
        logging.basicConfig(level=logging.DEBUG)
    else:
        logging.basicConfig(level=logging.INFO)

    console.print(f"[bold blue]AI-PR-Reviewer Evaluation[/bold blue]")
    console.print(f"Dataset: {dataset}")
    console.print(f"Config: {config}")

    # Load configuration
    eval_config = load_config(config)

    # Load test cases
    test_cases = load_dataset(dataset)
    console.print(f"Loaded {len(test_cases)} test cases")

    if not test_cases:
        console.print("[red]No test cases found![/red]")
        return

    # Run evaluation
    async def run() -> tuple[list[EvalResult], EvaluationMetrics]:
        runner = EvaluationRunner(eval_config)
        try:
            with Progress() as progress:
                task = progress.add_task("[cyan]Running evaluation...", total=len(test_cases))
                results = await runner.run_all(test_cases, progress, task)

            metrics = compute_metrics(results)
            return results, metrics
        finally:
            await runner.close()

    results, metrics = asyncio.run(run())

    # Print summary
    console.print("\n[bold green]Results:[/bold green]")
    console.print(f"  Precision: {metrics.precision:.3f}")
    console.print(f"  Recall: {metrics.recall:.3f}")
    console.print(f"  F1 Score: {metrics.f1:.3f}")
    console.print(f"  Latency P50: {metrics.latency_p50:.0f}ms")
    console.print(f"  Latency P95: {metrics.latency_p95:.0f}ms")

    # Save results
    save_results(results, metrics, output)
    console.print(f"\nResults saved to: {output}")


if __name__ == "__main__":
    main()
