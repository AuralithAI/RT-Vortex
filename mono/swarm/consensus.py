"""Consensus Engine — synthesise multi-LLM probe results into a single answer.

When an agent probes multiple LLMs and organises the responses
into a :class:`~conversation.DiscussionThread`, the Consensus Engine
decides which response to use or how to merge them.

Strategies:
    **PICK_BEST** — Select the first successful response (highest-priority
    provider in the role's priority matrix).  Instant, zero-cost.

    **MAJORITY_VOTE** — Compare all successful responses using token-overlap
    similarity and pick the one most agreed upon by the majority.  No extra
    LLM call, near-instant.

    **GPT_AS_JUDGE** — Send every provider's response to GPT (always the last
    model in the priority matrix) with a judge prompt asking it to evaluate
    accuracy, completeness, and code quality, then select or synthesise the
    best answer.  Costs one extra LLM call but produces the most reliable
    result.

    **AUTO** — Automatically choose the strategy based on the responses:
    - If only 1 successful response → PICK_BEST
    - If responses agree (high overlap) → PICK_BEST (consensus already exists)
    - If responses disagree → GPT_AS_JUDGE

The engine integrates with :class:`~conversation.SharedConversation` so the
chosen strategy, winner, and reasoning are recorded in the discussion thread
and broadcast to the UI.
"""

from __future__ import annotations

import logging
import re
from collections import Counter
from dataclasses import dataclass, field
from enum import Enum
from typing import TYPE_CHECKING, Any

if TYPE_CHECKING:
    from .conversation import DiscussionThread, ProviderResponse
    from .sdk.go_llm_client import ProbeResponse

logger = logging.getLogger(__name__)


# ── Strategy Enum ────────────────────────────────────────────────────────────


class ConsensusStrategy(str, Enum):
    """Available consensus strategies."""

    PICK_BEST = "pick_best"
    MAJORITY_VOTE = "majority_vote"
    GPT_AS_JUDGE = "gpt_as_judge"
    AUTO = "auto"


# ── Result Dataclass ─────────────────────────────────────────────────────────


@dataclass
class ConsensusResult:
    """The outcome of running the consensus engine on a discussion thread.

    Attributes:
        content: The final synthesised / selected answer text.
        strategy: Which strategy was used to arrive at this result.
        provider: The provider whose answer was selected (or "consensus"
            if the answer was synthesised from multiple).
        model: The model that produced the selected answer.
        confidence: A 0.0–1.0 score representing how confident the engine
            is in this result (1.0 = perfect agreement or explicit judge
            selection).
        reasoning: Human-readable explanation of why this answer was chosen.
        all_scores: Per-provider scores from the strategy (e.g. overlap
            scores for majority vote, GPT judge scores).
        judge_raw: Raw response from GPT when GPT_AS_JUDGE strategy is used.
    """

    content: str = ""
    strategy: ConsensusStrategy = ConsensusStrategy.PICK_BEST
    provider: str = ""
    model: str = ""
    confidence: float = 0.0
    reasoning: str = ""
    all_scores: dict[str, float] = field(default_factory=dict)
    judge_raw: str = ""

    @property
    def succeeded(self) -> bool:
        """True if a non-empty answer was produced."""
        return bool(self.content)

    def to_dict(self) -> dict[str, Any]:
        return {
            "content": self.content,
            "strategy": self.strategy.value,
            "provider": self.provider,
            "model": self.model,
            "confidence": round(self.confidence, 4),
            "reasoning": self.reasoning,
            "all_scores": self.all_scores,
        }


# ── Token Overlap Utilities ──────────────────────────────────────────────────

_WORD_RE = re.compile(r"\b\w+\b")


def _tokenise(text: str) -> list[str]:
    """Split text into lowercase word tokens for overlap comparison."""
    return _WORD_RE.findall(text.lower())


def _jaccard(a: list[str], b: list[str]) -> float:
    """Compute Jaccard similarity between two token lists."""
    sa, sb = set(a), set(b)
    if not sa and not sb:
        return 1.0
    if not sa or not sb:
        return 0.0
    return len(sa & sb) / len(sa | sb)


