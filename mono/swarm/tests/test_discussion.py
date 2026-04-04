"""Tests for the multi-LLM Discussion Protocol (Phase 4).

These are unit tests — they mock the Go client and test:
- DiscussionThread lifecycle (open → responses → complete → synthesise)
- ProviderResponse dataclass and properties
- DiscussionStatus enum transitions
- SharedConversation discussion methods (open, add_response, complete, synthesise)
- Agent.probe_and_gather() discussion thread integration
- Agent.pick_best_and_synthesise() discussion synthesis
- Summary rendering with probe_response and discussion_synthesis kinds
"""

from __future__ import annotations

import asyncio
import time
from unittest.mock import AsyncMock, MagicMock, patch

import pytest

from mono.swarm.conversation import (
    AgentMessage,
    DiscussionStatus,
    DiscussionThread,
    ProviderResponse,
    SharedConversation,
)
from mono.swarm.sdk.agent import Agent, AgentConfig
from mono.swarm.sdk.go_llm_client import ProbeResponse, ProbeResultItem


# ── ProviderResponse Tests ───────────────────────────────────────────────────


class TestProviderResponse:
    """Test the ProviderResponse dataclass."""

    def test_basic_construction(self):
        r = ProviderResponse(
            provider="grok",
            model="grok-3",
            content="Hello",
            latency_ms=100,
        )
        assert r.provider == "grok"
        assert r.model == "grok-3"
        assert r.content == "Hello"
        assert r.latency_ms == 100
        assert r.succeeded is True
        assert r.error == ""

    def test_failed_response(self):
        r = ProviderResponse(
            provider="anthropic",
            error="rate limited",
        )
        assert r.succeeded is False
        assert r.error == "rate limited"
        assert r.content == ""

    def test_to_dict(self):
        r = ProviderResponse(
            provider="openai",
            model="gpt-4o",
            content="GPT says hello",
            latency_ms=200,
            finish_reason="stop",
            token_usage={"prompt_tokens": 10, "completion_tokens": 20},
        )
        d = r.to_dict()
        assert d["provider"] == "openai"
        assert d["model"] == "gpt-4o"
        assert d["content"] == "GPT says hello"
        assert d["latency_ms"] == 200
        assert d["finish_reason"] == "stop"
        assert d["token_usage"]["prompt_tokens"] == 10
        assert d["error"] == ""
        assert "timestamp" in d


# ── DiscussionThread Tests ───────────────────────────────────────────────────


class TestDiscussionThread:
    """Test the DiscussionThread dataclass and lifecycle."""

    def test_creation(self):
        t = DiscussionThread(
            agent_id="agent-1",
            agent_role="senior_dev",
            topic="Review auth module",
        )
        assert t.agent_id == "agent-1"
        assert t.agent_role == "senior_dev"
        assert t.topic == "Review auth module"
        assert t.status == DiscussionStatus.OPEN
        assert t.provider_count == 0
        assert t.success_count == 0
        assert t.synthesis == ""
        assert len(t.thread_id) == 16

    def test_add_response(self):
        t = DiscussionThread(agent_id="a", agent_role="r")
        r1 = ProviderResponse(provider="grok", content="hello", latency_ms=100)
        r2 = ProviderResponse(provider="anthropic", error="rate limited")
        t.add_response(r1)
        t.add_response(r2)
        assert t.provider_count == 2
        assert t.success_count == 1
        assert t.all_contents == ["hello"]

    def test_successful_responses(self):
        t = DiscussionThread(agent_id="a", agent_role="r")
        t.add_response(ProviderResponse(provider="grok", content="a"))
        t.add_response(ProviderResponse(provider="anthropic", error="fail"))
        t.add_response(ProviderResponse(provider="openai", content="b"))
        assert len(t.successful_responses) == 2
        assert t.successful_responses[0].provider == "grok"
        assert t.successful_responses[1].provider == "openai"

    def test_complete(self):
        t = DiscussionThread(agent_id="a", agent_role="r")
        t.add_response(ProviderResponse(provider="grok", content="hello"))
        assert t.completed_at == 0.0
        t.complete()
        assert t.status == DiscussionStatus.COMPLETE
        assert t.completed_at > 0

    def test_synthesise(self):
        t = DiscussionThread(agent_id="a", agent_role="r")
        t.add_response(ProviderResponse(provider="grok", content="hello"))
        t.synthesise("hello is the answer", "grok")
        assert t.status == DiscussionStatus.SYNTHESISED
        assert t.synthesis == "hello is the answer"
        assert t.synthesis_provider == "grok"
        assert t.completed_at > 0

    def test_to_dict(self):
        t = DiscussionThread(
            agent_id="a",
            agent_role="r",
            topic="test",
            action_type="reasoning",
        )
        t.add_response(ProviderResponse(provider="grok", content="hi", latency_ms=100))
        d = t.to_dict()
        assert d["agent_id"] == "a"
        assert d["topic"] == "test"
        assert d["action_type"] == "reasoning"
        assert d["status"] == "open"
        assert d["provider_count"] == 1
        assert d["success_count"] == 1
        assert len(d["responses"]) == 1
        assert d["responses"][0]["provider"] == "grok"

    def test_summary_for_prompt(self):
        t = DiscussionThread(agent_id="a", agent_role="r", topic="test query")
        t.add_response(ProviderResponse(provider="grok", model="grok-3", content="Grok answer", latency_ms=100))
        t.add_response(ProviderResponse(provider="anthropic", model="claude", error="rate limited"))
        t.add_response(ProviderResponse(provider="openai", model="gpt-4o", content="GPT answer", latency_ms=200))
        summary = t.summary_for_prompt()
        assert "### Multi-LLM Discussion: test query" in summary
        assert "grok/grok-3" in summary
        assert "Grok answer" in summary
        assert "❌" in summary
        assert "rate limited" in summary
        assert "gpt-4o" in summary
        assert "GPT answer" in summary

    def test_summary_with_synthesis(self):
        t = DiscussionThread(agent_id="a", agent_role="r", topic="test")
        t.add_response(ProviderResponse(provider="grok", content="answer"))
        t.synthesise("answer", "grok")
        summary = t.summary_for_prompt()
        assert "**Selected answer** (grok)" in summary


