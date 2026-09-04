import datetime as dt

from trading_agent.app.config import AgentConfig
from trading_agent.app.strategy import _intent_key, run_deterministic


class FakeClient:
    def get(self, path, params=None):
        if path == "/recommendation-candidates":
            return {
                "items": [
                    {
                        "recommendation_event_id": 12,
                        "candidate_plan_id": 34,
                        "blogger_id": 56,
                        "symbol": "600000",
                        "market": "SH",
                        "asset_type": "STOCK",
                        "direction": "LONG",
                        "recommend_date": "2026-08-24",
                        "confidence": "0.80000000",
                        "position_ratio": "0.03000000",
                        "evidence_refs": ["recommendation_event:12"],
                    }
                ]
            }
        if path.startswith("/blogger-performance/"):
            return {"evaluable_count": 20, "win_rate": 0.6}
        if path == "/market-snapshot":
            return {"items": [{"symbol": "600000", "price": "10.23"}]}
        if path == "/portfolio":
            return {"positions": [], "cycles": [], "open_orders": []}
        if path == "/daily-history":
            return {
                "items": [
                    {"symbol": "600000", "trade_date": f"2026-08-{day:02d}", "close_price": price}
                    for day, price in zip(range(18, 25), [9.0, 9.2, 9.4, 9.7, 9.9, 10.1, 10.2])
                ]
            }
        raise AssertionError(path)


def config():
    return AgentConfig("127.0.0.1", 8110, "token", "trading-tools/v1", 0.65, 10, 0.55, 20, 5)


def test_deterministic_strategy_is_replayable():
    request = {
        "run_key": "a" * 64,
        "as_of_time": "2026-08-24T09:20:00+08:00",
        "trigger_type": "PRE_OPEN",
        "strategy_name": "blogger_follow_v1",
        "strategy_version": "1.0.0",
        "decision_provider": "DETERMINISTIC",
        "tool_base_url": "http://127.0.0.1:30006/api/v1/internal/trading-tools",
        "config_version": 22,
        "dry_run": False,
    }
    first = run_deterministic(request, config(), FakeClient())
    second = run_deterministic(request, config(), FakeClient())
    assert first == second
    assert first["schema_version"] == "trading-intent/v2"
    assert any(item["skill_name"] == "composite_buy_rank" for item in first["skill_decisions"])
    assert first["intents"][0]["intent_key"] == _intent_key(
        "1.0.0",
        dt.datetime(2026, 8, 24, 9, 20, tzinfo=dt.timezone(dt.timedelta(hours=8))),
        12,
        "600000",
        "BUY",
        dt.datetime(2026, 8, 24, 10, 0, tzinfo=dt.timezone(dt.timedelta(hours=8))),
    )


def test_strategy_rejects_sector_and_missing_performance():
    class SectorClient(FakeClient):
        def get(self, path, params=None):
            value = super().get(path, params)
            if path == "/recommendation-candidates":
                value["items"][0]["asset_type"] = "SECTOR"
            return value

    result = run_deterministic(
        {
            "run_key": "b" * 64,
            "as_of_time": "2026-08-24T09:20:00+08:00",
            "strategy_name": "s",
            "strategy_version": "1",
            "tool_base_url": "http://local",
        },
        config(),
        SectorClient(),
    )
    assert result["intents"] == []


def test_exit_skill_generates_sell_before_buy():
    class ExitClient(FakeClient):
        def get(self, path, params=None):
            if path == "/portfolio":
                return {
                    "positions": [{"symbol": "688002", "volume": 201, "available_volume": 201}],
                    "cycles": [{
                        "id": 8, "symbol": "688002", "ts_code": "688002.SH", "market": "SH",
                        "asset_type": "STOCK", "board_type": "STAR", "available_volume": 201,
                        "stop_loss_price": "9.00", "take_profit_price": "11.00",
                        "holding_trade_days": 3, "max_holding_trade_days": 20,
                    }],
                    "open_orders": [],
                }
            if path == "/market-snapshot":
                return {"items": [{"symbol": "688002", "price": "8.90"}, {"symbol": "600000", "price": "10.23"}]}
            return super().get(path, params)

    result = run_deterministic(
        {
            "run_key": "c" * 64, "as_of_time": "2026-08-24T09:40:00+08:00",
            "strategy_name": "s", "strategy_version": "1", "tool_base_url": "http://local",
        }, config(), ExitClient(),
    )
    assert result["intents"][0]["action"] == "SELL"
    assert result["intents"][0]["position_cycle_id"] == 8
    assert result["intents"][0]["proposed_volume"] == 201
