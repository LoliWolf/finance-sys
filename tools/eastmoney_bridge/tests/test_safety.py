import datetime as dt
import json
from dataclasses import replace
from unittest.mock import Mock

import pytest

from bridge_api.db import Database, now_iso
from bridge_api.order_safety import validate_request, validate_quote, identity, aware_time
from gm_runner import strategy
from test_auth_db import config

CST = dt.timezone(dt.timedelta(hours=8))
NOW = dt.datetime(2026, 9, 9, 9, 40, tzinfo=CST)


@pytest.mark.parametrize("fraction", ["6", "63", "630", "6301", "63012", "630123", "6301234", "63012345", "630123456"])
def test_go_rfc3339nano_fractional_precision(fraction):
    parsed = aware_time("2026-09-09T14:38:59."+fraction+"+08:00")
    assert parsed.microsecond == int(fraction[:6].ljust(6, "0"))


def ready(tmp_path):
    return replace(config(tmp_path), global_enabled=True, global_kill_switch=False,
                   calendar={"source": "FIXTURE", "version": "1", "coverage_from": "2026-09-01", "coverage_to": "2026-10-09", "open_dates": ["2026-09-09", "2026-09-10", "2026-10-08"]},
                   windows=(("09:35:00", "11:25:00"), ("13:05:00", "14:50:00")))


def order(cfg, **changes):
    value = dict(environment="SIMULATION", expected_account_id=cfg.expected_account_id,
                 strategy_id=cfg.strategy_id, symbol="SHSE.600000", ts_code="600000.SH",
                 asset_type="STOCK", board_type="SH_MAIN", trading_rule_version=cfg.trading_rule_version,
                 side="BUY", order_type="LIMIT", position_effect="OPEN", volume=100,
                 price="10.00", valid_until="2026-09-09T10:00:00+08:00")
    value.update(changes)
    return value


@pytest.mark.parametrize("change", [
    {"valid_until": "2000-01-01T00:00:00+00:00"},
    {"valid_until": "2026-09-10T09:40:00+08:00"},
    {"valid_until": "2026-09-09T13:40:00+08:00"},
    {"price": "NaN"}, {"price": "Infinity"}, {"price": "10.001"},
    {"volume": 100.5}, {"volume": True}, {"symbol": "SHSE.000001"},
    {"asset_type": "ETF", "board_type": "ETF"}, {"position_effect": "CLOSE"},
])
def test_invalid_orders_fail_closed(tmp_path, change):
    cfg = ready(tmp_path)
    with pytest.raises(ValueError):
        validate_request(cfg, order(cfg, **change), now=NOW)


def test_calendar_session_board_units_and_uat(tmp_path):
    cfg = ready(tmp_path)
    validate_request(cfg, order(cfg), now=NOW)
    with pytest.raises(ValueError):
        validate_request(cfg, order(cfg), now=NOW.replace(hour=12))
    holiday = NOW.replace(month=10, day=1)
    with pytest.raises(ValueError):
        validate_request(cfg, order(cfg, valid_until="2026-10-01T10:00:00+08:00"), now=holiday)
    for volume in (200, 201):
        validate_request(cfg, order(cfg, symbol="SHSE.688002", ts_code="688002.SH", board_type="STAR", volume=volume), now=NOW)
    with pytest.raises(ValueError):
        validate_request(cfg, order(cfg, symbol="SHSE.688002", ts_code="688002.SH", board_type="STAR", volume=100), now=NOW)
    validate_request(cfg, order(cfg, symbol="SHSE.688002", ts_code="688002.SH", board_type="STAR", side="SELL", position_effect="CLOSE", volume=199), available=199, now=NOW)
    with pytest.raises(ValueError):
        validate_request(cfg, order(cfg, side="SELL", position_effect="CLOSE"), available=0, now=NOW)
    unverified = replace(cfg, allowed_boards=("SH_MAIN",), verified_boards=("SH_MAIN",))
    star = order(cfg, symbol="SHSE.688002", ts_code="688002.SH", board_type="STAR", volume=200, uat_key="test-uat")
    with pytest.raises(ValueError):
        validate_request(unverified, star, now=NOW)
    scoped = replace(unverified, uat={"enabled": True, "key": "test-uat", "symbols": ["SHSE.688002"], "expires_at": "2026-09-09T11:00:00+08:00", "max_orders": 2})
    validate_request(scoped, star, now=NOW)
    with pytest.raises(ValueError):
        validate_request(scoped, {**star, "volume": 201}, now=NOW)


