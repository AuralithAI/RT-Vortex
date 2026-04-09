"""Phase 5 tests — Consensus Engine + Multi-Judge Panel.

Covers:
- ConsensusStrategy enum values (including MULTI_JUDGE_PANEL)
- ConsensusResult dataclass and serialisation (including judge fields)
- JudgeVerdict dataclass
- Token overlap utilities (_tokenise, _jaccard, pairwise_agreement)
- GPT-as-judge prompt construction
- ConsensusEngine.run() with all strategies:
  - PICK_BEST: single response, multiple responses
  - MAJORITY_VOTE: scoring, tie-breaking
  - GPT_AS_JUDGE: success, fallback on failure, JSON parsing
  - MULTI_JUDGE_PANEL: majority agreement, disagreement, fallbacks,
    synthesis, score-based tiebreak, probe failures, verdict parsing
  - AUTO: strategy selection logic (prefers MULTI_JUDGE_PANEL over GPT_AS_JUDGE)
- Agent.run_consensus() integration with DiscussionThread
- Agent.consensus_engine lazy initialisation (with llm_probe)
- Go client post_consensus_event() (including judge_count/judge_agreement)
"""

import json
import pytest
from unittest.mock import AsyncMock, MagicMock, patch

from mono.swarm.consensus import (
    ConsensusEngine,
    ConsensusResult,
    ConsensusStrategy,
    JudgeVerdict,
    _build_judge_user_prompt,
    _jaccard,
    _tokenise,
    pairwise_agreement,
)
from mono.swarm.conversation import (
    DiscussionThread,
    DiscussionStatus,
    ProviderResponse,
    SharedConversation,
)
from mono.swarm.sdk.go_llm_client import ProbeResponse, ProbeResultItem


# ── Helpers ──────────────────────────────────────────────────────────────────


def _make_response(provider="grok", model="grok-3", content="Answer A", latency_ms=100, error=""):
    return ProviderResponse(
        provider=provider, model=model, content=content,
        latency_ms=latency_ms, error=error,
    )


def _make_thread(*responses, topic="test question"):
    thread = DiscussionThread(
        thread_id="test-thread-001",
        agent_id="agent-001",
        agent_role="senior_dev",
        topic=topic,
    )
    for r in responses:
        thread.add_response(r)
    thread.complete()
    return thread


# ── ConsensusStrategy ────────────────────────────────────────────────────────


class TestConsensusStrategy:
    def test_values(self):
        assert ConsensusStrategy.PICK_BEST.value == "pick_best"
        assert ConsensusStrategy.MAJORITY_VOTE.value == "majority_vote"
        assert ConsensusStrategy.GPT_AS_JUDGE.value == "gpt_as_judge"
        assert ConsensusStrategy.MULTI_JUDGE_PANEL.value == "multi_judge_panel"
        assert ConsensusStrategy.AUTO.value == "auto"

    def test_string_enum(self):
        assert str(ConsensusStrategy.PICK_BEST) == "ConsensusStrategy.PICK_BEST"
        assert ConsensusStrategy("pick_best") == ConsensusStrategy.PICK_BEST
        assert ConsensusStrategy("multi_judge_panel") == ConsensusStrategy.MULTI_JUDGE_PANEL


# ── ConsensusResult ──────────────────────────────────────────────────────────


class TestConsensusResult:
    def test_basic_construction(self):
        result = ConsensusResult(
            content="The answer is 42",
            strategy=ConsensusStrategy.PICK_BEST,
            provider="grok",
            model="grok-3",
            confidence=0.95,
            reasoning="Highest priority provider.",
        )
        assert result.succeeded
        assert result.content == "The answer is 42"
        assert result.provider == "grok"
        assert result.confidence == 0.95

    def test_empty_result(self):
        result = ConsensusResult()
        assert not result.succeeded
        assert result.content == ""
        assert result.confidence == 0.0

    def test_to_dict(self):
        result = ConsensusResult(
            content="answer",
            strategy=ConsensusStrategy.GPT_AS_JUDGE,
            provider="openai",
            confidence=0.87654,
            all_scores={"grok": 0.7, "openai": 0.9},
        )
        d = result.to_dict()
        assert d["strategy"] == "gpt_as_judge"
        assert d["provider"] == "openai"
        assert d["confidence"] == 0.8765  # rounded to 4dp
        assert d["all_scores"]["grok"] == 0.7

    def test_to_dict_no_judge_raw(self):
        """judge_raw is excluded from to_dict (internal debugging only)."""
        result = ConsensusResult(content="x", judge_raw="raw output")
        d = result.to_dict()
        assert "judge_raw" not in d

    def test_to_dict_with_judge_panel_fields(self):
        """Multi-judge panel fields included when judge_count > 0."""
        result = ConsensusResult(
            content="answer",
            strategy=ConsensusStrategy.MULTI_JUDGE_PANEL,
            provider="grok",
            confidence=0.9,
            judge_count=3,
            judge_agreement=0.6667,
            judge_verdicts=[
                {"judge_provider": "openai", "winner": "grok"},
                {"judge_provider": "anthropic", "winner": "grok"},
                {"judge_provider": "grok", "winner": "anthropic"},
            ],
        )
        d = result.to_dict()
        assert d["strategy"] == "multi_judge_panel"
        assert d["judge_count"] == 3
        assert d["judge_agreement"] == 0.6667
        assert len(d["judge_verdicts"]) == 3

    def test_to_dict_without_judge_panel_fields(self):
        """Multi-judge fields excluded when judge_count == 0."""
        result = ConsensusResult(content="x")
        d = result.to_dict()
        assert "judge_count" not in d
        assert "judge_agreement" not in d
        assert "judge_verdicts" not in d


# ── Token Overlap Utilities ──────────────────────────────────────────────────


