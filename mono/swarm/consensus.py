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

    **MULTI_JUDGE_PANEL** — Send every provider's response to 3+ independent
    LLM judges (GPT, Claude, Gemini, Grok) in parallel via the probe endpoint.
    Each judge independently evaluates and scores all candidate responses.
    Their verdicts are then compared:
    - If ≥2/3 judges agree on the winner → high confidence, use that winner.
    - If judges disagree → use weighted scoring across all judge verdicts.
    Eliminates single-model bias that plagues GPT_AS_JUDGE.

    **AUTO** — Automatically choose the strategy based on the responses:
    - If only 1 successful response → PICK_BEST
    - If responses agree (high overlap) → PICK_BEST (consensus already exists)
    - If responses disagree + probe available → MULTI_JUDGE_PANEL
    - If responses disagree + only complete available → GPT_AS_JUDGE
    - If no LLM available → MAJORITY_VOTE

The engine integrates with :class:`~conversation.SharedConversation` so the
chosen strategy, winner, and reasoning are recorded in the discussion thread
and broadcast to the UI.
"""

from __future__ import annotations

import asyncio
import logging
import re
from collections import Counter
from dataclasses import dataclass, field
from enum import Enum
from typing import TYPE_CHECKING, Any, Callable

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
    MULTI_JUDGE_PANEL = "multi_judge_panel"
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
        judge_count: Number of judges used (MULTI_JUDGE_PANEL).
        judge_agreement: 0.0–1.0 inter-judge agreement (MULTI_JUDGE_PANEL).
        judge_verdicts: Per-judge verdict details (MULTI_JUDGE_PANEL).
    """

    content: str = ""
    strategy: ConsensusStrategy = ConsensusStrategy.PICK_BEST
    provider: str = ""
    model: str = ""
    confidence: float = 0.0
    reasoning: str = ""
    all_scores: dict[str, float] = field(default_factory=dict)
    judge_raw: str = ""
    judge_count: int = 0
    judge_agreement: float = 0.0
    judge_verdicts: list[dict[str, Any]] = field(default_factory=list)

    @property
    def succeeded(self) -> bool:
        """True if a non-empty answer was produced."""
        return bool(self.content)

    def to_dict(self) -> dict[str, Any]:
        d: dict[str, Any] = {
            "content": self.content,
            "strategy": self.strategy.value,
            "provider": self.provider,
            "model": self.model,
            "confidence": round(self.confidence, 4),
            "reasoning": self.reasoning,
            "all_scores": self.all_scores,
        }
        if self.judge_count > 0:
            d["judge_count"] = self.judge_count
            d["judge_agreement"] = round(self.judge_agreement, 4)
            d["judge_verdicts"] = self.judge_verdicts
        return d


# ── Judge Verdict (Multi-Judge Panel) ────────────────────────────────────────


