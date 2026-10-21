from __future__ import annotations

import datetime as dt
import json
import pathlib
import sqlite3
from contextlib import contextmanager
from typing import Any, Iterator

UTC = dt.timezone.utc


def now_iso() -> str:
    return dt.datetime.now(UTC).isoformat(timespec="milliseconds")


class Database:
    def __init__(self, path: pathlib.Path, migrations_dir: pathlib.Path | None = None) -> None:
        self.path = path
        self.path.parent.mkdir(parents=True, exist_ok=True)
        self.migrations_dir = migrations_dir or pathlib.Path(__file__).resolve().parents[1] / "migrations"
        self.migrate()

    def connect(self) -> sqlite3.Connection:
        connection = sqlite3.connect(self.path, timeout=5, isolation_level=None)
        connection.row_factory = sqlite3.Row
        connection.execute("PRAGMA busy_timeout=5000")
        connection.execute("PRAGMA foreign_keys=ON")
        return connection

    @contextmanager
    def transaction(self) -> Iterator[sqlite3.Connection]:
        connection = self.connect()
        try:
            connection.execute("BEGIN IMMEDIATE")
            yield connection
            connection.commit()
        except Exception:
            connection.rollback()
            raise
        finally:
            connection.close()

    def migrate(self) -> None:
        with self.connect() as connection:
            connection.execute("CREATE TABLE IF NOT EXISTS bridge_schema_migrations(version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)")
            applied = {row[0] for row in connection.execute("SELECT version FROM bridge_schema_migrations")}
            for path in sorted(self.migrations_dir.glob("*.sql")):
                if path.name in applied:
                    continue
                connection.executescript(path.read_text(encoding="utf-8"))
                connection.execute(
                    "INSERT OR IGNORE INTO bridge_schema_migrations(version,applied_at) VALUES (?,?)",
                    (path.name, now_iso()),
                )

    def state(self, key: str, default: str = "") -> str:
        with self.connect() as connection:
            row = connection.execute(
                "SELECT state_value FROM bridge_state WHERE state_key=?", (key,)
            ).fetchone()
        return str(row[0]) if row else default

    def set_state(self, key: str, value: Any) -> None:
        text = json.dumps(value, ensure_ascii=False, separators=(",", ":")) if not isinstance(value, str) else value
        with self.connect() as connection:
            connection.execute(
                "INSERT INTO bridge_state(state_key,state_value,updated_at) VALUES (?,?,?) "
                "ON CONFLICT(state_key) DO UPDATE SET state_value=excluded.state_value,updated_at=excluded.updated_at",
                (key, text, now_iso()),
            )

    def latest_snapshot(self) -> sqlite3.Row | None:
        with self.connect() as connection:
            return connection.execute("SELECT * FROM bridge_snapshots ORDER BY id DESC LIMIT 1").fetchone()

