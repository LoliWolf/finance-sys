from __future__ import annotations

import datetime as dt
import re
from decimal import Decimal, InvalidOperation
from typing import Any

from .config import BridgeConfig

CST = dt.timezone(dt.timedelta(hours=8))


def aware_time(value: Any) -> dt.datetime:
    try:
        text = str(value).replace("Z", "+00:00")
        # Go RFC3339Nano trims fractional zeros; Python 3.9/3.10 accepts only
        # three or six fractional digits. Normalize 1..9 digits without losing
        # the timezone or moving the expiry into the future.
        match = re.fullmatch(r"(.+T\d{2}:\d{2}:\d{2})\.(\d{1,9})([+-]\d{2}:\d{2})", text)
        if match:
            text = match[1] + "." + match[2][:6].ljust(6, "0") + match[3]
        result = dt.datetime.fromisoformat(text)
        if result.tzinfo is None:
            raise ValueError("timezone is required")
        return result.astimezone(CST)
    except (ValueError, TypeError) as exc:
        raise ValueError("invalid timezone-aware timestamp") from exc


def positive(value: Any) -> Decimal:
    try:
        number = Decimal(str(value))
    except (InvalidOperation, ValueError) as exc:
        raise ValueError("invalid decimal") from exc
    if not number.is_finite() or number <= 0:
        raise ValueError("decimal must be positive and finite")
    return number


def identity(symbol: str, asset: str) -> tuple[str, str]:
    if not re.fullmatch(r"(?:SHSE|SZSE)\.[0-9]{6}", symbol):
        raise ValueError("canonical Eastmoney symbol is required")
    market, code = symbol.split(".")
    if market == "SHSE" and code.startswith(("51", "52", "56", "58")) or market == "SZSE" and code.startswith("159"):
        board, expected = "ETF", "ETF"
    elif market == "SHSE" and code.startswith(("688", "689")):
        board, expected = "STAR", "STOCK"
    elif market == "SHSE" and code.startswith(("600", "601", "603", "605")):
        board, expected = "SH_MAIN", "STOCK"
    elif market == "SZSE" and code.startswith(("300", "301")):
        board, expected = "CHINEXT", "STOCK"
    elif market == "SZSE" and code.startswith(("000", "001", "002", "003")):
        board, expected = "SZ_MAIN", "STOCK"
    else:
        raise ValueError("unsupported security prefix")
    if asset != expected:
        raise ValueError("asset type conflicts with security identity")
    return board, code + (".SH" if market == "SHSE" else ".SZ")


def uat_authorized(cfg: BridgeConfig, payload: dict[str, Any], now: dt.datetime) -> bool:
    spec = cfg.uat
    if not spec.get("enabled") or not spec.get("key") or payload.get("uat_key") != spec["key"]:
        return False
    try:
        return payload.get("symbol") in spec.get("symbols", []) and aware_time(spec.get("expires_at")) > now
    except ValueError:
        return False


