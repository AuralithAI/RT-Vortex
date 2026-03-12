"""RTVortex Agent Swarm — asyncio entrypoint.

Wires together the three runtime dependencies (C++ engine gRPC, Go HTTP API,
Redis Streams or polling fallback), installs signal handlers, and runs the
consumer loop until the process receives SIGINT/SIGTERM.

Usage::

    python -m mono.swarm            # via __main__.py
    swarm-agent                     # via pyproject console_scripts
"""

from __future__ import annotations

import asyncio
import logging
import signal
import sys

from .agents_config import get_config
from .engine_client import EngineClient
from .go_client import GoClient
from .redis_consumer import RedisConsumer, TaskPollingConsumer
from .tools.engine_tools import init_engine_tools
from .tools.task_tools import init_task_tools

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    datefmt="%Y-%m-%d %H:%M:%S",
)
logger = logging.getLogger("swarm")


async def run() -> None:
    """Main async entrypoint — connect dependencies, run consumer."""

    cfg = get_config()

    logger.info("RTVortex Agent Swarm starting")
    logger.info("  Engine:  %s:%d", cfg.engine_host, cfg.engine_port)
    logger.info("  Go:      %s", cfg.go_server_url)
    logger.info("  Redis:   %s", cfg.redis_url)

    # ── Engine gRPC client ──────────────────────────────────────────────────
    engine = EngineClient()
    try:
        await engine.connect()
        logger.info("Engine gRPC connected")
    except Exception as e:
        logger.warning("Engine gRPC not available: %s (tools will fail gracefully)", e)

    # ── Go HTTP client ──────────────────────────────────────────────────────
    go_client = GoClient()

    # ── Initialise tool modules with shared clients ─────────────────────────
    init_engine_tools(engine)
    init_task_tools(go_client)

    # ── Choose consumer strategy ────────────────────────────────────────────
    use_redis = bool(cfg.redis_url)
    if use_redis:
        consumer: RedisConsumer | TaskPollingConsumer = RedisConsumer(
            engine_client=engine,
            go_client=go_client,
        )
    else:
        consumer = TaskPollingConsumer(
            engine_client=engine,
            go_client=go_client,
        )

    await consumer.start()

    # ── Graceful shutdown on SIGINT/SIGTERM ──────────────────────────────────
    shutdown_event = asyncio.Event()

    def _signal_handler():
        logger.info("Shutdown signal received")
        shutdown_event.set()

    loop = asyncio.get_running_loop()
    for sig in (signal.SIGINT, signal.SIGTERM):
        try:
            loop.add_signal_handler(sig, _signal_handler)
        except NotImplementedError:
            # Windows doesn't support add_signal_handler.
            pass

    # ── Run consumer loop ───────────────────────────────────────────────────
    consumer_task = asyncio.create_task(consumer.consume_loop(), name="consumer-loop")

    # Block until shutdown.
    await shutdown_event.wait()

    logger.info("Shutting down…")
    await consumer.stop()
    consumer_task.cancel()
    try:
        await consumer_task
    except asyncio.CancelledError:
        pass

    await engine.close()
    logger.info("RTVortex Agent Swarm stopped")


def main() -> None:
    """Sync entrypoint for pyproject.toml console_scripts."""
    try:
        asyncio.run(run())
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
