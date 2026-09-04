from __future__ import annotations

import datetime as dt
import hashlib
import json
import ssl
import threading
import urllib.parse
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any

from .auth import verify
from .config import BridgeConfig, load_config
from .db import Database, now_iso
from .outbox import OutboxWorker


class BridgeState:
    def __init__(self, cfg: BridgeConfig, db: Database) -> None:
        self.cfg = cfg
        self.db = db

    def health(self) -> dict[str, Any]:
        heartbeat = self.db.state("runner_heartbeat_at")
        auth_state = self.db.state("auth_state", "AUTH_UNKNOWN")
        account_id = self.db.state("account_id")
        terminal = self.db.state("terminal_state", "DISCONNECTED")
        account_state = self.db.state("account_state", "UNKNOWN")
        kill_switch = self.db.state("kill_switch", "true").lower() == "true"
        runner_ready = False
        if heartbeat:
            try:
                age = dt.datetime.now(dt.timezone.utc) - dt.datetime.fromisoformat(heartbeat)
                runner_ready = age.total_seconds() <= 10
            except ValueError:
                runner_ready = False
        account_ready = bool(cfg_account := self.cfg.expected_account_id) and account_id == cfg_account
        ready = (
            runner_ready
            and terminal == "CONNECTED"
            and account_ready
            and auth_state == "AUTH_OK"
            and not kill_switch
            and self.cfg.global_enabled
            and not self.cfg.global_kill_switch
        )
        return {
            "status": "READY" if ready else _degraded_status(runner_ready, terminal, account_id, cfg_account, auth_state),
            "api": "READY",
            "sqlite": "READY",
            "runner": "READY" if runner_ready else "OFFLINE",
            "terminal": terminal,
            "account": "READY" if account_ready else ("DISCOVERED" if account_id else "UNKNOWN"),
            "auth_state": auth_state,
            "kill_switch": kill_switch,
            "account_id": account_id,
            "strategy_id": self.cfg.strategy_id,
            "config_version": self.cfg.config_version,
            "runner_heartbeat_at": heartbeat or None,
            "last_auth_success_at": self.db.state("last_auth_success_at") or None,
            "token_fingerprint": self.cfg.token_fingerprint,
        }


def _degraded_status(runner_ready: bool, terminal: str, account_id: str, expected: str, auth: str) -> str:
    if not runner_ready:
        return "RUNNER_OFFLINE"
    if terminal != "CONNECTED":
        return "TERMINAL_DISCONNECTED"
    if not expected:
        return "ACCOUNT_DISCOVERY_REQUIRED"
    if account_id != expected:
        return "ACCOUNT_MISMATCH"
    if auth != "AUTH_OK":
        return auth
    return "DEGRADED"