def validate_request(cfg: BridgeConfig, payload: dict[str, Any], available: int = 0, now: dt.datetime | None = None) -> None:
    now = (now or dt.datetime.now(CST)).astimezone(CST)
    if not cfg.global_enabled or cfg.global_kill_switch:
        raise ValueError("global trading switch refuses placement")
    if cfg.environment != "SIMULATION" or cfg.allow_live or not cfg.simulation_only:
        raise ValueError("simulation-only invariant failed")
    if payload.get("environment") != "SIMULATION" or payload.get("expected_account_id") != cfg.expected_account_id:
        raise ValueError("simulation account mismatch")
    if payload.get("strategy_id") != cfg.strategy_id:
        raise ValueError("strategy_id mismatch")
    expires = aware_time(payload.get("valid_until"))
    if expires <= now or expires.date() != now.date():
        raise ValueError("expired or cross-day order")
    day = now.date().isoformat()
    cal = cfg.calendar
    if not cal.get("source") or not cal.get("version") or not cal.get("coverage_from", "") <= day <= cal.get("coverage_to", ""):
        raise ValueError("trading calendar unavailable or outside coverage")
    if day not in cal.get("open_dates", []) or now.weekday() >= 5:
        raise ValueError("not a trading day")
    clock = now.strftime("%H:%M:%S")
    active_end = next((end for start, end in cfg.windows if start <= clock < end), None)
    if active_end is None or expires.strftime("%H:%M:%S") > active_end:
        raise ValueError("outside trading window or expiry crosses session end")
    side = payload.get("side")
    if side not in {"BUY", "SELL"} or payload.get("order_type") != "LIMIT":
        raise ValueError("only BUY/SELL LIMIT orders are supported")
    if payload.get("position_effect") != ("OPEN" if side == "BUY" else "CLOSE"):
        raise ValueError("position effect mismatch")
    board, ts_code = identity(str(payload.get("symbol", "")), str(payload.get("asset_type", "")))
    if payload.get("board_type") != board or payload.get("ts_code", ts_code) != ts_code:
        raise ValueError("security identity mismatch")
    verified = board in cfg.allowed_boards and board in cfg.verified_boards
    acceptance = uat_authorized(cfg, payload, now)
    if cfg.uat.get("enabled") and not acceptance:
        raise ValueError("UAT mode permits only explicitly scoped commands")
    if not verified and not acceptance:
        raise ValueError("board is not verified and no scoped UAT authorization exists")
    if payload.get("uat_key") and not acceptance:
        raise ValueError("UAT authorization missing or expired")
    if payload.get("trading_rule_version") != cfg.trading_rule_version:
        raise ValueError("trading rule version mismatch")
    volume = payload.get("volume")
    if isinstance(volume, bool) or not isinstance(volume, int):
        raise ValueError("volume must be an integer")
    validate_unit(board, str(side), volume, available)
    if acceptance and volume > (200 if board == "STAR" else 100):
        raise ValueError("UAT is limited to the minimum trading unit")
    price = positive(payload.get("price"))
    tick = Decimal("0.001") if board == "ETF" else Decimal("0.01")
    if price % tick:
        raise ValueError("price does not match security tick")


def validate_unit(board: str, side: str, volume: int, available: int = 0) -> None:
    if board not in {"SH_MAIN", "SZ_MAIN", "CHINEXT", "STAR", "ETF"} or volume <= 0:
        raise ValueError("invalid board or volume")
    minimum, step, maximum = (200, 1, 100000) if board == "STAR" else (100, 100, 1000000)
    if volume > maximum:
        raise ValueError("volume exceeds maximum limit order size")
    if side == "BUY":
        if volume < minimum or (volume-minimum) % step:
            raise ValueError("buy volume violates board unit")
    elif side == "SELL":
        if volume > available:
            raise ValueError("sell volume exceeds provider sellable position")
        if volume != available and (volume < minimum or (volume-minimum) % step):
            raise ValueError("residual position must be sold in full")
    else:
        raise ValueError("unsupported side")


def validate_quote(cfg: BridgeConfig, payload: dict[str, Any], quote: dict[str, Any], now: dt.datetime | None = None) -> None:
    now = now or dt.datetime.now(CST)
    if quote.get("eastmoney_symbol") != payload["symbol"]:
        raise ValueError("quote symbol mismatch")
    age = (now-aware_time(quote.get("observed_at"))).total_seconds()
    if age < -1 or age > cfg.max_snapshot_age_seconds:
        raise ValueError("quote is stale")
    if quote.get("metadata_date") != now.astimezone(CST).date().isoformat() or quote.get("security_status") != "TRADING":
        raise ValueError("security metadata unavailable or suspended")
    if quote.get("asset_type") != payload["asset_type"] or quote.get("board_type") != payload["board_type"]:
        raise ValueError("provider security identity mismatch")
    p, upper, lower = positive(payload["price"]), positive(quote.get("upper_limit")), positive(quote.get("lower_limit"))
    if not lower <= p <= upper:
        raise ValueError("price outside daily limits")
    # Conservative executable subset of the continuous-auction price cage.
    # Orders at the current opposite best price are inside the cage; no guessed
    # book/price-cage values are used when full tick permission is unavailable.
    if payload["board_type"] != "ETF":
        reference = positive(quote.get("ask1" if payload["side"] == "BUY" else "bid1"))
        if payload["side"] == "BUY" and p > reference or payload["side"] == "SELL" and p < reference:
            raise ValueError("price outside conservative opposite-best-price cage")
