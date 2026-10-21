from __future__ import annotations

import hmac
import json
import threading
from collections import OrderedDict
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any

from .config import AgentConfig, load_config
from .http_client import ToolClient
from .strategy import run_deterministic


class State:
    def __init__(self, config: AgentConfig) -> None:
        self.config = config
        self.results: OrderedDict[tuple[str, int], dict[str, Any]] = OrderedDict()
        self.lock = threading.Lock()


def build_handler(state: State) -> type[BaseHTTPRequestHandler]:
    class Handler(BaseHTTPRequestHandler):
        server_version = "FinanceSysTradingAgent/1"

        def do_GET(self) -> None:  # noqa: N802
            if self.path == "/healthz":
                self._json(HTTPStatus.OK, {"status": "ok", "provider": "DETERMINISTIC"})
                return
            self._json(HTTPStatus.NOT_FOUND, {"error": "not found"})

        def do_POST(self) -> None:  # noqa: N802
            if self.path != "/internal/v1/trading-agent/runs":
                self._json(HTTPStatus.NOT_FOUND, {"error": "not found"})
                return
            try:
                config = load_config()
            except Exception:
                self._json(HTTPStatus.SERVICE_UNAVAILABLE, {"error": "Nacos configuration unavailable"})
                return
            expected = f"Bearer {config.internal_token}"
            if not hmac.compare_digest(self.headers.get("Authorization", ""), expected):
                self._json(HTTPStatus.UNAUTHORIZED, {"error": "unauthorized"})
                return
            try:
                length = int(self.headers.get("Content-Length", "0"))
                if length <= 0 or length > 4 * 1024 * 1024:
                    raise ValueError("invalid content length")
                request = json.loads(self.rfile.read(length).decode("utf-8"))
                if int(request.get("config_version", -1)) != config.config_version:
                    raise ValueError("config version mismatch; enqueue a fresh run")
                run_key = str(request["run_key"])
                cache_key = (run_key, config.config_version)
                with state.lock:
                    cached = state.results.get(cache_key)
                if cached is not None:
                    self._json(HTTPStatus.OK, cached)
                    return
                if str(request.get("decision_provider")) not in {"DETERMINISTIC", "SHADOW"}:
                    raise ValueError("unsupported decision provider")
                client = ToolClient(str(request["tool_base_url"]), config.internal_token)
                result = run_deterministic(request, config, client)
                with state.lock:
                    state.results[cache_key] = result
                    while len(state.results) > 256:
                        state.results.popitem(last=False)
                self._json(HTTPStatus.OK, result)
            except Exception as exc:  # fail closed and return no partial intents
                self._json(HTTPStatus.BAD_REQUEST, {"error": str(exc)[:1000]})

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


def main() -> None:
    config = load_config()
    server = ThreadingHTTPServer((config.host, config.port), build_handler(State(config)))
    server.serve_forever()


if __name__ == "__main__":
    main()
