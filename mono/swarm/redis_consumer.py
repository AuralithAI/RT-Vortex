"""Redis stream consumer for swarm task assignment.

Listens on ``swarm:events:team:create`` via ``XREADGROUP``.  When the Go
``assignLoop`` publishes a team-create event, this consumer spins up an agent
team (orchestrator + N workers), registers them with Go, and runs the agentic
loop to completion.

A fallback :class:`TaskPollingConsumer` is provided for deployments where
Redis Streams are unavailable — it polls ``GET /internal/swarm/tasks/next``
on a configurable interval.
"""

from __future__ import annotations

import asyncio
import json
import logging
import uuid
from typing import Any, Callable, Coroutine

import redis.asyncio as aioredis

from .agents_config import get_config
from .agents.architect import ArchitectAgent
from .agents.docs import DocsAgent
from .agents.junior_dev import JuniorDevAgent
from .agents.ops import OpsAgent
from .agents.orchestrator import OrchestratorAgent
from .agents.qa import QAAgent
from .agents.security import SecurityAgent
from .agents.senior_dev import SeniorDevAgent
from .auth import register_agent
from .engine_client import EngineClient
from .go_client import GoClient
from .sdk.agent import Agent, AgentConfig, AgentResult, Task
from .tools.engine_tools import init_engine_tools
from .tools.task_tools import init_task_tools

logger = logging.getLogger(__name__)

# Consumer group for team-create events.
_STREAM_KEY = "swarm:events:team:create"
_GROUP_NAME = "swarm-python"
_CONSUMER_NAME = f"consumer-{uuid.uuid4().hex[:8]}"

# How often to send heartbeats (seconds).
_HEARTBEAT_INTERVAL = 25

# How long to wait between status polls while waiting for human approval.
_APPROVAL_POLL_INTERVAL = 5

# Maximum wait time for plan approval before timing out (10 minutes).
_APPROVAL_TIMEOUT = 600


def _make_agent(role: str, team_id: str, agent_config: AgentConfig) -> Agent:
    """Instantiate an agent by role name.

    Args:
        role: One of ``orchestrator``, ``senior_dev``, ``architect``, ``qa``,
              ``security``, ``docs``, ``ops``, ``junior_dev``.
        team_id: Team UUID.
        agent_config: Shared agent runtime config.

    Returns:
        An :class:`Agent` subclass instance.

    Raises:
        ValueError: If the role is unknown.
    """
    agent_id = f"{role[:4]}-{team_id[:8]}-{uuid.uuid4().hex[:4]}"
    role_map: dict[str, type[Agent]] = {
        "orchestrator": OrchestratorAgent,
        "senior_dev": SeniorDevAgent,
        "architect": ArchitectAgent,
        "qa": QAAgent,
        "security": SecurityAgent,
        "docs": DocsAgent,
        "ops": OpsAgent,
        "junior_dev": JuniorDevAgent,
    }
    cls = role_map.get(role)
    if cls is None:
        raise ValueError(f"Unknown agent role: {role}")
    return cls(agent_id=agent_id, team_id=team_id, agent_config=agent_config)


async def _heartbeat_loop(
    go_client: GoClient,
    agent_ids: list[str],
    stop_event: asyncio.Event,
) -> None:
    """Send periodic heartbeats for all active agents until *stop_event* is set."""
    while not stop_event.is_set():
        for aid in list(agent_ids):
            try:
                await go_client.send_heartbeat(aid)
            except Exception as e:
                logger.warning("Heartbeat failed for %s: %s", aid, e)
        try:
            await asyncio.wait_for(stop_event.wait(), timeout=_HEARTBEAT_INTERVAL)
        except asyncio.TimeoutError:
            pass  # expected — loop again


async def _wait_for_status(
    go_client: GoClient,
    task_id: str,
    target_statuses: set[str],
    cancel_statuses: set[str] | None = None,
    timeout: float = _APPROVAL_TIMEOUT,
) -> str:
    """Poll Go for the task's status until it reaches one of *target_statuses*.

    Args:
        go_client: Go HTTP client.
        task_id: Task UUID to poll.
        target_statuses: Set of statuses that mean "proceed".
        cancel_statuses: Set of statuses that mean "abort" (e.g. cancelled).
        timeout: Maximum wait in seconds.

    Returns:
        The status that was reached.

    Raises:
        asyncio.TimeoutError: If *timeout* expires.
        RuntimeError: If a cancel status is reached.
    """
    cancel_statuses = cancel_statuses or {"cancelled", "failed", "timed_out"}
    elapsed = 0.0
    while elapsed < timeout:
        status = await go_client.get_task_status(task_id)
        if status in target_statuses:
            return status
        if status in cancel_statuses:
            raise RuntimeError(f"Task {task_id} reached terminal status: {status}")
        await asyncio.sleep(_APPROVAL_POLL_INTERVAL)
        elapsed += _APPROVAL_POLL_INTERVAL

    raise asyncio.TimeoutError(
        f"Timed out waiting for task {task_id} to reach {target_statuses}"
    )