# ── DiscussionStatus Tests ───────────────────────────────────────────────────


class TestDiscussionStatus:
    """Test the DiscussionStatus enum."""

    def test_values(self):
        assert DiscussionStatus.OPEN.value == "open"
        assert DiscussionStatus.COMPLETE.value == "complete"
        assert DiscussionStatus.SYNTHESISED.value == "synthesised"

    def test_string_enum(self):
        assert DiscussionStatus.OPEN == "open"
        assert DiscussionStatus.COMPLETE == "complete"


# ── SharedConversation Discussion Tests ──────────────────────────────────────


class TestSharedConversationDiscussion:
    """Test the discussion thread methods on SharedConversation."""

    def _make_conv(self) -> SharedConversation:
        mock_go = AsyncMock()
        mock_go.post_agent_message = AsyncMock()
        mock_go.post_discussion_event = AsyncMock()
        return SharedConversation(task_id="task-1", go_client=mock_go)

    @pytest.mark.asyncio
    async def test_open_discussion(self):
        conv = self._make_conv()
        thread = await conv.open_discussion(
            agent_id="agent-1",
            agent_role="senior_dev",
            topic="Review the auth module",
        )
        assert isinstance(thread, DiscussionThread)
        assert thread.agent_id == "agent-1"
        assert thread.agent_role == "senior_dev"
        assert thread.topic == "Review the auth module"
        assert thread.status == DiscussionStatus.OPEN
        assert conv.discussion_count == 1

        # Verify Go client was called.
        conv._go.post_discussion_event.assert_awaited_once()
        call_args = conv._go.post_discussion_event.call_args
        assert call_args[0][0] == "task-1"
        assert call_args[0][1]["event"] == "thread_opened"

    @pytest.mark.asyncio
    async def test_add_discussion_response(self):
        conv = self._make_conv()
        thread = await conv.open_discussion(
            agent_id="a", agent_role="r", topic="test"
        )

        resp = await conv.add_discussion_response(
            thread_id=thread.thread_id,
            provider="grok",
            model="grok-3",
            content="Grok answer",
            latency_ms=100,
        )

        assert resp is not None
        assert resp.provider == "grok"
        assert resp.content == "Grok answer"
        assert thread.provider_count == 1

        # Should also append a probe_response message to the flat log.
        assert conv.message_count == 1
        msg = conv.get_messages()[0]
        assert msg.kind == "probe_response"
        assert msg.metadata["provider"] == "grok"
        assert msg.metadata["thread_id"] == thread.thread_id

    @pytest.mark.asyncio
    async def test_add_discussion_response_with_error(self):
        conv = self._make_conv()
        thread = await conv.open_discussion(
            agent_id="a", agent_role="r", topic="test"
        )

        resp = await conv.add_discussion_response(
            thread_id=thread.thread_id,
            provider="anthropic",
            model="claude",
            content="",
            error="rate limited",
        )

        assert resp is not None
        assert not resp.succeeded
        # Failed responses should NOT be appended to the flat message log.
        assert conv.message_count == 0

    @pytest.mark.asyncio
    async def test_add_discussion_response_unknown_thread(self):
        conv = self._make_conv()
        resp = await conv.add_discussion_response(
            thread_id="nonexistent",
            provider="grok",
            model="grok-3",
            content="hello",
        )
        assert resp is None

    @pytest.mark.asyncio
    async def test_complete_discussion(self):
        conv = self._make_conv()
        thread = await conv.open_discussion(
            agent_id="a", agent_role="r", topic="test"
        )
        await conv.add_discussion_response(
            thread_id=thread.thread_id,
            provider="grok", model="grok-3", content="hello", latency_ms=100,
        )

        result = await conv.complete_discussion(thread.thread_id)
        assert result is not None
        assert result.status == DiscussionStatus.COMPLETE

        # Verify broadcast.
        calls = conv._go.post_discussion_event.call_args_list
        # Last call should be thread_completed.
        last_call = calls[-1]
        assert last_call[0][1]["event"] == "thread_completed"
        assert last_call[0][1]["provider_count"] == 1

    @pytest.mark.asyncio
    async def test_complete_discussion_unknown_thread(self):
        conv = self._make_conv()
        result = await conv.complete_discussion("nonexistent")
        assert result is None

    @pytest.mark.asyncio
    async def test_synthesise_discussion(self):
        conv = self._make_conv()
        thread = await conv.open_discussion(
            agent_id="a", agent_role="r", topic="test"
        )
        await conv.add_discussion_response(
            thread_id=thread.thread_id,
            provider="grok", model="grok-3", content="hello", latency_ms=100,
        )

        result = await conv.synthesise_discussion(
            thread.thread_id, "hello is best", "grok"
        )
        assert result is not None
        assert result.status == DiscussionStatus.SYNTHESISED
        assert result.synthesis == "hello is best"
        assert result.synthesis_provider == "grok"

        # Should append a discussion_synthesis message.
        msgs = [m for m in conv.get_messages() if m.kind == "discussion_synthesis"]
        assert len(msgs) == 1
        assert msgs[0].content == "hello is best"

    @pytest.mark.asyncio
    async def test_synthesise_discussion_unknown_thread(self):
        conv = self._make_conv()
        result = await conv.synthesise_discussion("nonexistent", "content")
        assert result is None

    @pytest.mark.asyncio
    async def test_get_discussion(self):
        conv = self._make_conv()
        thread = await conv.open_discussion(
            agent_id="a", agent_role="r", topic="test"
        )
        found = conv.get_discussion(thread.thread_id)
        assert found is thread

    @pytest.mark.asyncio
    async def test_get_discussions(self):
        conv = self._make_conv()
        t1 = await conv.open_discussion(agent_id="a", agent_role="r", topic="t1")
        t2 = await conv.open_discussion(agent_id="a", agent_role="r", topic="t2")
        discussions = conv.get_discussions()
        assert len(discussions) == 2
        ids = {d.thread_id for d in discussions}
        assert t1.thread_id in ids
        assert t2.thread_id in ids

    @pytest.mark.asyncio
    async def test_get_discussion_summary(self):
        conv = self._make_conv()
        thread = await conv.open_discussion(
            agent_id="a", agent_role="r", topic="my question"
        )
        await conv.add_discussion_response(
            thread_id=thread.thread_id,
            provider="grok", model="grok-3", content="answer", latency_ms=100,
        )
        summary = conv.get_discussion_summary(thread.thread_id)
        assert "### Multi-LLM Discussion: my question" in summary
        assert "grok" in summary

    def test_get_discussion_summary_not_found(self):
        conv = self._make_conv()
        assert conv.get_discussion_summary("nonexistent") == ""

    @pytest.mark.asyncio
    async def test_full_lifecycle(self):
        """Test the complete discussion lifecycle: open → responses → complete → synthesise."""
        conv = self._make_conv()

        # 1. Open
        thread = await conv.open_discussion(
            agent_id="agent-1",
            agent_role="senior_dev",
            topic="Review the login flow",
            action_type="reasoning",
        )
        assert thread.status == DiscussionStatus.OPEN

        # 2. Add provider responses
        await conv.add_discussion_response(
            thread_id=thread.thread_id,
            provider="grok", model="grok-3",
            content="Grok: The login flow looks correct.",
            latency_ms=100, finish_reason="stop",
        )
        await conv.add_discussion_response(
            thread_id=thread.thread_id,
            provider="anthropic", model="claude",
            content="", error="rate limited",
        )
        await conv.add_discussion_response(
            thread_id=thread.thread_id,
            provider="openai", model="gpt-4o",
            content="GPT: I found a potential CSRF issue.",
            latency_ms=200, finish_reason="stop",
        )
        assert thread.provider_count == 3
        assert thread.success_count == 2

        # 3. Complete
        await conv.complete_discussion(thread.thread_id)
        assert thread.status == DiscussionStatus.COMPLETE

        # 4. Synthesise
        await conv.synthesise_discussion(
            thread.thread_id,
            "GPT: I found a potential CSRF issue.",
            "openai",
        )
        assert thread.status == DiscussionStatus.SYNTHESISED
        assert thread.synthesis_provider == "openai"

        # Verify flat messages: 2 probe_responses (successful only) + 1 synthesis
        kinds = [m.kind for m in conv.get_messages()]
        assert kinds.count("probe_response") == 2
        assert kinds.count("discussion_synthesis") == 1


