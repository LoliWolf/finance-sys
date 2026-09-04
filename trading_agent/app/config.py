from __future__ import annotations

import json
import os
import pathlib
import urllib.parse
import urllib.request
from dataclasses import dataclass


@dataclass(frozen=True)
class AgentConfig:
    host: str
    port: int
    internal_token: str
    tool_contract_version: str
    min_confidence: float
    min_blogger_sample_count: int
    min_blogger_win_rate: float
    max_candidates: int
    max_intents: int
    schema_version: str = "trading-intent/v2"
    stop_loss_ratio: float = 0.03
    take_profit_ratio: float = 0.06
    max_holding_trade_days: int = 20
    sell_limit_discount_ratio: float = 0.002


def _nacos_address() -> str:
    value = os.getenv("NACOS_SERVER_ADDR", "").strip()
    if value:
        return value
    roots = [pathlib.Path.cwd(), pathlib.Path(__file__).resolve().parents[2]]
    for root in roots:
        for name in ("bootstrap_go122.env", "bootstrap_go122.env.example"):
            path = root / name
            if not path.exists():
                continue
            for line in path.read_text(encoding="utf-8").splitlines():
                if line.startswith("NACOS_SERVER_ADDR="):
                    return line.split("=", 1)[1].strip()
    raise RuntimeError("NACOS_SERVER_ADDR is required")


def load_config() -> AgentConfig:
    address = _nacos_address()
    query = urllib.parse.urlencode(
        {"dataId": "expert_trade", "group": "DEFAULT_GROUP", "tenant": "public"}
    )
    url = f"http://{address}/nacos/v1/cs/configs?{query}"
    with urllib.request.urlopen(url, timeout=10) as response:
        root = json.loads(response.read().decode("utf-8"))
    trading = root["trading"]
    agent = trading["agent"]
    decision = trading["decision"]
    exit_config = trading["exit"]
    endpoint = urllib.parse.urlparse(agent["health_endpoint"])
    return AgentConfig(
        host=endpoint.hostname or "127.0.0.1",
        port=endpoint.port or 8110,
        internal_token=str(agent["internal_token"]),
        tool_contract_version=str(decision["tool_contract_version"]),
        min_confidence=float(decision["min_recommendation_confidence"]),
        min_blogger_sample_count=int(decision["min_blogger_sample_count"]),
        min_blogger_win_rate=float(decision["min_blogger_win_rate"]),
        max_candidates=int(decision["max_candidates_per_run"]),
        max_intents=int(decision["max_intents_per_run"]),
        schema_version=str(agent["schema_version"]),
        stop_loss_ratio=float(exit_config["stop_loss_ratio"]),
        take_profit_ratio=float(exit_config["take_profit_ratio"]),
        max_holding_trade_days=int(exit_config["max_holding_trade_days"]),
        sell_limit_discount_ratio=float(exit_config["sell_limit_discount_ratio"]),
    )