class TestTokenOverlap:
    def test_tokenise_basic(self):
        tokens = _tokenise("Hello, world! This is a test.")
        assert tokens == ["hello", "world", "this", "is", "a", "test"]

    def test_tokenise_code(self):
        tokens = _tokenise("def foo(bar): return bar + 1")
        assert "def" in tokens
        assert "foo" in tokens
        assert "bar" in tokens

    def test_tokenise_empty(self):
        assert _tokenise("") == []

    def test_jaccard_identical(self):
        a = ["hello", "world"]
        assert _jaccard(a, a) == 1.0

    def test_jaccard_disjoint(self):
        assert _jaccard(["a", "b"], ["c", "d"]) == 0.0

    def test_jaccard_partial(self):
        j = _jaccard(["a", "b", "c"], ["b", "c", "d"])
        # intersection = {b, c}, union = {a, b, c, d}
        assert abs(j - 2 / 4) < 0.001

    def test_jaccard_empty(self):
        assert _jaccard([], []) == 1.0
        assert _jaccard(["a"], []) == 0.0

    def test_pairwise_agreement_identical(self):
        agreement = pairwise_agreement(["hello world", "hello world", "hello world"])
        assert agreement == 1.0

    def test_pairwise_agreement_disjoint(self):
        agreement = pairwise_agreement(["apple banana", "cat dog", "elephant fish"])
        assert agreement == 0.0

    def test_pairwise_agreement_single(self):
        assert pairwise_agreement(["only one"]) == 0.0

    def test_pairwise_agreement_empty(self):
        assert pairwise_agreement([]) == 0.0

    def test_pairwise_agreement_partial(self):
        agreement = pairwise_agreement([
            "the quick brown fox jumps",
            "the quick red fox leaps",
            "the slow brown fox jumps",
        ])
        assert 0.0 < agreement < 1.0


# ── Judge Prompt ─────────────────────────────────────────────────────────────


class TestJudgePrompt:
    def test_build_judge_user_prompt(self):
        responses = [
            _make_response("grok", "grok-3", "Use a hashmap"),
            _make_response("anthropic", "claude-3", "Use a tree"),
        ]
        prompt = _build_judge_user_prompt("How to implement lookup?", responses)
        assert "How to implement lookup?" in prompt
        assert "grok/grok-3" in prompt
        assert "anthropic/claude-3" in prompt
        assert "Use a hashmap" in prompt
        assert "Use a tree" in prompt
        assert "Response 1" in prompt
        assert "Response 2" in prompt

    def test_build_judge_prompt_preserves_order(self):
        responses = [
            _make_response("a", "m1", "First"),
            _make_response("b", "m2", "Second"),
            _make_response("c", "m3", "Third"),
        ]
        prompt = _build_judge_user_prompt("topic", responses)
        idx_a = prompt.index("Response 1")
        idx_b = prompt.index("Response 2")
        idx_c = prompt.index("Response 3")
        assert idx_a < idx_b < idx_c


# ── ConsensusEngine — PICK_BEST ─────────────────────────────────────────────


class TestPickBest:
    @pytest.mark.asyncio
    async def test_pick_best_single_response(self):
        engine = ConsensusEngine()
        thread = _make_thread(
            _make_response("grok", "grok-3", "Only answer"),
        )
        result = await engine.run(thread, ConsensusStrategy.PICK_BEST)
        assert result.content == "Only answer"
        assert result.provider == "grok"
        assert result.strategy == ConsensusStrategy.PICK_BEST
        assert result.confidence == 1.0  # single response = full confidence

    @pytest.mark.asyncio
    async def test_pick_best_multiple_responses(self):
        engine = ConsensusEngine()
        thread = _make_thread(
            _make_response("grok", "grok-3", "Grok answer", latency_ms=80),
            _make_response("anthropic", "claude", "Claude answer", latency_ms=120),
            _make_response("openai", "gpt-4o", "GPT answer", latency_ms=200),
        )
        result = await engine.run(thread, ConsensusStrategy.PICK_BEST)
        assert result.content == "Grok answer"  # first = highest priority
        assert result.provider == "grok"
        assert result.confidence == 0.7  # multiple responses = lower confidence

    @pytest.mark.asyncio
    async def test_pick_best_skips_failures(self):
        engine = ConsensusEngine()
        thread = _make_thread(
            _make_response("grok", "grok-3", "", error="rate limited"),
            _make_response("anthropic", "claude", "Claude answer"),
        )
        result = await engine.run(thread, ConsensusStrategy.PICK_BEST)
        assert result.content == "Claude answer"
        assert result.provider == "anthropic"

    @pytest.mark.asyncio
    async def test_pick_best_all_failed(self):
        engine = ConsensusEngine()
        thread = _make_thread(
            _make_response("grok", error="fail1"),
            _make_response("anthropic", error="fail2"),
        )
        result = await engine.run(thread, ConsensusStrategy.PICK_BEST)
        assert not result.succeeded
        assert "No successful" in result.reasoning


# ── ConsensusEngine — MAJORITY_VOTE ──────────────────────────────────────────


class TestMajorityVote:
    @pytest.mark.asyncio
    async def test_majority_vote_agreement(self):
        """When two providers agree and one differs, the agreeing one wins."""
        engine = ConsensusEngine()
        thread = _make_thread(
            _make_response("grok", "grok-3", "Use a hashmap for O(1) lookup"),
            _make_response("anthropic", "claude", "Use a hashmap data structure for constant time lookup"),
            _make_response("openai", "gpt-4o", "Use a balanced binary search tree for O(log n) lookup"),
        )
        result = await engine.run(thread, ConsensusStrategy.MAJORITY_VOTE)
        assert result.strategy == ConsensusStrategy.MAJORITY_VOTE
        # The two "hashmap" answers should be more similar to each other
        # than to the "binary search tree" answer.
        assert result.provider in ("grok", "anthropic")
        assert result.confidence > 0.0

    @pytest.mark.asyncio
    async def test_majority_vote_scores(self):
        engine = ConsensusEngine()
        thread = _make_thread(
            _make_response("a", content="alpha beta gamma"),
            _make_response("b", content="alpha beta delta"),
            _make_response("c", content="epsilon zeta eta"),
        )
        result = await engine.run(thread, ConsensusStrategy.MAJORITY_VOTE)
        assert "a" in result.all_scores
        assert "b" in result.all_scores
        assert "c" in result.all_scores
        # a and b overlap more, so c should have lowest score
        assert result.all_scores["c"] < result.all_scores["a"]
        assert result.all_scores["c"] < result.all_scores["b"]

    @pytest.mark.asyncio
    async def test_majority_vote_identical(self):
        engine = ConsensusEngine()
        thread = _make_thread(
            _make_response("a", content="same answer"),
            _make_response("b", content="same answer"),
        )
        result = await engine.run(thread, ConsensusStrategy.MAJORITY_VOTE)
        assert result.confidence > 0.9  # high confidence when they agree


# ── ConsensusEngine — GPT_AS_JUDGE ───────────────────────────────────────────


