"""Adaptive Probe Tuning — Python client for Phase 10.

Before each multi-LLM probe, agents fetch an adaptive probe configuration
from the Go server.  After the probe completes, the outcome is recorded so
the Go tuning engine can learn and adjust future configs.

This module wraps the two Go endpoints:
    GET  /internal/swarm/probe-config   → fetch best params for (role, repo, action)
    POST /internal/swarm/probe-history  → record probe outcome

Usage in agent.py::

    from mono.swarm.probe_tuning import fetch_probe_config, record_probe_outcome

    cfg = await fetch_probe_config(go_client, role, repo_id, action_type, ...)
    # ...run probe with cfg.num_models, cfg.temperature, cfg.preferred_providers...
    await record_probe_outcome(go_client, task_id, role, repo_id, ...)
"""

from __future__ import annotations

import logging
import time
from dataclasses import dataclass, field
from typing import Any

logger = logging.getLogger(__name__)


# ── Probe Config (fetched from Go) ──────────────────────────────────────────

@dataclass
class AdaptiveProbeConfig:
    """Adaptive probe configuration returned by the Go tuning service."""

    num_models: int = 3
    preferred_providers: list[str] = field(default_factory=list)
    excluded_providers: list[str] = field(default_factory=list)
    temperature: float = 0.7
    max_tokens: int = 4096
    timeout_seconds: int = 120
    budget_cap_usd: float = 0.0
    tokens_spent: int = 0
    strategy: str = "adaptive"
    confidence_threshold: float = 0.7
    retries: int = 1
    reasoning: str = ""

    @classmethod
    def from_dict(cls, d: dict[str, Any]) -> "AdaptiveProbeConfig":
        return cls(
            num_models=d.get("num_models", 3),
            preferred_providers=d.get("preferred_providers") or [],
            excluded_providers=d.get("excluded_providers") or [],
            temperature=d.get("temperature", 0.7),
            max_tokens=d.get("max_tokens", 4096),
            timeout_seconds=d.get("timeout_seconds", 120),
            budget_cap_usd=d.get("budget_cap_usd", 0.0),
            tokens_spent=d.get("tokens_spent", 0),
            strategy=d.get("strategy", "adaptive"),
            confidence_threshold=d.get("confidence_threshold", 0.7),
            retries=d.get("retries", 1),
            reasoning=d.get("reasoning", ""),
        )

    @classmethod
    def default_for_role(cls, role: str) -> "AdaptiveProbeConfig":
        """Local fallback defaults when Go is unreachable."""
        defaults: dict[str, dict[str, Any]] = {
            "orchestrator": {"num_models": 3, "temperature": 0.5, "timeout_seconds": 180, "max_tokens": 8192},
            "architect": {"num_models": 3, "temperature": 0.6, "max_tokens": 8192},
            "senior_dev": {"num_models": 2, "temperature": 0.4, "timeout_seconds": 150, "max_tokens": 8192},
            "qa": {"num_models": 3, "temperature": 0.3, "confidence_threshold": 0.8, "max_tokens": 6144},
            "security": {"num_models": 3, "temperature": 0.3, "confidence_threshold": 0.8, "max_tokens": 6144},
            "junior_dev": {"num_models": 2, "temperature": 0.5},
            "docs": {"num_models": 2, "temperature": 0.7, "timeout_seconds": 90},
        }
        role_defaults = defaults.get(role, {})
        return cls(**role_defaults)


# ── Probe Outcome (sent to Go) ──────────────────────────────────────────────

@dataclass
class ProbeOutcome:
    """Captured probe outcome to send to Go for the tuning history."""

    task_id: str = ""
    role: str = ""
    repo_id: str = ""
    action_type: str = ""
    providers_queried: list[str] = field(default_factory=list)
    providers_succeeded: list[str] = field(default_factory=list)
    provider_winner: str = ""
    strategy_used: str = ""
    consensus_confidence: float = 0.0
    provider_latencies: dict[str, int] = field(default_factory=dict)
    provider_tokens: dict[str, dict[str, int]] = field(default_factory=dict)
    total_ms: int = 0
    total_tokens: int = 0
    estimated_cost_usd: float = 0.0
    success: bool = True
    error_detail: str = ""
    complexity_label: str = ""
    num_models_used: int = 0
    temperature_used: float = 0.7

    def to_dict(self) -> dict[str, Any]:
        return {
            "task_id": self.task_id,
            "role": self.role,
            "repo_id": self.repo_id,
            "action_type": self.action_type,
            "providers_queried": self.providers_queried,
            "providers_succeeded": self.providers_succeeded,
            "provider_winner": self.provider_winner,
            "strategy_used": self.strategy_used,
            "consensus_confidence": self.consensus_confidence,
            "provider_latencies": self.provider_latencies,
            "provider_tokens": self.provider_tokens,
            "total_ms": self.total_ms,
            "total_tokens": self.total_tokens,
            "estimated_cost_usd": self.estimated_cost_usd,
            "success": self.success,
            "error_detail": self.error_detail,
            "complexity_label": self.complexity_label,
            "num_models_used": self.num_models_used,
            "temperature_used": self.temperature_used,
        }


# ── Cost Estimation ─────────────────────────────────────────────────────────

# Rough per-1K token cost by provider (updated by real history on Go side).
_COST_PER_1K: dict[str, dict[str, float]] = {
    "openai": {"prompt": 0.005, "completion": 0.015},
    "anthropic": {"prompt": 0.003, "completion": 0.015},
    "grok": {"prompt": 0.002, "completion": 0.010},
    "gemini": {"prompt": 0.001, "completion": 0.004},
    "ollama": {"prompt": 0.0, "completion": 0.0},
}


