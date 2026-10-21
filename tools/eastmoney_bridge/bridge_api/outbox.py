from __future__ import annotations

import json
import threading
import time
import urllib.parse
import urllib.request

from .auth import canonical_string, sign
from .config import BridgeConfig
from .db import Database, now_iso


class OutboxWorker:
    def __init__(self, db: Database, cfg: BridgeConfig) -> None:
        self.db = db
        self.cfg = cfg
        self.stopped = threading.Event()

    def run(self) -> None:
        while not self.stopped.wait(1.0):
            self.deliver_one()

    def deliver_one(self) -> bool:
        with self.db.transaction() as connection:
            row = connection.execute(
                "SELECT * FROM bridge_callback_outbox WHERE status IN ('PENDING','RETRY') "
                "AND next_attempt_at <= ? ORDER BY id LIMIT 1",
                (now_iso(),),
            ).fetchone()
            if row is None:
                return False
            connection.execute(
                "UPDATE bridge_callback_outbox SET status='SENDING',attempt_count=attempt_count+1,updated_at=? WHERE id=?",
                (now_iso(), row["id"]),
            )
        body = row["payload_json"].encode("utf-8")
        parsed = urllib.parse.urlparse(row["callback_url"])
        timestamp = str(int(time.time() * 1000))
        nonce = f"outbox-{row['id']}-{timestamp}"
        canonical = canonical_string("POST", parsed.path, urllib.parse.parse_qs(parsed.query), body, timestamp, nonce)
        request = urllib.request.Request(
            row["callback_url"],
            data=body,
            method="POST",
            headers={
                "Content-Type": "application/json",
                "X-FS-Key-Id": self.cfg.hmac_key_id,
                "X-FS-Timestamp": timestamp,
                "X-FS-Nonce": nonce,
                "X-FS-Signature": sign(self.cfg.hmac_secret, canonical),
            },
        )
        status = 0
        error = ""
        try:
            with urllib.request.urlopen(request, timeout=5) as response:
                status = response.status
                response.read()
        except Exception as exc:
            error = str(exc)[:1000]
        with self.db.connect() as connection:
            if 200 <= status < 300:
                connection.execute(
                    "UPDATE bridge_callback_outbox SET status='DELIVERED',last_http_status=?,last_error='',delivered_at=?,updated_at=? WHERE id=?",
                    (status, now_iso(), now_iso(), row["id"]),
                )
            else:
                delay = min(300, 2 ** min(int(row["attempt_count"]) + 1, 8))
                next_attempt = time.time() + delay
                connection.execute(
                    "UPDATE bridge_callback_outbox SET status='RETRY',last_http_status=?,last_error=?,next_attempt_at=?,updated_at=? WHERE id=?",
                    (
                        status or None,
                        error or f"HTTP {status}",
                        time.strftime("%Y-%m-%dT%H:%M:%S", time.gmtime(next_attempt)) + "+00:00",
                        now_iso(),
                        row["id"],
                    ),
                )
        return True

