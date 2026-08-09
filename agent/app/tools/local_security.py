from dataclasses import dataclass
from typing import Any, Dict, List, Optional

import httpx

from app.config import get_settings


@dataclass(frozen=True)
class SecurityMatch:
    ts_code: str
    symbol: str
    name: str
    asset_type: str
    market: str
    match_source: str = ""


class SecurityClientError(RuntimeError):
    pass


class LocalSecurityClient:
    def __init__(
        self,
        base_url: Optional[str] = None,
        auth_header: Optional[str] = None,
        auth_token: Optional[str] = None,
        timeout_ms: Optional[int] = None,
        max_candidates: Optional[int] = None,
        http_client: Optional[httpx.Client] = None,
    ) -> None:
        settings = get_settings().internal_api
        self.base_url = (base_url if base_url is not None else settings.base_url).rstrip("/")
        self.auth_header = auth_header if auth_header is not None else settings.auth_header
        self.auth_token = auth_token if auth_token is not None else settings.auth_token
        self.timeout_ms = timeout_ms if timeout_ms is not None else settings.timeout_ms
        self.max_candidates = max_candidates if max_candidates is not None else settings.max_candidates
        self.http_client = http_client or httpx.Client(
            timeout=self.timeout_ms / 1000,
            trust_env=False,
        )

    def enabled(self) -> bool:
        return bool(self.base_url)

    def lookup(self, raw_symbol: str) -> List[SecurityMatch]:
        if not self.enabled():
            return []
        try:
            response = self.http_client.post(
                f"{self.base_url}/api/v1/internal/security/resolve",
                json={"query": raw_symbol, "max_candidates": self.max_candidates},
                headers=self._headers(),
                timeout=self.timeout_ms / 1000,
            )
            response.raise_for_status()
        except httpx.HTTPError as exc:
            raise SecurityClientError(f"local security resolve failed: {exc}") from exc
        payload = response.json()
        return [_match_from_payload(item) for item in payload.get("candidates", [])]

    def verify(self, ts_code: str, raw_symbol: str = "") -> Optional[SecurityMatch]:
        if not self.enabled():
            return None
        try:
            response = self.http_client.post(
                f"{self.base_url}/api/v1/internal/security/verify",
                json={"ts_code": ts_code, "raw_symbol": raw_symbol},
                headers=self._headers(),
                timeout=self.timeout_ms / 1000,
            )
            if response.status_code == 404:
                return None
            response.raise_for_status()
        except httpx.HTTPError as exc:
            raise SecurityClientError(f"local security verify failed: {exc}") from exc
        payload = response.json()
        if not payload.get("verified"):
            return None
        security = payload.get("security")
        if not security:
            return None
        return _match_from_payload(security)

    def _headers(self) -> Dict[str, str]:
        if not self.auth_header or not self.auth_token:
            return {}
        return {self.auth_header: self.auth_token}


def _match_from_payload(payload: Dict[str, Any]) -> SecurityMatch:
    return SecurityMatch(
        ts_code=str(payload.get("ts_code", "")).strip().upper(),
        symbol=str(payload.get("symbol", "")).strip(),
        name=str(payload.get("name", "")).strip(),
        asset_type=_agent_asset_type(str(payload.get("asset_type", "")).strip()),
        market=str(payload.get("market", "")).strip().upper(),
        match_source=str(payload.get("match_source", "")).strip(),
    )


def _agent_asset_type(value: str) -> str:
    value = value.upper()
    if value in {"A_SHARE", "ASHARE", "STOCK"}:
        return "STOCK"
    return value