class TestGPTAsJudge:
    @pytest.mark.asyncio
    async def test_gpt_as_judge_success(self):
        """GPT selects a winner from the provider responses."""
        judge_response = {
            "choices": [{
                "message": {
                    "role": "assistant",
                    "content": json.dumps({
                        "winner": "anthropic",
                        "confidence": 0.92,
                        "scores": {"grok": 0.7, "anthropic": 0.92, "openai": 0.85},
                        "reasoning": "Claude provided the most complete answer.",
                        "synthesised_answer": "",
                    }),
                },
            }],
        }
        mock_llm = AsyncMock(return_value=judge_response)
        engine = ConsensusEngine(llm_complete=mock_llm, go_base_url="http://test", agent_token="tok")

        thread = _make_thread(
            _make_response("grok", "grok-3", "Grok answer"),
            _make_response("anthropic", "claude", "Claude answer"),
            _make_response("openai", "gpt-4o", "GPT answer"),
        )

        result = await engine.run(thread, ConsensusStrategy.GPT_AS_JUDGE)
        assert result.strategy == ConsensusStrategy.GPT_AS_JUDGE
        assert result.content == "Claude answer"
        assert result.provider == "anthropic"
        assert result.confidence == 0.92
        assert "complete" in result.reasoning.lower()

        # Verify the LLM was called with judge prompts.
        mock_llm.assert_awaited_once()
        call_args = mock_llm.call_args
        messages = call_args.kwargs.get("messages") or call_args[0][0]
        assert messages[0]["role"] == "system"
        assert "judge" in messages[0]["content"].lower()

    @pytest.mark.asyncio
    async def test_gpt_as_judge_synthesis(self):
        """GPT synthesises a new answer instead of picking a winner."""
        judge_response = {
            "choices": [{
                "message": {
                    "content": json.dumps({
                        "winner": "synthesis",
                        "confidence": 0.95,
                        "scores": {"grok": 0.6, "anthropic": 0.8},
                        "reasoning": "Both had good ideas, combining them.",
                        "synthesised_answer": "Combined best of both: use a hashmap with a tree fallback.",
                    }),
                },
            }],
        }
        mock_llm = AsyncMock(return_value=judge_response)
        engine = ConsensusEngine(llm_complete=mock_llm)

        thread = _make_thread(
            _make_response("grok", content="Use a hashmap"),
            _make_response("anthropic", content="Use a tree"),
        )
        result = await engine.run(thread, ConsensusStrategy.GPT_AS_JUDGE)
        assert result.provider == "consensus"
        assert "Combined best" in result.content
        assert result.confidence == 0.95

    @pytest.mark.asyncio
    async def test_gpt_as_judge_llm_failure_fallback(self):
        """If the GPT call fails, fall back to majority vote."""
        mock_llm = AsyncMock(side_effect=Exception("LLM timeout"))
        engine = ConsensusEngine(llm_complete=mock_llm)

        thread = _make_thread(
            _make_response("grok", content="Answer A"),
            _make_response("anthropic", content="Answer B"),
        )
        result = await engine.run(thread, ConsensusStrategy.GPT_AS_JUDGE)
        # Should have fallen back to majority vote.
        assert result.strategy == ConsensusStrategy.MAJORITY_VOTE
        assert "fell back" in result.reasoning.lower()
        assert result.succeeded

    @pytest.mark.asyncio
    async def test_gpt_as_judge_malformed_json(self):
        """If GPT returns non-JSON, fall back to majority vote."""
        judge_response = {
            "choices": [{
                "message": {"content": "I think the best answer is from Grok because..."},
            }],
        }
        mock_llm = AsyncMock(return_value=judge_response)
        engine = ConsensusEngine(llm_complete=mock_llm)

        thread = _make_thread(
            _make_response("grok", content="Answer A"),
            _make_response("anthropic", content="Answer B"),
        )
        result = await engine.run(thread, ConsensusStrategy.GPT_AS_JUDGE)
        assert result.strategy == ConsensusStrategy.MAJORITY_VOTE
        assert "non-JSON" in result.reasoning

    @pytest.mark.asyncio
    async def test_gpt_as_judge_markdown_fences(self):
        """GPT wraps JSON in markdown code fences — should still parse."""
        raw_json = json.dumps({
            "winner": "grok",
            "confidence": 0.88,
            "scores": {"grok": 0.88},
            "reasoning": "Best answer.",
            "synthesised_answer": "",
        })
        judge_response = {
            "choices": [{
                "message": {"content": f"```json\n{raw_json}\n```"},
            }],
        }
        mock_llm = AsyncMock(return_value=judge_response)
        engine = ConsensusEngine(llm_complete=mock_llm)

        thread = _make_thread(_make_response("grok", content="Good answer"))
        result = await engine.run(thread, ConsensusStrategy.GPT_AS_JUDGE)
        assert result.provider == "grok"
        assert result.confidence == 0.88

    @pytest.mark.asyncio
    async def test_gpt_as_judge_no_llm_fallback(self):
        """If no llm_complete is provided, GPT_AS_JUDGE falls to majority vote."""
        engine = ConsensusEngine(llm_complete=None)
        thread = _make_thread(
            _make_response("a", content="Answer 1"),
            _make_response("b", content="Answer 2"),
        )
        result = await engine.run(thread, ConsensusStrategy.GPT_AS_JUDGE)
        assert result.strategy == ConsensusStrategy.MAJORITY_VOTE

    @pytest.mark.asyncio
    async def test_gpt_as_judge_unknown_winner(self):
        """If GPT returns a winner name that doesn't match any provider, use highest score."""
        judge_response = {
            "choices": [{
                "message": {
                    "content": json.dumps({
                        "winner": "unknown_provider",
                        "confidence": 0.8,
                        "scores": {"grok": 0.5, "anthropic": 0.9},
                        "reasoning": "Anthropic was best.",
                        "synthesised_answer": "",
                    }),
                },
            }],
        }
        mock_llm = AsyncMock(return_value=judge_response)
        engine = ConsensusEngine(llm_complete=mock_llm)

        thread = _make_thread(
            _make_response("grok", content="Grok answer"),
            _make_response("anthropic", content="Anthropic answer"),
        )
        result = await engine.run(thread, ConsensusStrategy.GPT_AS_JUDGE)
        assert result.provider == "anthropic"
        assert result.content == "Anthropic answer"

    @pytest.mark.asyncio
    async def test_gpt_as_judge_empty_response(self):
        """If GPT returns empty choices, fall back to majority vote."""
        mock_llm = AsyncMock(return_value={"choices": []})
        engine = ConsensusEngine(llm_complete=mock_llm)
        thread = _make_thread(
            _make_response("a", content="X"),
            _make_response("b", content="Y"),
        )
        result = await engine.run(thread, ConsensusStrategy.GPT_AS_JUDGE)
        assert result.strategy == ConsensusStrategy.MAJORITY_VOTE


