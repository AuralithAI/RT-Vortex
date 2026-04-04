"""Phase 5 tests — Consensus Engine.

Covers:
- ConsensusStrategy enum values
- ConsensusResult dataclass and serialisation
- Token overlap utilities (_tokenise, _jaccard, pairwise_agreement)
- GPT-as-judge prompt construction
- ConsensusEngine.run() with all strategies:
  - PICK_BEST: single response, multiple responses
  - MAJORITY_VOTE: scoring, tie-breaking
  - GPT_AS_JUDGE: success, fallback on failure, JSON parsing
  - AUTO: strategy selection logic
- Agent.run_consensus() integration with DiscussionThread
- Agent.consensus_engine lazy initialisation
- Go client post_consensus_event()
"""

import json
import pytest
from unittest.mock import AsyncMock, MagicMock, patch

from mono.swarm.consensus import (
    ConsensusEngine,
    ConsensusResult,
    ConsensusStrategy,
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
        assert ConsensusStrategy.AUTO.value == "auto"

    def test_string_enum(self):
        assert str(ConsensusStrategy.PICK_BEST) == "ConsensusStrategy.PICK_BEST"
        assert ConsensusStrategy("pick_best") == ConsensusStrategy.PICK_BEST


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
    async def test_auto_disagreement_with_llm_uses_judge(self):
        """When responses disagree and LLM is available, AUTO uses GPT_AS_JUDGE."""
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
        engine = ConsensusEngine(llm_complete=mock_llm, agreement_threshold=0.9)

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
        engine = ConsensusEngine(llm_complete=None, agreement_threshold=0.9)
        thread = _make_thread(
            _make_response("grok", content="Completely different approach one"),
            _make_response("anthropic", content="Totally unrelated approach two"),
        )
        result = await engine.run(thread, ConsensusStrategy.AUTO)
        assert result.strategy == ConsensusStrategy.MAJORITY_VOTE


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