@dataclass
class JudgeVerdict:
    """A single judge's verdict from the Multi-Judge Panel.

    Each judge (a different LLM provider) independently evaluates all
    candidate responses and picks a winner with scores.

    Attributes:
        judge_provider: Which LLM provider served as judge (e.g. "anthropic").
        judge_model: Which model the judge used (e.g. "claude-3").
        winner: Name of the provider the judge selected, or "synthesis".
        confidence: The judge's self-reported confidence 0.0–1.0.
        scores: Per-candidate-provider scores from this judge.
        reasoning: The judge's explanation.
        synthesised_answer: If the judge chose synthesis, the merged answer.
        error: Non-empty if this judge failed (timeout, parse error, etc).
    """

    judge_provider: str = ""
    judge_model: str = ""
    winner: str = ""
    confidence: float = 0.0
    scores: dict[str, float] = field(default_factory=dict)
    reasoning: str = ""
    synthesised_answer: str = ""
    error: str = ""

    @property
    def succeeded(self) -> bool:
        return not self.error and bool(self.winner)

    def to_dict(self) -> dict[str, Any]:
        return {
            "judge_provider": self.judge_provider,
            "judge_model": self.judge_model,
            "winner": self.winner,
            "confidence": round(self.confidence, 4),
            "scores": self.scores,
            "reasoning": self.reasoning,
            "error": self.error,
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

Evaluate on these criteria (in order of importance):
1. **Specificity & Precision** — Does the response identify the EXACT files, \
functions, and line-level locations that need to change? Vague references \
like "modify the configuration" score low; exact paths like \
"tensorrt_llm/compile/graph_utils.py:build_model()" score high.
2. **Correctness** — Is the code/answer factually correct? Does it fix the \
actual root cause, not just a symptom?
3. **Completeness** — Does it fully address the question with ALL necessary \
changes? A response that identifies the right file but only makes a partial \
fix scores lower than one that covers every required edit.
4. **Actionability** — Does it provide concrete, implementable code changes \
(exact old/new text, or full function bodies)? Responses that merely narrate \
what SHOULD be done ("I will modify..." / "Let me implement...") without \
showing the actual code score MUCH lower than responses with real code.
5. **Code Quality** — Is the code clean, idiomatic, and well-structured?
6. **Reasoning** — Is the explanation clear and logical?
7. **Security** — Are there any security issues or bad practices?

CRITICAL anti-bias rules:
- Do NOT reward brevity or confidence over substance. A short, assertive \
response that says "I fixed it" with 3 lines of code is WORSE than a longer \
response that provides a complete, correct solution with proper context.
- Do NOT reward responses that claim to have made changes without showing \
the actual code. Narrating tool calls (e.g. "workspace_edit_file(...)") is \
NOT the same as providing concrete code.
- An INCOMPLETE response that has correct reasoning but stopped mid-stream \
should be scored lower than a COMPLETE response, even if the incomplete one \
started well.
- Judge by the SUBSTANCE of the code changes proposed, not the polish of \
the prose around them.

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
        "Evaluate each response and respond with the JSON format specified "
        "in your system prompt.\n\n"
        "IMPORTANT scoring reminders:\n"
        "- A response that identifies the EXACT correct file and provides "
        "concrete code changes is better than one that gives vague guidance.\n"
        "- A response that is INCOMPLETE (cut off mid-sentence, or says "
        "'Let me implement...' without showing the code) should be penalised "
        "heavily on Completeness and Actionability.\n"
        "- Do NOT confuse narrative confidence ('I have successfully fixed...') "
        "with actual substance. Score based on what code was actually shown."
    )
    return "\n".join(lines)


# ── Consensus Engine ─────────────────────────────────────────────────────────