async def _run_full_pipeline(
    team_id: str,
    task_data: dict[str, Any],
    engine_client: EngineClient | None,
    go_client: GoClient,
) -> None:
    """Execute the full multi-agent pipeline for a task.

    Lifecycle:
        1. **Planning** — Orchestrator analyses the task and produces a plan.
        2. **Human approval** — Plan is submitted to Go; we poll until approved.
        3. **Implementation** — SeniorDev (+ optional Architect, QA, Security,
           Docs, Ops, JuniorDev) produce diffs.
        4. **Diff submission** — Each agent's diffs are submitted to Go.
        5. **Completion** — Task is marked complete; Go triggers PR creation.

    Args:
        team_id: Team UUID.
        task_data: Raw task dict from Go.
        engine_client: Optional engine gRPC client.
        go_client: Go HTTP client.
    """
    task = Task(
        id=task_data["id"],
        repo_id=task_data.get("repo_id", ""),
        description=task_data.get("description", ""),
        status=task_data.get("status", "submitted"),
        plan_document=task_data.get("plan_document"),
    )
    agent_config = AgentConfig()

    # Initialise shared tools.
    if engine_client:
        init_engine_tools(engine_client)
    init_task_tools(go_client)

    # Track all agent IDs for heartbeats.
    agent_ids: list[str] = []
    stop_heartbeat = asyncio.Event()
    heartbeat_task: asyncio.Task | None = None

    try:
        # ── Step 1: Orchestrator produces the plan ──────────────────────
        orchestrator = _make_agent("orchestrator", team_id, agent_config)
        await orchestrator.register()
        agent_ids.append(orchestrator.agent_id)

        # Start heartbeat loop.
        heartbeat_task = asyncio.create_task(
            _heartbeat_loop(go_client, agent_ids, stop_heartbeat),
            name=f"heartbeat-{team_id[:8]}",
        )

        logger.info("Team %s: Orchestrator planning task %s", team_id[:8], task.id)
        orch_result = await orchestrator.run(task)

        if orch_result.error:
            logger.error("Team %s: Orchestrator error: %s", team_id[:8], orch_result.error)
            await go_client.fail_task(task.id, f"Orchestrator error: {orch_result.error}")
            return

        if not orch_result.plan:
            logger.warning("Team %s: Orchestrator produced no plan", team_id[:8])
            await go_client.fail_task(task.id, "Orchestrator produced no plan")
            return

        logger.info("Team %s: Plan submitted, waiting for human approval…", team_id[:8])

        # ── Step 2: Wait for human plan approval ────────────────────────
        try:
            status = await _wait_for_status(
                go_client,
                task.id,
                target_statuses={"implementing"},
                cancel_statuses={"cancelled", "failed", "timed_out"},
            )
        except asyncio.TimeoutError:
            logger.error("Team %s: Plan approval timed out for task %s", team_id[:8], task.id)
            await go_client.fail_task(task.id, "Plan approval timed out")
            return
        except RuntimeError as e:
            logger.warning("Team %s: %s", team_id[:8], e)
            return

        logger.info("Team %s: Plan approved — spinning up implementation agents", team_id[:8])

        # Inject the approved plan into the task so implementation agents see it.
        task.plan_document = orch_result.plan
        task.status = "implementing"

        # ── Step 3: Determine team composition from the plan ────────────
        # The plan's estimated_complexity drives team size.
        complexity = (orch_result.plan or {}).get("estimated_complexity", "medium")
        if complexity == "small":
            roles = ["senior_dev"]
        elif complexity == "large":
            roles = ["architect", "senior_dev", "junior_dev", "qa", "security", "docs"]
        else:  # medium
            roles = ["senior_dev", "qa", "security"]

        # Declare team size to Go.
        await go_client.declare_team_size(task.id, len(roles) + 1)  # +1 for orchestrator

        # ── Step 4: Run implementation agents ───────────────────────────
        all_diffs: list[dict] = []
        implementation_agents: list[Agent] = []

        for role in roles:
            agent = _make_agent(role, team_id, agent_config)
            await agent.register()
            agent_ids.append(agent.agent_id)
            implementation_agents.append(agent)

        # Run code-generating agents concurrently, review agents sequentially.
        code_agents = [a for a in implementation_agents if a.role in ("senior_dev", "junior_dev", "architect", "docs", "ops")]
        review_agents = [a for a in implementation_agents if a.role in ("qa", "security")]

        # Run code agents in parallel.
        if code_agents:
            code_tasks = [a.run(task) for a in code_agents]
            code_results: list[AgentResult] = await asyncio.gather(
                *code_tasks, return_exceptions=True
            )

            for agent, result in zip(code_agents, code_results):
                if isinstance(result, Exception):
                    logger.error("Team %s: %s agent failed: %s",
                                 team_id[:8], agent.role, result)
                    continue
                if result.diffs:
                    all_diffs.extend(result.diffs)
                    logger.info("Team %s: %s produced %d diffs",
                                team_id[:8], agent.role, len(result.diffs))
                else:
                    logger.info("Team %s: %s completed (no diffs)", team_id[:8], agent.role)

        # Run review agents sequentially so they can see prior diffs.
        for agent in review_agents:
            try:
                result = await agent.run(task)
                if result.diffs:
                    all_diffs.extend(result.diffs)
                    logger.info("Team %s: %s produced %d diffs",
                                team_id[:8], agent.role, len(result.diffs))
                else:
                    logger.info("Team %s: %s completed (no diffs)", team_id[:8], agent.role)
            except Exception as e:
                logger.error("Team %s: %s agent failed: %s",
                             team_id[:8], agent.role, e)

        # ── Step 5: Submit remaining diffs and complete ─────────────────
        # Most diffs are submitted in real-time by agent tool calls, but
        # if any diffs came via parse_result, submit them here.
        for diff in all_diffs:
            if not diff.get("_submitted"):
                try:
                    await go_client.report_diff(task.id, diff)
                except Exception as e:
                    logger.error("Team %s: Failed to submit diff for %s: %s",
                                 team_id[:8], diff.get("file_path", "?"), e)

        # Mark task complete — Go triggers PR creation.
        await go_client.report_result(task.id)
        logger.info("Team %s: Task %s completed successfully", team_id[:8], task.id)

    except Exception as e:
        logger.error("Team %s: Fatal pipeline error: %s", team_id[:8], e, exc_info=True)
        try:
            await go_client.fail_task(task.id, f"Pipeline error: {e}")
        except Exception:
            pass

    finally:
        # Stop heartbeats.
        stop_heartbeat.set()
        if heartbeat_task:
            heartbeat_task.cancel()
            try:
                await heartbeat_task
            except (asyncio.CancelledError, Exception):
                pass


