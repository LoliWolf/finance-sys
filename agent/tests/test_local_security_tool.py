import json

import httpx
import pytest

from app.tools.local_security import LocalSecurityClient, SecurityClientError


def test_local_security_lookup_maps_go_candidate_to_agent_security():
    requests = []

    def handler(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.url.path == "/api/v1/internal/security/resolve"
        assert request.headers["X-Internal-Token"] == "Bearer test-token"
        payload = json.loads(request.content)
        assert payload == {"query": "300502.SZ", "max_candidates": 5}
        return httpx.Response(
            200,
            json={
                "query": "300502.SZ",
                "candidates": [
                    {
                        "ts_code": "300502.SZ",
                        "symbol": "300502",
                        "name": "Sample",
                        "asset_type": "A_SHARE",
                        "market": "SZ",
                        "match_source": "direct",
                    }
                ],
            },
        )

    client = LocalSecurityClient(
        base_url="http://go.test",
        auth_header="X-Internal-Token",
        auth_token="Bearer test-token",
        http_client=httpx.Client(transport=httpx.MockTransport(handler)),
    )

    matches = client.lookup("300502.SZ")

    assert len(requests) == 1
    assert matches[0].ts_code == "300502.SZ"
    assert matches[0].asset_type == "STOCK"
    assert matches[0].market == "SZ"


def test_local_security_verify_returns_none_for_unverified_candidate():
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(404, json={"error": "not verified"})

    client = LocalSecurityClient(
        base_url="http://go.test",
        http_client=httpx.Client(transport=httpx.MockTransport(handler)),
    )

    assert client.verify("300999.SZ", "Unknown") is None


def test_local_security_http_errors_are_reported():
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(500, json={"error": "db down"})

    client = LocalSecurityClient(
        base_url="http://go.test",
        http_client=httpx.Client(transport=httpx.MockTransport(handler)),
    )

    with pytest.raises(SecurityClientError):
        client.lookup("300502.SZ")
