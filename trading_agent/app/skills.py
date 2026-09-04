from __future__ import annotations

import hashlib
from decimal import Decimal
from typing import Any


def trend_confirmation(history: list[dict[str, Any]]) -> tuple[bool, Decimal, str, dict[str, Any]]:
    closes = [Decimal(str(item["close_price"])) for item in history if Decimal(str(item.get("close_price", 0))) > 0]
    if len(closes) < 5:
        return False, Decimal("0"), "少于5个有效交易日，趋势不可评估", {"sample_count": len(closes)}
    short = sum(closes[-5:]) / Decimal(5)
    long_window = closes[-20:]
    long = sum(long_window) / Decimal(len(long_window))
    momentum = closes[-1] / closes[-5] - Decimal(1)
    passed = closes[-1] >= short and short >= long and momentum > Decimal("-0.03")
    score = _clamp(Decimal("0.5") + momentum * Decimal(5), Decimal(0), Decimal(1))
    return passed, score, "收盘价、5日均线、20日均线和5日动量满足趋势确认" if passed else "趋势确认未通过", {
        "sample_count": len(closes), "latest_close": str(closes[-1]), "ma5": _fixed(short),
        "ma20": _fixed(long), "momentum_5d": _fixed(momentum),
    }


def blogger_quality(performance: dict[str, Any], minimum_samples: int, minimum_win_rate: float) -> tuple[bool, Decimal, str, dict[str, Any]]:
    samples = int(performance.get("evaluable_count", 0))
    win_rate = Decimal(str(performance.get("win_rate", 0)))
    passed = samples >= minimum_samples and win_rate >= Decimal(str(minimum_win_rate))
    return passed, _clamp(win_rate, Decimal(0), Decimal(1)), "博主可评估样本数和胜率均达到阈值" if passed else "博主样本数或胜率未达到阈值", {
        "evaluable_count": samples, "win_rate": _fixed(win_rate), "minimum_samples": minimum_samples,
        "minimum_win_rate": str(minimum_win_rate),
    }


def composite_buy_score(confidence: Decimal, blogger_score: Decimal, trend_score: Decimal) -> Decimal:
    return _clamp(confidence * Decimal("0.40") + blogger_score * Decimal("0.35") + trend_score * Decimal("0.25"), Decimal(0), Decimal(1))


def exit_signal(cycle: dict[str, Any], latest_price: Decimal) -> tuple[bool, str, Decimal, dict[str, Any]]:
    stop = Decimal(str(cycle.get("stop_loss_price", 0)))
    take = Decimal(str(cycle.get("take_profit_price", 0)))
    holding_days = int(cycle.get("holding_trade_days", 0))
    max_days = int(cycle.get("max_holding_trade_days", 0))
    reason = ""
    if stop > 0 and latest_price <= stop:
        reason = "STOP_LOSS"
    elif take > 0 and latest_price >= take:
        reason = "TAKE_PROFIT"
    elif max_days > 0 and holding_days >= max_days:
        reason = "MAX_HOLDING_DAYS"
    return bool(reason), reason, Decimal(1) if reason else Decimal(0), {
        "latest_price": str(latest_price), "stop_loss_price": str(stop), "take_profit_price": str(take),
        "holding_trade_days": holding_days, "max_holding_trade_days": max_days,
    }


def decision_key(run_key: str, skill_name: str, entity_key: str) -> str:
    return hashlib.sha256(f"{run_key}|{skill_name}|{entity_key}".encode("utf-8")).hexdigest()


def fixed(value: Decimal) -> str:
    return _fixed(value)


def _fixed(value: Decimal) -> str:
    return value.quantize(Decimal("0.00000001")).to_eng_string()


def _clamp(value: Decimal, lower: Decimal, upper: Decimal) -> Decimal:
    return max(lower, min(value, upper))
