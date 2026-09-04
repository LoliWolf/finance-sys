from __future__ import annotations

import json
import pathlib
import sqlite3
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[2]))

from bridge_api.config import load_config


def main() -> None:
    cfg = load_config()
    connection = sqlite3.connect(cfg.sqlite_path)
    connection.row_factory = sqlite3.Row
    try:
        command_counts = {
            row["command_type"] + ":" + row["status"]: row["count"]
            for row in connection.execute(
                "SELECT command_type,status,COUNT(*) AS count FROM bridge_commands GROUP BY command_type,status"
            )
        }
        states = {
            row["state_key"]: row["state_value"]
            for row in connection.execute(
                "SELECT state_key,state_value FROM bridge_state WHERE state_key IN "
                "('kill_switch','auth_state','account_id','account_state','terminal_state','runner_heartbeat_at','last_auth_success_at')"
            )
        }
        event_count = connection.execute("SELECT COUNT(*) FROM bridge_order_events").fetchone()[0]
        pending_callbacks = connection.execute(
            "SELECT COUNT(*) FROM bridge_callback_outbox WHERE status <> 'DELIVERED'"
        ).fetchone()[0]
        snapshot_count = connection.execute("SELECT COUNT(*) FROM bridge_snapshots").fetchone()[0]
        migrations = [
            row[0]
            for row in connection.execute(
                "SELECT version FROM bridge_schema_migrations ORDER BY version"
            )
        ]
        quotes = json.loads(
            connection.execute(
                "SELECT COALESCE((SELECT state_value FROM bridge_state WHERE state_key='quotes_json'),'[]')"
            ).fetchone()[0]
        )
    finally:
        connection.close()
    print(
        json.dumps(
            {
                "config_version": cfg.config_version,
                "command_counts": command_counts,
                "event_count": event_count,
                "pending_callbacks": pending_callbacks,
                "snapshot_count": snapshot_count,
                "quote_count": len(quotes) if isinstance(quotes, list) else 0,
                "migrations": migrations,
                "state": states,
            },
            ensure_ascii=False,
            separators=(",", ":"),
        )
    )


if __name__ == "__main__":
    main()