def build_handler(state: BridgeState) -> type[BaseHTTPRequestHandler]:
    class Handler(BaseHTTPRequestHandler):
        server_version = "FinanceSysEastmoneyBridge/1"

        def do_GET(self) -> None:  # noqa: N802
            self._dispatch()

        def do_POST(self) -> None:  # noqa: N802
            self._dispatch()

        def _dispatch(self) -> None:
            try:
                length = int(self.headers.get("Content-Length", "0"))
                if length < 0 or length > 8 * 1024 * 1024:
                    raise ValueError("invalid content length")
                body = self.rfile.read(length) if length else b""
                parsed = urllib.parse.urlparse(self.path)
                headers = {key: value for key, value in self.headers.items()}
                verify(state.db, state.cfg, self.command, parsed.path, urllib.parse.parse_qs(parsed.query), body, headers)
                payload = json.loads(body.decode("utf-8")) if body else {}
                self._route(parsed.path, urllib.parse.parse_qs(parsed.query), payload)
            except PermissionError as exc:
                self._json(HTTPStatus.UNAUTHORIZED, {"error": str(exc)})
            except ValueError as exc:
                self._json(HTTPStatus.BAD_REQUEST, {"error": str(exc)})
            except Exception as exc:
                self._json(HTTPStatus.INTERNAL_SERVER_ERROR, {"error": str(exc)[:1000]})

        def _route(self, path: str, query: dict[str, list[str]], payload: dict[str, Any]) -> None:
            if self.command == "GET" and path == "/healthz":
                self._json(HTTPStatus.OK, state.health())
                return
            if self.command == "GET" and path == "/v1/capabilities":
                self._json(
                    HTTPStatus.OK,
                    {
                        "supported_asset_types": ["STOCK", "ETF"],
                        "supported_order_types": ["LIMIT"],
                        "supported_boards": list(state.cfg.allowed_boards),
                        "verified_boards": list(state.cfg.verified_boards),
                        "trading_rule_version": state.cfg.trading_rule_version,
                        "simulation_only": True,
                        "max_subscribed_symbols": 45,
                        "token_ttl_known": False,
                        "history_daily_quota_known": False,
                        "history_max_rows_per_request": 33000,
                        "simulation_account_limit_known": False,
                        "verified_simulation_account_count": 1 if state.db.state("account_id") else 0,
                        "max_simulation_strategies_per_account": 10,
                    },
                )
                return
            if self.command == "GET" and path in {"/v1/account", "/v1/positions", "/v1/reconciliation-snapshot", "/v1/orders", "/v1/executions"}:
                self._snapshot_route(path)
                return
            if self.command == "GET" and path == "/v1/quotes":
                self._quotes_route(query)
                return
            if self.command == "POST" and path == "/v1/orders":
                self._enqueue("PLACE_ORDER", payload)
                return
            if self.command == "POST" and path.endswith("/cancel") and path.startswith("/v1/orders/"):
                client_order_id = urllib.parse.unquote(path[len("/v1/orders/") : -len("/cancel")])
                payload["client_order_id"] = client_order_id
                self._enqueue("CANCEL_ORDER", payload)
                return
            if self.command == "POST" and path == "/v1/refresh-snapshot":
                payload["client_order_id"] = payload.get("client_order_id") or "snapshot-" + hashlib.sha256(json.dumps(payload, sort_keys=True).encode()).hexdigest()[:24]
                self._enqueue("REFRESH_SNAPSHOT", payload)
                return
            if self.command == "POST" and path == "/v1/quotes/refresh":
                payload["symbols"] = _normalize_quote_symbols(payload.get("symbols"))
                payload["client_order_id"] = payload.get("client_order_id") or "quotes-" + hashlib.sha256(json.dumps(payload, sort_keys=True).encode()).hexdigest()[:24]
                self._enqueue("REFRESH_QUOTES", payload)
                return
            if self.command == "POST" and path == "/v1/kill-switch":
                enabled = bool(payload.get("enabled"))
                if not enabled:
                    health = state.health()
                    if not state.cfg.expected_account_id or health["account_id"] != state.cfg.expected_account_id or health["auth_state"] != "AUTH_OK":
                        raise ValueError("cannot disable Bridge kill switch before account and auth verification")
                state.db.set_state("kill_switch", "true" if enabled else "false")
                state.db.set_state("kill_switch_reason", str(payload.get("reason", "")))
                self._json(HTTPStatus.OK, {"kill_switch": enabled})
                return
            self._json(HTTPStatus.NOT_FOUND, {"error": "not found"})

        def _snapshot_route(self, path: str) -> None:
            row = state.db.latest_snapshot()
            if row is None:
                self._json(HTTPStatus.SERVICE_UNAVAILABLE, {"error": "snapshot unavailable"})
                return
            account = json.loads(row["account_json"])
            positions = json.loads(row["positions_json"])
            orders = json.loads(row["orders_json"])
            executions = json.loads(row["executions_json"])
            if path == "/v1/account":
                self._json(HTTPStatus.OK, account)
            elif path == "/v1/positions":
                self._json(HTTPStatus.OK, positions)
            elif path == "/v1/orders":
                self._json(HTTPStatus.OK, orders)
            elif path == "/v1/executions":
                self._json(HTTPStatus.OK, executions)
            else:
                self._json(
                    HTTPStatus.OK,
                    {
                        "snapshot_version": row["snapshot_version"],
                        "cursor": str(row["id"]),
                        "health": state.health(),
                        "account": account,
                        "positions": positions,
                        "orders": orders,
                        "executions": executions,
                    },
                )

        def _quotes_route(self, query: dict[str, list[str]]) -> None:
            requested = _normalize_quote_symbols(query.get("symbols", [""])[0])
            cached = json.loads(state.db.state("quotes_json", "[]"))
            if not isinstance(cached, list):
                raise ValueError("cached quotes are invalid")
            requested_set = set(requested)
            result = [
                item
                for item in cached
                if isinstance(item, dict)
                and (
                    str(item.get("eastmoney_symbol", "")).upper() in requested_set
                    or _normalize_quote_code(str(item.get("symbol", ""))) in requested_set
                )
            ]
            self._json(HTTPStatus.OK, result)

        def _enqueue(self, command_type: str, payload: dict[str, Any]) -> None:
            client_order_id = str(payload.get("client_order_id", ""))
            idempotency_key = self.headers.get("Idempotency-Key", "")
            if not client_order_id or not idempotency_key:
                raise ValueError("client_order_id and Idempotency-Key are required")
            if command_type == "PLACE_ORDER":
                _validate_order(state, payload)
            created = now_iso()
            with state.db.transaction() as connection:
                existing = connection.execute(
                    "SELECT * FROM bridge_commands WHERE idempotency_key=?", (idempotency_key,)
                ).fetchone()
                if existing is not None:
                    self._json(HTTPStatus.ACCEPTED, _command_response(existing, True))
                    return
                cursor = connection.execute(
                    "INSERT INTO bridge_commands(command_type,client_order_id,idempotency_key,status,payload_json,next_attempt_at,created_at,updated_at) VALUES (?,?,?,'QUEUED',?,?,?,?)",
                    (
                        command_type,
                        client_order_id,
                        idempotency_key,
                        json.dumps(payload, ensure_ascii=False, separators=(",", ":")),
                        created,
                        created,
                        created,
                    ),
                )
                row = connection.execute("SELECT * FROM bridge_commands WHERE id=?", (cursor.lastrowid,)).fetchone()
            self._json(HTTPStatus.ACCEPTED, _command_response(row, False))

        def log_message(self, format: str, *args: Any) -> None:
            return

        def _json(self, status: int, payload: Any) -> None:
            raw = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
            self.send_response(status)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)

    return Handler


