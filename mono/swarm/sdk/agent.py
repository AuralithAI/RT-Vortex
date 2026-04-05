"""Agent base class — registration, tool execution, and the agentic loop.

Every agent role (orchestrator, senior_dev, architect, …) extends this class
and overrides :meth:`build_system_prompt` and :meth:`parse_result`.

The agent registers with the Go server on first use, obtaining a short-lived
JWT.  All subsequent LLM and task-management calls use that token.

If a :class:`~mono.swarm.conversation.SharedConversation` is attached, all
LLM responses and tool calls are broadcast to the shared conversation for
live UI display.

* **Memory hierarchy** — Every agent gets an :class:`AgentMemory` instance
  with STM/MTM/LTM tiers.  Memory context is injected into the system prompt
  and reflections run after each tool call.
"""

from __future__ import annotations

import logging
import socket
from dataclasses import dataclass, field
from typing import TYPE_CHECKING, Any

import httpx

from ..agents_config import get_config, _read_version
from ..consensus import ConsensusEngine, ConsensusResult, ConsensusStrategy
from .go_llm_client import llm_complete, llm_probe, ProbeResponse, ProbeResultItem
from .loop import agent_loop
from .tool import ToolDef

if TYPE_CHECKING:
    from ..conversation import SharedConversation
    from ..memory import AgentMemory
    from ..probe_tuning import AdaptiveProbeConfig
    from ..workspace import VirtualWorkspace

logger = logging.getLogger(__name__)


def _build_version() -> str:
    """Return the build version from ``mono/VERSION`` (same source as Makefile)."""
    return _read_version()


@dataclass
class AgentConfig:
    """Runtime configuration injected into every agent instance.

    Attributes:
        go_base_url: Base URL of the Go API server.
    """

    go_base_url: str = ""

    def __post_init__(self) -> None:
        if not self.go_base_url:
            self.go_base_url = get_config().go_server_url


@dataclass
class Task:
    """A task received from Go."""

    id: str
    repo_id: str
    description: str
    status: str = "submitted"
    plan_document: dict | None = None


@dataclass
class AgentResult:
    """Result returned by an agent after processing a task."""

    output: str = ""
    plan: dict | None = None
    diffs: list[dict] = field(default_factory=list)
    error: str | None = None