def estimate_cost(
    provider_tokens: dict[str, dict[str, int]],
) -> float:
    """Estimate USD cost from per-provider token usage."""
    total = 0.0
    for provider, usage in provider_tokens.items():
        rates = _COST_PER_1K.get(provider, {"prompt": 0.003, "completion": 0.015})
        prompt = usage.get("prompt", 0)
        completion = usage.get("completion", 0)
        total += (prompt / 1000.0) * rates["prompt"]
        total += (completion / 1000.0) * rates["completion"]
    return round(total, 6)


# ── Fetch Config (before probe) ─────────────────────────────────────────────

async def fetch_probe_config(
    go_client: Any,
    role: str,
    repo_id: str = "",
    action_type: str = "",
    complexity_label: str = "",
    tier: str = "",
) -> AdaptiveProbeConfig:
    """Fetch the adaptive probe configuration from Go.

    Falls back to local defaults on failure so probing is never blocked.

    Args:
        go_client: GoClient instance with auth token.
        role: Agent role (e.g. "senior_dev").
        repo_id: Repository ID for repo-specific tuning.
        action_type: Optional action filter (e.g. "reasoning", "code_gen").
        complexity_label: Task complexity (trivial/small/medium/large/critical).
        tier: Agent's current ELO tier (standard/expert/restricted).

    Returns:
        AdaptiveProbeConfig with tuned parameters.
    """
    try:
        data = await go_client.get_probe_config(
            role=role,
            repo_id=repo_id,
            action_type=action_type,
            complexity_label=complexity_label,
            tier=tier,
        )
        cfg = AdaptiveProbeConfig.from_dict(data)
        logger.info(
            "probe-tuning: config fetched role=%s models=%d temp=%.2f strategy=%s",
            role, cfg.num_models, cfg.temperature, cfg.strategy,
        )
        return cfg
    except Exception:
        logger.warning(
            "probe-tuning: Go fetch failed, using defaults for role=%s",
            role, exc_info=True,
        )
        return AdaptiveProbeConfig.default_for_role(role)


# ── Record Outcome (after probe) ────────────────────────────────────────────

async def record_probe_outcome(
    go_client: Any,
    outcome: ProbeOutcome,
) -> None:
    """Send probe outcome to Go for the adaptive tuning history.

    This is fire-and-forget — failure to record does not affect the agent.
    """
    try:
        await go_client.record_probe_history(outcome.to_dict())
        logger.debug(
            "probe-tuning: outcome recorded task=%s role=%s winner=%s",
            outcome.task_id, outcome.role, outcome.provider_winner,
        )
    except Exception:
        logger.warning(
            "probe-tuning: failed to record outcome task=%s",
            outcome.task_id, exc_info=True,
        )


# ── Build Outcome from Probe Response ───────────────────────────────────────

def build_probe_outcome(
    *,
    task_id: str,
    role: str,
    repo_id: str,
    action_type: str,
    probe_resp: Any,  # ProbeResponse from go_llm_client
    consensus_result: Any | None = None,  # ConsensusResult if available
    config: AdaptiveProbeConfig | None = None,
    complexity_label: str = "",
    start_time_ns: int = 0,
) -> ProbeOutcome:
    """Build a ProbeOutcome from a ProbeResponse and optional ConsensusResult.

    This is a convenience function that extracts all the fields from the
    existing probe/consensus data structures.
    """
    outcome = ProbeOutcome(
        task_id=task_id,
        role=role,
        repo_id=repo_id,
        action_type=action_type,
        complexity_label=complexity_label,
    )

    if probe_resp is not None:
        outcome.providers_queried = [r.provider for r in probe_resp.results]
        outcome.providers_succeeded = [r.provider for r in probe_resp.results if r.succeeded]
        outcome.total_ms = probe_resp.total_ms
        outcome.num_models_used = probe_resp.providers
        outcome.success = probe_resp.successes > 0

        # Per-provider details.
        for r in probe_resp.results:
            if r.provider:
                outcome.provider_latencies[r.provider] = r.latency_ms
                if r.usage:
                    outcome.provider_tokens[r.provider] = {
                        "prompt": r.usage.get("prompt_tokens", 0),
                        "completion": r.usage.get("completion_tokens", 0),
                    }

        # Total tokens.
        total_tok = 0
        for r in probe_resp.results:
            if r.usage:
                total_tok += r.usage.get("total_tokens", 0)
        outcome.total_tokens = total_tok

    if consensus_result is not None:
        outcome.provider_winner = getattr(consensus_result, "provider", "")
        outcome.strategy_used = getattr(consensus_result, "strategy", "")
        if hasattr(consensus_result, "strategy") and hasattr(consensus_result.strategy, "value"):
            outcome.strategy_used = consensus_result.strategy.value
        outcome.consensus_confidence = getattr(consensus_result, "confidence", 0.0)
        outcome.success = getattr(consensus_result, "succeeded", outcome.success)
    elif probe_resp is not None and probe_resp.best_result:
        outcome.provider_winner = probe_resp.best_result.provider
        outcome.strategy_used = "pick_best"
        outcome.consensus_confidence = 1.0

    if config is not None:
        outcome.temperature_used = config.temperature

    # Cost estimation.
    if outcome.provider_tokens:
        outcome.estimated_cost_usd = estimate_cost(outcome.provider_tokens)

    # Override total_ms if we have a start time.
    if start_time_ns > 0:
        outcome.total_ms = int((time.time_ns() - start_time_ns) / 1_000_000)

    return outcome
