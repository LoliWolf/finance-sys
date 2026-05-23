from dataclasses import dataclass
from typing import Optional


@dataclass(frozen=True)
class SecurityMatch:
    ts_code: str
    symbol: str
    name: str
    asset_type: str
    market: str


class SecurityClient:
    """Placeholder for a future Go internal security lookup API.

    M4 keeps Python away from MySQL. Until Go exposes a dedicated internal
    lookup endpoint, this client deliberately returns no match and lets Go M3
    resolve raw symbols after the Agent response comes back.
    """

    def lookup(self, raw_symbol: str) -> Optional[SecurityMatch]:
        return None
