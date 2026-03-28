"""Shared fixtures for rtvortex-sdk tests."""

from __future__ import annotations

import pytest


@pytest.fixture()
def base_url() -> str:
    return "https://api.rtvortex.test"


@pytest.fixture()
def token() -> str:
    return "test-token-abc123"
