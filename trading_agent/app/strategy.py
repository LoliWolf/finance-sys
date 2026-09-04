from __future__ import annotations

import datetime as dt
import hashlib
from decimal import ROUND_DOWN, Decimal, InvalidOperation
from typing import Any

from .config import AgentConfig
from .http_client import ToolClient
from .skills import blogger_quality, composite_buy_score, decision_key, exit_signal, fixed, trend_confirmation

CST = dt.timezone(dt.timedelta(hours=8))


def run_deterministic(request: dict[str, Any], config: AgentConfig, client: ToolClient) -> dict[str, Any]:
    as_of = _parse_time(str(request["as_of_time"]))
    run_key = str(request["run_key"])
    exit_only = str(request.get("trigger_type", "")).upper() == "EXIT_MONITOR"
    candidates = [] if exit_only else list(client.get("/recommendation-candidates", {"as_of_time": as_of.isoformat(), "limit": config.max_candidates}).get("items", []))
    portfolio = client.get("/portfolio")
    positions = list(portfolio.get("positions", []))
    cycles = list(portfolio.get("cycles", []))
    held_symbols = {str(item.get("symbol", "")) for item in positions if int(item.get("volume", 0)) > 0}
    open_order_symbols = {str(item.get("symbol", "")) for item in portfolio.get("open_orders", [])}
    symbols = sorted({str(item.get("symbol", "")) for item in candidates + positions if item.get("symbol")})
    market_response = client.get("/market-snapshot", {"symbols": ",".join(symbols), "as_of_time": as_of.isoformat()}) if symbols else {"items": []}
    prices = {str(item.get("symbol")): str(item.get("price", "")) for item in market_response.get("items", []) if not item.get("missing_reason") and item.get("price")}
    history_response = client.get("/daily-history", {"symbols": ",".join(symbols), "as_of_time": as_of.isoformat(), "limit": 20}) if symbols and not exit_only else {"items": []}
    history_by_symbol: dict[str, list[dict[str, Any]]] = {}
    for item in history_response.get("items", []):
        history_by_symbol.setdefault(str(item.get("symbol", "")), []).append(item)

    intents: list[dict[str, Any]] = []
    decisions: list[dict[str, Any]] = []
    valid_from, valid_until = _valid_window(as_of)

    position_by_symbol = {str(item.get("symbol", "")): item for item in positions}
    for cycle in sorted(cycles, key=lambda item: int(item.get("id", 0))):
        if len(intents) >= config.max_intents:
            break
        symbol = str(cycle.get("symbol", ""))
        position = position_by_symbol.get(symbol, {})
        available = int(position.get("available_volume", cycle.get("available_volume", 0)) or 0)
        price = _decimal(prices.get(symbol, "0"))
        triggered, exit_reason, score, observed = exit_signal(cycle, price)
        cycle_id = int(cycle.get("id", 0))
        if not triggered or available <= 0 or price <= 0 or symbol in open_order_symbols:
            continue
        limit_price = (price * (Decimal(1) - Decimal(str(config.sell_limit_discount_ratio)))).quantize(Decimal("0.01"), rounding=ROUND_DOWN)
        intent_key = _intent_key(str(request["strategy_version"]), as_of, 0, symbol, "SELL", valid_until, cycle_id)
        intent = {
            "intent_key": intent_key, "position_cycle_id": cycle_id, "symbol": symbol,
            "ts_code": str(cycle.get("ts_code", "")), "market": str(cycle.get("market", "")).upper(),
            "asset_type": str(cycle.get("asset_type", "STOCK")).upper(), "board_type": str(cycle.get("board_type", "")).upper(),
            "action": "SELL", "proposed_order_type": "LIMIT", "proposed_limit_price": limit_price.to_eng_string(),
            "proposed_position_ratio": "1.00000000", "proposed_volume": available,
            "valid_from": valid_from.isoformat(), "valid_until": valid_until.isoformat(), "confidence": "1.00000000",
            "evidence_refs": [f"position_cycle:{cycle_id}", f"exit_rule:{exit_reason}"],
            "reason": f"确定性退出规则触发：{exit_reason}",
        }
        intents.append(intent)
        decisions.append(_decision(run_key, intent_key, cycle_id, "EXIT", "position_exit_rule", "exit-rule-v1", "SELL", score, intent["reason"], observed, {"exit_reason": exit_reason}, as_of))

    scored_candidates: list[tuple[Decimal, int, dict[str, Any], dict[str, Any]]] = []
    performance_by_blogger: dict[int, dict[str, Any]] = {}
    for candidate in candidates:
        symbol = str(candidate.get("symbol", ""))
        if symbol in held_symbols or symbol in open_order_symbols:
            continue
        if str(candidate.get("asset_type", "")).upper() not in {"STOCK", "ETF"} or str(candidate.get("market", "")).upper() not in {"SH", "SZ"}:
            continue
        if str(candidate.get("direction", "")).upper() not in {"LONG", "BUY"}:
            continue
        if bool(candidate.get("no_price_limit_period")):
            continue
        confidence = _decimal(candidate.get("confidence", "0"))
        if confidence < Decimal(str(config.min_confidence)):
            continue
        blogger_id = int(candidate.get("blogger_id", 0))
        if blogger_id not in performance_by_blogger:
            performance_by_blogger[blogger_id] = client.get(f"/blogger-performance/{blogger_id}", {"window_days": 30})
        performance = performance_by_blogger[blogger_id]
        blogger_pass, blogger_score, blogger_reason, blogger_observed = blogger_quality(performance, config.min_blogger_sample_count, config.min_blogger_win_rate)
        trend_pass, trend_score, trend_reason, trend_observed = trend_confirmation(history_by_symbol.get(symbol, []))
        event_id = int(candidate.get("recommendation_event_id", 0))
        decisions.append(_decision(run_key, "", None, "BUY_FILTER", "blogger_quality", "blogger-quality-v1", "PASS" if blogger_pass else "REJECT", blogger_score, blogger_reason, {"blogger_id": blogger_id, **blogger_observed}, {}, as_of, f"event:{event_id}"))
        decisions.append(_decision(run_key, "", None, "BUY_FILTER", "trend_confirmation", "trend-confirmation-v1", "PASS" if trend_pass else "REJECT", trend_score, trend_reason, {"symbol": symbol, **trend_observed}, {}, as_of, f"event:{event_id}"))
        if not blogger_pass or not trend_pass:
            continue
        price = prices.get(symbol, "")
        if not price or _decimal(price) <= 0:
            continue
        score = composite_buy_score(confidence, blogger_score, trend_score)
        scored_candidates.append((score, event_id, candidate, {"blogger_score": fixed(blogger_score), "trend_score": fixed(trend_score), "confidence": fixed(confidence)}))

    scored_candidates.sort(key=lambda item: (item[0], str(item[2].get("recommend_date", "")), item[1]), reverse=True)
    for score, event_id, candidate, score_parts in scored_candidates:
        if len(intents) >= config.max_intents:
            break
        symbol = str(candidate["symbol"])
        intent_key = _intent_key(str(request["strategy_version"]), as_of, event_id, symbol, "BUY", valid_until)
        intent = {
            "intent_key": intent_key, "recommendation_event_id": event_id, "candidate_plan_id": candidate.get("candidate_plan_id"),
            "symbol": symbol, "ts_code": str(candidate.get("ts_code", "")), "market": str(candidate["market"]).upper(),
            "asset_type": str(candidate["asset_type"]).upper(), "board_type": str(candidate.get("board_type", "")).upper(),
            "action": "BUY", "proposed_order_type": "LIMIT",
            "proposed_limit_price": _decimal(prices[symbol]).quantize(Decimal("0.000001")).to_eng_string(),
            "proposed_position_ratio": min(_decimal(candidate.get("position_ratio", "0.03")), Decimal("0.05")).quantize(Decimal("0.00000001")).to_eng_string(),
            "valid_from": valid_from.isoformat(), "valid_until": valid_until.isoformat(), "confidence": fixed(score),
            "evidence_refs": list(candidate.get("evidence_refs", [])),
            "reason": "推荐事实、博主质量和本地日线趋势均满足确定性选股规则",
        }
        intents.append(intent)
        decisions.append(_decision(run_key, intent_key, None, "RANK", "composite_buy_rank", "composite-rank-v1", "BUY", score, intent["reason"], score_parts, {"rank_score": fixed(score)}, as_of, f"event:{event_id}:rank"))

    return {
        "schema_version": config.schema_version, "run_key": run_key, "as_of_time": str(request["as_of_time"]),
        "strategy_name": str(request["strategy_name"]), "strategy_version": str(request["strategy_version"]),
        "prompt_version": "deterministic-skills-v2", "tool_contract_version": config.tool_contract_version,
        "candidate_count": len(candidates), "intents": intents, "skill_decisions": decisions,
    }


