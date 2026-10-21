"""Selection must honor the Go tool's per-symbol intraday availability flag."""
import copy

import pytest

from trading_agent.app.strategy import run_deterministic
from trading_agent.tests.test_strategy import FakeClient, config


@pytest.mark.parametrize("unavailable_price", ["", "10.23"])
def test_unavailable_quote_cannot_buy_or_suppress_healthy_peer(unavailable_price):
    class PartialQuoteClient(FakeClient):
        def get(self, path, params=None):
            result = super().get(path, params)
            if path == "/recommendation-candidates":
                healthy = copy.deepcopy(result["items"][0])
                healthy.update(
                    recommendation_event_id=13,
                    candidate_plan_id=35,
                    symbol="600001",
                    reference_price="0",
                    evidence_refs=["recommendation_event:13"],
                )
                result["items"].append(healthy)
            elif path == "/market-snapshot":
                result = {"items": [
                    {"symbol": "600000", "price": unavailable_price, "source": "BRIDGE",
                     "missing_reason": "REALTIME_QUOTE_UNAVAILABLE"},
                    {"symbol": "600001", "price": "10.23", "source": "EASTMONEY_GM_CURRENT_PRICE"},
                ]}
            elif path == "/daily-history":
                result["items"].extend(
                    dict(row, symbol="600001")
                    for row in list(result["items"]) if row["symbol"] == "600000"
                )
            return result

    request = dict(run_key="quote-availability-fixture", as_of_time="2026-08-25T09:40:00+08:00",
                   strategy_name="weekly", strategy_version="3.0.0", dry_run=True)
    result = run_deterministic(request, config(), PartialQuoteClient())
    assert result == run_deterministic(request, config(), PartialQuoteClient())
    assert result["candidate_count"] == 2
    assert [intent["symbol"] for intent in result["intents"]] == ["600001"]
    assert result["intents"][0]["proposed_limit_price"] == "10.230000"
