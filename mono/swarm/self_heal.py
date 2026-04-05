"""Self-Healing Pipeline — Python client.

After each LLM provider call, agents report the outcome to the Go server so the
self-healing service can track circuit-breaker states.  Before routing to a
provider, agents can check whether the provider is available.

This module wraps the two Go endpoints:
    POST /internal/swarm/self-heal/provider-outcome  → report success/failure
    GET  /internal/swarm/self-heal/provider-status    → check provider availability

Usage in agent.py::

    from mono.swarm.self_heal import report_provider_outcome, is_provider_available

    if not await is_provider_available(go_client, "openai"):
        # skip this provider, circuit breaker is open
        ...

    # After an LLM call:
    await report_provider_outcome(go_client, "openai", success=True, latency_ms=1234)
"""

from __future__ import annotations

import logging
from dataclasses import dataclass, field
from typing import Any

logger = logging.getLogger(__name__)


# ── Provider Health Status ───────────────────────────────────────────────────

@dataclass
class ProviderHealthStatus:
    """Provider circuit-breaker health returned by the Go service."""

    provider: str = ""
    available: bool = True
    state: str = "closed"  # closed | half_open | open
    consecutive_failures: int = 0
    open_until: str | None = None

    @classmethod
    def from_dict(cls, d: dict[str, Any]) -> "ProviderHealthStatus":
        return cls(
            provider=d.get("provider", ""),
            available=d.get("available", True),
            state=d.get("state", "closed"),
            consecutive_failures=d.get("consecutive_failures", 0),
            open_until=d.get("open_until"),
        )

    @classmethod
    def default(cls, provider: str) -> "ProviderHealthStatus":
        """Fallback when Go is unreachable — assume healthy."""
        return cls(provider=provider, available=True, state="closed")


# ── Report Provider Outcome ──────────────────────────────────────────────────

async def report_provider_outcome(
    go_client: Any,
    provider: str,
    success: bool,
    latency_ms: float = 0.0,
    error_msg: str = "",
    task_id: str = "",
    agent_id: str = "",
) -> None:
    """Report a provider call outcome to the Go self-heal service.

    Fire-and-forget — errors are logged but never raised.

    Args:
        go_client: :class:`~mono.swarm.go_client.GoClient` instance.
        provider: LLM provider name (e.g. "openai", "anthropic").
        success: Whether the call succeeded.
        latency_ms: Call latency in milliseconds.
        error_msg: Error message on failure.
        task_id: Optional task context.
        agent_id: Optional agent context.
    """
    try:
        import httpx

        payload = {
            "provider": provider,
            "success": success,
            "latency_ms": latency_ms,
            "error_msg": error_msg,
            "task_id": task_id,
            "agent_id": agent_id,
        }
        async with httpx.AsyncClient(timeout=10.0) as client:
            resp = await client.post(
                f"{go_client.base_url}/internal/swarm/self-heal/provider-outcome",
                headers=go_client._headers(),
                json=payload,
            )
            if resp.status_code >= 400:
                logger.warning(
                    "self-heal: provider-outcome report failed status=%d body=%s",
                    resp.status_code,
                    resp.text[:200],
                )
    except Exception:
        logger.debug("self-heal: failed to report provider outcome", exc_info=True)


# ── Check Provider Availability ──────────────────────────────────────────────

async def is_provider_available(
    go_client: Any,
    provider: str,
) -> ProviderHealthStatus:
    """Check whether a provider is available (circuit breaker is closed/half-open).

    Returns:
        ProviderHealthStatus with availability info.
        On failure, returns default (available=True) to avoid blocking traffic.
    """
    try:
        import httpx

        async with httpx.AsyncClient(timeout=5.0) as client:
            resp = await client.get(
                f"{go_client.base_url}/internal/swarm/self-heal/provider-status",
                headers=go_client._headers(),
                params={"provider": provider},
            )
            if resp.status_code == 200:
                return ProviderHealthStatus.from_dict(resp.json())
            logger.warning(
                "self-heal: provider-status check failed status=%d",
                resp.status_code,
            )
    except Exception:
        logger.debug("self-heal: failed to check provider status", exc_info=True)

    return ProviderHealthStatus.default(provider)