def _decision(run_key: str, intent_key: str, cycle_id: int | None, stage: str, skill_name: str, skill_version: str, decision: str, score: Decimal, reason: str, input_value: dict[str, Any], output: dict[str, Any], at: dt.datetime, entity_key: str = "") -> dict[str, Any]:
    key_entity = entity_key or intent_key or f"cycle:{cycle_id or 0}"
    return {
        "decision_key": decision_key(run_key, skill_name, key_entity), "intent_key": intent_key,
        "position_cycle_id": cycle_id, "stage": stage, "skill_name": skill_name, "skill_version": skill_version,
        "decision": decision, "score": fixed(score), "reason": reason, "input": input_value,
        "output": output, "evaluated_at": at.isoformat(),
    }


def _parse_time(value: str) -> dt.datetime:
    parsed = dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    if parsed.tzinfo is None:
        raise ValueError("as_of_time must include a timezone")
    return parsed.astimezone(CST)


def _valid_window(as_of: dt.datetime) -> tuple[dt.datetime, dt.datetime]:
    local = as_of.astimezone(CST).replace(microsecond=0)
    if (local.hour, local.minute) < (9, 35):
        return local.replace(hour=9, minute=35, second=0), local.replace(hour=10, minute=0, second=0)
    return local, local + dt.timedelta(minutes=25)


def _intent_key(strategy_version: str, as_of: dt.datetime, event_id: int, symbol: str, action: str, valid_until: dt.datetime, position_cycle_id: int = 0) -> str:
    identity = f"event:{event_id}" if event_id else f"cycle:{position_cycle_id}"
    parts = [strategy_version, as_of.astimezone(CST).date().isoformat(), identity, symbol.upper(), action.upper(), valid_until.isoformat()]
    return hashlib.sha256("|".join(parts).encode("utf-8")).hexdigest()


def _decimal(value: Any) -> Decimal:
    try:
        return Decimal(str(value))
    except (InvalidOperation, ValueError) as exc:
        raise ValueError(f"invalid decimal value: {value!r}") from exc