class Agent:
    """Base agent class. Each role overrides build_system_prompt and parse_result."""

    def __init__(self, agent_id: str, role: str, team_id: str, agent_config: AgentConfig | None = None):
        self.agent_id = agent_id
        self.role = role
        self.team_id = team_id
        self.config = agent_config or AgentConfig()
        self.token: str | None = None
        self.tools: list[ToolDef] = []
        self.conversation: SharedConversation | None = None
        self.workspace: VirtualWorkspace | None = None
        self.memory: AgentMemory | None = None
        self._consensus_engine: ConsensusEngine | None = None
        self._tier: str = "standard"
        self._elo_score: float = 1200.0
        self._probe_count: int = 3
        self._repo_id: str = ""
        self._adaptive_config: AdaptiveProbeConfig | None = None
        self._complexity_label: str = ""

    @property
    def tier(self) -> str:
        """The role's current ELO tier (standard, expert, restricted)."""
        return self._tier

    @property
    def elo_score(self) -> float:
        """The role's current ELO score for this repo."""
        return self._elo_score

    @property
    def probe_count(self) -> int:
        """Recommended number of LLM probes based on tier.

        restricted → 5 (more perspectives needed)
        standard   → 3 (balanced)
        expert     → 2 (trusted, lean probing)
        """
        return self._probe_count

    @property
    def consensus_engine(self) -> ConsensusEngine:
        """Lazy-initialised consensus engine.

        Creates the engine on first access with the agent's LLM credentials,
        so GPT_AS_JUDGE can make an LLM call via the Go proxy and
        MULTI_JUDGE_PANEL can fan out to all providers via the probe endpoint.
        """
        if self._consensus_engine is None:
            self._consensus_engine = ConsensusEngine(
                llm_complete=llm_complete,
                llm_probe=llm_probe,
                go_base_url=self.config.go_base_url,
                agent_token=self.token or "",
            )
        return self._consensus_engine

    async def register(self) -> None:
        """Register with Go and obtain a short-lived agent JWT.

        Sends the build version (from ``mono/VERSION``), hostname, and role
        to ``POST /internal/swarm/auth/register``.  The Go server validates
        the service secret (derived from the JWT signing key via the same
        SHA-256 algorithm both sides share) and returns a 3-hour JWT.

        The server may assign a canonical UUID for the agent, which replaces
        the original short-form ``agent_id``.

        If ``_repo_id`` is set before calling register, the Go server will
        look up the role's ELO tier for that repo and inject ``tier``,
        ``elo_score``, and ``probe_count`` into the response.
        """
        cfg = get_config()
        url = f"{self.config.go_base_url}/internal/swarm/auth/register"
        payload = {
            "agent_id": self.agent_id,
            "role": self.role,
            "team_id": self.team_id,
            "hostname": socket.gethostname(),
            "version": _build_version(),
        }
        # Include repo_id for role ELO tier injection.
        if self._repo_id:
            payload["repo_id"] = self._repo_id

        async with httpx.AsyncClient(timeout=30.0) as client:
            resp = await client.post(
                url,
                headers={"X-Service-Secret": cfg.service_secret},
                json=payload,
            )
            resp.raise_for_status()
            data = resp.json()
            self.token = data["access_token"]
            # The server may assign a canonical UUID; always prefer it.
            if data.get("agent_id"):
                self.agent_id = data["agent_id"]

            # Parse role ELO tier if injected by Go.
            if "tier" in data:
                self._tier = data["tier"]
                self._elo_score = data.get("elo_score", 1200.0)
                self._probe_count = data.get("probe_count", 3)
                logger.info(
                    "Agent %s registered with role ELO: tier=%s elo=%.0f probes=%d",
                    self.agent_id, self._tier, self._elo_score, self._probe_count,
                )
            else:
                logger.info(
                    "Agent %s registered (role=%s, team=%s)",
                    self.agent_id, self.role, self.team_id,
                )

    async def run(self, task: Task) -> AgentResult:
        """Execute the agentic loop for the given task.

        If the agent has not yet registered, it does so first. Then it builds
        a role-specific system prompt and hands off to :func:`agent_loop`,
        which iterates tool calls until the LLM signals completion.

        If a :attr:`conversation` is attached, the system prompt is augmented
        with a summary of what other agents have said so far, and every LLM
        response / tool call is broadcast to the conversation.

        If a :attr:`memory` is attached, memory context (STM scratchpad,
        MTM insights, LTM search) is injected into the system prompt.

        Args:
            task: The task to execute, received from the Go server.

        Returns:
            An :class:`AgentResult` with the role-specific output.
        """
        # Set repo_id before registration so the Go server can inject
        # role-level ELO tier, score, and probe count for this (role, repo).
        self._repo_id = task.repo_id

        if not self.token:
            await self.register()

        system_prompt = self.build_system_prompt(task)

        # Inject role ELO context so the LLM knows its own performance tier.
        if self._tier and self._tier != "standard":
            system_prompt += (
                f"\n\n=== Role Performance Tier ===\n"
                f"Your role ({self.role}) is currently rated as: {self._tier.upper()}\n"
                f"ELO score: {self._elo_score:.0f}  |  Training probes: {self._probe_count}\n"
            )
            if self._tier == "restricted":
                system_prompt += (
                    "⚠ Your role is on a RESTRICTED tier. Take extra care with "
                    "accuracy and follow instructions precisely. Additional "
                    "verification probes will be used.\n"
                )
            elif self._tier == "expert":
                system_prompt += (
                    "★ Your role is on the EXPERT tier. You have earned reduced "
                    "oversight. Maintain high quality to keep this status.\n"
                )

        # Inject conversation context so this agent sees what others did.
        if self.conversation and self.conversation.message_count > 0:
            summary = self.conversation.get_summary_for(self.role)
            if summary:
                system_prompt += f"\n\n{summary}\n"

        # Inject memory context (STM observations + MTM insights + LTM search).
        if self.memory:
            try:
                mem_context = await self.memory.build_memory_context(
                    task_description=task.description
                )
                if mem_context:
                    system_prompt += (
                        f"\n\n=== Memory Context ===\n{mem_context}\n"
                    )
            except Exception as e:
                logger.warning("Agent %s: memory context build failed: %s",
                               self.agent_id, e)

        initial_message = f"Task: {task.description}\nRepo: {task.repo_id}"

        messages = await agent_loop(
            system_prompt=system_prompt,
            tools=self.tools,
            tool_executor=self.execute_tool,
            agent_token=self.token or "",
            go_base_url=self.config.go_base_url,
            initial_message=initial_message,
            agent_role=self.role,
            agent_id=self.agent_id,
            conversation=self.conversation,
            memory=self.memory,
        )

        return self.parse_result(messages)

    async def execute_tool(self, name: str, args: dict) -> Any:
        """Dispatch a tool call by name.

        Args:
            name: Tool function name (must match a registered :class:`ToolDef`).
            args: Keyword arguments parsed from the LLM's function call.

        Returns:
            The tool's return value (typically a JSON-serialisable string).

        Raises:
            ValueError: If no tool with the given name is registered.
        """
        for t in self.tools:
            if t.name == name:
                return await t.fn(**args)
        raise ValueError(f"Unknown tool: {name}")

    # ── Multi-LLM Probe ─────────────────────────────────────────────────

    async def probe_and_gather(
        self,
        messages: list[dict],
        *,
        action_type: str = "",
        num_models: int = 0,
        tools: list[dict] | None = None,
        max_tokens: int | None = None,
    ) -> ProbeResponse:
        """Probe multiple LLMs in parallel and return all responses.

        This is the agent-level entry point for Perplexity-style multi-LLM
        querying.  The agent sends the same messages to all providers in its
        role's priority matrix, and each provider's response is returned
        separately.

        The agent (or a future consensus engine) can then:
        - Pick the best single response
        - Synthesise a combined answer from multiple perspectives
        - Use GPT (always last) as the final judge

        If a :attr:`conversation` is attached, the probe is wrapped in a
        :class:`DiscussionThread` (Phase 4) so the UI can render a structured
        multi-model comparison panel.  Each provider's response is added to
        the thread, and the thread is marked complete when all providers
        have responded.

        Args:
            messages: Full message history to send to all providers.
            action_type: Optional filter (e.g. "reasoning", "code_gen").
            num_models: Max providers to query. 0 = all in priority matrix.
            tools: Optional tool schemas (same tools sent to all providers).
            max_tokens: Max tokens per provider completion.

        Returns:
            A :class:`ProbeResponse` with per-provider results in priority order.

        Raises:
            RuntimeError: If the agent has not been registered yet.
            httpx.HTTPStatusError: On non-2xx response from the Go server.
        """
        if not self.token:
            raise RuntimeError(
                f"Agent {self.agent_id} must be registered before probing. "
                "Call await agent.register() first."
            )

        logger.info(
            "Agent %s probing %d models (role=%s, action=%s)",
            self.agent_id, num_models or -1, self.role, action_type or "all",
        )

        # Fetch adaptive probe configuration from Go.
        # This overrides tier-based defaults with data-driven parameters.
        adaptive_cfg: AdaptiveProbeConfig | None = None
        probe_start_ns = __import__("time").time_ns()
        try:
            from ..probe_tuning import fetch_probe_config
            from ..go_client import GoClient

            # Build a temporary GoClient to fetch config (uses agent's own token).
            _go = GoClient(token=self.token or "")
            adaptive_cfg = await fetch_probe_config(
                _go,
                role=self.role,
                repo_id=self._repo_id,
                action_type=action_type,
                complexity_label=self._complexity_label,
                tier=self._tier,
            )
            self._adaptive_config = adaptive_cfg
            logger.info(
                "Agent %s: adaptive probe config — models=%d temp=%.2f strategy=%s",
                self.agent_id, adaptive_cfg.num_models, adaptive_cfg.temperature,
                adaptive_cfg.strategy,
            )
        except Exception as e:
            logger.debug("Agent %s: adaptive config fetch skipped: %s", self.agent_id, e)

        # Determine num_models: caller override > adaptive config > tier-based > default.
        if num_models == 0:
            if adaptive_cfg and adaptive_cfg.num_models > 0:
                num_models = adaptive_cfg.num_models
            elif self._probe_count > 0:
                num_models = self._probe_count
            logger.debug(
                "Agent %s: probe count resolved to %d (adaptive=%s, tier=%s)",
                self.agent_id, num_models,
                "yes" if adaptive_cfg else "no", self._tier,
            )

        # Use adaptive max_tokens if caller didn't specify.
        if max_tokens is None and adaptive_cfg and adaptive_cfg.max_tokens > 0:
            max_tokens = adaptive_cfg.max_tokens

        # Extract the topic from the last user message for the discussion thread.
        topic = ""
        for m in reversed(messages):
            if m.get("role") == "user" and m.get("content"):
                topic = m["content"][:200]
                break

        # Open a discussion thread if conversation is attached (Phase 4).
        discussion_thread = None
        if self.conversation:
            discussion_thread = await self.conversation.open_discussion(
                agent_id=self.agent_id,
                agent_role=self.role,
                topic=topic or "multi-LLM probe",
                action_type=action_type,
            )

        probe_resp = await llm_probe(
            messages=messages,
            tools=tools,
            max_tokens=max_tokens,
            go_base_url=self.config.go_base_url,
            agent_token=self.token,
            agent_role=self.role,
            action_type=action_type,
            num_models=num_models,
        )

        # Record each provider's response in the discussion thread.
        if self.conversation and discussion_thread:
            for result in probe_resp.results:
                await self.conversation.add_discussion_response(
                    thread_id=discussion_thread.thread_id,
                    provider=result.provider,
                    model=result.model,
                    content=result.content,
                    latency_ms=result.latency_ms,
                    finish_reason=result.finish_reason,
                    token_usage=result.usage,
                    error=result.error,
                )

            # Mark the discussion as complete.
            await self.conversation.complete_discussion(
                discussion_thread.thread_id,
            )

        # Store the thread_id on the response for downstream use.
        probe_resp._discussion_thread_id = (
            discussion_thread.thread_id if discussion_thread else ""
        )

        logger.info(
            "Agent %s probe complete: %d/%d providers succeeded in %dms (thread=%s)",
            self.agent_id,
            probe_resp.successes,
            probe_resp.providers,
            probe_resp.total_ms,
            discussion_thread.thread_id if discussion_thread else "none",
        )

        # ── Record probe outcome for adaptive tuning (fire-and-forget) ───
        try:
            from ..probe_tuning import build_probe_outcome, record_probe_outcome
            from ..go_client import GoClient

            _task_id = ""
            if self.conversation and hasattr(self.conversation, "task_id"):
                _task_id = self.conversation.task_id or ""

            outcome = build_probe_outcome(
                task_id=_task_id,
                role=self.role,
                repo_id=self._repo_id,
                action_type=action_type or "",
                probe_resp=probe_resp,
                config=self._adaptive_config,
                complexity_label=self._complexity_label,
                start_time_ns=probe_start_ns,
            )
            _go_out = GoClient(token=self.token or "")
            await record_probe_outcome(_go_out, outcome)
        except Exception:
            logger.debug(
                "Agent %s: probe outcome recording skipped",
                self.agent_id, exc_info=True,
            )

        # ── Report per-provider outcomes for self-healing (fire-and-forget) ─
        try:
            from ..self_heal import report_provider_outcome
            from ..go_client import GoClient

            _sh_go = GoClient(token=self.token or "")
            _sh_task_id = ""
            if self.conversation and hasattr(self.conversation, "task_id"):
                _sh_task_id = self.conversation.task_id or ""
            for result in probe_resp.results:
                await report_provider_outcome(
                    _sh_go,
                    provider=result.provider,
                    success=result.succeeded,
                    latency_ms=float(result.latency_ms),
                    error_msg=result.error,
                    task_id=_sh_task_id,
                    agent_id=self.agent_id,
                )
        except Exception:
            logger.debug(
                "Agent %s: self-heal outcome reporting skipped",
                self.agent_id, exc_info=True,
            )

        return probe_resp

    def pick_best_probe_result(self, probe_resp: ProbeResponse) -> str:
        """Select the best content from a multi-LLM probe response.

        Default strategy: return the first successful result (highest-priority
        provider).  For more sophisticated selection, use
        :meth:`run_consensus` which supports majority vote and GPT-as-judge.

        Args:
            probe_resp: The response from :meth:`probe_and_gather`.

        Returns:
            The content string from the best provider, or an empty string
            if all providers failed.
        """
        best = probe_resp.best_result
        if best:
            logger.debug(
                "Agent %s picked probe result from %s/%s (%dms)",
                self.agent_id, best.provider, best.model, best.latency_ms,
            )
            return best.content
        return ""

    async def pick_best_and_synthesise(self, probe_resp: ProbeResponse) -> str:
        """Select the best probe result and record it as discussion synthesis.

        Like :meth:`pick_best_probe_result` but also calls
        :meth:`SharedConversation.synthesise_discussion` so the choice is
        broadcast to the UI and persisted in the conversation log.

        Args:
            probe_resp: The response from :meth:`probe_and_gather`.

        Returns:
            The content string from the best provider, or empty string.
        """
        content = self.pick_best_probe_result(probe_resp)
        if not content:
            return ""

        best = probe_resp.best_result
        thread_id = getattr(probe_resp, "_discussion_thread_id", "")

        if self.conversation and thread_id and best:
            await self.conversation.synthesise_discussion(
                thread_id=thread_id,
                content=content,
                provider=best.provider,
            )

        return content

    async def run_consensus(
        self,
        probe_resp: ProbeResponse,
        strategy: ConsensusStrategy = ConsensusStrategy.AUTO,
    ) -> ConsensusResult:
        """Run the consensus engine on a multi-LLM probe response.

        This is the Phase 5 upgrade to :meth:`pick_best_and_synthesise`.
        Instead of always picking the first successful result, it applies
        the chosen strategy (auto, pick-best, majority-vote, or GPT-as-judge)
        to select or synthesise the best answer.

        The result is recorded as the discussion synthesis in the conversation
        so the UI and other agents can see both the strategy and reasoning.

        Args:
            probe_resp: The response from :meth:`probe_and_gather`.
            strategy: Consensus strategy. ``AUTO`` picks the best strategy
                based on how much the providers agree.

        Returns:
            A :class:`ConsensusResult` with the final answer, confidence,
            and reasoning.
        """
        thread_id = getattr(probe_resp, "_discussion_thread_id", "")

        # If a conversation is attached, use the DiscussionThread for richer input.
        thread = None
        if self.conversation and thread_id:
            thread = self.conversation.get_discussion(thread_id)

        if thread:
            result = await self.consensus_engine.run(thread, strategy)
        else:
            # No discussion thread — build a lightweight one from the probe response.
            from ..conversation import DiscussionThread, ProviderResponse as PResp
            temp_thread = DiscussionThread(
                agent_id=self.agent_id,
                agent_role=self.role,
                topic="multi-LLM probe",
            )
            for r in probe_resp.results:
                temp_thread.add_response(PResp(
                    provider=r.provider,
                    model=r.model,
                    content=r.content,
                    latency_ms=r.latency_ms,
                    error=r.error,
                ))
            temp_thread.complete()
            result = await self.consensus_engine.run(temp_thread, strategy)

        logger.info(
            "Agent %s consensus: strategy=%s provider=%s confidence=%.2f",
            self.agent_id, result.strategy.value, result.provider, result.confidence,
        )

        # Record the synthesis in the conversation.
        if self.conversation and thread_id and result.succeeded:
            await self.conversation.synthesise_discussion(
                thread_id=thread_id,
                content=result.content,
                provider=result.provider,
            )

        # Broadcast consensus result to Go → Prometheus + WebSocket.
        if self.conversation and hasattr(self.conversation, '_go') and self.conversation._go:
            try:
                event_payload: dict[str, Any] = {
                    "thread_id": thread_id,
                    "strategy": result.strategy.value,
                    "provider": result.provider,
                    "model": result.model,
                    "confidence": result.confidence,
                    "reasoning": result.reasoning,
                    "scores": result.all_scores,
                    "agent_role": self.role,
                }
                # Include multi-judge panel metadata when available.
                if result.judge_count > 0:
                    event_payload["judge_count"] = result.judge_count
                    event_payload["judge_agreement"] = result.judge_agreement
                await self.conversation._go.post_consensus_event(
                    self.conversation.task_id,
                    event_payload,
                )
            except Exception as e:
                logger.debug("Failed to broadcast consensus result: %s", e)

        return result

    def build_system_prompt(self, task: Task) -> str:
        """Override in subclass to provide role-specific system prompt."""
        return (
            f"You are a {self.role} agent in the RTVortex Agent Swarm. "
            f"Your agent ID is {self.agent_id}. "
            f"You are working on repository {task.repo_id}. "
            "Use the provided tools to accomplish the task."
        )

    def parse_result(self, messages: list[dict]) -> AgentResult:
        """Override in subclass to extract role-specific results."""
        # Default: return the last assistant message content.
        for msg in reversed(messages):
            if msg.get("role") == "assistant" and msg.get("content"):
                return AgentResult(output=msg["content"])
        return AgentResult(output="No output produced.")