# ── ConsensusEngine — AUTO strategy ──────────────────────────────────────────


class TestAutoStrategy:
    @pytest.mark.asyncio
    async def test_auto_single_response_picks_best(self):
        engine = ConsensusEngine()
        thread = _make_thread(_make_response("grok", content="Only one"))
        result = await engine.run(thread, ConsensusStrategy.AUTO)
        assert result.strategy == ConsensusStrategy.PICK_BEST

    @pytest.mark.asyncio
    async def test_auto_high_agreement_picks_best(self):
        """When responses are very similar, AUTO selects PICK_BEST."""
        engine = ConsensusEngine(agreement_threshold=0.5)
        thread = _make_thread(
            _make_response("grok", content="The answer is clearly forty two"),
            _make_response("anthropic", content="The answer is clearly forty two"),
        )
        result = await engine.run(thread, ConsensusStrategy.AUTO)
        assert result.strategy == ConsensusStrategy.PICK_BEST

    @pytest.mark.asyncio
    async def test_auto_disagreement_with_probe_uses_multi_judge(self):
        """When responses disagree and probe is available, AUTO uses MULTI_JUDGE_PANEL."""
        # Build a mock probe that returns 3 judge verdicts.
        def _make_probe_resp():
            return MagicMock(
                results=[
                    MagicMock(provider="openai", model="gpt-4o", content=json.dumps({
                        "winner": "grok", "confidence": 0.85,
                        "scores": {"grok": 0.85, "anthropic": 0.65},
                        "reasoning": "Grok was more correct.", "synthesised_answer": "",
                    }), error=""),
                    MagicMock(provider="anthropic", model="claude-3", content=json.dumps({
                        "winner": "grok", "confidence": 0.80,
                        "scores": {"grok": 0.80, "anthropic": 0.70},
                        "reasoning": "Grok answer is better.", "synthesised_answer": "",
                    }), error=""),
                    MagicMock(provider="grok", model="grok-3", content=json.dumps({
                        "winner": "grok", "confidence": 0.90,
                        "scores": {"grok": 0.90, "anthropic": 0.60},
                        "reasoning": "Grok clearly wins.", "synthesised_answer": "",
                    }), error=""),
                ],
            )

        mock_probe = AsyncMock(return_value=_make_probe_resp())
        engine = ConsensusEngine(
            llm_complete=AsyncMock(), llm_probe=mock_probe,
            agreement_threshold=0.9,
        )

        thread = _make_thread(
            _make_response("grok", content="Use approach alpha with method X"),
            _make_response("anthropic", content="Use approach beta with technique Y"),
        )
        result = await engine.run(thread, ConsensusStrategy.AUTO)
        assert result.strategy == ConsensusStrategy.MULTI_JUDGE_PANEL
        mock_probe.assert_awaited_once()

    @pytest.mark.asyncio
    async def test_auto_disagreement_with_only_complete_uses_gpt_judge(self):
        """When responses disagree and only llm_complete available, AUTO uses GPT_AS_JUDGE."""
        judge_response = {
            "choices": [{
                "message": {
                    "content": json.dumps({
                        "winner": "grok",
                        "confidence": 0.85,
                        "scores": {"grok": 0.85, "anthropic": 0.65},
                        "reasoning": "Grok was more correct.",
                        "synthesised_answer": "",
                    }),
                },
            }],
        }
        mock_llm = AsyncMock(return_value=judge_response)
        engine = ConsensusEngine(llm_complete=mock_llm, llm_probe=None, agreement_threshold=0.9)

        thread = _make_thread(
            _make_response("grok", content="Use approach alpha with method X"),
            _make_response("anthropic", content="Use approach beta with technique Y"),
        )
        result = await engine.run(thread, ConsensusStrategy.AUTO)
        assert result.strategy == ConsensusStrategy.GPT_AS_JUDGE
        mock_llm.assert_awaited_once()

    @pytest.mark.asyncio
    async def test_auto_disagreement_without_llm_uses_vote(self):
        """When responses disagree and no LLM, AUTO falls back to MAJORITY_VOTE."""
        engine = ConsensusEngine(llm_complete=None, llm_probe=None, agreement_threshold=0.9)
        thread = _make_thread(
            _make_response("grok", content="Completely different approach one"),
            _make_response("anthropic", content="Totally unrelated approach two"),
        )
        result = await engine.run(thread, ConsensusStrategy.AUTO)
        assert result.strategy == ConsensusStrategy.MAJORITY_VOTE


# ── JudgeVerdict ─────────────────────────────────────────────────────────────


class TestJudgeVerdict:
    def test_basic_construction(self):
        v = JudgeVerdict(
            judge_provider="openai",
            judge_model="gpt-4o",
            winner="grok",
            confidence=0.9,
            scores={"grok": 0.9, "anthropic": 0.7},
            reasoning="Grok answer was better.",
        )
        assert v.succeeded
        assert v.winner == "grok"
        assert v.judge_provider == "openai"

    def test_failed_verdict(self):
        v = JudgeVerdict(judge_provider="openai", error="timeout")
        assert not v.succeeded

    def test_empty_winner_not_succeeded(self):
        v = JudgeVerdict(judge_provider="openai", winner="")
        assert not v.succeeded

    def test_to_dict(self):
        v = JudgeVerdict(
            judge_provider="anthropic",
            judge_model="claude-3",
            winner="grok",
            confidence=0.85,
            scores={"grok": 0.85},
            reasoning="Best answer.",
        )
        d = v.to_dict()
        assert d["judge_provider"] == "anthropic"
        assert d["winner"] == "grok"
        assert d["confidence"] == 0.85
        assert "error" in d


# ── ConsensusEngine — MULTI_JUDGE_PANEL ──────────────────────────────────────


def _make_judge_probe_response(verdicts: list[dict]) -> MagicMock:
    """Build a mock ProbeResponse with judge verdicts.

    Each dict in verdicts has: provider, model, winner, confidence, scores,
    reasoning, synthesised_answer, error (all optional).
    """
    results = []
    for v in verdicts:
        error = v.get("error", "")
        content = ""
        if not error:
            content = json.dumps({
                "winner": v.get("winner", ""),
                "confidence": v.get("confidence", 0.5),
                "scores": v.get("scores", {}),
                "reasoning": v.get("reasoning", ""),
                "synthesised_answer": v.get("synthesised_answer", ""),
            })
        results.append(MagicMock(
            provider=v.get("provider", "unknown"),
            model=v.get("model", ""),
            content=content,
            error=error,
        ))
    return MagicMock(results=results)


