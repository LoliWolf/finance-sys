from __future__ import annotations

import datetime as dt
import hashlib
import json
import socket
import sqlite3
import traceback
from dataclasses import asdict
from decimal import ROUND_HALF_UP, Decimal
from typing import Any

from bridge_api.config import BridgeConfig
from bridge_api.db import Database, now_iso

_config: BridgeConfig | None = None
_db: Database | None = None
_account_id = ""
_last_probe_at: dt.datetime | None = None
_auth_failures = 0


def configure(config: BridgeConfig, db: Database) -> None:
    global _config, _db
    _config = config
    _db = db


def init(context: Any) -> None:
    cfg, db = _require_state()
    accounts = list(context.accounts.values())
    account_ids = [str(getattr(account, "id", "")) for account in accounts if getattr(account, "id", "")]
    db.set_state("runner_host", socket.gethostname())
    db.set_state("strategy_id", cfg.strategy_id)
    db.set_state("token_fingerprint", cfg.token_fingerprint)
    db.set_state("terminal_state", "CONNECTED")
    db.set_state("runner_heartbeat_at", now_iso())
    if len(account_ids) != 1:
        _fail_closed("ACCOUNT_MISMATCH", f"expected one simulation account, discovered {len(account_ids)}")
    else:
        _set_account(account_ids[0])
    result = timer(_on_timer, 500, 100)
    if int(result.get("status", -1)) != 0:
        _fail_closed("RUNNER_TIMER_FAILED", json.dumps(result, ensure_ascii=False))


def _set_account(account_id: str) -> None:
    global _account_id
    cfg, db = _require_state()
    _account_id = account_id
    db.set_state("account_id", account_id)
    if not cfg.expected_account_id:
        _fail_closed("ACCOUNT_DISCOVERY_REQUIRED", "account discovered; expected_account_id must be recorded in Nacos")
    elif account_id != cfg.expected_account_id:
        _fail_closed("ACCOUNT_MISMATCH", "discovered account does not match expected_account_id")
    else:
        db.set_state("account_state", "READY")


def _on_timer(context: Any) -> None:
    global _last_probe_at
    cfg, db = _require_state()
    db.set_state("runner_heartbeat_at", now_iso())
    now = dt.datetime.now(dt.timezone.utc)
    if _last_probe_at is None or (now - _last_probe_at).total_seconds() >= cfg.probe_interval_seconds:
        _last_probe_at = now
        _probe_and_snapshot()
    command = _claim_command()
    if command is not None:
        _execute_command(command)


def _probe_and_snapshot() -> None:
    global _auth_failures
    cfg, db = _require_state()
    if not _account_id:
        return
    try:
        cash = get_cash(_account_id)
        positions = get_position(_account_id)
        orders = get_orders()
        executions = get_execution_reports()
        _auth_failures = 0
        db.set_state("auth_state", "AUTH_OK")
        db.set_state("last_auth_success_at", now_iso())
        db.set_state("account_state", "READY" if _account_id == cfg.expected_account_id else "MISMATCH")
        _save_snapshot(cash, positions, orders, executions)
    except Exception as exc:
        _auth_failures += 1
        code = _error_code(exc)
        if code in cfg.invalid_token_error_codes:
            _fail_closed("AUTH_FAILED", f"AUTH_TOKEN_INVALID:{code}")
        elif code == 1026:
            _fail_closed("AUTH_FAILED", "TOKEN_REFRESH_ERROR:1026")
        elif code in cfg.auth_service_error_codes:
            db.set_state("auth_state", "AUTH_DEGRADED")
            if _auth_failures >= cfg.transient_failure_threshold:
                _fail_closed("AUTH_FAILED", f"AUTH_SERVICE_UNAVAILABLE:{code}")
        else:
            db.set_state("auth_state", "AUTH_DEGRADED")
            db.set_state("last_error", _safe_error(exc))


def _claim_command() -> sqlite3.Row | None:
    _, db = _require_state()
    with db.transaction() as connection:
        row = connection.execute(
            "SELECT * FROM bridge_commands WHERE status IN ('QUEUED','RETRY') "
            "AND (next_attempt_at IS NULL OR next_attempt_at <= ?) ORDER BY id LIMIT 1",
            (now_iso(),),
        ).fetchone()
        if row is None:
            return None
        connection.execute(
            "UPDATE bridge_commands SET status='RUNNING',claimed_by=?,claimed_at=?,attempt_count=attempt_count+1,updated_at=? WHERE id=?",
            (socket.gethostname(), now_iso(), now_iso(), row["id"]),
        )
        return row


