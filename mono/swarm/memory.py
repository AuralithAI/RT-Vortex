"""Three-tier agent memory: STM (Redis), MTM (Postgres), LTM (engine embeddings)."""

from __future__ import annotations

import json
import logging
import time
from dataclasses import dataclass, field
from typing import Any

logger = logging.getLogger(__name__)

# Lazy-import heavy deps to keep import fast.
_aioredis = None


def _get_aioredis():
    global _aioredis
    if _aioredis is None:
        import redis.asyncio as aioredis
        _aioredis = aioredis
    return _aioredis

_STM_TTL = 30 * 60  # 30 minutes
_STM_MAX_OBSERVATIONS = 50


@dataclass
class STMEntry:
    key: str
    value: str
    timestamp: float = field(default_factory=time.time)


class ShortTermMemory:
    """Redis-backed STM scoped to task + agent. Keys: swarm:stm:{task_id}:{agent_id}:*"""

    def __init__(self, redis_url: str, task_id: str, agent_id: str):
        self._redis_url = redis_url
        self._task_id = task_id
        self._agent_id = agent_id
        self._redis = None
        self._prefix = f"swarm:stm:{task_id}:{agent_id}"

    async def init(self) -> None:
        aioredis = _get_aioredis()
        self._redis = aioredis.from_url(self._redis_url, decode_responses=True)

    async def put(self, key: str, value: str) -> None:
        if not self._redis:
            return
        full_key = f"{self._prefix}:{key}"
        await self._redis.set(full_key, value, ex=_STM_TTL)

    async def get(self, key: str) -> str | None:
        if not self._redis:
            return None
        full_key = f"{self._prefix}:{key}"
        return await self._redis.get(full_key)

    async def append_observation(self, observation: str) -> None:
        if not self._redis:
            return
        list_key = f"{self._prefix}:observations"
        entry = json.dumps({
            "text": observation,
            "ts": time.time(),
        })
        await self._redis.rpush(list_key, entry)
        await self._redis.ltrim(list_key, -_STM_MAX_OBSERVATIONS, -1)
        await self._redis.expire(list_key, _STM_TTL)

    async def get_observations(self, limit: int = 20) -> list[str]:
        if not self._redis:
            return []
        list_key = f"{self._prefix}:observations"
        raw = await self._redis.lrange(list_key, -limit, -1)
        results = []
        for r in raw:
            try:
                entry = json.loads(r)
                results.append(entry["text"])
            except (json.JSONDecodeError, KeyError):
                results.append(r)
        return results

    async def get_scratchpad(self) -> str:
        obs = await self.get_observations(limit=15)
        if not obs:
            return ""
        lines = [f"  - {o}" for o in obs]
        return "Recent observations:\n" + "\n".join(lines)

    async def clear(self) -> None:
        if not self._redis:
            return
        pattern = f"{self._prefix}:*"
        cursor = 0
        while True:
            cursor, keys = await self._redis.scan(cursor, match=pattern, count=100)
            if keys:
                await self._redis.delete(*keys)
            if cursor == 0:
                break

_MTM_TTL_DAYS = 7
_MTM_MAX_ENTRIES = 100


class MediumTermMemory:
    """Postgres-backed MTM scoped to agent role + repo, accessed via the Go API."""

    def __init__(self, go_client: Any, agent_role: str, repo_id: str):
        self._go = go_client
        self._role = agent_role
        self._repo_id = repo_id

    async def store(self, key: str, insight: str, confidence: float = 0.8) -> None:
        try:
            await self._go.mtm_store(
                repo_id=self._repo_id,
                agent_role=self._role,
                key=key,
                insight=insight,
                confidence=confidence,
            )
        except Exception as e:
            logger.warning("MTM store failed: %s", e)

    async def recall(self, limit: int = 10) -> list[dict[str, Any]]:
        try:
            return await self._go.mtm_recall(
                repo_id=self._repo_id,
                agent_role=self._role,
                limit=limit,
            )
        except Exception as e:
            logger.warning("MTM recall failed: %s", e)
            return []

    async def recall_as_text(self, limit: int = 10) -> str:
        entries = await self.recall(limit=limit)
        if not entries:
            return ""
        lines = []
        for e in entries:
            conf = e.get("confidence", 0)
            lines.append(f"  - [{conf:.0%}] {e.get('insight', '')}")
        return "Repository insights from past tasks:\n" + "\n".join(lines)