class RedisConsumer:
    """Consumes ``team:create`` events from Redis Streams and spins up agent teams.

    Each event triggers :meth:`_run_team`, which creates the agent instances,
    registers them with Go, and runs the agentic loop.  Active teams are
    tracked so they can be cancelled on shutdown.
    """

    def __init__(
        self,
        redis_url: str = "",
        engine_client: EngineClient | None = None,
        go_client: GoClient | None = None,
    ):
        self._redis_url = redis_url or get_config().redis_url
        self._redis: aioredis.Redis | None = None
        self._engine = engine_client
        self._go_client = go_client
        self._running = False
        self._active_teams: dict[str, asyncio.Task] = {}

    async def start(self) -> None:
        """Connect to Redis, register a controller agent, and begin consuming."""
        # Register a controller agent with Go to obtain a JWT for internal API
        # calls (poll_next_task, heartbeats, etc.).  This is a long-lived
        # "consumer" agent — individual team agents register separately.
        try:
            controller_id = f"ctrl-{_CONSUMER_NAME}"
            token, controller_id = await register_agent(
                agent_id=controller_id,
                role="controller",
                team_id="00000000-0000-0000-0000-000000000000",
                hostname=__import__("socket").gethostname(),
            )
            if self._go_client:
                self._go_client.set_token(token)
            logger.info("Controller agent registered: %s", controller_id)
        except Exception as e:
            logger.warning("Controller registration failed (internal calls may 401): %s", e)

        self._redis = aioredis.from_url(self._redis_url, decode_responses=True)

        # Ensure stream + consumer group exist.
        try:
            await self._redis.xgroup_create(
                name=_STREAM_KEY,
                groupname=_GROUP_NAME,
                id="0",
                mkstream=True,
            )
            logger.info("Created consumer group %s on %s", _GROUP_NAME, _STREAM_KEY)
        except aioredis.ResponseError as e:
            if "BUSYGROUP" not in str(e):
                raise
            logger.debug("Consumer group %s already exists", _GROUP_NAME)

        self._running = True
        logger.info("Redis consumer started on %s (consumer=%s)", _STREAM_KEY, _CONSUMER_NAME)

    async def stop(self) -> None:
        """Stop consuming and cancel active teams."""
        self._running = False

        # Cancel all active team tasks.
        for team_id, task in self._active_teams.items():
            task.cancel()
            logger.info("Cancelled team %s", team_id)

        self._active_teams.clear()

        if self._redis:
            await self._redis.aclose()
            self._redis = None

    async def consume_loop(self) -> None:
        """Main consume loop — blocks indefinitely while self._running."""
        if not self._redis:
            raise RuntimeError("Consumer not started — call start() first")

        while self._running:
            try:
                entries = await self._redis.xreadgroup(
                    groupname=_GROUP_NAME,
                    consumername=_CONSUMER_NAME,
                    streams={_STREAM_KEY: ">"},
                    count=1,
                    block=2000,  # 2s block timeout
                )

                if not entries:
                    continue

                for stream_name, messages in entries:
                    for msg_id, data in messages:
                        await self._handle_event(msg_id, data)
                        await self._redis.xack(_STREAM_KEY, _GROUP_NAME, msg_id)

            except asyncio.CancelledError:
                logger.info("Consumer loop cancelled")
                break
            except Exception as e:
                logger.error("Redis consumer error: %s", e, exc_info=True)
                await asyncio.sleep(2)

    async def _handle_event(self, msg_id: str, data: dict[str, str]) -> None:
        """Handle a team:create event — spin up a new agent team."""
        task_id = data.get("task_id", "")
        if not task_id:
            logger.warning("team:create event without task_id: %s", data)
            return

        logger.info("Received team:create event for task %s", task_id)

        # Fetch full task from Go by ID.
        if not self._go_client:
            logger.error("No GoClient available — cannot fetch task")
            return

        task_data = await self._go_client.get_task(task_id)
        if not task_data:
            logger.warning("No task available from Go for task_id %s", task_id)
            return

        # Spin up a team for this task.
        team_id = str(uuid.uuid4())
        team_task = asyncio.create_task(
            _run_full_pipeline(team_id, task_data, self._engine, self._go_client),
            name=f"team-{team_id[:8]}",
        )
        self._active_teams[team_id] = team_task

        # Clean up when done.
        team_task.add_done_callback(lambda _: self._active_teams.pop(team_id, None))


