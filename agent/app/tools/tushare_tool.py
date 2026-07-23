from dataclasses import dataclass
from typing import Any, Dict, List, Optional

import httpx

from app.config import get_settings
from app.http_client import HTTPClientError, StdlibHTTPClient, redact_secret


TUSHARE_STOCK_BASIC_TOOL_NAME = "tushare_stock_basic_tool"
DEFAULT_TUSHARE_ENDPOINT = "https://api.tushare.pro"


@dataclass(frozen=True)
class TushareSecurityCandidate:
    ts_code: str
    symbol: str
    name: str
    asset_type: str
    market: str
    list_status: str
    source_tool: str = TUSHARE_STOCK_BASIC_TOOL_NAME


class TushareToolError(RuntimeError):
    pass


class OfficialTushareStockBasicTool:
    """Read-only wrapper around the official Tushare stock_basic API contract."""

    def __init__(
        self,
        token: Optional[str] = None,
        endpoint: Optional[str] = None,
        timeout_ms: Optional[int] = None,
        http_client: Optional[httpx.Client] = None,
    ) -> None:
        settings = get_settings().tushare
        self.config_enabled = settings.enabled or token is not None
        self.token = token if token is not None else settings.token
        configured_endpoint = endpoint if endpoint is not None else settings.endpoint
        self.endpoint = (configured_endpoint or DEFAULT_TUSHARE_ENDPOINT).rstrip("/")
        self.timeout_ms = timeout_ms if timeout_ms is not None else settings.timeout_ms
        self.http_client = (
            http_client
            if http_client is not None
            else StdlibHTTPClient(timeout=self.timeout_ms / 1000)
        )

    def enabled(self) -> bool:
        return bool(self.config_enabled and self.token and self.endpoint)

    def search(self, raw_symbol: str, max_candidates: int = 5) -> List[TushareSecurityCandidate]:
        if not self.enabled():
            return []
        try:
            response = self.http_client.post(
                self.endpoint,
                json={
                    "api_name": "stock_basic",
                    "token": self.token,
                    "params": {"list_status": "L"},
                    "fields": "ts_code,symbol,name,market,list_status,exchange",
                },
                timeout=self.timeout_ms / 1000,
            )
            response.raise_for_status()
        except (httpx.HTTPError, HTTPClientError) as exc:
            safe_error = redact_secret(str(exc), self.token)
            raise TushareToolError(
                f"tushare stock_basic failed: {safe_error}"
            ) from exc
        payload = response.json()
        if int(payload.get("code", 0)) != 0:
            message = redact_secret(str(payload.get("msg", "")), self.token)
            raise TushareToolError(
                f"tushare stock_basic returned code {payload.get('code')}: {message}"
            )
        candidates = _candidates_from_tushare_payload(payload)
        matched = [item for item in candidates if _matches_query(raw_symbol, item)]
        return matched[:max_candidates]


def _candidates_from_tushare_payload(payload: Dict[str, Any]) -> List[TushareSecurityCandidate]:
    data = payload.get("data") or {}
    fields = data.get("fields") or []
    items = data.get("items") or []
    candidates: List[TushareSecurityCandidate] = []
    for row in items:
        if len(row) != len(fields):
            continue
        item = {str(fields[i]): row[i] for i in range(len(fields))}
        ts_code = str(item.get("ts_code", "")).strip().upper()
        if not ts_code:
            continue
        candidates.append(
            TushareSecurityCandidate(
                ts_code=ts_code,
                symbol=str(item.get("symbol", "")).strip(),
                name=str(item.get("name", "")).strip(),
                asset_type="STOCK",
                market=_market_from_ts_code(ts_code),
                list_status=str(item.get("list_status", "")).strip(),
            )
        )
    return candidates


def _matches_query(query: str, candidate: TushareSecurityCandidate) -> bool:
    query = query.strip().upper()
    if not query:
        return False
    return query in {
        candidate.ts_code.upper(),
        candidate.symbol.upper(),
        candidate.name.upper(),
    }


def _market_from_ts_code(ts_code: str) -> str:
    if ts_code.endswith(".SH"):
        return "SH"
    if ts_code.endswith(".SZ"):
        return "SZ"
    if ts_code.endswith(".BJ"):
        return "BJ"
    return ""