def test_runner_final_gate_never_calls_sdk_when_closed(tmp_path, monkeypatch):
    cfg = ready(tmp_path)
    db = Database(cfg.sqlite_path)
    strategy.configure(cfg, db)
    db.set_state("kill_switch", "false")
    db.set_state("auth_state", "AUTH_OK")
    monkeypatch.setattr(strategy, "_account_id", cfg.expected_account_id)
    sdk = Mock()
    monkeypatch.setattr(strategy, "order_volume", sdk, raising=False)
    monkeypatch.setattr(strategy, "_record_event", Mock())
    class Clock(dt.datetime):
        @classmethod
        def now(cls, tz=None):
            return NOW.astimezone(tz) if tz else NOW.replace(tzinfo=None)
    monkeypatch.setattr(strategy.dt, "datetime", Clock)
    for latest in (replace(cfg, global_enabled=False), replace(cfg, global_kill_switch=True)):
        monkeypatch.setattr(strategy, "fresh_order_config", lambda previous: latest)
        strategy._execute_command({"id": 1, "client_order_id": "fixture", "command_type": "PLACE_ORDER", "payload_json": json.dumps(order(cfg))})
        sdk.assert_not_called()
    monkeypatch.setattr(strategy, "fresh_order_config", lambda previous: cfg)
    strategy._execute_command({"id": 1, "client_order_id": "fixture", "command_type": "PLACE_ORDER", "payload_json": json.dumps(order(cfg, valid_until="2000-01-01T00:00:00Z"))})
    sdk.assert_not_called()
    def unavailable(previous):
        raise OSError("Nacos offline")
    monkeypatch.setattr(strategy, "fresh_order_config", unavailable)
    strategy._execute_command({"id": 1, "client_order_id": "fixture", "command_type": "PLACE_ORDER", "payload_json": json.dumps(order(cfg))})
    sdk.assert_not_called()


def test_quotes_require_actual_book_limits_and_metadata(tmp_path):
    cfg = ready(tmp_path)
    quote = {"eastmoney_symbol": "SHSE.600000", "asset_type": "STOCK", "board_type": "SH_MAIN", "observed_at": NOW.isoformat(), "metadata_date": "2026-09-09", "security_status": "TRADING", "upper_limit": "11", "lower_limit": "9", "ask1": "10", "bid1": "9.99"}
    validate_quote(cfg, order(cfg), quote, NOW)
    for changes in ({"ask1": "0"}, {"upper_limit": "0"}, {"security_status": "SUSPENDED"}, {"metadata_date": "2026-09-08"}, {"asset_type": "ETF"}, {"observed_at": "2026-09-09T09:39:00+08:00"}):
        with pytest.raises(ValueError):
            validate_quote(cfg, order(cfg), {**quote, **changes}, NOW)


def test_full_quote_preserves_nested_book_and_metadata(tmp_path, monkeypatch):
    cfg = ready(tmp_path)
    strategy.configure(cfg, Database(cfg.sqlite_path))
    monkeypatch.setattr(strategy, "current", lambda symbols: [{"symbol": "SHSE.600000", "price": 10.0, "created_at": NOW, "quotes": [{"bid_p": 9.99, "ask_p": 10.0, "bid_v": 1000, "ask_v": 1000}]}], raising=False)
    monkeypatch.setattr(strategy, "get_instruments", lambda **kwargs: [{"symbol": "SHSE.600000", "sec_type": 1, "upper_limit": 11, "lower_limit": 9, "price_tick": .01, "trade_n": 1, "trade_date": NOW, "listed_date": "1999-01-01T00:00:00+08:00", "is_suspended": False}], raising=False)
    result = strategy._refresh_quotes(["SHSE.600000"])[0]
    assert result["bid1"] == "9.99" and result["ask1"] == "10.0"
    assert result["metadata_date"] == "2026-09-09"
    validate_quote(cfg, order(cfg), result, NOW)
