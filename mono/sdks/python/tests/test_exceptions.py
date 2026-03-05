"""Tests for SDK exception classes."""

from __future__ import annotations

import pytest

from rtvortex_sdk.exceptions import (
    AuthenticationError,
    NotFoundError,
    QuotaExceededError,
    RTVortexError,
    ServerError,
    ValidationError,
)


class TestRTVortexError:
    def test_base_error(self):
        err = RTVortexError("boom", status_code=500, body={"detail": "oops"})
        assert str(err) == "boom"
        assert err.status_code == 500
        assert err.body == {"detail": "oops"}

    def test_base_error_defaults(self):
        err = RTVortexError("something")
        assert err.status_code is None
        assert err.body is None

    def test_repr(self):
        err = RTVortexError("fail", status_code=418)
        assert "RTVortexError" in repr(err)
        assert "418" in repr(err)


class TestSubclasses:
    @pytest.mark.parametrize(
        ("cls", "code"),
        [
            (AuthenticationError, 401),
            (NotFoundError, 404),
            (ValidationError, 422),
            (QuotaExceededError, 429),
            (ServerError, 500),
        ],
    )
    def test_hierarchy(self, cls: type, code: int):
        err = cls("msg", status_code=code)
        assert isinstance(err, RTVortexError)
        assert err.status_code == code

    def test_catch_base(self):
        with pytest.raises(RTVortexError):
            raise AuthenticationError("no auth", status_code=401)
