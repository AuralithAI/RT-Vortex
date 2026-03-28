"""Tests for SSE streaming parser."""

from __future__ import annotations

import pytest

from rtvortex_sdk.streaming import _parse_sse_block
from rtvortex_sdk.models import ProgressEvent


class TestParseSSEBlock:
    def test_basic_event(self):
        block = "event: progress\ndata: {\"step\": \"parsing\", \"status\": \"running\"}"
        evt = _parse_sse_block(block)
        assert evt is not None
        assert evt.event == "progress"
        assert evt.step == "parsing"
        assert evt.status == "running"

    def test_data_only(self):
        block = 'data: {"message": "hello"}'
        evt = _parse_sse_block(block)
        assert evt is not None
        assert evt.message == "hello"
        assert evt.event == "progress"  # default

    def test_complete_event(self):
        block = "event: complete\ndata: {\"step\": \"done\", \"status\": \"completed\"}"
        evt = _parse_sse_block(block)
        assert evt is not None
        assert evt.event == "complete"

    def test_multi_line_data(self):
        block = "event: progress\ndata: {\"step\": \"a\",\ndata:  \"status\": \"running\"}"
        evt = _parse_sse_block(block)
        assert evt is not None
        assert evt.step == "a"
        assert evt.status == "running"

    def test_no_data_returns_none(self):
        block = "event: heartbeat"
        evt = _parse_sse_block(block)
        assert evt is None

    def test_non_json_data(self):
        block = "data: just plain text"
        evt = _parse_sse_block(block)
        assert evt is not None
        assert evt.message == "just plain text"

    def test_error_event(self):
        block = 'event: error\ndata: {"message": "internal error", "status": "failed"}'
        evt = _parse_sse_block(block)
        assert evt is not None
        assert evt.event == "error"
        assert evt.message == "internal error"

    def test_empty_block(self):
        evt = _parse_sse_block("")
        assert evt is None
