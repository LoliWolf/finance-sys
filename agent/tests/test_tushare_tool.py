import json

import httpx
import pytest

from app.tools.tushare_tool import OfficialTushareStockBasicTool, TushareToolError


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


def test_tushare_stock_basic_disabled_without_token():
    tool = OfficialTushareStockBasicTool(token="", endpoint="http://tushare.test")

    assert tool.enabled() is False
    assert tool.search("300502.SZ") == []


def test_tushare_stock_basic_reports_api_error_without_token_value():
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json={"code": -1, "msg": "bad token"})

    tool = OfficialTushareStockBasicTool(
        token="secret-token",
        endpoint="http://tushare.test",
        http_client=httpx.Client(transport=httpx.MockTransport(handler)),
    )

    with pytest.raises(TushareToolError) as exc:
        tool.search("300502.SZ")
    assert "secret-token" not in str(exc.value)
