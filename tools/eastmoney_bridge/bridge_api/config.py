from __future__ import annotations

import hashlib
import json
import os
import pathlib
import urllib.parse
import urllib.request
from dataclasses import dataclass
from typing import Any


@dataclass(frozen=True)
class BridgeConfig:
    config_version: int
    base_url: str
    callback_url: str
    expected_account_id: str
    strategy_id: str
    simulation_only: bool
    hmac_key_id: str
    hmac_secret: str
    max_clock_skew_seconds: int
    nonce_ttl_seconds: int
    cert_file: pathlib.Path
    key_file: pathlib.Path
    sqlite_path: pathlib.Path
    token: str
    token_fingerprint: str
    probe_interval_seconds: int
    invalid_token_error_codes: tuple[int, ...]
    auth_service_error_codes: tuple[int, ...]
    transient_failure_threshold: int
    global_enabled: bool
    global_kill_switch: bool
    allowed_boards: tuple[str, ...]
    verified_boards: tuple[str, ...]
    trading_rule_version: str


def load_root_config(base_dir: pathlib.Path | None = None) -> dict[str, Any]:
    address = _nacos_address(base_dir)
    query = urllib.parse.urlencode(
        {"dataId": "expert_trade", "group": "DEFAULT_GROUP", "tenant": "public"}
    )
    with urllib.request.urlopen(
        f"http://{address}/nacos/v1/cs/configs?{query}", timeout=10
    ) as response:
        return json.loads(response.read().decode("utf-8"))


def load_config(base_dir: pathlib.Path | None = None) -> BridgeConfig:
    base = (base_dir or pathlib.Path(__file__).resolve().parents[1]).resolve()
    root = load_root_config(base)
    trading = root["trading"]
    bridge = trading["bridge"]
    eastmoney = trading["eastmoney"]
    token = str(eastmoney["token"])
    tls = bridge["tls"]
    health = eastmoney["token_health"]
    risk = trading["risk"]
    account_policy = eastmoney["account_policy"]
    cfg = BridgeConfig(
        config_version=int(root["meta"]["config_version"]),
        base_url=str(bridge["base_url"]),
        callback_url=str(bridge["callback_url"]),
        expected_account_id=str(bridge["expected_account_id"]),
        strategy_id=str(bridge["strategy_id"]),
        simulation_only=bool(bridge["simulation_only"]),
        hmac_key_id=str(bridge["hmac"]["key_id"]),
        hmac_secret=str(bridge["hmac"]["secret"]),
        max_clock_skew_seconds=int(bridge["hmac"]["max_clock_skew_seconds"]),
        nonce_ttl_seconds=int(bridge["hmac"]["nonce_ttl_seconds"]),
        cert_file=_resolve(base, str(tls["cert_file"])),
        key_file=_resolve(base, str(tls["key_file"])),
        sqlite_path=_resolve(base, str(eastmoney["sqlite_path"])),
        token=token,
        token_fingerprint=hashlib.sha256(token.encode("utf-8")).hexdigest()[:12],
        probe_interval_seconds=int(health["probe_interval_seconds"]),
        invalid_token_error_codes=tuple(int(value) for value in health["invalid_token_error_codes"]),
        auth_service_error_codes=tuple(int(value) for value in health["auth_service_error_codes"]),
        transient_failure_threshold=int(health["transient_failure_threshold"]),
        global_enabled=bool(trading["enabled"]),
        global_kill_switch=bool(trading["kill_switch"]),
        allowed_boards=tuple(str(value).strip().upper() for value in risk["allowed_boards"]),
        verified_boards=tuple(str(value).strip().upper() for value in account_policy["verified_boards"]),
        trading_rule_version=str(risk["trading_rule_version"]).strip(),
    )
    _validate(cfg)
    return cfg


def _nacos_address(base_dir: pathlib.Path | None) -> str:
    value = os.getenv("NACOS_SERVER_ADDR", "").strip()
    if value:
        return value
    roots = [base_dir or pathlib.Path.cwd(), pathlib.Path.cwd()]
    for root in roots:
        for name in ("bootstrap.env", "bootstrap_go122.env", "bootstrap_go122.env.example"):
            path = pathlib.Path(root) / name
            if not path.exists():
                continue
            for line in path.read_text(encoding="utf-8-sig").splitlines():
                if line.startswith("NACOS_SERVER_ADDR="):
                    return line.split("=", 1)[1].strip()
    raise RuntimeError("NACOS_SERVER_ADDR is required")


def _resolve(base: pathlib.Path, value: str) -> pathlib.Path:
    path = pathlib.Path(value)
    return path if path.is_absolute() else base / path


def _validate(cfg: BridgeConfig) -> None:
    if not cfg.simulation_only:
        raise RuntimeError("Bridge refuses simulation_only=false")
    if not cfg.strategy_id:
        raise RuntimeError("strategy_id is required")
    if not cfg.hmac_key_id or not cfg.hmac_secret:
        raise RuntimeError("Bridge HMAC configuration is required")
    if not cfg.token:
        raise RuntimeError("Eastmoney token is required")
    supported_boards = {"SH_MAIN", "SZ_MAIN", "CHINEXT", "STAR", "ETF"}
    if not cfg.allowed_boards or not set(cfg.allowed_boards).issubset(supported_boards):
        raise RuntimeError("trading.risk.allowed_boards is invalid")
    if not cfg.verified_boards or not set(cfg.verified_boards).issubset(supported_boards):
        raise RuntimeError("trading.eastmoney.account_policy.verified_boards is invalid")
    if not cfg.trading_rule_version:
        raise RuntimeError("trading.risk.trading_rule_version is required")