# ── Summary Rendering Tests ──────────────────────────────────────────────────


class TestSummaryRendering:
    """Test that get_summary_for correctly renders new message kinds."""

    def _make_conv_sync(self) -> SharedConversation:
        mock_go = AsyncMock()
        mock_go.post_agent_message = AsyncMock()
        mock_go.post_discussion_event = AsyncMock()
        return SharedConversation(task_id="task-1", go_client=mock_go)

    @pytest.mark.asyncio
    async def test_probe_response_in_summary(self):
        conv = self._make_conv_sync()
        # Manually append a probe_response message.
        await conv.append(AgentMessage(
            agent_id="agent-1234",
            agent_role="senior_dev",
            kind="probe_response",
            content="Grok says hello",
            metadata={"provider": "grok", "model": "grok-3"},
        ))
        summary = conv.get_summary_for("qa")
        assert "🔀" in summary
        assert "grok/grok-3" in summary
        assert "Grok says hello" in summary

    @pytest.mark.asyncio
    async def test_discussion_synthesis_in_summary(self):
        conv = self._make_conv_sync()
        await conv.append(AgentMessage(
            agent_id="agent-1234",
            agent_role="senior_dev",
            kind="discussion_synthesis",
            content="Best answer selected",
        ))
        summary = conv.get_summary_for("qa")
        assert "🏆" in summary
        assert "Best answer selected" in summary