def pairwise_agreement(contents: list[str]) -> float:
    """Compute average pairwise Jaccard similarity across all contents.

    Returns 0.0 if fewer than 2 contents are provided, or 1.0 if all
    contents are identical.
    """
    if len(contents) < 2:
        return 0.0
    tokenised = [_tokenise(c) for c in contents]
    total = 0.0
    pairs = 0
    for i in range(len(tokenised)):
        for j in range(i + 1, len(tokenised)):
            total += _jaccard(tokenised[i], tokenised[j])
            pairs += 1
    return total / pairs if pairs else 0.0


# ── GPT-as-Judge Prompt ─────────────────────────────────────────────────────

_JUDGE_SYSTEM_PROMPT = """\
You are an expert code review judge. You are given multiple LLM responses to \
the same coding question or task. Your job is to evaluate each response and \
either select the best one or synthesise an improved answer.

Evaluate on these criteria:
1. **Correctness** — Is the code/answer factually correct?
2. **Completeness** — Does it fully address the question?
3. **Code Quality** — Is the code clean, idiomatic, and well-structured?
4. **Reasoning** — Is the explanation clear and logical?
5. **Security** — Are there any security issues or bad practices?

Respond with EXACTLY this JSON format (no markdown fencing):
{
  "winner": "<provider_name or 'synthesis'>",
  "confidence": <0.0 to 1.0>,
  "scores": {
    "<provider_name>": <0.0 to 1.0>,
    ...
  },
  "reasoning": "<1-2 sentence explanation of your choice>",
  "synthesised_answer": "<your improved answer if winner is 'synthesis', else empty string>"
}
"""


def _build_judge_user_prompt(
    topic: str,
    responses: list["ProviderResponse"],
) -> str:
    """Build the user prompt that presents all provider responses to the judge."""
    lines = [f"## Task / Question\n{topic}\n\n## Provider Responses\n"]
    for i, r in enumerate(responses, 1):
        label = f"{r.provider}/{r.model}" if r.model else r.provider
        lines.append(f"### Response {i} — {label} ({r.latency_ms}ms)")
        lines.append(r.content)
        lines.append("")  # blank line separator
    lines.append(
        "## Instructions\n"
        "Evaluate each response and respond with the JSON format specified in your system prompt."
    )
    return "\n".join(lines)


# ── Consensus Engine ─────────────────────────────────────────────────────────