def _execute_command(row: sqlite3.Row) -> None:
    cfg, db = _require_state()
    payload = json.loads(row["payload_json"])
    command_type = row["command_type"]
    try:
        if command_type == "PLACE_ORDER":
            if db.state("kill_switch", "true").lower() == "true" or db.state("auth_state") != "AUTH_OK" or _account_id != cfg.expected_account_id:
                raise RuntimeError("PLACE_ORDER blocked by Bridge safety state")
            result = order_volume(
                symbol=str(payload["symbol"]),
                volume=int(payload["volume"]),
                side=OrderSide_Buy if payload["side"] == "BUY" else OrderSide_Sell,
                order_type=OrderType_Limit,
                position_effect=PositionEffect_Open if payload["position_effect"] == "OPEN" else PositionEffect_Close,
                price=float(Decimal(str(payload["price"]))),
                account=_account_id,
            )
            if not result:
                raise RuntimeError("gm order_volume returned no order")
            provider_order = _plain(result[0])
            cl_ord_id = str(provider_order.get("cl_ord_id", ""))
            with db.connect() as connection:
                connection.execute(
                    "UPDATE bridge_commands SET status='DONE',cl_ord_id=?,finished_at=?,updated_at=? WHERE id=?",
                    (cl_ord_id, now_iso(), now_iso(), row["id"]),
                )
            _record_event("ORDER_STATUS", row["client_order_id"], provider_order)
        elif command_type == "CANCEL_ORDER":
            original = _find_place_command(str(payload["client_order_id"]))
            if original is None or not original["cl_ord_id"]:
                raise RuntimeError("provider order id is unavailable for cancel")
            order_cancel({"cl_ord_id": original["cl_ord_id"], "account_id": _account_id})
            _mark_done(row["id"])
        elif command_type == "REFRESH_SNAPSHOT":
            _probe_and_snapshot()
            _mark_done(row["id"])
        elif command_type == "REFRESH_QUOTES":
            quotes = _refresh_quotes(payload.get("symbols", []))
            _mark_done(row["id"], quotes)
        else:
            raise RuntimeError(f"unsupported command type {command_type}")
    except Exception as exc:
        code = _error_code(exc)
        with db.connect() as connection:
            connection.execute(
                "UPDATE bridge_commands SET status='FAILED',error_code=?,error_message=?,finished_at=?,updated_at=? WHERE id=?",
                (str(code or "RUNNER_ERROR"), _safe_error(exc), now_iso(), now_iso(), row["id"]),
            )
        if code in cfg.invalid_token_error_codes or code in cfg.auth_service_error_codes:
            _fail_closed("AUTH_FAILED", f"gm command authentication failure:{code}")


def _mark_done(command_id: int, result: Any = None) -> None:
    _, db = _require_state()
    result_json = json.dumps(result if result is not None else {}, ensure_ascii=False, separators=(",", ":"), default=str)
    with db.connect() as connection:
        connection.execute(
            "UPDATE bridge_commands SET status='DONE',result_json=?,finished_at=?,updated_at=? WHERE id=?",
            (result_json, now_iso(), now_iso(), command_id),
        )


def _refresh_quotes(symbols: Any) -> list[dict[str, Any]]:
    _, db = _require_state()
    if not isinstance(symbols, list) or not symbols:
        raise RuntimeError("REFRESH_QUOTES requires symbols")
    requested = [str(symbol).strip().upper() for symbol in symbols if str(symbol).strip()]
    captured_at = now_iso()
    try:
        raw_quotes = current(requested, fields="symbol,price,created_at")
    except Exception as exc:
        if _error_code(exc) != 1028:
            raise
        raw_quotes = current_price(requested)
    normalized: list[dict[str, Any]] = []
    for raw_quote in raw_quotes or []:
        item = _plain(raw_quote)
        eastmoney_symbol = str(item.get("symbol", "")).strip().upper()
        price = _number(item, "price", "last_price")
        try:
            if not eastmoney_symbol or Decimal(price) <= 0:
                continue
        except Exception:
            continue
        normalized.append(
            {
                "symbol": eastmoney_symbol.split(".")[-1],
                "eastmoney_symbol": eastmoney_symbol,
                "price": price,
                "observed_at": _quote_time(item, captured_at),
                "source": "EASTMONEY_GM_CURRENT_PRICE",
            }
        )
    db.set_state("quotes_json", normalized)
    db.set_state("quotes_refreshed_at", captured_at)
    return normalized


