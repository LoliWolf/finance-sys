from __future__ import annotations

import json
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT))

from bridge_api.config import load_config  # noqa: E402
from bridge_api.server import _normalize_quote_symbols, _validate_trading_unit  # noqa: E402


def main() -> None:
    cfg = load_config(ROOT)
    assert cfg.simulation_only
    assert cfg.global_kill_switch
    assert cfg.trading_rule_version
    assert _normalize_quote_symbols(["600000.SH", "300502.SZ", "SHSE.688002"]) == [
        "SHSE.600000",
        "SZSE.300502",
        "SHSE.688002",
    ]
    _validate_trading_unit("STAR", "BUY", 200)
    _validate_trading_unit("STAR", "BUY", 201)
    _validate_trading_unit("STAR", "SELL", 199, available=199)
    _validate_trading_unit("CHINEXT", "BUY", 100)
    for args in (("STAR", "BUY", 100, 0), ("STAR", "SELL", 100, 199), ("CHINEXT", "BUY", 101, 0)):
        try:
            _validate_trading_unit(*args)
        except ValueError:
            continue
        raise AssertionError(f"expected board trading-unit rejection: {args}")
    print(
        json.dumps(
            {
                "status": "ok",
                "config_version": cfg.config_version,
                "allowed_boards": cfg.allowed_boards,
                "verified_boards": cfg.verified_boards,
                "trading_rule_version": cfg.trading_rule_version,
                "global_kill_switch": cfg.global_kill_switch,
            },
            ensure_ascii=False,
            separators=(",", ":"),
        )
    )


if __name__ == "__main__":
    main()