class ConsensusEngine:
    """Determines the final answer from a multi-LLM discussion thread.

    Usage::

        engine = ConsensusEngine(
            llm_complete=llm_complete,
            go_base_url="http://localhost:8080",
            agent_token="...",
        )
        result = await engine.run(thread, strategy=ConsensusStrategy.AUTO)
        print(result.content, result.provider, result.confidence)

    The engine does NOT require a live LLM connection for PICK_BEST or
    MAJORITY_VOTE — only GPT_AS_JUDGE makes an LLM call (via the same
    Go proxy used by agents).
    """

    def __init__(
        self,
        llm_complete=None,
        go_base_url: str = "",
        agent_token: str = "",
        *,
        agreement_threshold: float = 0.6,
    ):
        """Create a consensus engine.

        Args:
            llm_complete: Async callable for LLM completions (typically
                :func:`~sdk.go_llm_client.llm_complete`).  Required only
                for GPT_AS_JUDGE strategy.
            go_base_url: Go server base URL for LLM calls.
            agent_token: JWT token for authenticated LLM calls.
            agreement_threshold: Pairwise agreement above this level means
                responses agree and PICK_BEST is sufficient (AUTO mode).
        """
        self._llm_complete = llm_complete
        self._go_base_url = go_base_url
        self._agent_token = agent_token
        self._agreement_threshold = agreement_threshold

    async def run(
        self,
        thread: "DiscussionThread",
        strategy: ConsensusStrategy = ConsensusStrategy.AUTO,
    ) -> ConsensusResult:
        """Run the consensus engine on a completed discussion thread.

        Args:
            thread: A :class:`DiscussionThread` with at least one
                successful response.
            strategy: Which strategy to use. ``AUTO`` chooses automatically.

        Returns:
            A :class:`ConsensusResult` with the final answer.
        """
        successful = thread.successful_responses
        if not successful:
            logger.warning("Consensus: no successful responses in thread %s", thread.thread_id)
            return ConsensusResult(
                strategy=strategy if strategy != ConsensusStrategy.AUTO else ConsensusStrategy.PICK_BEST,
                reasoning="No successful provider responses to evaluate.",
            )

        if strategy == ConsensusStrategy.AUTO:
            strategy = self._auto_strategy(successful)
            logger.info(
                "Consensus auto-selected %s for thread %s (%d responses)",
                strategy.value, thread.thread_id, len(successful),
            )

        if strategy == ConsensusStrategy.PICK_BEST:
            return self._pick_best(successful)
        elif strategy == ConsensusStrategy.MAJORITY_VOTE:
            return self._majority_vote(successful)
        elif strategy == ConsensusStrategy.GPT_AS_JUDGE:
            return await self._gpt_as_judge(thread.topic, successful)
        else:
            # Fallback — should never reach here.
            return self._pick_best(successful)

    def _auto_strategy(self, responses: list["ProviderResponse"]) -> ConsensusStrategy:
        """Choose a strategy based on the shape and agreement of responses.

        Rules:
        1. Single response → PICK_BEST (nothing to compare).
        2. High pairwise agreement → PICK_BEST (they already agree).
        3. Multiple disagreeing responses + LLM available → GPT_AS_JUDGE.
        4. Multiple disagreeing responses, no LLM → MAJORITY_VOTE.
        """
        if len(responses) <= 1:
            return ConsensusStrategy.PICK_BEST

        contents = [r.content for r in responses if r.content]
        if len(contents) < 2:
            return ConsensusStrategy.PICK_BEST

        agreement = pairwise_agreement(contents)
        logger.debug("Consensus: pairwise agreement = %.3f", agreement)

        if agreement >= self._agreement_threshold:
            return ConsensusStrategy.PICK_BEST

        if self._llm_complete is not None:
            return ConsensusStrategy.GPT_AS_JUDGE

        return ConsensusStrategy.MAJORITY_VOTE

    def _pick_best(self, responses: list["ProviderResponse"]) -> ConsensusResult:
        """Select the first successful response (highest priority).

        The responses are in priority-matrix order from the Go probe, so
        index 0 is the highest-priority provider.
        """
        best = responses[0]
        return ConsensusResult(
            content=best.content,
            strategy=ConsensusStrategy.PICK_BEST,
            provider=best.provider,
            model=best.model,
            confidence=1.0 if len(responses) == 1 else 0.7,
            reasoning=(
                f"Selected highest-priority provider {best.provider}"
                + (f"/{best.model}" if best.model else "")
                + f" ({best.latency_ms}ms)."
            ),
            all_scores={r.provider: (1.0 if r is best else 0.0) for r in responses},
        )

    def _majority_vote(self, responses: list["ProviderResponse"]) -> ConsensusResult:
        """Select the response most agreed upon by token overlap.

        For each response, compute the average Jaccard similarity to all other
        responses.  The one with the highest average is the "majority" answer.
        """
        contents = [r.content for r in responses]
        tokenised = [_tokenise(c) for c in contents]

        # Compute average similarity for each response.
        scores: list[float] = []
        for i in range(len(tokenised)):
            sim_sum = 0.0
            count = 0
            for j in range(len(tokenised)):
                if i != j:
                    sim_sum += _jaccard(tokenised[i], tokenised[j])
                    count += 1
            avg = sim_sum / count if count else 0.0
            scores.append(avg)

        best_idx = max(range(len(scores)), key=lambda i: scores[i])
        best = responses[best_idx]
        agreement = pairwise_agreement(contents)

        score_map = {responses[i].provider: round(scores[i], 4) for i in range(len(responses))}

        return ConsensusResult(
            content=best.content,
            strategy=ConsensusStrategy.MAJORITY_VOTE,
            provider=best.provider,
            model=best.model,
            confidence=min(1.0, agreement + 0.2),  # slight boost for majority selection
            reasoning=(
                f"Majority vote selected {best.provider}"
                + (f"/{best.model}" if best.model else "")
                + f" (avg overlap={scores[best_idx]:.3f}, agreement={agreement:.3f})."
            ),
            all_scores=score_map,
        )

    async def _gpt_as_judge(
        self,
        topic: str,
        responses: list["ProviderResponse"],
    ) -> ConsensusResult:
        """Send all responses to GPT for expert arbitration.

        GPT evaluates each response on correctness, completeness, code
        quality, reasoning, and security, then selects the best or
        synthesises an improved answer.
        """
        if self._llm_complete is None:
            logger.warning("Consensus: GPT_AS_JUDGE requested but no llm_complete — falling back to majority vote")
            return self._majority_vote(responses)

        user_prompt = _build_judge_user_prompt(topic, responses)

        logger.info(
            "Consensus: calling GPT-as-judge for %d provider responses",
            len(responses),
        )

        try:
            raw_resp = await self._llm_complete(
                messages=[
                    {"role": "system", "content": _JUDGE_SYSTEM_PROMPT},
                    {"role": "user", "content": user_prompt},
                ],
                go_base_url=self._go_base_url,
                agent_token=self._agent_token,
                agent_role="consensus_judge",
            )
        except Exception as e:
            logger.error("Consensus: GPT-as-judge LLM call failed: %s", e)
            # Graceful degradation — fall back to majority vote.
            result = self._majority_vote(responses)
            result.reasoning = f"GPT-as-judge failed ({e}); fell back to majority vote. " + result.reasoning
            return result

        # Parse the judge response.
        return self._parse_judge_response(raw_resp, responses)

    def _parse_judge_response(
        self,
        raw_resp: dict,
        responses: list["ProviderResponse"],
    ) -> ConsensusResult:
        """Parse the structured JSON from GPT's judge response.

        Handles malformed JSON gracefully by falling back to majority vote.
        """
        import json

        # Extract the assistant's content.
        content = ""
        choices = raw_resp.get("choices", [])
        if choices:
            content = choices[0].get("message", {}).get("content", "")

        if not content:
            logger.warning("Consensus: GPT judge returned empty content")
            result = self._majority_vote(responses)
            result.reasoning = "GPT-as-judge returned empty response; fell back to majority vote."
            result.judge_raw = str(raw_resp)
            return result

        # Try to parse JSON from the response.
        try:
            # Strip markdown code fences if present.
            cleaned = content.strip()
            if cleaned.startswith("```"):
                # Remove first and last lines (fences).
                lines = cleaned.split("\n")
                cleaned = "\n".join(lines[1:-1] if lines[-1].strip() == "```" else lines[1:])
            judge_data = json.loads(cleaned)
        except json.JSONDecodeError:
            logger.warning("Consensus: GPT judge returned non-JSON: %s", content[:200])
            result = self._majority_vote(responses)
            result.reasoning = "GPT-as-judge returned non-JSON; fell back to majority vote."
            result.judge_raw = content
            return result

        winner_name = judge_data.get("winner", "")
        confidence = float(judge_data.get("confidence", 0.5))
        scores = judge_data.get("scores", {})
        reasoning = judge_data.get("reasoning", "")
        synthesised = judge_data.get("synthesised_answer", "")

        # Build score map.
        all_scores = {}
        for r in responses:
            all_scores[r.provider] = float(scores.get(r.provider, 0.0))

        # Determine final content.
        if winner_name == "synthesis" and synthesised:
            return ConsensusResult(
                content=synthesised,
                strategy=ConsensusStrategy.GPT_AS_JUDGE,
                provider="consensus",
                model="gpt-judge",
                confidence=confidence,
                reasoning=reasoning,
                all_scores=all_scores,
                judge_raw=content,
            )

        # Find the winning provider.
        winner_resp = None
        for r in responses:
            if r.provider == winner_name:
                winner_resp = r
                break

        if winner_resp is None:
            # Winner name doesn't match any provider — use highest-scored.
            if scores:
                best_provider = max(scores, key=lambda k: float(scores[k]))
                for r in responses:
                    if r.provider == best_provider:
                        winner_resp = r
                        break

        if winner_resp is None:
            # Ultimate fallback — first response.
            winner_resp = responses[0]

        return ConsensusResult(
            content=winner_resp.content,
            strategy=ConsensusStrategy.GPT_AS_JUDGE,
            provider=winner_resp.provider,
            model=winner_resp.model,
            confidence=confidence,
            reasoning=reasoning,
            all_scores=all_scores,
            judge_raw=content,
        )