def _quote_time(raw: dict[str, Any], fallback: str) -> str:
    for key in ("created_at", "updated_at", "trade_time", "last_time", "time", "datetime"):
        value = raw.get(key)
        if value in (None, ""):
            continue
        if hasattr(value, "isoformat"):
            candidate = value
        elif isinstance(value, (int, float)):
            seconds = float(value) / 1000 if float(value) > 10_000_000_000 else float(value)
            candidate = dt.datetime.fromtimestamp(seconds, tz=dt.timezone.utc)
        else:
            try:
                candidate = dt.datetime.fromisoformat(str(value).replace("Z", "+00:00"))
            except ValueError:
                continue
        if isinstance(candidate, dt.datetime):
            if candidate.tzinfo is None:
                candidate = candidate.replace(tzinfo=dt.timezone(dt.timedelta(hours=8)))
            return candidate.isoformat(timespec="milliseconds")
    return fallback


def _find_place_command(client_order_id: str) -> sqlite3.Row | None:
    _, db = _require_state()
    with db.connect() as connection:
        return connection.execute(
            "SELECT * FROM bridge_commands WHERE client_order_id=? AND command_type='PLACE_ORDER' ORDER BY id DESC LIMIT 1",
            (client_order_id,),
        ).fetchone()


def on_order_status(context: Any, order: Any) -> None:
    raw = _plain(order)
    command = _command_by_cl_ord(str(raw.get("cl_ord_id", "")))
    if command is not None:
        _record_event("ORDER_STATUS", command["client_order_id"], raw)


def on_execution_report(context: Any, execrpt: Any) -> None:
    raw = _plain(execrpt)
    command = _command_by_cl_ord(str(raw.get("cl_ord_id", "")))
    if command is not None:
        _record_event("EXECUTION_REPORT", command["client_order_id"], raw)


def on_account_status(context: Any, account: Any) -> None:
    _, db = _require_state()
    raw = _plain(account)
    db.set_state("account_state", str(raw.get("state", raw.get("status", "UNKNOWN"))))


def on_trade_data_connected(context: Any) -> None:
    _, db = _require_state()
    db.set_state("terminal_state", "CONNECTED")


def on_trade_data_disconnected(context: Any) -> None:
    _fail_closed("TERMINAL_DISCONNECTED", "gm trade data disconnected")


def on_error(context: Any, code: int, info: str) -> None:
    cfg, db = _require_state()
    db.set_state("last_error", f"{code}:{str(info)[:500]}")
    if int(code) in cfg.invalid_token_error_codes or int(code) in cfg.auth_service_error_codes:
        _fail_closed("AUTH_FAILED", f"{code}:{str(info)[:500]}")


def _command_by_cl_ord(cl_ord_id: str) -> sqlite3.Row | None:
    if not cl_ord_id:
        return None
    _, db = _require_state()
    with db.connect() as connection:
        return connection.execute(
            "SELECT * FROM bridge_commands WHERE cl_ord_id=? ORDER BY id DESC LIMIT 1", (cl_ord_id,)
        ).fetchone()