class LongTermMemory:
    """Engine-embedding-backed LTM scoped to repo."""

    def __init__(self, engine_client: Any | None, repo_id: str):
        self._engine = engine_client
        self._repo_id = repo_id

    async def search(self, query: str, top_k: int = 5) -> list[dict[str, Any]]:
        if not self._engine:
            return []
        try:
            result = await self._engine.search(query, self._repo_id, top_k=top_k)
            return result.get("chunks", [])
        except Exception as e:
            logger.warning("LTM search failed: %s", e)
            return []

    async def search_as_text(self, query: str, top_k: int = 5) -> str:
        chunks = await self.search(query, top_k=top_k)
        if not chunks:
            return ""
        lines = []
        for c in chunks:
            path = c.get("file_path", "?")
            score = c.get("score", 0)
            content = c.get("content", "")[:200]
            lines.append(f"  [{score:.2f}] {path}: {content}")
        return "Relevant code from long-term memory:\n" + "\n".join(lines)


class AgentMemory:
    """Unified three-tier memory for a single agent instance."""

    def __init__(
        self,
        agent_id: str,
        agent_role: str,
        task_id: str,
        repo_id: str,
        redis_url: str = "",
        go_client: Any = None,
        engine_client: Any = None,
    ):
        self.agent_id = agent_id
        self.agent_role = agent_role
        self.task_id = task_id
        self.repo_id = repo_id

        self.stm = ShortTermMemory(redis_url, task_id, agent_id)
        self.mtm = MediumTermMemory(go_client, agent_role, repo_id) if go_client else None
        self.ltm = LongTermMemory(engine_client, repo_id)

        self._reflection_count = 0

    async def init(self) -> None:
        await self.stm.init()

    async def stm_put(self, key: str, value: str) -> None:
        await self.stm.put(key, value)

    async def stm_get(self, key: str) -> str | None:
        return await self.stm.get(key)

    async def stm_append_observation(self, observation: str) -> None:
        await self.stm.append_observation(observation)

    async def mtm_store(self, key: str, insight: str, confidence: float = 0.8) -> None:
        if self.mtm:
            await self.mtm.store(key, insight, confidence)

    async def mtm_recall(self, limit: int = 10) -> list[dict]:
        if self.mtm:
            return await self.mtm.recall(limit)
        return []

    async def ltm_search(self, query: str, top_k: int = 5) -> list[dict]:
        return await self.ltm.search(query, top_k)

    # ── Context builder ────────────────────────────────────────────────

    async def build_memory_context(self, task_description: str = "") -> str:
        sections: list[str] = []

        scratchpad = await self.stm.get_scratchpad()
        if scratchpad:
            sections.append(scratchpad)

        if self.mtm:
            mtm_text = await self.mtm.recall_as_text(limit=8)
            if mtm_text:
                sections.append(mtm_text)

        if task_description:
            ltm_text = await self.ltm.search_as_text(task_description, top_k=3)
            if ltm_text:
                sections.append(ltm_text)

        if not sections:
            return ""
        return "\n\n".join(sections)

    # ── Reflection ───────────────────────────────────────────────────────

    async def reflect(
        self,
        tool_name: str,
        tool_args: dict | None = None,
        observation: str = "",
        was_error: bool = False,
    ) -> None:
        self._reflection_count += 1

        # Always store in STM
        prefix = "[ERROR] " if was_error else ""
        stm_text = f"{prefix}{tool_name}: {observation[:500]}"
        await self.stm.append_observation(stm_text)

        # Track tool usage patterns
        usage_key = f"tool_usage:{tool_name}"
        count = await self.stm.get(usage_key)
        await self.stm.put(usage_key, str(int(count or 0) + 1))

        # Promote to MTM if the observation looks like a structural insight
        if not was_error and self.mtm and self._is_structural_insight(observation):
            # Extract a key from the tool name and observation
            mtm_key = f"{tool_name}:{self._extract_insight_key(observation)}"
            await self.mtm.store(mtm_key, observation, confidence=0.7)
            logger.debug("Promoted observation to MTM: %s", mtm_key)

    @staticmethod
    def _is_structural_insight(observation: str) -> bool:
        structural_signals = [
            "uses ", "pattern", "convention", "framework",
            "directory", "structure", "configuration",
            "located in", "stored in", "defined in",
            "test framework", "orm", "database",
        ]
        lower = observation.lower()
        return any(signal in lower for signal in structural_signals)

    @staticmethod
    def _extract_insight_key(observation: str) -> str:
        words = [w for w in observation.split() if len(w) > 3][:3]
        return "_".join(words).lower()[:50] if words else "general"

    async def cleanup(self) -> None:
        """Clean up STM at end of task."""
        await self.stm.clear()
