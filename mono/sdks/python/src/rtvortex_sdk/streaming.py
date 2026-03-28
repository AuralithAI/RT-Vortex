"""SSE stream parser for RTVortex review streaming responses."""

from __future__ import annotations

from collections.abc import AsyncIterator, Iterator
from typing import TYPE_CHECKING

from rtvortex_sdk.models import ProgressEvent

if TYPE_CHECKING:
    import httpx


def _parse_sse_block(block: str) -> ProgressEvent | None:
    """Parse a single SSE text block into a ProgressEvent."""
    import json

    event_type = ""
    data_lines: list[str] = []

    for line in block.splitlines():
        if line.startswith("event:"):
            event_type = line[len("event:"):].strip()
        elif line.startswith("data:"):
            data_lines.append(line[len("data:"):].strip())

    if not data_lines:
        return None

    raw = "\n".join(data_lines)
    try:
        payload = json.loads(raw)
    except json.JSONDecodeError:
        payload = {"message": raw}

    if event_type:
        payload.setdefault("event", event_type)

    return ProgressEvent.model_validate(payload)


def iter_sse_events(response: httpx.Response) -> Iterator[ProgressEvent]:
    """Iterate over SSE events from a *synchronous* httpx streaming response.

    Usage::

        with client.stream("GET", url) as resp:
            for event in iter_sse_events(resp):
                print(event)
    """
    buf = ""
    for chunk in response.iter_text():
        buf += chunk
        while "\n\n" in buf:
            block, buf = buf.split("\n\n", 1)
            block = block.strip()
            if not block:
                continue
            evt = _parse_sse_block(block)
            if evt is not None:
                yield evt


async def aiter_sse_events(
    response: httpx.Response,
) -> AsyncIterator[ProgressEvent]:
    """Iterate over SSE events from an *async* httpx streaming response.

    Usage::

        async with client.stream("GET", url) as resp:
            async for event in aiter_sse_events(resp):
                print(event)
    """
    buf = ""
    async for chunk in response.aiter_text():
        buf += chunk
        while "\n\n" in buf:
            block, buf = buf.split("\n\n", 1)
            block = block.strip()
            if not block:
                continue
            evt = _parse_sse_block(block)
            if evt is not None:
                yield evt