def _validate_order(state: BridgeState, payload: dict[str, Any]) -> None:
    health = state.health()
    if health["kill_switch"] or health["status"] != "READY":
        raise ValueError(f"Bridge refuses new orders while status={health['status']}")
    if payload.get("environment") != "SIMULATION" or payload.get("expected_account_id") != state.cfg.expected_account_id:
        raise ValueError("simulation account mismatch")
    if payload.get("strategy_id") != state.cfg.strategy_id:
        raise ValueError("strategy_id mismatch")
    if payload.get("order_type") != "LIMIT" or payload.get("side") not in {"BUY", "SELL"}:
        raise ValueError("only BUY/SELL LIMIT orders are supported")
    symbol = _normalize_quote_code(str(payload.get("symbol", "")))
    if not symbol:
        raise ValueError("only SHSE/SZSE securities are supported")
    asset_type = str(payload.get("asset_type", "")).strip().upper()
    board_type = _board_type(symbol, asset_type)
    if str(payload.get("board_type", "")).strip().upper() != board_type:
        raise ValueError("board_type does not match symbol and asset_type")
    if board_type not in state.cfg.allowed_boards or board_type not in state.cfg.verified_boards:
        raise ValueError(f"board {board_type} has not passed both risk and account verification")
    if str(payload.get("trading_rule_version", "")).strip() != state.cfg.trading_rule_version:
        raise ValueError("trading_rule_version mismatch")
    volume = int(payload.get("volume", 0))
    available = _available_volume(state.db, symbol) if payload.get("side") == "SELL" else 0
    _validate_trading_unit(board_type, str(payload.get("side")), volume, available)