def _record_event(event_type: str, client_order_id: str, raw: dict[str, Any]) -> None:
    cfg, db = _require_state()
    provider_status = str(raw.get("status", raw.get("ord_status", "")))
    normalized = _normalize_status(raw.get("status", raw.get("ord_status")))
    event_at = _event_time(raw)
    cl_ord_id = str(raw.get("cl_ord_id", ""))
    exec_id = str(raw.get("exec_id", raw.get("execution_id", "")))
    event_material = json.dumps(
        {"type": event_type, "account_id": _account_id, "cl_ord_id": cl_ord_id, "exec_id": exec_id, "status": provider_status, "raw": raw},
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
        default=str,
    )
    event_hash = hashlib.sha256(event_material.encode("utf-8")).hexdigest()
    filled_volume = int(raw.get("filled_volume", raw.get("cum_qty", 0)) or 0)
    filled_vwap = str(raw.get("filled_vwap", raw.get("avg_px", "")) or "")
    payload = {
        "schema_version": "eastmoney-bridge-event/v1",
        "event_hash": event_hash,
        "event_type": event_type,
        "client_order_id": client_order_id,
        "account_id": _account_id,
        "cl_ord_id": cl_ord_id,
        "exec_id": exec_id,
        "provider_status": provider_status,
        "normalized_status": normalized,
        "symbol": str(raw.get("symbol", "")),
        "eastmoney_symbol": str(raw.get("symbol", "")),
        "side": _normalize_side(raw.get("side")),
        "filled_volume": filled_volume,
        "filled_vwap": filled_vwap,
        "fill_price": str(raw.get("price", raw.get("last_px", "")) or ""),
        "fill_volume": int(raw.get("volume", raw.get("last_qty", 0)) or 0) if event_type == "EXECUTION_REPORT" else 0,
        "commission": str(raw.get("commission", "0") or "0"),
        "exec_type": str(raw.get("exec_type", "")),
        "event_at": event_at,
        "raw_payload": raw,
    }
    payload_json = json.dumps(payload, ensure_ascii=False, separators=(",", ":"), default=str)
    raw_json = json.dumps(raw, ensure_ascii=False, separators=(",", ":"), default=str)
    with db.transaction() as connection:
        cursor = connection.execute(
            "INSERT OR IGNORE INTO bridge_order_events(event_hash,client_order_id,account_id,cl_ord_id,exec_id,event_type,provider_status,normalized_status,event_at,raw_payload_json,created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)",
            (event_hash, client_order_id, _account_id, cl_ord_id, exec_id, event_type, provider_status, normalized, event_at, raw_json, now_iso()),
        )
        if cursor.rowcount:
            connection.execute(
                "INSERT OR IGNORE INTO bridge_callback_outbox(event_hash,callback_url,payload_json,status,next_attempt_at,created_at,updated_at) VALUES (?,?,?,'PENDING',?,?,?)",
                (event_hash, cfg.callback_url, payload_json, now_iso(), now_iso(), now_iso()),
            )


