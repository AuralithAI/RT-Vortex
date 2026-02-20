#!/usr/bin/env python3
"""Report generation for AI-PR-Reviewer evaluation."""

from __future__ import annotations

import json
from datetime import datetime
from pathlib import Path

import click
from jinja2 import Environment, FileSystemLoader
from rich.console import Console

console = Console()


REPORT_TEMPLATE = """
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>AI-PR-Reviewer Evaluation Report</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            max-width: 1200px;
            margin: 0 auto;
            padding: 20px;
            background: #f5f5f5;
        }
        h1, h2, h3 {
            color: #333;
        }
        .summary-cards {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 20px;
            margin: 20px 0;
        }
        .card {
            background: white;
            padding: 20px;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        .card h3 {
            margin: 0 0 10px 0;
            font-size: 14px;
            color: #666;
            text-transform: uppercase;
        }
        .card .value {
            font-size: 32px;
            font-weight: bold;
            color: #333;
        }
        .card.good .value { color: #22c55e; }
        .card.warning .value { color: #f59e0b; }
        .card.bad .value { color: #ef4444; }
        table {
            width: 100%;
            border-collapse: collapse;
            background: white;
            border-radius: 8px;
            overflow: hidden;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        th, td {
            padding: 12px 16px;
            text-align: left;
            border-bottom: 1px solid #eee;
        }
        th {
            background: #f9fafb;
            font-weight: 600;
            color: #333;
        }
        .success { color: #22c55e; }
        .error { color: #ef4444; }
        .timestamp {
            color: #666;
            font-size: 14px;
        }
        .section {
            margin: 30px 0;
        }
    </style>
</head>
<body>
    <h1>AI-PR-Reviewer Evaluation Report</h1>
    <p class="timestamp">Generated: {{ timestamp }}</p>

    <div class="section">
        <h2>Summary</h2>
        <div class="summary-cards">
            <div class="card {{ 'good' if summary.precision >= 0.8 else 'warning' if summary.precision >= 0.6 else 'bad' }}">
                <h3>Precision</h3>
                <div class="value">{{ "%.1f" | format(summary.precision * 100) }}%</div>
            </div>
            <div class="card {{ 'good' if summary.recall >= 0.8 else 'warning' if summary.recall >= 0.6 else 'bad' }}">
                <h3>Recall</h3>
                <div class="value">{{ "%.1f" | format(summary.recall * 100) }}%</div>
            </div>
            <div class="card {{ 'good' if summary.f1 >= 0.8 else 'warning' if summary.f1 >= 0.6 else 'bad' }}">
                <h3>F1 Score</h3>
                <div class="value">{{ "%.1f" | format(summary.f1 * 100) }}%</div>
            </div>
            <div class="card">
                <h3>Tests Run</h3>
                <div class="value">{{ summary.total_tests }}</div>
            </div>
        </div>
    </div>

    <div class="section">
        <h2>Performance</h2>
        <div class="summary-cards">
            <div class="card">
                <h3>Latency P50</h3>
                <div class="value">{{ "%.0f" | format(summary.latency_p50_ms) }}ms</div>
            </div>
            <div class="card {{ 'good' if summary.latency_p95_ms < 5000 else 'warning' if summary.latency_p95_ms < 10000 else 'bad' }}">
                <h3>Latency P95</h3>
                <div class="value">{{ "%.0f" | format(summary.latency_p95_ms) }}ms</div>
            </div>
            <div class="card">
                <h3>Latency P99</h3>
                <div class="value">{{ "%.0f" | format(summary.latency_p99_ms) }}ms</div>
            </div>
            <div class="card">
                <h3>Success Rate</h3>
                <div class="value">{{ "%.1f" | format(summary.successful / summary.total_tests * 100) }}%</div>
            </div>
        </div>
    </div>

    <div class="section">
        <h2>Test Results</h2>
        <table>
            <thead>
                <tr>
                    <th>Test Case</th>
                    <th>Status</th>
                    <th>Expected</th>
                    <th>Actual</th>
                    <th>Latency</th>
                </tr>
            </thead>
            <tbody>
                {% for result in results %}
                <tr>
                    <td>{{ result.test_case_id }}</td>
                    <td class="{{ 'success' if result.success else 'error' }}">
                        {{ 'Pass' if result.success else 'Fail' }}
                    </td>
                    <td>{{ result.expected_comments | length }}</td>
                    <td>{{ result.actual_comments | length }}</td>
                    <td>{{ "%.0f" | format(result.latency_ms) }}ms</td>
                </tr>
                {% endfor %}
            </tbody>
        </table>
    </div>
</body>
</html>
"""