def _normalize_quote_symbols(value: Any) -> list[str]:
    raw_values = value if isinstance(value, list) else str(value or "").split(",")
    result: list[str] = []
    seen: set[str] = set()
    for raw in raw_values:
        symbol = _normalize_quote_code(str(raw))
        if symbol and symbol not in seen:
            seen.add(symbol)
            result.append(symbol)
    if not result:
        raise ValueError("at least one quote symbol is required")
    if len(result) > 45:
        raise ValueError("at most 45 quote symbols are supported")
    return result


def _normalize_quote_code(value: str) -> str:
    symbol = value.strip().upper()
    if symbol.startswith(("SHSE.", "SZSE.")):
        exchange, code = symbol.split(".", 1)
        if len(code) == 6 and code.isdigit():
            return f"{exchange}.{code}"
        return ""
    if symbol.endswith((".SH", ".SZ")):
        code, market = symbol.split(".", 1)
        if len(code) == 6 and code.isdigit():
            return f"{'SHSE' if market == 'SH' else 'SZSE'}.{code}"
        return ""
    if len(symbol) == 6 and symbol.isdigit():
        market = "SHSE" if symbol[0] in {"5", "6", "9"} else "SZSE"
        return f"{market}.{symbol}"
    return ""


def _board_type(symbol: str, asset_type: str) -> str:
    code = symbol.split(".", 1)[1]
    if asset_type == "ETF":
        return "ETF"
    if asset_type != "STOCK":
        raise ValueError("only STOCK/ETF assets are supported")
    if symbol.startswith("SZSE.") and code.startswith(("300", "301")):
        return "CHINEXT"
    if symbol.startswith("SHSE.") and code.startswith(("688", "689")):
        return "STAR"
    return "SH_MAIN" if symbol.startswith("SHSE.") else "SZ_MAIN"


def _available_volume(db: Database, symbol: str) -> int:
    row = db.latest_snapshot()
    if row is None:
        return 0
    try:
        positions = json.loads(row["positions_json"])
    except (TypeError, ValueError, KeyError):
        return 0
    for item in positions if isinstance(positions, list) else []:
        if not isinstance(item, dict):
            continue
        candidate = _normalize_quote_code(
            str(item.get("eastmoney_symbol", "") or item.get("symbol", ""))
        )
        if candidate == symbol:
            return int(item.get("available_volume", 0) or 0)
    return 0


def _validate_trading_unit(board_type: str, side: str, volume: int, available: int = 0) -> None:
    if volume <= 0:
        raise ValueError("volume must be positive")
    if side == "BUY":
        if board_type == "STAR":
            if volume < 200:
                raise ValueError("STAR buy volume must be at least 200 shares")
            return
        if volume < 100 or volume % 100 != 0:
            raise ValueError("buy volume must be a positive 100-share board lot")
        return
    if side != "SELL":
        raise ValueError("side must be BUY or SELL")
    minimum = 200 if board_type == "STAR" else 100
    step_valid = board_type == "STAR" or volume % 100 == 0
    if volume >= minimum and step_valid:
        return
    if available > 0 and volume == available:
        return
    raise ValueError("non-standard residual position must be sold in full")


def _command_response(row: Any, replay: bool) -> dict[str, Any]:
    return {
        "request_id": str(row["id"]),
        "client_order_id": row["client_order_id"],
        "command_id": row["id"],
        "status": row["status"],
        "idempotent_replay": replay,
        "accepted_at": row["created_at"],
    }


def run_server(cfg: BridgeConfig | None = None) -> None:
    config = cfg or load_config()
    db = Database(config.sqlite_path)
    state = BridgeState(config, db)
    parsed = urllib.parse.urlparse(config.base_url)
    server = ThreadingHTTPServer(("0.0.0.0", parsed.port or 8111), build_handler(state))
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.minimum_version = ssl.TLSVersion.TLSv1_2
    context.load_cert_chain(config.cert_file, config.key_file)
    server.socket = context.wrap_socket(server.socket, server_side=True)
    worker = OutboxWorker(db, config)
    thread = threading.Thread(target=worker.run, name="callback-outbox", daemon=True)
    thread.start()
    try:
        server.serve_forever()
    finally:
        worker.stopped.set()