class TaskPollingConsumer:
    """Alternative consumer that polls Go for tasks instead of Redis streams.

    Use this if Redis Streams are not available or for simpler deployments.
    The Go assignLoop still manages FIFO ordering — this just polls for
    assigned work.
    """

    def __init__(
        self,
        engine_client: EngineClient | None = None,
        go_client: GoClient | None = None,
        poll_interval: float = 0,
    ):
        self._engine = engine_client
        self._go_client = go_client
        self._poll_interval = poll_interval or get_config().task_poll_interval
        self._running = False
        self._active_teams: dict[str, asyncio.Task] = {}

    async def start(self) -> None:
        # Register a controller agent for internal API auth (same as RedisConsumer).
        try:
            controller_id = f"ctrl-poll-{uuid.uuid4().hex[:8]}"
            token, controller_id = await register_agent(
                agent_id=controller_id,
                role="controller",
                team_id="00000000-0000-0000-0000-000000000000",
                hostname=__import__("socket").gethostname(),
            )
            if self._go_client:
                self._go_client.set_token(token)
            logger.info("Controller agent registered: %s", controller_id)
        except Exception as e:
            logger.warning("Controller registration failed: %s", e)

        self._running = True
        logger.info("Task polling consumer started (interval=%ss)", self._poll_interval)

    async def stop(self) -> None:
        self._running = False
        for team_id, task in self._active_teams.items():
            task.cancel()
        self._active_teams.clear()

    async def consume_loop(self) -> None:
        """Poll Go for tasks in a loop."""
        if not self._go_client:
            raise RuntimeError("GoClient not set")

        while self._running:
            try:
                task_data = await self._go_client.poll_next_task()
                if task_data:
                    team_id = str(uuid.uuid4())
                    team_task = asyncio.create_task(
                        _run_full_pipeline(
                            team_id, task_data, self._engine, self._go_client
                        ),
                        name=f"team-{team_id[:8]}",
                    )
                    self._active_teams[team_id] = team_task
                    team_task.add_done_callback(
                        lambda _, tid=team_id: self._active_teams.pop(tid, None)
                    )

            except Exception as e:
                logger.error("Polling error: %s", e)

            await asyncio.sleep(self._poll_interval)
