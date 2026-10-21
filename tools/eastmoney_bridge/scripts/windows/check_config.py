from __future__ import annotations

import json
import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[2]))

from bridge_api.config import load_config


def main() -> None:
    cfg = load_config()
    print(
        json.dumps(
            {
                "config_version": cfg.config_version,
                "base_url": cfg.base_url,
                "callback_url": cfg.callback_url,
                "strategy_id": cfg.strategy_id,
                "expected_account_id_configured": bool(cfg.expected_account_id),
                "simulation_only": cfg.simulation_only,
                "token_fingerprint": cfg.token_fingerprint,
                "sqlite_path": str(cfg.sqlite_path),
                "cert_exists": cfg.cert_file.exists(),
                "key_exists": cfg.key_file.exists(),
                "global_enabled": cfg.global_enabled,
                "global_kill_switch": cfg.global_kill_switch,
            },
            ensure_ascii=False,
            separators=(",", ":"),
        )
    )


if __name__ == "__main__":
    main()
