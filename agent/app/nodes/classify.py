from typing import List

from app.schemas import AgentRawIntent, AgentUntrackableTarget


def classify_untrackable_targets(
    raw_intents: List[AgentRawIntent],
    max_untrackable_targets: int,
) -> List[AgentUntrackableTarget]:
    targets: List[AgentUntrackableTarget] = []
    for intent in raw_intents:
        kind, reason = _classify(intent.raw_symbol)
        if not kind:
            continue
        targets.append(
            AgentUntrackableTarget(
                raw_symbol=intent.raw_symbol,
                target_kind=kind,
                reason=reason,
                evidence=intent.evidence,
            )
        )
        if len(targets) >= max_untrackable_targets:
            break
    return targets


def _classify(raw_symbol: str):
    if "板块" in raw_symbol:
        return "SECTOR", "sector was not resolved to a supported BKxxxx.DC index"
    if "行业" in raw_symbol:
        return "INDUSTRY", "industry was not resolved to a supported BKxxxx.DC index"
    if "指数" in raw_symbol:
        return "INDEX", "index is not a directly tradable security"
    if "主题" in raw_symbol or "概念" in raw_symbol:
        return "THEME", "theme was not resolved to a supported BKxxxx.DC index"
    if "个股" in raw_symbol or "相关标的" in raw_symbol or "龙头股" in raw_symbol:
        return "BROAD_PHRASE", "broad phrase is not a single tradable security"
    return None, None