def generate_html_report(data: dict, output_path: Path) -> None:
    """Generate HTML report from evaluation data."""
    env = Environment(autoescape=True)
    template = env.from_string(REPORT_TEMPLATE)

    html = template.render(
        timestamp=data.get("timestamp", datetime.utcnow().isoformat()),
        summary=data.get("summary", {}),
        results=data.get("results", []),
    )

    with open(output_path, "w") as f:
        f.write(html)


def generate_markdown_report(data: dict, output_path: Path) -> None:
    """Generate Markdown report from evaluation data."""
    summary = data.get("summary", {})
    results = data.get("results", [])

    lines = [
        "# AI-PR-Reviewer Evaluation Report",
        "",
        f"Generated: {data.get('timestamp', datetime.utcnow().isoformat())}",
        "",
        "## Summary",
        "",
        "| Metric | Value |",
        "|--------|-------|",
        f"| Precision | {summary.get('precision', 0):.1%} |",
        f"| Recall | {summary.get('recall', 0):.1%} |",
        f"| F1 Score | {summary.get('f1', 0):.1%} |",
        f"| Total Tests | {summary.get('total_tests', 0)} |",
        f"| Successful | {summary.get('successful', 0)} |",
        f"| Failed | {summary.get('failed', 0)} |",
        "",
        "## Performance",
        "",
        "| Metric | Value |",
        "|--------|-------|",
        f"| Latency P50 | {summary.get('latency_p50_ms', 0):.0f}ms |",
        f"| Latency P95 | {summary.get('latency_p95_ms', 0):.0f}ms |",
        f"| Latency P99 | {summary.get('latency_p99_ms', 0):.0f}ms |",
        "",
        "## Test Results",
        "",
        "| Test Case | Status | Expected | Actual | Latency |",
        "|-----------|--------|----------|--------|---------|",
    ]

    for result in results:
        status = "✅ Pass" if result.get("success") else "❌ Fail"
        expected = len(result.get("expected_comments", []))
        actual = len(result.get("actual_comments", []))
        latency = result.get("latency_ms", 0)
        lines.append(f"| {result.get('test_case_id')} | {status} | {expected} | {actual} | {latency:.0f}ms |")

    with open(output_path, "w") as f:
        f.write("\n".join(lines))


@click.command()
@click.option(
    "--results",
    "-r",
    type=click.Path(exists=True, path_type=Path),
    required=True,
    help="Path to results JSON file",
)
@click.option(
    "--output",
    "-o",
    type=click.Path(path_type=Path),
    required=True,
    help="Output path for report",
)
@click.option(
    "--format",
    "-f",
    type=click.Choice(["html", "markdown", "json"]),
    default="html",
    help="Output format",
)
def main(results: Path, output: Path, format: str) -> None:
    """Generate evaluation report."""
    console.print(f"[bold blue]Generating {format} report[/bold blue]")

    # Load results
    with open(results) as f:
        data = json.load(f)

    # Generate report
    output.parent.mkdir(parents=True, exist_ok=True)

    if format == "html":
        generate_html_report(data, output)
    elif format == "markdown":
        generate_markdown_report(data, output)
    elif format == "json":
        # Just copy with pretty printing
        with open(output, "w") as f:
            json.dump(data, f, indent=2)

    console.print(f"[green]Report saved to: {output}[/green]")


if __name__ == "__main__":
    main()
