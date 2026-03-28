"""Tests for rtvortex_cli.client module — retry logic and error handling."""

from __future__ import annotations

import httpx
import pytest
import respx

from rtvortex_cli.client import APIClient, APIError
from rtvortex_cli.config import Config


@pytest.fixture
def cfg() -> Config:
    return Config(server_url="http://test-server:8080", token="tok")


class TestAPIClient:
    """Basic client construction and request routing."""

    def test_constructs_with_auth_header(self, cfg: Config) -> None:
        client = APIClient(cfg)
        assert "Authorization" in client._client.headers
        assert client._client.headers["Authorization"] == "Bearer tok"
        client.close()

    def test_constructs_without_token(self) -> None:
        client = APIClient(Config())
        assert "Authorization" not in client._client.headers
        client.close()

    def test_user_agent(self, cfg: Config) -> None:
        client = APIClient(cfg)
        assert "rtvortex-cli" in client._client.headers["User-Agent"]
        client.close()


class TestRetryLogic:
    """Verify retry behaviour with mocked transports."""

    @respx.mock
    def test_success_no_retry(self, cfg: Config) -> None:
        respx.get("http://test-server:8080/health").respond(200, json={"status": "ok"})
        client = APIClient(cfg)
        resp = client.get("/health")
        assert resp["status"] == "ok"
        client.close()

    @respx.mock
    def test_404_raises_immediately(self, cfg: Config) -> None:
        respx.get("http://test-server:8080/missing").respond(
            404, json={"message": "not found"}
        )
        client = APIClient(cfg)
        with pytest.raises(APIError) as exc_info:
            client.get("/missing")
        assert exc_info.value.status == 404
        client.close()

    @respx.mock
    def test_422_raises_with_body(self, cfg: Config) -> None:
        respx.post("http://test-server:8080/api/v1/orgs").respond(
            422, json={"errors": [{"field": "slug", "message": "invalid"}]}
        )
        client = APIClient(cfg)
        with pytest.raises(APIError) as exc_info:
            client.post("/api/v1/orgs", json={})
        assert exc_info.value.status == 422
        client.close()

    @respx.mock
    def test_204_returns_empty_dict(self, cfg: Config) -> None:
        respx.delete("http://test-server:8080/api/v1/orgs/1/members/2").respond(204)
        client = APIClient(cfg)
        resp = client.delete("/api/v1/orgs/1/members/2")
        assert resp == {}
        client.close()


class TestAPIError:
    """APIError structure."""

    def test_str(self) -> None:
        err = APIError(403, "forbidden")
        assert "403" in str(err)
        assert "forbidden" in str(err)

    def test_body(self) -> None:
        err = APIError(422, "bad", {"errors": [{"field": "x"}]})
        assert err.body["errors"][0]["field"] == "x"