class ConsensusEngine:
    """Determines the final answer from a multi-LLM discussion thread.

    Usage::

        engine = ConsensusEngine(
            llm_complete=llm_complete,
            llm_probe=llm_probe,
            go_base_url="http://localhost:8080",
            agent_token="...",
        )
        result = await engine.run(thread, strategy=ConsensusStrategy.AUTO)
        print(result.content, result.provider, result.confidence)

    The engine does NOT require a live LLM connection for PICK_BEST or
    MAJORITY_VOTE — only GPT_AS_JUDGE makes a single LLM call, and
    MULTI_JUDGE_PANEL fans out to multiple LLM judges via the probe endpoint.
    """

    def __init__(
        self,
        llm_complete=None,
        llm_probe=None,
        go_base_url: str = "",
        agent_token: str = "",
        *,
        agreement_threshold: float = 0.6,
        min_judges: int = 2,
    ):
        """Create a consensus engine.

        Args:
            llm_complete: Async callable for single LLM completions (typically
                :func:`~sdk.go_llm_client.llm_complete`).  Required for
                GPT_AS_JUDGE strategy.
            llm_probe: Async callable for multi-LLM probe (typically
                :func:`~sdk.go_llm_client.llm_probe`).  Required for
                MULTI_JUDGE_PANEL strategy.  Sends the judge prompt to all
                providers in parallel and returns per-provider verdicts.
            go_base_url: Go server base URL for LLM calls.
            agent_token: JWT token for authenticated LLM calls.
            agreement_threshold: Pairwise agreement above this level means
                responses agree and PICK_BEST is sufficient (AUTO mode).
            min_judges: Minimum judges required for MULTI_JUDGE_PANEL.
                If probe returns fewer successful results, falls back to
                GPT_AS_JUDGE or majority vote.
        """
        self._llm_complete = llm_complete
        self._llm_probe = llm_probe
        self._go_base_url = go_base_url
        self._agent_token = agent_token
        self._agreement_threshold = agreement_threshold
        self._min_judges = max(2, min_judges)

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
        elif strategy == ConsensusStrategy.MULTI_JUDGE_PANEL:
            return await self._multi_judge_panel(thread.topic, successful)
        else:
            # Fallback — should never reach here.
            return self._pick_best(successful)

    def _auto_strategy(self, responses: list["ProviderResponse"]) -> ConsensusStrategy:
        """Choose a strategy based on the shape and agreement of responses.

        Rules:
        1. Single response → PICK_BEST (nothing to compare).
        2. High pairwise agreement → PICK_BEST (they already agree).
        3. Multiple disagreeing responses + probe available → MULTI_JUDGE_PANEL.
        4. Multiple disagreeing responses + only complete → GPT_AS_JUDGE.
        5. Multiple disagreeing responses, no LLM → MAJORITY_VOTE.
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

        # Prefer multi-judge panel (eliminates single-model bias).
        if self._llm_probe is not None:
            return ConsensusStrategy.MULTI_JUDGE_PANEL

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

    # ── Multi-Judge Panel ────────────────────────────────────────────────

    async def _multi_judge_panel(
        self,
        topic: str,
        responses: list["ProviderResponse"],
    ) -> ConsensusResult:
        """Send all responses to multiple independent LLM judges in parallel.

        Uses ``llm_probe`` to fan out the judge prompt to GPT, Claude,
        Gemini, Grok, etc.  Each judge independently evaluates every
        candidate response and returns a structured verdict.

        The verdicts are then aggregated:
        - **Winner voting**: Each judge votes for a winner.  If ≥2/3 agree,
          we use that winner with boosted confidence.
        - **Score averaging**: Per-provider scores are averaged across all
          judges, giving a bias-resistant composite score.
        - **Synthesis check**: If any judge synthesises and the judges
          disagree on a winner, we use the synthesis with the highest
          confidence.

        Falls back to ``_gpt_as_judge`` if probe is unavailable or returns
        fewer than ``min_judges`` successful verdicts, and to
        ``_majority_vote`` if all LLM calls fail.
        """
        if self._llm_probe is None:
            logger.warning("Consensus: MULTI_JUDGE_PANEL requested but no llm_probe — falling back")
            if self._llm_complete is not None:
                return await self._gpt_as_judge(topic, responses)
            return self._majority_vote(responses)

        user_prompt = _build_judge_user_prompt(topic, responses)

        logger.info(
            "Consensus: calling multi-judge panel for %d provider responses",
            len(responses),
        )

        try:
            probe_resp = await self._llm_probe(
                messages=[
                    {"role": "system", "content": _JUDGE_SYSTEM_PROMPT},
                    {"role": "user", "content": user_prompt},
                ],
                go_base_url=self._go_base_url,
                agent_token=self._agent_token,
                agent_role="consensus_judge",
            )
        except Exception as e:
            logger.error("Consensus: multi-judge probe call failed: %s", e)
            if self._llm_complete is not None:
                result = await self._gpt_as_judge(topic, responses)
                result.reasoning = (
                    f"Multi-judge probe failed ({e}); fell back to single GPT judge. "
                    + result.reasoning
                )
                return result
            result = self._majority_vote(responses)
            result.reasoning = (
                f"Multi-judge probe failed ({e}); fell back to majority vote. "
                + result.reasoning
            )
            return result

        # Parse each judge's verdict.
        verdicts = self._parse_probe_verdicts(probe_resp, responses)
        successful_verdicts = [v for v in verdicts if v.succeeded]

        logger.info(
            "Consensus: multi-judge panel returned %d/%d successful verdicts",
            len(successful_verdicts), len(verdicts),
        )

        if len(successful_verdicts) < self._min_judges:
            logger.warning(
                "Consensus: only %d judges succeeded (min=%d) — falling back",
                len(successful_verdicts), self._min_judges,
            )
            if self._llm_complete is not None:
                result = await self._gpt_as_judge(topic, responses)
                result.reasoning = (
                    f"Multi-judge panel had only {len(successful_verdicts)} "
                    f"successful verdicts (min={self._min_judges}); "
                    f"fell back to single GPT judge. " + result.reasoning
                )
                return result
            result = self._majority_vote(responses)
            result.reasoning = (
                f"Multi-judge panel had only {len(successful_verdicts)} "
                f"successful verdicts; fell back to majority vote. "
                + result.reasoning
            )
            return result

        # Aggregate verdicts.
        return self._aggregate_judge_verdicts(successful_verdicts, responses)

    def _parse_probe_verdicts(
        self,
        probe_resp: Any,
        responses: list["ProviderResponse"],
    ) -> list[JudgeVerdict]:
        """Parse each judge's response from the probe into JudgeVerdict objects."""
        import json

        verdicts: list[JudgeVerdict] = []
        results = getattr(probe_resp, "results", [])

        for result in results:
            provider = getattr(result, "provider", "unknown")
            model = getattr(result, "model", "")
            content = getattr(result, "content", "")
            error = getattr(result, "error", "")

            if error:
                verdicts.append(JudgeVerdict(
                    judge_provider=provider,
                    judge_model=model,
                    error=error,
                ))
                continue

            if not content:
                verdicts.append(JudgeVerdict(
                    judge_provider=provider,
                    judge_model=model,
                    error="empty response",
                ))
                continue

            # Parse JSON from the judge response.
            try:
                cleaned = content.strip()
                if cleaned.startswith("```"):
                    lines = cleaned.split("\n")
                    cleaned = "\n".join(
                        lines[1:-1] if lines[-1].strip() == "```" else lines[1:]
                    )
                judge_data = json.loads(cleaned)
            except json.JSONDecodeError:
                verdicts.append(JudgeVerdict(
                    judge_provider=provider,
                    judge_model=model,
                    error=f"non-JSON response: {content[:100]}",
                ))
                continue

            scores = {}
            raw_scores = judge_data.get("scores", {})
            for r in responses:
                scores[r.provider] = float(raw_scores.get(r.provider, 0.0))

            verdicts.append(JudgeVerdict(
                judge_provider=provider,
                judge_model=model,
                winner=judge_data.get("winner", ""),
                confidence=float(judge_data.get("confidence", 0.5)),
                scores=scores,
                reasoning=judge_data.get("reasoning", ""),
                synthesised_answer=judge_data.get("synthesised_answer", ""),
            ))

        return verdicts

    def _aggregate_judge_verdicts(
        self,
        verdicts: list[JudgeVerdict],
        responses: list["ProviderResponse"],
    ) -> ConsensusResult:
        """Aggregate multiple judge verdicts into a single consensus result.

        Aggregation rules:
        1. **Winner voting** — count which provider each judge selected.
           If ≥ (2/3 of judges) agree on the same winner, that's the pick
           with boosted confidence.
        2. **Score averaging** — average each provider's score across all
           judges for a bias-resistant composite ranking.
        3. **Synthesis handling** — if any judge synthesised and judges
           disagree on winner, use the highest-confidence synthesis.
        4. **Tiebreak** — if no clear majority, pick the provider with the
           highest average composite score.
        """
        num_judges = len(verdicts)
        quorum = max(2, (num_judges * 2 + 2) // 3)  # ceiling of 2/3

        # 1. Winner voting.
        winner_votes: Counter[str] = Counter()
        for v in verdicts:
            if v.winner and v.winner != "synthesis":
                winner_votes[v.winner] += 1

        # 2. Composite score averaging.
        provider_names = [r.provider for r in responses]
        composite_scores: dict[str, float] = {p: 0.0 for p in provider_names}
        for v in verdicts:
            for p in provider_names:
                composite_scores[p] += v.scores.get(p, 0.0)
        for p in provider_names:
            composite_scores[p] /= num_judges

        # Round scores.
        composite_scores = {p: round(s, 4) for p, s in composite_scores.items()}

        # 3. Average confidence across judges.
        avg_confidence = sum(v.confidence for v in verdicts) / num_judges

        # 4. Determine winner.
        majority_winner = ""
        if winner_votes:
            top_winner, top_count = winner_votes.most_common(1)[0]
            if top_count >= quorum:
                majority_winner = top_winner

        # Compute judge agreement (fraction agreeing on top winner).
        judge_agreement = 0.0
        if winner_votes:
            top_count = winner_votes.most_common(1)[0][1]
            judge_agreement = top_count / num_judges

        # Build verdict dicts for the result.
        verdict_dicts = [v.to_dict() for v in verdicts]

        # Check for synthesis from any judge.
        best_synthesis = ""
        best_synthesis_confidence = 0.0
        for v in verdicts:
            if v.winner == "synthesis" and v.synthesised_answer:
                if v.confidence > best_synthesis_confidence:
                    best_synthesis = v.synthesised_answer
                    best_synthesis_confidence = v.confidence

        if majority_winner:
            # Strong agreement — use the majority winner.
            winner_resp = None
            for r in responses:
                if r.provider == majority_winner:
                    winner_resp = r
                    break
            if winner_resp is None:
                winner_resp = responses[0]

            # Boost confidence when judges agree.
            boosted_confidence = min(1.0, avg_confidence + 0.1 * judge_agreement)

            judge_names = [v.judge_provider for v in verdicts]
            reasoning = (
                f"Multi-judge panel ({num_judges} judges: {', '.join(judge_names)}) "
                f"— {winner_votes[majority_winner]}/{num_judges} judges selected "
                f"{majority_winner} (agreement={judge_agreement:.2f})."
            )

            return ConsensusResult(
                content=winner_resp.content,
                strategy=ConsensusStrategy.MULTI_JUDGE_PANEL,
                provider=winner_resp.provider,
                model=winner_resp.model,
                confidence=round(boosted_confidence, 4),
                reasoning=reasoning,
                all_scores=composite_scores,
                judge_count=num_judges,
                judge_agreement=judge_agreement,
                judge_verdicts=verdict_dicts,
            )

        # No majority — check if synthesis is available.
        if best_synthesis:
            judge_names = [v.judge_provider for v in verdicts]
            reasoning = (
                f"Multi-judge panel ({num_judges} judges: {', '.join(judge_names)}) "
                f"disagreed on winner (agreement={judge_agreement:.2f}); "
                f"using best synthesis."
            )
            return ConsensusResult(
                content=best_synthesis,
                strategy=ConsensusStrategy.MULTI_JUDGE_PANEL,
                provider="consensus",
                model="multi-judge-synthesis",
                confidence=round(best_synthesis_confidence * 0.9, 4),  # slight penalty for disagreement
                reasoning=reasoning,
                all_scores=composite_scores,
                judge_count=num_judges,
                judge_agreement=judge_agreement,
                judge_verdicts=verdict_dicts,
            )

        # No majority, no synthesis — use highest composite-scored provider.
        best_provider = max(composite_scores, key=lambda p: composite_scores[p])
        winner_resp = None
        for r in responses:
            if r.provider == best_provider:
                winner_resp = r
                break
        if winner_resp is None:
            winner_resp = responses[0]

        # Lower confidence when judges disagree.
        penalised_confidence = max(0.1, avg_confidence * judge_agreement) if judge_agreement > 0 else 0.3

        judge_names = [v.judge_provider for v in verdicts]
        reasoning = (
            f"Multi-judge panel ({num_judges} judges: {', '.join(judge_names)}) "
            f"disagreed (agreement={judge_agreement:.2f}); "
            f"selected {best_provider} by highest composite score "
            f"({composite_scores[best_provider]:.3f})."
        )

        return ConsensusResult(
            content=winner_resp.content,
            strategy=ConsensusStrategy.MULTI_JUDGE_PANEL,
            provider=winner_resp.provider,
            model=winner_resp.model,
            confidence=round(penalised_confidence, 4),
            reasoning=reasoning,
            all_scores=composite_scores,
            judge_count=num_judges,
            judge_agreement=judge_agreement,
            judge_verdicts=verdict_dicts,
        )