# ── Agent Discussion Integration Tests ───────────────────────────────────────


class TestAgentDiscussionIntegration:
    """Test that Agent.probe_and_gather() creates discussion threads."""

    def _make_agent(self) -> Agent:
        agent = Agent(
            agent_id="test-agent-1",
            role="senior_dev",
            team_id="team-1",
            agent_config=AgentConfig(go_base_url="http://localhost:8080"),
        )
        agent.token = "fake-jwt-token"
        return agent

    def _make_conversation(self) -> SharedConversation:
        mock_go = AsyncMock()
        mock_go.post_agent_message = AsyncMock()
        mock_go.post_discussion_event = AsyncMock()
        return SharedConversation(task_id="task-1", go_client=mock_go)

    @pytest.mark.asyncio
    async def test_probe_creates_discussion_thread(self):
        agent = self._make_agent()
        conv = self._make_conversation()
        agent.conversation = conv

        mock_probe_resp = ProbeResponse(
            results=[
                ProbeResultItem(provider="grok", model="grok-3", content="Grok answer", latency_ms=100),
                ProbeResultItem(provider="openai", model="gpt-4o", content="GPT answer", latency_ms=200),
            ],
            total_ms=210,
            providers=2,
            successes=2,
            agent_role="senior_dev",
        )

        with patch("mono.swarm.sdk.agent.llm_probe", new_callable=AsyncMock, return_value=mock_probe_resp):
            resp = await agent.probe_and_gather(
                messages=[{"role": "user", "content": "review this code"}],
            )

        # Should have created a discussion thread.
        assert conv.discussion_count == 1
        thread = conv.get_discussions()[0]
        assert thread.agent_id == "test-agent-1"
        assert thread.agent_role == "senior_dev"
        assert thread.status == DiscussionStatus.COMPLETE  # auto-completed
        assert thread.provider_count == 2
        assert thread.success_count == 2
        assert "review this code" in thread.topic

    @pytest.mark.asyncio
    async def test_probe_without_conversation_no_discussion(self):
        agent = self._make_agent()
        # No conversation attached

        mock_probe_resp = ProbeResponse(
            results=[ProbeResultItem(provider="grok", content="answer")],
            providers=1,
            successes=1,
        )

        with patch("mono.swarm.sdk.agent.llm_probe", new_callable=AsyncMock, return_value=mock_probe_resp):
            resp = await agent.probe_and_gather(
                messages=[{"role": "user", "content": "test"}],
            )

        assert resp.providers == 1
        # No conversation = no discussion thread created

    @pytest.mark.asyncio
    async def test_probe_records_failed_responses_in_thread(self):
        agent = self._make_agent()
        conv = self._make_conversation()
        agent.conversation = conv

        mock_probe_resp = ProbeResponse(
            results=[
                ProbeResultItem(provider="grok", model="grok-3", content="answer", latency_ms=100),
                ProbeResultItem(provider="anthropic", model="claude", content="", error="rate limited", latency_ms=50),
            ],
            total_ms=110,
            providers=2,
            successes=1,
            agent_role="senior_dev",
        )

        with patch("mono.swarm.sdk.agent.llm_probe", new_callable=AsyncMock, return_value=mock_probe_resp):
            await agent.probe_and_gather(
                messages=[{"role": "user", "content": "test"}],
            )

        thread = conv.get_discussions()[0]
        assert thread.provider_count == 2
        assert thread.success_count == 1

        # Only successful responses should be in the flat message log.
        probe_msgs = [m for m in conv.get_messages() if m.kind == "probe_response"]
        assert len(probe_msgs) == 1
        assert probe_msgs[0].metadata["provider"] == "grok"

    @pytest.mark.asyncio
    async def test_pick_best_and_synthesise(self):
        agent = self._make_agent()
        conv = self._make_conversation()
        agent.conversation = conv

        mock_probe_resp = ProbeResponse(
            results=[
                ProbeResultItem(provider="grok", model="grok-3", content="Grok answer", latency_ms=100),
                ProbeResultItem(provider="openai", model="gpt-4o", content="GPT answer", latency_ms=200),
            ],
            total_ms=210,
            providers=2,
            successes=2,
            agent_role="senior_dev",
        )

        with patch("mono.swarm.sdk.agent.llm_probe", new_callable=AsyncMock, return_value=mock_probe_resp):
            resp = await agent.probe_and_gather(
                messages=[{"role": "user", "content": "test"}],
            )

        # Now synthesise.
        content = await agent.pick_best_and_synthesise(resp)
        assert content == "Grok answer"

        thread = conv.get_discussions()[0]
        assert thread.status == DiscussionStatus.SYNTHESISED
        assert thread.synthesis == "Grok answer"
        assert thread.synthesis_provider == "grok"

        # Check synthesis message in the flat log.
        synth_msgs = [m for m in conv.get_messages() if m.kind == "discussion_synthesis"]
        assert len(synth_msgs) == 1

    @pytest.mark.asyncio
    async def test_pick_best_and_synthesise_all_failed(self):
        agent = self._make_agent()
        conv = self._make_conversation()
        agent.conversation = conv

        mock_probe_resp = ProbeResponse(
            results=[
                ProbeResultItem(provider="grok", error="fail1"),
                ProbeResultItem(provider="openai", error="fail2"),
            ],
            total_ms=50,
            providers=2,
            successes=0,
        )

        with patch("mono.swarm.sdk.agent.llm_probe", new_callable=AsyncMock, return_value=mock_probe_resp):
            resp = await agent.probe_and_gather(
                messages=[{"role": "user", "content": "test"}],
            )

        content = await agent.pick_best_and_synthesise(resp)
        assert content == ""

        # Thread should NOT be synthesised (no good answer).
        thread = conv.get_discussions()[0]
        assert thread.status == DiscussionStatus.COMPLETE  # auto-completed but not synthesised

    @pytest.mark.asyncio
    async def test_discussion_broadcasts_to_go(self):
        """Verify that discussion events are forwarded to the Go client."""
        agent = self._make_agent()
        conv = self._make_conversation()
        agent.conversation = conv

        mock_probe_resp = ProbeResponse(
            results=[
                ProbeResultItem(provider="grok", content="answer", latency_ms=100),
            ],
            total_ms=110,
            providers=1,
            successes=1,
        )

        with patch("mono.swarm.sdk.agent.llm_probe", new_callable=AsyncMock, return_value=mock_probe_resp):
            resp = await agent.probe_and_gather(
                messages=[{"role": "user", "content": "test"}],
            )

        # The Go client should have received:
        # 1. thread_opened
        # 2. provider_response
        # 3. thread_completed
        discussion_calls = conv._go.post_discussion_event.call_args_list
        events = [c[0][1]["event"] for c in discussion_calls]
        assert "thread_opened" in events
        assert "provider_response" in events
        assert "thread_completed" in events