class TestMultiJudgePanel:
    """Tests for the MULTI_JUDGE_PANEL consensus strategy."""

    @pytest.mark.asyncio
    async def test_majority_agreement(self):
        """When 2/3 judges agree on the same winner, that winner is selected."""
        probe_resp = _make_judge_probe_response([
            {"provider": "openai", "model": "gpt-4o", "winner": "grok",
             "confidence": 0.9, "scores": {"grok": 0.9, "anthropic": 0.6}},
            {"provider": "anthropic", "model": "claude-3", "winner": "grok",
             "confidence": 0.85, "scores": {"grok": 0.85, "anthropic": 0.7}},
            {"provider": "grok", "model": "grok-3", "winner": "anthropic",
             "confidence": 0.8, "scores": {"grok": 0.7, "anthropic": 0.8}},
        ])
        mock_probe = AsyncMock(return_value=probe_resp)
        engine = ConsensusEngine(llm_probe=mock_probe)

        thread = _make_thread(
            _make_response("grok", "grok-3", "Grok answer"),
            _make_response("anthropic", "claude", "Claude answer"),
        )
        result = await engine.run(thread, ConsensusStrategy.MULTI_JUDGE_PANEL)

        assert result.strategy == ConsensusStrategy.MULTI_JUDGE_PANEL
        assert result.provider == "grok"
        assert result.content == "Grok answer"
        assert result.judge_count == 3
        assert result.judge_agreement > 0.5
        assert result.confidence > 0.7
        assert len(result.judge_verdicts) == 3
        # Composite scores should be averaged across judges.
        assert "grok" in result.all_scores
        assert "anthropic" in result.all_scores
        mock_probe.assert_awaited_once()

    @pytest.mark.asyncio
    async def test_unanimous_agreement(self):
        """When all 3 judges agree, confidence is highest."""
        probe_resp = _make_judge_probe_response([
            {"provider": "openai", "winner": "grok",
             "confidence": 0.95, "scores": {"grok": 0.95, "anthropic": 0.5}},
            {"provider": "anthropic", "winner": "grok",
             "confidence": 0.9, "scores": {"grok": 0.9, "anthropic": 0.55}},
            {"provider": "grok", "winner": "grok",
             "confidence": 0.92, "scores": {"grok": 0.92, "anthropic": 0.52}},
        ])
        mock_probe = AsyncMock(return_value=probe_resp)
        engine = ConsensusEngine(llm_probe=mock_probe)

        thread = _make_thread(
            _make_response("grok", content="Grok answer"),
            _make_response("anthropic", content="Claude answer"),
        )
        result = await engine.run(thread, ConsensusStrategy.MULTI_JUDGE_PANEL)

        assert result.provider == "grok"
        assert result.judge_agreement == 1.0
        assert result.confidence > 0.9  # boosted by perfect agreement

    @pytest.mark.asyncio
    async def test_all_judges_disagree_uses_composite_score(self):
        """When no majority, pick the provider with the highest composite score."""
        probe_resp = _make_judge_probe_response([
            {"provider": "openai", "winner": "grok",
             "confidence": 0.7, "scores": {"grok": 0.8, "anthropic": 0.6, "gemini": 0.5}},
            {"provider": "anthropic", "winner": "anthropic",
             "confidence": 0.7, "scores": {"grok": 0.5, "anthropic": 0.85, "gemini": 0.6}},
            {"provider": "grok", "winner": "gemini",
             "confidence": 0.7, "scores": {"grok": 0.6, "anthropic": 0.5, "gemini": 0.9}},
        ])
        mock_probe = AsyncMock(return_value=probe_resp)
        engine = ConsensusEngine(llm_probe=mock_probe)

        thread = _make_thread(
            _make_response("grok", content="Grok answer"),
            _make_response("anthropic", content="Claude answer"),
            _make_response("gemini", content="Gemini answer"),
        )
        result = await engine.run(thread, ConsensusStrategy.MULTI_JUDGE_PANEL)

        assert result.strategy == ConsensusStrategy.MULTI_JUDGE_PANEL
        # Each judge picks different winner, composite score decides.
        # grok: (0.8+0.5+0.6)/3 = 0.633, anthropic: (0.6+0.85+0.5)/3 = 0.65, gemini: (0.5+0.6+0.9)/3 = 0.667
        assert result.provider == "gemini"
        assert result.judge_agreement < 0.5  # low agreement
        assert result.confidence < 0.5  # penalised for disagreement

    @pytest.mark.asyncio
    async def test_synthesis_when_judges_disagree(self):
        """When judges disagree and one synthesises, use the synthesis."""
        probe_resp = _make_judge_probe_response([
            {"provider": "openai", "winner": "grok",
             "confidence": 0.7, "scores": {"grok": 0.8, "anthropic": 0.6}},
            {"provider": "anthropic", "winner": "synthesis",
             "confidence": 0.85, "scores": {"grok": 0.7, "anthropic": 0.7},
             "synthesised_answer": "Best of both: use hashmap with tree fallback."},
            {"provider": "grok", "winner": "anthropic",
             "confidence": 0.75, "scores": {"grok": 0.65, "anthropic": 0.8}},
        ])
        mock_probe = AsyncMock(return_value=probe_resp)
        engine = ConsensusEngine(llm_probe=mock_probe)

        thread = _make_thread(
            _make_response("grok", content="Use a hashmap"),
            _make_response("anthropic", content="Use a tree"),
        )
        result = await engine.run(thread, ConsensusStrategy.MULTI_JUDGE_PANEL)

        assert result.provider == "consensus"
        assert "Best of both" in result.content
        assert "disagree" in result.reasoning.lower()

    @pytest.mark.asyncio
    async def test_probe_failure_fallback_to_gpt_judge(self):
        """If the probe call fails, fall back to GPT_AS_JUDGE."""
        mock_probe = AsyncMock(side_effect=Exception("network error"))
        judge_resp = {
            "choices": [{"message": {"content": json.dumps({
                "winner": "grok", "confidence": 0.8,
                "scores": {"grok": 0.8}, "reasoning": "Best.", "synthesised_answer": "",
            })}}],
        }
        mock_complete = AsyncMock(return_value=judge_resp)
        engine = ConsensusEngine(llm_complete=mock_complete, llm_probe=mock_probe)

        thread = _make_thread(
            _make_response("grok", content="Answer A"),
        )
        result = await engine.run(thread, ConsensusStrategy.MULTI_JUDGE_PANEL)
        assert "fell back" in result.reasoning.lower()
        assert "single GPT judge" in result.reasoning

    @pytest.mark.asyncio
    async def test_probe_failure_no_complete_fallback_to_majority(self):
        """If probe fails and no llm_complete, fall back to majority vote."""
        mock_probe = AsyncMock(side_effect=Exception("timeout"))
        engine = ConsensusEngine(llm_complete=None, llm_probe=mock_probe)

        thread = _make_thread(
            _make_response("grok", content="Answer A"),
            _make_response("anthropic", content="Answer B"),
        )
        result = await engine.run(thread, ConsensusStrategy.MULTI_JUDGE_PANEL)
        assert "fell back to majority vote" in result.reasoning.lower()

    @pytest.mark.asyncio
    async def test_no_probe_available_fallback_to_gpt_judge(self):
        """If no llm_probe is set, fall back to GPT_AS_JUDGE."""
        judge_resp = {
            "choices": [{"message": {"content": json.dumps({
                "winner": "grok", "confidence": 0.8,
                "scores": {"grok": 0.8}, "reasoning": "Best.", "synthesised_answer": "",
            })}}],
        }
        mock_complete = AsyncMock(return_value=judge_resp)
        engine = ConsensusEngine(llm_complete=mock_complete, llm_probe=None)

        thread = _make_thread(_make_response("grok", content="Answer"))
        result = await engine.run(thread, ConsensusStrategy.MULTI_JUDGE_PANEL)
        assert result.strategy == ConsensusStrategy.GPT_AS_JUDGE

    @pytest.mark.asyncio
    async def test_no_probe_no_complete_fallback_to_majority(self):
        """If neither probe nor complete is available, fall to majority vote."""
        engine = ConsensusEngine(llm_complete=None, llm_probe=None)
        thread = _make_thread(
            _make_response("a", content="X"),
            _make_response("b", content="Y"),
        )
        result = await engine.run(thread, ConsensusStrategy.MULTI_JUDGE_PANEL)
        assert result.strategy == ConsensusStrategy.MAJORITY_VOTE

    @pytest.mark.asyncio
    async def test_too_few_successful_judges_fallback(self):
        """If fewer than min_judges succeed, fall back."""
        probe_resp = _make_judge_probe_response([
            {"provider": "openai", "winner": "grok",
             "confidence": 0.9, "scores": {"grok": 0.9}},
            {"provider": "anthropic", "error": "rate limited"},
            {"provider": "grok", "error": "timeout"},
        ])
        mock_probe = AsyncMock(return_value=probe_resp)
        judge_resp = {
            "choices": [{"message": {"content": json.dumps({
                "winner": "grok", "confidence": 0.8,
                "scores": {"grok": 0.8}, "reasoning": "OK.", "synthesised_answer": "",
            })}}],
        }
        mock_complete = AsyncMock(return_value=judge_resp)
        engine = ConsensusEngine(
            llm_complete=mock_complete, llm_probe=mock_probe, min_judges=2,
        )

        thread = _make_thread(_make_response("grok", content="Answer"))
        result = await engine.run(thread, ConsensusStrategy.MULTI_JUDGE_PANEL)
        assert "only 1 successful verdicts" in result.reasoning.lower()

    @pytest.mark.asyncio
    async def test_malformed_json_from_one_judge(self):
        """If one judge returns non-JSON, it's excluded; remaining judges still work."""
        results = [
            MagicMock(provider="openai", model="gpt-4o", content="Not valid JSON", error=""),
            MagicMock(provider="anthropic", model="claude-3", content=json.dumps({
                "winner": "grok", "confidence": 0.85,
                "scores": {"grok": 0.85, "anthropic": 0.6},
                "reasoning": "Grok wins.", "synthesised_answer": "",
            }), error=""),
            MagicMock(provider="grok", model="grok-3", content=json.dumps({
                "winner": "grok", "confidence": 0.9,
                "scores": {"grok": 0.9, "anthropic": 0.5},
                "reasoning": "Grok is best.", "synthesised_answer": "",
            }), error=""),
        ]
        probe_resp = MagicMock(results=results)
        mock_probe = AsyncMock(return_value=probe_resp)
        engine = ConsensusEngine(llm_probe=mock_probe, min_judges=2)

        thread = _make_thread(
            _make_response("grok", content="Grok answer"),
            _make_response("anthropic", content="Claude answer"),
        )
        result = await engine.run(thread, ConsensusStrategy.MULTI_JUDGE_PANEL)

        # 2 out of 3 judges succeeded — still enough.
        assert result.strategy == ConsensusStrategy.MULTI_JUDGE_PANEL
        assert result.provider == "grok"
        assert result.judge_count == 2  # only successful verdicts counted

    @pytest.mark.asyncio
    async def test_probe_called_with_judge_role(self):
        """Probe should be called with agent_role='consensus_judge'."""
        probe_resp = _make_judge_probe_response([
            {"provider": "openai", "winner": "grok", "confidence": 0.8, "scores": {"grok": 0.8}},
            {"provider": "anthropic", "winner": "grok", "confidence": 0.8, "scores": {"grok": 0.8}},
        ])
        mock_probe = AsyncMock(return_value=probe_resp)
        engine = ConsensusEngine(
            llm_probe=mock_probe,
            go_base_url="http://test:8080",
            agent_token="jwt-123",
        )

        thread = _make_thread(_make_response("grok", content="Answer"))
        await engine.run(thread, ConsensusStrategy.MULTI_JUDGE_PANEL)

        call_kwargs = mock_probe.call_args.kwargs
        assert call_kwargs["agent_role"] == "consensus_judge"
        assert call_kwargs["go_base_url"] == "http://test:8080"
        assert call_kwargs["agent_token"] == "jwt-123"
        # System prompt should mention "judge".
        messages = mock_probe.call_args.kwargs.get("messages") or mock_probe.call_args[0][0]
        assert "judge" in messages[0]["content"].lower()

    @pytest.mark.asyncio
    async def test_composite_scores_averaged(self):
        """Composite scores should be the average across all judges."""
        probe_resp = _make_judge_probe_response([
            {"provider": "openai", "winner": "grok",
             "confidence": 0.8, "scores": {"grok": 0.9, "anthropic": 0.6}},
            {"provider": "anthropic", "winner": "grok",
             "confidence": 0.8, "scores": {"grok": 0.7, "anthropic": 0.8}},
        ])
        mock_probe = AsyncMock(return_value=probe_resp)
        engine = ConsensusEngine(llm_probe=mock_probe)

        thread = _make_thread(
            _make_response("grok", content="Grok answer"),
            _make_response("anthropic", content="Claude answer"),
        )
        result = await engine.run(thread, ConsensusStrategy.MULTI_JUDGE_PANEL)

        # grok: (0.9 + 0.7) / 2 = 0.8, anthropic: (0.6 + 0.8) / 2 = 0.7
        assert abs(result.all_scores["grok"] - 0.8) < 0.01
        assert abs(result.all_scores["anthropic"] - 0.7) < 0.01

    @pytest.mark.asyncio
    async def test_two_judges_minimum(self):
        """Two judges is enough for a valid panel."""
        probe_resp = _make_judge_probe_response([
            {"provider": "openai", "winner": "grok",
             "confidence": 0.85, "scores": {"grok": 0.85}},
            {"provider": "anthropic", "winner": "grok",
             "confidence": 0.9, "scores": {"grok": 0.9}},
        ])
        mock_probe = AsyncMock(return_value=probe_resp)
        engine = ConsensusEngine(llm_probe=mock_probe, min_judges=2)

        thread = _make_thread(_make_response("grok", content="Answer"))
        result = await engine.run(thread, ConsensusStrategy.MULTI_JUDGE_PANEL)
        assert result.strategy == ConsensusStrategy.MULTI_JUDGE_PANEL
        assert result.judge_count == 2

    @pytest.mark.asyncio
    async def test_four_judges_quorum(self):
        """With 4 judges, quorum is ceil(4*2/3) = 3, so 3/4 agreement needed."""
        probe_resp = _make_judge_probe_response([
            {"provider": "openai", "winner": "grok",
             "confidence": 0.8, "scores": {"grok": 0.8, "anthropic": 0.6}},
            {"provider": "anthropic", "winner": "grok",
             "confidence": 0.85, "scores": {"grok": 0.85, "anthropic": 0.65}},
            {"provider": "grok", "winner": "grok",
             "confidence": 0.9, "scores": {"grok": 0.9, "anthropic": 0.5}},
            {"provider": "gemini", "winner": "anthropic",
             "confidence": 0.75, "scores": {"grok": 0.6, "anthropic": 0.75}},
        ])
        mock_probe = AsyncMock(return_value=probe_resp)
        engine = ConsensusEngine(llm_probe=mock_probe)

        thread = _make_thread(
            _make_response("grok", content="Grok answer"),
            _make_response("anthropic", content="Claude answer"),
        )
        result = await engine.run(thread, ConsensusStrategy.MULTI_JUDGE_PANEL)

        # 3/4 judges agree on grok → quorum met (3 >= ceil(4*2/3) = 3).
        assert result.provider == "grok"
        assert result.judge_agreement == 0.75

    @pytest.mark.asyncio
    async def test_markdown_fenced_json_from_judge(self):
        """Judges that return JSON wrapped in markdown fences are still parsed."""
        raw_json = json.dumps({
            "winner": "grok", "confidence": 0.88,
            "scores": {"grok": 0.88}, "reasoning": "Good.", "synthesised_answer": "",
        })
        results = [
            MagicMock(provider="openai", model="gpt-4o",
                     content=f"```json\n{raw_json}\n```", error=""),
            MagicMock(provider="anthropic", model="claude-3",
                     content=raw_json, error=""),
        ]
        probe_resp = MagicMock(results=results)
        mock_probe = AsyncMock(return_value=probe_resp)
        engine = ConsensusEngine(llm_probe=mock_probe)

        thread = _make_thread(_make_response("grok", content="Answer"))
        result = await engine.run(thread, ConsensusStrategy.MULTI_JUDGE_PANEL)
        assert result.provider == "grok"
        assert result.judge_count == 2

    @pytest.mark.asyncio
    async def test_empty_response_from_judge(self):
        """A judge returning empty content is treated as a failure."""
        probe_resp = _make_judge_probe_response([
            {"provider": "openai", "winner": "grok",
             "confidence": 0.85, "scores": {"grok": 0.85}},
            {"provider": "anthropic", "winner": "grok",
             "confidence": 0.9, "scores": {"grok": 0.9}},
        ])
        # Add an empty-content judge.
        probe_resp.results.append(MagicMock(
            provider="grok", model="grok-3", content="", error="",
        ))
        mock_probe = AsyncMock(return_value=probe_resp)
        engine = ConsensusEngine(llm_probe=mock_probe, min_judges=2)

        thread = _make_thread(_make_response("grok", content="Answer"))
        result = await engine.run(thread, ConsensusStrategy.MULTI_JUDGE_PANEL)
        # Empty response judge excluded; still have 2 successful.
        assert result.judge_count == 2


