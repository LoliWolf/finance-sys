from __future__ import annotations

import datetime as dt
import hashlib
import hmac
import urllib.parse

from .config import BridgeConfig
from .db import Database, now_iso


def canonical_string(
    method: str,
    path: str,
    query: dict[str, list[str]],
    body: bytes,
    timestamp_ms: str,
    nonce: str,
) -> str:
    pairs: list[tuple[str, str]] = []
    for key in sorted(query):
        for value in sorted(query[key]):
            pairs.append((key, value))
    normalized = urllib.parse.urlencode(pairs)
    body_hash = hashlib.sha256(body).hexdigest()
    return "\n".join([method.upper(), path, normalized, body_hash, timestamp_ms, nonce])


def sign(secret: str, canonical: str) -> str:
    return hmac.new(secret.encode("utf-8"), canonical.encode("utf-8"), hashlib.sha256).hexdigest()


def verify(
    db: Database,
    cfg: BridgeConfig,
    method: str,
    path: str,
    query: dict[str, list[str]],
    body: bytes,
    headers: dict[str, str],
) -> None:
    normalized_headers = {key.lower(): value for key, value in headers.items()}
    if normalized_headers.get("x-fs-key-id", "") != cfg.hmac_key_id:
        raise PermissionError("unknown key id")
    timestamp_raw = normalized_headers.get("x-fs-timestamp", "")
    nonce = normalized_headers.get("x-fs-nonce", "")
    signature = normalized_headers.get("x-fs-signature", "")
    if not timestamp_raw or not nonce or not signature:
        raise PermissionError("missing HMAC headers")
    try:
        timestamp = dt.datetime.fromtimestamp(int(timestamp_raw) / 1000, tz=dt.timezone.utc)
    except (ValueError, OSError) as exc:
        raise PermissionError("invalid timestamp") from exc
    if abs((dt.datetime.now(dt.timezone.utc) - timestamp).total_seconds()) > cfg.max_clock_skew_seconds:
        raise PermissionError("timestamp outside allowed skew")
    expected = sign(cfg.hmac_secret, canonical_string(method, path, query, body, timestamp_raw, nonce))
    if not hmac.compare_digest(signature, expected):
        raise PermissionError("invalid signature")
    expires = dt.datetime.now(dt.timezone.utc) + dt.timedelta(seconds=cfg.nonce_ttl_seconds)
    with db.transaction() as connection:
        connection.execute("DELETE FROM bridge_nonces WHERE expires_at <= ?", (now_iso(),))
        try:
            connection.execute(
                "INSERT INTO bridge_nonces(nonce,key_id,expires_at,created_at) VALUES (?,?,?,?)",
                (nonce, cfg.hmac_key_id, expires.isoformat(timespec="milliseconds"), now_iso()),
            )
        except Exception as exc:
            raise PermissionError("replayed nonce") from exc
