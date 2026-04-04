"""Tests for the multi-LLM probe Python client (Phase 3).

These are unit tests — they mock the HTTP layer and test:
- ProbeResultItem construction and properties
- ProbeResponse construction and convenience methods
- llm_probe() HTTP call and response parsing
- Agent.probe_and_gather() method
- Agent.pick_best_probe_result() default strategy
"""

from __future__ import annotations

import json
from unittest.mock import AsyncMock, MagicMock, patch

import pytest

from mono.swarm.sdk.go_llm_client import (
    ProbeResponse,
    ProbeResultItem,
    llm_probe,
)
from mono.swarm.sdk.agent import Agent, AgentConfig


# ── ProbeResultItem Tests ────────────────────────────────────────────────────


class TestProbeResultItem:
    """Test the ProbeResultItem dataclass."""

    def test_from_dict_success(self):
        d = {
            "provider": "grok",
            "model": "grok-3",
            "content": "Hello from Grok",
            "finish_reason": "stop",
            "usage": {"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30},
            "latency_ms": 150,
        }
        item = ProbeResultItem.from_dict(d)
        assert item.provider == "grok"
        assert item.model == "grok-3"
        assert item.content == "Hello from Grok"
        assert item.finish_reason == "stop"
        assert item.latency_ms == 150
        assert item.error == ""
        assert item.succeeded is True

    def test_from_dict_with_error(self):
        d = {
            "provider": "anthropic",
            "model": "",
            "content": "",
            "error": "rate limited",
            "latency_ms": 50,
        }
        item = ProbeResultItem.from_dict(d)
        assert item.provider == "anthropic"
        assert item.error == "rate limited"
        assert item.succeeded is False
        assert item.content == ""

    def test_from_dict_empty(self):
        item = ProbeResultItem.from_dict({})
        assert item.provider == ""
        assert item.model == ""
        assert item.succeeded is True  # no error = success
        assert item.latency_ms == 0

    def test_from_dict_with_tool_calls(self):
        d = {
            "provider": "openai",
            "model": "gpt-4o",
            "content": "",
            "finish_reason": "tool_calls",
            "tool_calls": [
                {"id": "call_1", "type": "function", "function": {"name": "read_file", "arguments": "{}"}}
            ],
            "latency_ms": 200,
        }
        item = ProbeResultItem.from_dict(d)
        assert len(item.tool_calls) == 1
        assert item.tool_calls[0]["function"]["name"] == "read_file"

    def test_succeeded_property(self):
        assert ProbeResultItem(provider="a", content="ok").succeeded is True
        assert ProbeResultItem(provider="a", error="fail").succeeded is False


# ── ProbeResponse Tests ──────────────────────────────────────────────────────


class TestProbeResponse:
    """Test the ProbeResponse dataclass."""

    def _sample_response_dict(self) -> dict:
        return {
            "results": [
                {
                    "provider": "grok",
                    "model": "grok-3",
                    "content": "Grok says hello",
                    "finish_reason": "stop",
                    "usage": {"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30},
                    "latency_ms": 100,
                },
                {
                    "provider": "anthropic",
                    "model": "claude-sonnet-4-20250514",
                    "content": "",
                    "error": "rate limited",
                    "latency_ms": 50,
                },
                {
                    "provider": "openai",
                    "model": "gpt-4o",
                    "content": "GPT says hello",
                    "finish_reason": "stop",
                    "usage": {"prompt_tokens": 10, "completion_tokens": 25, "total_tokens": 35},
                    "latency_ms": 200,
                },
            ],
            "total_ms": 220,
            "providers": 3,
            "successes": 2,
            "agent_role": "orchestrator",
        }

    def test_from_dict(self):
        resp = ProbeResponse.from_dict(self._sample_response_dict())
        assert resp.providers == 3
        assert resp.successes == 2
        assert resp.total_ms == 220
        assert resp.agent_role == "orchestrator"
        assert len(resp.results) == 3

    def test_successful_results(self):
        resp = ProbeResponse.from_dict(self._sample_response_dict())
        successful = resp.successful_results
        assert len(successful) == 2
        assert successful[0].provider == "grok"
        assert successful[1].provider == "openai"

    def test_best_result(self):
        resp = ProbeResponse.from_dict(self._sample_response_dict())
        best = resp.best_result
        assert best is not None
        assert best.provider == "grok"  # first successful in priority order

    def test_best_result_all_failed(self):
        d = {
            "results": [
                {"provider": "grok", "error": "fail1", "latency_ms": 10},
                {"provider": "openai", "error": "fail2", "latency_ms": 20},
            ],
            "providers": 2,
            "successes": 0,
        }
        resp = ProbeResponse.from_dict(d)
        assert resp.best_result is None

    def test_all_contents(self):
        resp = ProbeResponse.from_dict(self._sample_response_dict())
        contents = resp.all_contents
        assert len(contents) == 2
        assert "Grok says hello" in contents
        assert "GPT says hello" in contents

    def test_from_dict_empty(self):
        resp = ProbeResponse.from_dict({})
        assert resp.providers == 0
        assert resp.successes == 0
        assert len(resp.results) == 0
        assert resp.best_result is None
        assert resp.all_contents == []


# ── llm_probe() Tests ────────────────────────────────────────────────────────


class TestLLMProbe:
    """Test the llm_probe async function (mocked HTTP)."""

    @pytest.mark.asyncio
    async def test_llm_probe_success(self):
        """Verify llm_probe parses the Go response correctly."""
        mock_json = {
            "results": [
                {
                    "provider": "grok",
                    "model": "grok-3",
                    "content": "test response",
                    "finish_reason": "stop",
                    "usage": {"prompt_tokens": 5, "completion_tokens": 10, "total_tokens": 15},
                    "latency_ms": 100,
                },
            ],
            "total_ms": 110,
            "providers": 1,
            "successes": 1,
            "agent_role": "senior_dev",
        }

        mock_response = MagicMock()
        mock_response.json.return_value = mock_json
        mock_response.raise_for_status = MagicMock()

        mock_client = AsyncMock()
        mock_client.post.return_value = mock_response
        mock_client.__aenter__ = AsyncMock(return_value=mock_client)
        mock_client.__aexit__ = AsyncMock(return_value=False)

        with patch("mono.swarm.sdk.go_llm_client.httpx.AsyncClient", return_value=mock_client):
            with patch("mono.swarm.sdk.go_llm_client.get_config") as mock_cfg:
                mock_cfg.return_value = MagicMock(
                    go_server_url="http://localhost:8080",
                    llm_max_tokens=4096,
                    llm_timeout=120.0,
                )
                resp = await llm_probe(
                    messages=[{"role": "user", "content": "hello"}],
                    agent_token="test-token",
                    agent_role="senior_dev",
                )

        assert isinstance(resp, ProbeResponse)
        assert resp.providers == 1
        assert resp.successes == 1
        assert resp.total_ms == 110
        assert len(resp.results) == 1
        assert resp.results[0].provider == "grok"
        assert resp.results[0].content == "test response"

    @pytest.mark.asyncio
    async def test_llm_probe_sends_correct_payload(self):
        """Verify the HTTP payload includes all probe parameters."""
        mock_response = MagicMock()
        mock_response.json.return_value = {"results": [], "providers": 0, "successes": 0}
        mock_response.raise_for_status = MagicMock()

        mock_client = AsyncMock()
        mock_client.post.return_value = mock_response
        mock_client.__aenter__ = AsyncMock(return_value=mock_client)
        mock_client.__aexit__ = AsyncMock(return_value=False)

        with patch("mono.swarm.sdk.go_llm_client.httpx.AsyncClient", return_value=mock_client):
            with patch("mono.swarm.sdk.go_llm_client.get_config") as mock_cfg:
                mock_cfg.return_value = MagicMock(
                    go_server_url="http://localhost:8080",
                    llm_max_tokens=4096,
                    llm_timeout=120.0,
                )
                await llm_probe(
                    messages=[{"role": "user", "content": "test"}],
                    agent_token="tok",
                    agent_role="architect",
                    action_type="reasoning",
                    num_models=3,
                    max_tokens=2048,
                )

        # Check the POST was called with the right URL and payload.
        mock_client.post.assert_called_once()
        call_args = mock_client.post.call_args
        assert "/internal/swarm/llm/probe" in call_args[1].get("url", call_args[0][0] if call_args[0] else "")

        payload = call_args[1].get("json", call_args.kwargs.get("json", {}))
        assert payload["agent_role"] == "architect"
        assert payload["action_type"] == "reasoning"
        assert payload["num_models"] == 3
        assert payload["max_tokens"] == 2048


# ── Agent.probe_and_gather() Tests ──────────────────────────────────────────


class TestAgentProbe:
    """Test the Agent.probe_and_gather() method."""

    def _make_agent(self) -> Agent:
        agent = Agent(
            agent_id="test-agent-1",
            role="senior_dev",
            team_id="team-1",
            agent_config=AgentConfig(go_base_url="http://localhost:8080"),
        )
        agent.token = "fake-jwt-token"
        return agent

    @pytest.mark.asyncio
    async def test_probe_and_gather_success(self):
        agent = self._make_agent()

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

        assert isinstance(resp, ProbeResponse)
        assert resp.providers == 2
        assert resp.successes == 2
        assert len(resp.results) == 2

    @pytest.mark.asyncio
    async def test_probe_and_gather_not_registered(self):
        agent = Agent(
            agent_id="test-agent-2",
            role="qa",
            team_id="team-1",
        )
        # token is None — not registered

        with pytest.raises(RuntimeError, match="must be registered"):
            await agent.probe_and_gather(
                messages=[{"role": "user", "content": "test"}],
            )

    @pytest.mark.asyncio
    async def test_probe_and_gather_broadcasts_to_conversation(self):
        """Phase 4 upgraded probe_and_gather to use DiscussionThread instead
        of raw append_thinking.  Verify the discussion lifecycle is driven."""
        agent = self._make_agent()
        mock_conv = AsyncMock()

        # open_discussion must return a mock thread with a thread_id
        from unittest.mock import MagicMock
        mock_thread = MagicMock()
        mock_thread.thread_id = "disc-001"
        mock_conv.open_discussion = AsyncMock(return_value=mock_thread)
        mock_conv.add_discussion_response = AsyncMock()
        mock_conv.complete_discussion = AsyncMock()

        agent.conversation = mock_conv

        mock_probe_resp = ProbeResponse(
            results=[
                ProbeResultItem(provider="grok", model="grok-3", content="Grok answer", latency_ms=100),
                ProbeResultItem(provider="anthropic", model="claude", error="fail", latency_ms=50),
                ProbeResultItem(provider="openai", model="gpt-4o", content="GPT answer", latency_ms=200),
            ],
            total_ms=210,
            providers=3,
            successes=2,
            agent_role="senior_dev",
        )

        with patch("mono.swarm.sdk.agent.llm_probe", new_callable=AsyncMock, return_value=mock_probe_resp):
            await agent.probe_and_gather(
                messages=[{"role": "user", "content": "test"}],
            )

        # Discussion thread should be opened once.
        mock_conv.open_discussion.assert_awaited_once()

        # All 3 results (including the failure) are recorded in the thread.
        assert mock_conv.add_discussion_response.await_count == 3

        # Thread is marked complete.
        mock_conv.complete_discussion.assert_awaited_once_with("disc-001")

    @pytest.mark.asyncio
    async def test_probe_and_gather_with_action_type(self):
        agent = self._make_agent()

        mock_probe_resp = ProbeResponse(
            results=[ProbeResultItem(provider="grok", content="answer")],
            providers=1,
            successes=1,
        )

        with patch("mono.swarm.sdk.agent.llm_probe", new_callable=AsyncMock, return_value=mock_probe_resp) as mock_probe:
            await agent.probe_and_gather(
                messages=[{"role": "user", "content": "test"}],
                action_type="reasoning",
                num_models=2,
            )

        # Verify llm_probe was called with the right params.
        mock_probe.assert_called_once()
        call_kwargs = mock_probe.call_args[1]
        assert call_kwargs["action_type"] == "reasoning"
        assert call_kwargs["num_models"] == 2
        assert call_kwargs["agent_role"] == "senior_dev"

    def test_pick_best_probe_result(self):
        agent = self._make_agent()

        resp = ProbeResponse(
            results=[
                ProbeResultItem(provider="grok", content="Grok answer", latency_ms=100),
                ProbeResultItem(provider="anthropic", error="fail"),
                ProbeResultItem(provider="openai", content="GPT answer", latency_ms=200),
            ],
            providers=3,
            successes=2,
        )

        best = agent.pick_best_probe_result(resp)
        assert best == "Grok answer"  # first successful

    def test_pick_best_probe_result_all_failed(self):
        agent = self._make_agent()

        resp = ProbeResponse(
            results=[
                ProbeResultItem(provider="grok", error="fail1"),
                ProbeResultItem(provider="openai", error="fail2"),
            ],
            providers=2,
            successes=0,
        )

        best = agent.pick_best_probe_result(resp)
        assert best == ""
