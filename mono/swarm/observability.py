"""Observability Dashboard — Python client.

Provides helpers for agents to query observability data from the Go server.
This is primarily used by monitoring/debugging tools, not by agents themselves
during task execution.

Endpoints:
    GET /api/v1/swarm/observability/dashboard     → full dashboard
    GET /api/v1/swarm/observability/time-series    → metric time-series
    GET /api/v1/swarm/observability/providers      → provider performance
    GET /api/v1/swarm/observability/cost           → cost summary
    GET /api/v1/swarm/observability/health         → health score breakdown
"""

from __future__ import annotations

import logging
from dataclasses import dataclass, field
from typing import Any

logger = logging.getLogger(__name__)


# ── Data Types ───────────────────────────────────────────────────────────────

@dataclass
class HealthBreakdown:
    """Composite health score breakdown."""

    score: int = 100
    task_health_pct: float = 100.0
    agent_health_pct: float = 100.0
    provider_health_pct: float = 100.0
    queue_health_pct: float = 100.0
    error_rate_pct: float = 0.0
    details: str = ""

    @classmethod
    def from_dict(cls, d: dict[str, Any]) -> "HealthBreakdown":
        return cls(
            score=d.get("score", 100),
            task_health_pct=d.get("task_health_pct", 100),
            agent_health_pct=d.get("agent_health_pct", 100),
            provider_health_pct=d.get("provider_health_pct", 100),
            queue_health_pct=d.get("queue_health_pct", 100),
            error_rate_pct=d.get("error_rate_pct", 0),
            details=d.get("details", ""),
        )


@dataclass
class CostSummary:
    """Aggregated cost data."""

    today_usd: float = 0.0
    this_week_usd: float = 0.0
    this_month_usd: float = 0.0
    by_provider: dict[str, float] = field(default_factory=dict)

    @classmethod
    def from_dict(cls, d: dict[str, Any]) -> "CostSummary":
        return cls(
            today_usd=d.get("today_usd", 0),
            this_week_usd=d.get("this_week_usd", 0),
            this_month_usd=d.get("this_month_usd", 0),
            by_provider=d.get("by_provider", {}),
        )


@dataclass
class ProviderPerfPoint:
    """A single provider performance data point."""

    provider: str = ""
    calls: int = 0
    successes: int = 0
    failures: int = 0
    tokens_used: int = 0
    avg_latency_ms: float = 0.0
    p95_latency_ms: float = 0.0
    p99_latency_ms: float = 0.0
    error_rate: float = 0.0
    estimated_cost_usd: float = 0.0
    consensus_wins: int = 0
    consensus_total: int = 0
    created_at: str = ""

    @classmethod
    def from_dict(cls, d: dict[str, Any]) -> "ProviderPerfPoint":
        return cls(
            provider=d.get("provider", ""),
            calls=d.get("calls", 0),
            successes=d.get("successes", 0),
            failures=d.get("failures", 0),
            tokens_used=d.get("tokens_used", 0),
            avg_latency_ms=d.get("avg_latency_ms", 0),
            p95_latency_ms=d.get("p95_latency_ms", 0),
            p99_latency_ms=d.get("p99_latency_ms", 0),
            error_rate=d.get("error_rate", 0),
            estimated_cost_usd=d.get("estimated_cost_usd", 0),
            consensus_wins=d.get("consensus_wins", 0),
            consensus_total=d.get("consensus_total", 0),
            created_at=d.get("created_at", ""),
        )


# ── API Helpers ──────────────────────────────────────────────────────────────

async def get_observability_dashboard(
    go_client: Any,
    hours: int = 24,
) -> dict:
    """Fetch the full observability dashboard from the Go server.

    Args:
        go_client: GoClient instance with auth token.
        hours: Time range for time-series data (default 24h).

    Returns:
        Raw dashboard dict with current snapshot, time_series, provider_perf,
        health_breakdown, cost_summary.
    """
    try:
        import httpx

        async with httpx.AsyncClient(timeout=30.0) as client:
            resp = await client.get(
                f"{go_client.base_url}/api/v1/swarm/observability/dashboard",
                headers=go_client._headers(),
                params={"hours": hours},
            )
            if resp.status_code == 200:
                return resp.json()
            logger.warning(
                "observability: dashboard fetch failed status=%d",
                resp.status_code,
            )
    except Exception:
        logger.debug("observability: failed to fetch dashboard", exc_info=True)

    return {}


async def get_health_score(go_client: Any) -> HealthBreakdown:
    """Fetch the current system health score.

    Returns:
        HealthBreakdown with composite score and per-dimension percentages.
    """
    try:
        import httpx

        async with httpx.AsyncClient(timeout=10.0) as client:
            resp = await client.get(
                f"{go_client.base_url}/api/v1/swarm/observability/health",
                headers=go_client._headers(),
            )
            if resp.status_code == 200:
                return HealthBreakdown.from_dict(resp.json())
    except Exception:
        logger.debug("observability: failed to fetch health", exc_info=True)

    return HealthBreakdown()


async def get_cost_summary(go_client: Any) -> CostSummary:
    """Fetch current cost summary.

    Returns:
        CostSummary with today/week/month costs and per-provider breakdown.
    """
    try:
        import httpx

        async with httpx.AsyncClient(timeout=10.0) as client:
            resp = await client.get(
                f"{go_client.base_url}/api/v1/swarm/observability/cost",
                headers=go_client._headers(),
            )
            if resp.status_code == 200:
                return CostSummary.from_dict(resp.json())
    except Exception:
        logger.debug("observability: failed to fetch cost", exc_info=True)

    return CostSummary()


async def get_provider_perf(
    go_client: Any,
    provider: str = "",
    hours: int = 24,
) -> list[ProviderPerfPoint]:
    """Fetch per-provider performance data.

    Args:
        provider: Optional specific provider name. Empty = all providers.
        hours: Time range (default 24h).

    Returns:
        List of ProviderPerfPoint data points.
    """
    try:
        import httpx

        url = f"{go_client.base_url}/api/v1/swarm/observability/providers"
        if provider:
            url += f"/{provider}"

        async with httpx.AsyncClient(timeout=15.0) as client:
            resp = await client.get(
                url,
                headers=go_client._headers(),
                params={"hours": hours},
            )
            if resp.status_code == 200:
                data = resp.json()
                items = data.get("providers") or data.get("data_points") or []
                return [ProviderPerfPoint.from_dict(p) for p in items]
    except Exception:
        logger.debug("observability: failed to fetch provider perf", exc_info=True)

    return []