def _save_snapshot(cash: Any, positions: Any, orders: Any, executions: Any) -> None:
    _, db = _require_state()
    captured = dt.datetime.now(dt.timezone.utc).replace(microsecond=(dt.datetime.now(dt.timezone.utc).microsecond // 1000) * 1000)
    cash_raw = _plain(cash)
    positions_raw = [_plain(item) for item in (positions or [])]
    orders_raw = [_plain(item) for item in (orders or [])]
    executions_raw = [_plain(item) for item in (executions or [])]
    account = {
        "snapshot_version": "",
        "environment": "SIMULATION",
        "account_id": _account_id,
        "account_name": str(cash_raw.get("account_name", "agent")),
        "nav": _decimal(cash_raw, 2, "nav", "balance"),
        "balance": _decimal(cash_raw, 2, "balance", "nav"),
        "available_cash": _decimal(cash_raw, 2, "available", "available_cash"),
        "frozen_cash": _decimal(cash_raw, 2, "order_frozen", "frozen_cash", "frozen"),
        "market_value": _decimal(cash_raw, 2, "market_value", "marketvalue"),
        "floating_pnl": _decimal(cash_raw, 2, "fpnl", "floating_pnl"),
        "cumulative_inout": _decimal(cash_raw, 6, "cum_inout", "cumulative_inout"),
        "cumulative_trade": _decimal(cash_raw, 6, "cum_trade", "cumulative_trade"),
        "cumulative_pnl": _decimal(cash_raw, 6, "cum_pnl", "cumulative_pnl"),
        "cumulative_commission": _decimal(cash_raw, 6, "cum_commission", "cumulative_commission"),
        "last_trade": _decimal(cash_raw, 6, "last_trade"),
        "last_pnl": _decimal(cash_raw, 6, "last_pnl"),
        "last_commission": _decimal(cash_raw, 6, "last_commission"),
        "commission_data_status": _commission_data_status(cash_raw),
        "terminal_state": "CONNECTED",
        "account_state": "READY",
        "snapshot_at": captured.isoformat(timespec="milliseconds"),
    }
    normalized_positions = []
    for item in positions_raw:
        symbol = str(item.get("symbol", ""))
        normalized_positions.append(_normalize_position(_account_id, symbol, item))
    material = json.dumps([account, normalized_positions, orders_raw, executions_raw], sort_keys=True, separators=(",", ":"), default=str)
    version = hashlib.sha256(material.encode("utf-8")).hexdigest()
    account["snapshot_version"] = version
    with db.connect() as connection:
        connection.execute(
            "INSERT OR IGNORE INTO bridge_snapshots(snapshot_version,account_id,terminal_state,account_state,account_json,positions_json,orders_json,executions_json,snapshot_at,created_at) VALUES (?,?,?,?,?,?,?,?,?,?)",
            (
                version,
                _account_id,
                "CONNECTED",
                "READY",
                json.dumps(account, ensure_ascii=False, separators=(",", ":")),
                json.dumps(normalized_positions, ensure_ascii=False, separators=(",", ":")),
                json.dumps(orders_raw, ensure_ascii=False, separators=(",", ":"), default=str),
                json.dumps(executions_raw, ensure_ascii=False, separators=(",", ":"), default=str),
                account["snapshot_at"],
                now_iso(),
            ),
        )


def _normalize_status(value: Any) -> str:
    mapping = {
        globals().get("OrderStatus_PendingNew"): "SUBMITTED",
        globals().get("OrderStatus_New"): "SUBMITTED",
        globals().get("OrderStatus_PartiallyFilled"): "PARTIALLY_FILLED",
        globals().get("OrderStatus_Filled"): "FILLED",
        globals().get("OrderStatus_Canceled"): "CANCELED",
        globals().get("OrderStatus_Rejected"): "REJECTED",
    }
    return mapping.get(value, "UNKNOWN")


def _normalize_side(value: Any) -> str:
    return "BUY" if value == globals().get("OrderSide_Buy") else "SELL" if value == globals().get("OrderSide_Sell") else ""


def _event_time(raw: dict[str, Any]) -> str:
    for key in ("updated_at", "created_at", "exec_time", "transact_time"):
        value = raw.get(key)
        if value:
            if hasattr(value, "isoformat"):
                return value.isoformat()
            return str(value)
    return now_iso()


def _plain(value: Any) -> dict[str, Any]:
    if value is None:
        return {}
    if isinstance(value, dict):
        return {str(key): _json_value(item) for key, item in value.items()}
    try:
        return {str(key): _json_value(item) for key, item in dict(value).items()}
    except Exception:
        result = {}
        for name in dir(value):
            if name.startswith("_"):
                continue
            try:
                item = getattr(value, name)
            except Exception:
                continue
            if callable(item):
                continue
            result[name] = _json_value(item)
        return result


def _json_value(value: Any) -> Any:
    if isinstance(value, (str, int, float, bool)) or value is None:
        return value
    if hasattr(value, "isoformat"):
        return value.isoformat()
    return str(value)


def _number(raw: dict[str, Any], *keys: str) -> str:
    for key in keys:
        if key in raw and raw[key] not in (None, ""):
            return str(raw[key])
    return "0"


def _decimal(raw: dict[str, Any], scale: int, *keys: str) -> str:
    try:
        value = Decimal(_number(raw, *keys))
    except Exception:
        value = Decimal(0)
    quantum = Decimal(1).scaleb(-scale)
    return format(value.quantize(quantum, rounding=ROUND_HALF_UP), f".{scale}f")


def _commission_data_status(raw: dict[str, Any]) -> str:
    required = ("cum_trade", "cum_commission", "last_trade", "last_commission")
    return "REPORTED" if all(key in raw and raw[key] not in (None, "") for key in required) else "UNAVAILABLE"


def _normalize_position(account_id: str, symbol: str, item: dict[str, Any]) -> dict[str, Any]:
    return {
        "account_id": account_id,
        "symbol": symbol.split(".")[-1],
        "eastmoney_symbol": symbol,
        "position_side": "LONG",
        "volume": int(item.get("volume", 0) or 0),
        "available_volume": int(item.get("available_now", item.get("available", item.get("available_volume", 0))) or 0),
        "today_volume": int(item.get("volume_today", item.get("today_volume", 0)) or 0),
        "vwap": _decimal(item, 6, "vwap"),
        "last_price": _decimal(item, 6, "last_price", "price"),
        "market_value": _decimal(item, 2, "market_value", "amount"),
        "floating_pnl": _decimal(item, 2, "fpnl", "floating_pnl"),
    }


def _error_code(exc: Exception) -> int:
    try:
        payload = json.loads(str(exc))
        return int(payload.get("status", payload.get("code", 0)))
    except Exception:
        return 0


def _safe_error(exc: Exception) -> str:
    return f"{type(exc).__name__}:{str(exc)[:800]}"


def _fail_closed(state: str, message: str) -> None:
    _, db = _require_state()
    db.set_state("kill_switch", "true")
    db.set_state("auth_state", state if state.startswith("AUTH") else db.state("auth_state", "AUTH_UNKNOWN"))
    db.set_state("account_state", state)
    db.set_state("last_error", message[:1000])


def _require_state() -> tuple[BridgeConfig, Database]:
    if _config is None or _db is None:
        raise RuntimeError("runner is not configured")
    return _config, _db