# ── Agent.run_consensus() integration ────────────────────────────────────────


class TestAgentConsensusIntegration:
    def _make_agent(self):
        from mono.swarm.sdk.agent import Agent, AgentConfig
        agent = Agent(
            agent_id="agent-test",
            role="senior_dev",
            team_id="team-1",
            agent_config=AgentConfig(go_base_url="http://localhost:8080"),
        )
        agent.token = "test-jwt"
        return agent

    def test_consensus_engine_lazy_init(self):
        agent = self._make_agent()
        assert agent._consensus_engine is None
        engine = agent.consensus_engine
        assert engine is not None
        assert isinstance(engine, ConsensusEngine)
        # Second access returns same instance.
        assert agent.consensus_engine is engine

    def test_consensus_engine_has_probe(self):
        """Lazy-init engine should have llm_probe set for multi-judge panel."""
        agent = self._make_agent()
        engine = agent.consensus_engine
        assert engine._llm_probe is not None
        assert engine._llm_complete is not None

    @pytest.mark.asyncio
    async def test_run_consensus_with_discussion_thread(self):
        agent = self._make_agent()
        # Override the consensus engine to avoid real LLM calls.
        agent._consensus_engine = ConsensusEngine(llm_complete=None)

        mock_conv = MagicMock()
        mock_thread = DiscussionThread(
            thread_id="disc-100",
            agent_id="agent-test",
            agent_role="senior_dev",
            topic="test topic",
        )
        mock_thread.add_response(_make_response("grok", "grok-3", "Best answer"))
        mock_thread.add_response(_make_response("anthropic", "claude", "Other answer"))
        mock_thread.complete()

        mock_conv.get_discussion = MagicMock(return_value=mock_thread)
        mock_conv.synthesise_discussion = AsyncMock()
        mock_conv.task_id = "task-001"
        mock_go = AsyncMock()
        mock_conv._go = mock_go
        agent.conversation = mock_conv

        probe_resp = ProbeResponse(
            results=[
                ProbeResultItem(provider="grok", model="grok-3", content="Best answer"),
                ProbeResultItem(provider="anthropic", model="claude", content="Other answer"),
            ],
            providers=2, successes=2,
        )
        probe_resp._discussion_thread_id = "disc-100"

        result = await agent.run_consensus(probe_resp, strategy=ConsensusStrategy.PICK_BEST)
        assert result.succeeded
        assert result.content == "Best answer"
        assert result.provider == "grok"

        # Synthesis should be recorded in conversation.
        mock_conv.synthesise_discussion.assert_awaited_once()

        # Consensus event should be broadcast to Go.
        mock_go.post_consensus_event.assert_awaited_once()
        event_data = mock_go.post_consensus_event.call_args[0][1]
        assert event_data["strategy"] == "pick_best"
        assert event_data["provider"] == "grok"

    @pytest.mark.asyncio
    async def test_run_consensus_without_conversation(self):
        """run_consensus works even without a conversation (builds temp thread)."""
        agent = self._make_agent()
        agent.conversation = None

        probe_resp = ProbeResponse(
            results=[
                ProbeResultItem(provider="grok", content="Answer A"),
                ProbeResultItem(provider="anthropic", content="Answer B"),
            ],
            providers=2, successes=2,
        )

        result = await agent.run_consensus(probe_resp)
        assert result.succeeded

    @pytest.mark.asyncio
    async def test_run_consensus_explicit_strategy(self):
        agent = self._make_agent()
        probe_resp = ProbeResponse(
            results=[
                ProbeResultItem(provider="a", content="alpha beta gamma"),
                ProbeResultItem(provider="b", content="alpha beta delta"),
                ProbeResultItem(provider="c", content="epsilon zeta eta"),
            ],
            providers=3, successes=3,
        )

        result = await agent.run_consensus(probe_resp, strategy=ConsensusStrategy.MAJORITY_VOTE)
        assert result.strategy == ConsensusStrategy.MAJORITY_VOTE
        assert result.provider in ("a", "b")  # they overlap more

    @pytest.mark.asyncio
    async def test_run_consensus_all_failed(self):
        agent = self._make_agent()
        probe_resp = ProbeResponse(
            results=[
                ProbeResultItem(provider="a", error="fail"),
                ProbeResultItem(provider="b", error="fail"),
            ],
            providers=2, successes=0,
        )
        result = await agent.run_consensus(probe_resp)
        assert not result.succeeded

    @pytest.mark.asyncio
    async def test_run_consensus_broadcasts_go_event(self):
        """Verify the consensus result is broadcast to the Go server."""
        agent = self._make_agent()
        mock_conv = MagicMock()
        mock_conv.get_discussion = MagicMock(return_value=None)
        mock_conv.synthesise_discussion = AsyncMock()
        mock_conv.task_id = "task-555"
        mock_go = AsyncMock()
        mock_conv._go = mock_go
        agent.conversation = mock_conv

        probe_resp = ProbeResponse(
            results=[ProbeResultItem(provider="grok", content="Answer")],
            providers=1, successes=1,
        )
        probe_resp._discussion_thread_id = "disc-999"

        await agent.run_consensus(probe_resp)

        mock_go.post_consensus_event.assert_awaited_once()
        call_args = mock_go.post_consensus_event.call_args
        assert call_args[0][0] == "task-555"  # task_id
        payload = call_args[0][1]
        assert payload["thread_id"] == "disc-999"
        assert "strategy" in payload
        assert "confidence" in payload

    @pytest.mark.asyncio
    async def test_run_consensus_broadcasts_judge_panel_metadata(self):
        """Multi-judge panel metadata is included in Go event broadcast."""
        agent = self._make_agent()

        # Set up a mock engine that returns a multi-judge result.
        probe_resp_mock = _make_judge_probe_response([
            {"provider": "openai", "winner": "grok",
             "confidence": 0.85, "scores": {"grok": 0.85}},
            {"provider": "anthropic", "winner": "grok",
             "confidence": 0.9, "scores": {"grok": 0.9}},
        ])
        mock_probe = AsyncMock(return_value=probe_resp_mock)
        agent._consensus_engine = ConsensusEngine(llm_probe=mock_probe, min_judges=2)

        mock_conv = MagicMock()
        mock_conv.get_discussion = MagicMock(return_value=None)
        mock_conv.synthesise_discussion = AsyncMock()
        mock_conv.task_id = "task-777"
        mock_go = AsyncMock()
        mock_conv._go = mock_go
        agent.conversation = mock_conv

        probe_resp = ProbeResponse(
            results=[ProbeResultItem(provider="grok", content="Answer")],
            providers=1, successes=1,
        )
        probe_resp._discussion_thread_id = "disc-panel"

        result = await agent.run_consensus(probe_resp, strategy=ConsensusStrategy.MULTI_JUDGE_PANEL)
        assert result.strategy == ConsensusStrategy.MULTI_JUDGE_PANEL

        mock_go.post_consensus_event.assert_awaited_once()
        payload = mock_go.post_consensus_event.call_args[0][1]
        assert payload["strategy"] == "multi_judge_panel"
        assert payload["judge_count"] == 2
        assert "judge_agreement" in payload


# ── Go Client ────────────────────────────────────────────────────────────────


class TestGoClientConsensus:
    @pytest.mark.asyncio
    async def test_post_consensus_event(self):
        from mono.swarm.go_client import GoClient

        with patch("mono.swarm.go_client.get_config") as mock_cfg:
            mock_cfg.return_value = MagicMock(go_server_url="http://localhost:8080")
            client = GoClient(token="test-token")

        with patch("httpx.AsyncClient") as mock_http_cls:
            mock_resp = MagicMock()
            mock_resp.raise_for_status = MagicMock()
            mock_client_instance = AsyncMock()
            mock_client_instance.post = AsyncMock(return_value=mock_resp)
            mock_client_instance.__aenter__ = AsyncMock(return_value=mock_client_instance)
            mock_client_instance.__aexit__ = AsyncMock(return_value=False)
            mock_http_cls.return_value = mock_client_instance

            await client.post_consensus_event("task-123", {
                "strategy": "gpt_as_judge",
                "provider": "anthropic",
                "confidence": 0.92,
            })

            mock_client_instance.post.assert_awaited_once()
            call_args = mock_client_instance.post.call_args
            assert "/tasks/task-123/consensus" in call_args[0][0]
