"""Pure, versioned weekly-pool selection. No broker or model access."""
from decimal import Decimal
from typing import Any


def scan_candidates(client, as_of: str, page_size: int, budget: int):
    if page_size <= 0 or budget < page_size:
        raise ValueError("invalid candidate scan budget")
    rows, seen, cursors = [], set(), set()
    cursor = ""
    while True:
        params = {"as_of_time": as_of, "limit": page_size}
        if cursor:
            params["after_id"] = cursor
        page = client.get("/recommendation-candidates", params)
        for row in page.get("items", []):
            key = int(row["recommendation_event_id"])
            if key in seen:
                raise ValueError("candidate pagination repeated an event")
            seen.add(key)
            rows.append(row)
            if len(rows) > budget:
                raise ValueError("CANDIDATE_SCAN_INCOMPLETE: scan budget exceeded")
        next_cursor = str(page.get("next_cursor") or "")
        if not next_cursor:
            return rows
        if next_cursor in cursors or len(cursors) >= budget:
            raise ValueError("candidate pagination did not converge")
        cursors.add(next_cursor)
        cursor = next_cursor


def bars_before(rows, day):
    bars = sorted((r for r in rows if str(r.get("trade_date", "")) < day), key=lambda r: r["trade_date"])
    if len({r["trade_date"] for r in bars}) != len(bars):
        raise ValueError("duplicate daily bars")
    return bars


def technical_selection(history: list[dict[str, Any]], benchmark: list[dict[str, Any]], price: str,
                        max_extension: float, max_atr: float):
    """Twenty complete sessions, positive relative strength, controlled pullback.

    Parameters are frozen before validation; no parameter search is performed.
    Missing input is a rejection, never imputed with invented prices/volume.
    """
    observed = {"strategy": "TREND_PULLBACK_V1", "sample_count": len(history)}
    def reject(reason):
        return False, Decimal(0), reason, observed
    if len(history) < 20 or len(benchmark) < 20:
        return reject("INSUFFICIENT_20D_HISTORY")
    bars, index = history[-20:], benchmark[-20:]
    if [r["trade_date"] for r in bars] != [r["trade_date"] for r in index]:
        return reject("HISTORY_GAP_OR_STALE")
    # Unadjusted corporate-action jumps must not masquerade as momentum.
    for prior, current in zip(bars, bars[1:]):
        if current.get('pre_close_price') is not None:
            try:
                before, reference = Decimal(str(prior['close_price'])), Decimal(str(current['pre_close_price']))
                if not before.is_finite() or not reference.is_finite() or before <= 0 or abs(reference/before-1) > Decimal('.005'):
                    return reject('CORPORATE_ACTION_OR_INCONSISTENT_HISTORY')
            except (KeyError, ValueError, ArithmeticError):
                return reject('INVALID_PRICE_OR_VOLUME')
    try:
        closes = [Decimal(str(r["close_price"])) for r in bars]
        bc = [Decimal(str(r["close_price"])) for r in index]
        p = Decimal(str(price))
        volumes = [Decimal(str(r["volume"])) for r in bars]
        if not all(v.is_finite() and v > 0 for v in closes + bc + volumes + [p]):
            return reject("INVALID_PRICE_OR_VOLUME")
        ranges = []
        for i, r in enumerate(bars[1:], 1):
            high, low = Decimal(str(r["high_price"])), Decimal(str(r["low_price"]))
            if not high.is_finite() or not low.is_finite() or not (0 < low <= closes[i] <= high):
                return reject("INVALID_OHLC")
            ranges.append(max(high-low, abs(high-closes[i-1]), abs(low-closes[i-1])))
    except (KeyError, ValueError, ArithmeticError):
        return reject("MISSING_MARKET_FEATURE")
    ma5, ma20 = sum(closes[-5:])/5, sum(closes)/20
    bma5, bma20 = sum(bc[-5:])/5, sum(bc)/20
    relative = closes[-1]/closes[0] - bc[-1]/bc[0]
    atr = sum(ranges[-14:])/14/closes[-1]
    extension = p/ma5-1
    volume_ratio = volumes[-1]/(sum(volumes[-6:-1])/5)
    observed.update({k:str(v) for k,v in {"ma5":ma5,"ma20":ma20,"relative_strength":relative,
        "atr_ratio":atr,"entry_extension":extension,"volume_ratio":volume_ratio,"price":p,
        "benchmark_close":bc[-1],"benchmark_ma5":bma5,"benchmark_ma20":bma20}.items()})
    if bc[-1] < bma20 or bma5 < bma20:
        return reject("MARKET_RISK_OFF")
    if closes[-1] < ma20 or ma5 < ma20 or relative < 0:
        return reject("TREND_OR_RELATIVE_STRENGTH_FAILED")
    if atr > Decimal(str(max_atr)):
        return reject("VOLATILITY_TOO_HIGH")
    if extension > Decimal(str(max_extension)) or p < ma20:
        return reject("WATCH_ENTRY_PRICE")
    if closes[-1] < closes[-2] or volume_ratio < Decimal("0.5"):
        return reject("WATCH_STABILIZATION")
    score = max(Decimal(0), min(Decimal(1), Decimal("0.5") + relative*2 - atr - max(extension,Decimal(0))))
    return True, score, "TREND_PULLBACK_CONFIRMED", observed
