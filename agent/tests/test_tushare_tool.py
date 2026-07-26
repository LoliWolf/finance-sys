import json
from http.server import BaseHTTPRequestHandler

import httpx
import pytest

from app.config import AgentSettings, TushareSettings
from app.tools.tushare_tool import OfficialTushareStockBasicTool, TushareToolError


class QuietHTTPHandler(BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        return


def test_tushare_stock_basic_filters_official_candidates_without_logging_token():
    requests = []

    def handler(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        payload = json.loads(request.content)
        assert payload["api_name"] == "stock_basic"
        assert payload["token"] == "secret-token"
        assert payload["params"] == {"list_status": "L"}
        return httpx.Response(
            200,
            json={
                "code": 0,
                "data": {
                    "fields": ["ts_code", "symbol", "name", "market", "list_status", "exchange"],
                    "items": [
                        ["300502.SZ", "300502", "Sample", "SZ", "L", "SZSE"],
                        ["300308.SZ", "300308", "Other", "SZ", "L", "SZSE"],
                    ],
                },
            },
        )

    tool = OfficialTushareStockBasicTool(
        token="secret-token",
        endpoint="http://tushare.test",
        http_client=httpx.Client(transport=httpx.MockTransport(handler)),
    )

    candidates = tool.search("300502.SZ")

    assert len(requests) == 1
    assert [item.ts_code for item in candidates] == ["300502.SZ"]
    assert candidates[0].asset_type == "STOCK"


def test_tushare_default_stdlib_transport_posts_json(run_http_server):
    observed = {}

    class Handler(QuietHTTPHandler):
        def do_POST(self):
            length = int(self.headers["Content-Length"])
            observed.update(json.loads(self.rfile.read(length)))
            body = json.dumps(
                {
                    "code": 0,
                    "data": {
                        "fields": [
                            "ts_code",
                            "symbol",
                            "name",
                            "market",
                            "list_status",
                            "exchange",
                        ],
                        "items": [
                            ["300502.SZ", "300502", "新易盛", "SZ", "L", "SZSE"]
                        ],
                    },
                }
            ).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

    tool = OfficialTushareStockBasicTool(
        token="secret-token",
        endpoint=f"http://{run_http_server(Handler)}",
    )

    candidates = tool.search("新易盛")

    assert [item.ts_code for item in candidates] == ["300502.SZ"]
    assert observed["api_name"] == "stock_basic"
    assert observed["token"] == "secret-token"


def test_tushare_default_transport_http_error_redacts_token(run_http_server):
    class Handler(QuietHTTPHandler):
        def do_POST(self):
            body = b"upstream unavailable"
            self.send_response(503, "secret-token unavailable")
            self.send_header("Content-Type", "text/plain")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

    tool = OfficialTushareStockBasicTool(
        token="secret-token",
        endpoint=f"http://{run_http_server(Handler)}",
    )

    with pytest.raises(TushareToolError) as exc:
        tool.search("300502.SZ")

    assert "secret-token" not in str(exc.value)
    assert "[REDACTED]" in str(exc.value)


def test_tushare_default_transport_follows_redirect(run_http_server):
    source_requests = 0
    target_requests = []

    class TargetHandler(QuietHTTPHandler):
        def do_GET(self):
            target_requests.append(("GET", self.path))
            body = json.dumps(
                {
                    "code": 0,
                    "data": {
                        "fields": [
                            "ts_code",
                            "symbol",
                            "name",
                            "market",
                            "list_status",
                            "exchange",
                        ],
                        "items": [
                            ["300502.SZ", "300502", "新易盛", "创业板", "L", "SZSE"]
                        ],
                    },
                }
            ).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_POST(self):
            target_requests.append(("POST", self.path))
            self.send_response(204)
            self.end_headers()

    target_address = run_http_server(TargetHandler)

    class SourceHandler(QuietHTTPHandler):
        def do_POST(self):
            nonlocal source_requests
            source_requests += 1
            length = int(self.headers["Content-Length"])
            assert json.loads(self.rfile.read(length))["token"] == "secret-token"
            body = b"redirect refused"
            self.send_response(302, "secret-token redirect")
            self.send_header("Location", f"http://{target_address}/capture")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

    tool = OfficialTushareStockBasicTool(
        token="secret-token",
        endpoint=f"http://{run_http_server(SourceHandler)}",
    )

    results = tool.search("300502.SZ")

    assert source_requests == 1
    assert target_requests == [("GET", "/capture")]
    assert results[0].ts_code == "300502.SZ"


def test_tushare_stock_basic_disabled_without_token():
    tool = OfficialTushareStockBasicTool(token="", endpoint="http://tushare.test")

    assert tool.enabled() is False
    assert tool.search("300502.SZ") == []


def test_tushare_nacos_settings_do_not_fall_back_to_env_token(monkeypatch):
    monkeypatch.setenv("TUSHARE_TOKEN", "env-secret-token")
    monkeypatch.setattr(
        "app.tools.tushare_tool.get_settings",
        lambda: AgentSettings(
            config_source="nacos",
            tushare=TushareSettings(enabled=True, token=""),
        ),
    )

    tool = OfficialTushareStockBasicTool()

    assert tool.token == ""
    assert tool.enabled() is False


def test_tushare_stock_basic_reports_api_error_without_token_value():
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            json={"code": -1, "msg": "bad token secret-token"},
        )

    tool = OfficialTushareStockBasicTool(
        token="secret-token",
        endpoint="http://tushare.test",
        http_client=httpx.Client(transport=httpx.MockTransport(handler)),
    )

    with pytest.raises(TushareToolError) as exc:
        tool.search("300502.SZ")
    assert "secret-token" not in str(exc.value)
    assert "[REDACTED]" in str(exc.value)
