from __future__ import annotations

import pathlib

from bridge_api.auth import canonical_string, sign, verify
from bridge_api.config import BridgeConfig
from bridge_api.db import Database
from bridge_api.server import _normalize_quote_symbols, _validate_trading_unit
from gm_runner import strategy


def config(tmp_path: pathlib.Path) -> BridgeConfig:
    return BridgeConfig(
        config_version=22,
        base_url="https://127.0.0.1:8111",
        callback_url="http://127.0.0.1:30006/internal/v1/trading/bridge-events",
        expected_account_id="sim",
        strategy_id="strategy",
        simulation_only=True,
        hmac_key_id="key",
        hmac_secret="secret",
        max_clock_skew_seconds=60,
        nonce_ttl_seconds=300,
        cert_file=tmp_path / "cert.pem",
        key_file=tmp_path / "key.pem",
        sqlite_path=tmp_path / "bridge.db",
        token="token",
        token_fingerprint="fp",
        probe_interval_seconds=60,
        invalid_token_error_codes=(1000,),
        auth_service_error_codes=(1025, 1026),
        transient_failure_threshold=2,
        global_enabled=False,
        global_kill_switch=True,
        allowed_boards=("SH_MAIN", "SZ_MAIN", "CHINEXT", "STAR", "ETF"),
        verified_boards=("SH_MAIN", "SZ_MAIN", "CHINEXT", "STAR", "ETF"),
        trading_rule_version="cn-equity-board-rules-2026-v1",
    )


def test_database_defaults_to_fail_closed(tmp_path):
    db = Database(tmp_path / "bridge.db")
    assert db.state("kill_switch") == "true"
    db.set_state("account_id", "sim")
    assert db.state("account_id") == "sim"
    with db.connect() as connection:
        columns = {row[1] for row in connection.execute("PRAGMA table_info(bridge_commands)")}
    assert "result_json" in columns


def test_hmac_and_nonce_replay(tmp_path):
    cfg = config(tmp_path)
    db = Database(cfg.sqlite_path)
    body = b'{"x":1}'
    import time

    timestamp = str(int(time.time() * 1000))
    nonce = "nonce-1"
    canonical = canonical_string("POST", "/v1/orders", {}, body, timestamp, nonce)
    headers = {
        "X-FS-Key-Id": "key",
        "X-FS-Timestamp": timestamp,
        "X-FS-Nonce": nonce,
        "X-FS-Signature": sign("secret", canonical),
    }
    verify(db, cfg, "POST", "/v1/orders", {}, body, headers)
    try:
        verify(db, cfg, "POST", "/v1/orders", {}, body, headers)
        assert False, "replayed nonce must fail"
    except PermissionError:
        pass


def test_quote_symbol_normalization():
    assert _normalize_quote_symbols(["600000.SH", "SZSE.000001", "600000"]) == [
        "SHSE.600000",
        "SZSE.000001",
    ]


def test_board_trading_units():
    for volume in (200, 201):
        _validate_trading_unit("STAR", "BUY", volume)
    try:
        _validate_trading_unit("STAR", "BUY", 100)
        assert False, "STAR 100-share buy must fail"
    except ValueError:
        pass
    _validate_trading_unit("STAR", "SELL", 199, available=199)
    try:
        _validate_trading_unit("STAR", "SELL", 100, available=199)
        assert False, "STAR residual position must be sold in full"
    except ValueError:
        pass
    _validate_trading_unit("CHINEXT", "BUY", 100)
    try:
        _validate_trading_unit("CHINEXT", "BUY", 101)
        assert False, "ChiNext buy must follow 100-share lots"
    except ValueError:
        pass


def test_runner_refreshes_normalized_quote_cache(tmp_path, monkeypatch):
    cfg = config(tmp_path)
    db = Database(cfg.sqlite_path)
    strategy.configure(cfg, db)
    monkeypatch.setattr(
        strategy,
        "current",
        lambda symbols, fields="": [
            {
                "symbol": symbols[0],
                "price": 10.23,
                "created_at": "2026-08-24T09:31:00+08:00",
            }
        ],
        raising=False,
    )
    quotes = strategy._refresh_quotes(["SHSE.600000"])
    assert quotes == [
        {
            "symbol": "600000",
            "eastmoney_symbol": "SHSE.600000",
            "price": "10.23",
            "observed_at": "2026-08-24T09:31:00.000+08:00",
            "source": "EASTMONEY_GM_CURRENT_PRICE",
        }
    ]
    assert "SHSE.600000" in db.state("quotes_json")


def test_position_normalization_uses_t1_sellable_and_market_fields():
    item = {
        "volume": 100,
        "volume_today": 100,
        "available": 100,
        "available_now": 0,
        "vwap": 9.18,
        "price": 9.19,
        "last_price": 9.180000305175781,
        "amount": 918.0000305175781,
        "market_value": 918.9999580383301,
        "fpnl": 0.9999275207519531,
    }
    normalized = strategy._normalize_position("sim", "SHSE.600000", item)
    assert normalized["available_volume"] == 0
    assert normalized["today_volume"] == 100
    assert normalized["last_price"] == "9.180000"
    assert normalized["market_value"] == "919.00"
    assert normalized["floating_pnl"] == "1.00"


def test_cash_frozen_prefers_open_order_frozen():
    raw = {"frozen": 918.0000305175781, "order_frozen": 0.0}
    assert strategy._decimal(raw, 2, "order_frozen", "frozen_cash", "frozen") == "0.00"


def test_commission_data_status_requires_official_cash_fields():
    reported = {
        "cum_trade": 1814.0,
        "cum_commission": 10.0,
        "last_trade": 896.0,
        "last_commission": 5.0,
    }
    assert strategy._commission_data_status(reported) == "REPORTED"
    assert strategy._commission_data_status({**reported, "last_commission": None}) == "UNAVAILABLE"
