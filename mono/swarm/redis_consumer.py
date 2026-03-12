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
from .agents.orchestrator import OrchestratorAgent
from .agents.senior_dev import SeniorDevAgent
from .engine_client import EngineClient
from .go_client import GoClient
from .sdk.agent import AgentConfig, Task
from .tools.engine_tools import init_engine_tools
from .tools.task_tools import init_task_tools

logger = logging.getLogger(__name__)

# Consumer group for team-create events.
_STREAM_KEY = "swarm:events:team:create"
_GROUP_NAME = "swarm-python"
_CONSUMER_NAME = f"consumer-{uuid.uuid4().hex[:8]}"


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
        """Connect to Redis and begin consuming."""
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

        # Fetch full task from Go.
        if not self._go_client:
            logger.error("No GoClient available — cannot fetch task")
            return

        task_data = await self._go_client.poll_next_task()
        if not task_data:
            logger.warning("No task available from Go for task_id %s", task_id)
            return

        # Spin up a team for this task.
        team_id = str(uuid.uuid4())
        team_task = asyncio.create_task(
            self._run_team(team_id, task_data),
            name=f"team-{team_id[:8]}",
        )
        self._active_teams[team_id] = team_task

        # Clean up when done.
        team_task.add_done_callback(lambda _: self._active_teams.pop(team_id, None))

    async def _run_team(self, team_id: str, task_data: dict[str, Any]) -> None:
        """Run a minimal Phase 0 team: Orchestrator + SeniorDev.

        Phase 0 flow:
        1. Create Orchestrator + SeniorDev agents
        2. Register both with Go
        3. Orchestrator searches codebase and produces a plan
        4. Plan submitted to Go for human review
        """
        task = Task(
            id=task_data["id"],
            repo_id=task_data.get("repo_id", ""),
            description=task_data.get("description", ""),
            status=task_data.get("status", "submitted"),
            plan_document=task_data.get("plan_document"),
        )

        agent_config = AgentConfig()

        # Initialise tools with shared clients.
        if self._engine:
            init_engine_tools(self._engine)
        if self._go_client:
            init_task_tools(self._go_client)

        # Phase 0: Only Orchestrator for planning.
        orchestrator = OrchestratorAgent(
            agent_id=f"orch-{team_id[:8]}",
            team_id=team_id,
            agent_config=agent_config,
        )

        try:
            # Register and run orchestrator.
            await orchestrator.register()
            logger.info("Team %s: Orchestrator running for task %s", team_id[:8], task.id)

            result = await orchestrator.run(task)

            if result.error:
                logger.error("Team %s: Orchestrator error: %s", team_id[:8], result.error)
            else:
                logger.info("Team %s: Orchestrator completed. Plan: %s",
                            team_id[:8], "yes" if result.plan else "no")

        except Exception as e:
            logger.error("Team %s: Fatal error: %s", team_id[:8], e, exc_info=True)


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
                        self._run_team(team_id, task_data),
                        name=f"team-{team_id[:8]}",
                    )
                    self._active_teams[team_id] = team_task
                    team_task.add_done_callback(
                        lambda _, tid=team_id: self._active_teams.pop(tid, None)
                    )

            except Exception as e:
                logger.error("Polling error: %s", e)

            await asyncio.sleep(self._poll_interval)

    async def _run_team(self, team_id: str, task_data: dict[str, Any]) -> None:
        """Same team logic as RedisConsumer._run_team."""
        task = Task(
            id=task_data["id"],
            repo_id=task_data.get("repo_id", ""),
            description=task_data.get("description", ""),
            status=task_data.get("status", "submitted"),
            plan_document=task_data.get("plan_document"),
        )

        agent_config = AgentConfig()

        if self._engine:
            init_engine_tools(self._engine)
        if self._go_client:
            init_task_tools(self._go_client)

        orchestrator = OrchestratorAgent(
            agent_id=f"orch-{team_id[:8]}",
            team_id=team_id,
            agent_config=agent_config,
        )

        try:
            await orchestrator.register()
            logger.info("Team %s: Orchestrator running for task %s", team_id[:8], task.id)
            result = await orchestrator.run(task)

            if result.error:
                logger.error("Team %s: Orchestrator error: %s", team_id[:8], result.error)
            else:
                logger.info("Team %s: Orchestrator completed. Plan: %s",
                            team_id[:8], "yes" if result.plan else "no")
        except Exception as e:
            logger.error("Team %s: Fatal error: %s", team_id[:8], e, exc_info=True)
