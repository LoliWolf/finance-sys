import re
from typing import Iterable, List, Tuple

from app.schemas import AgentRawIntent, EvidenceSpan


KNOWN_TARGETS = (
    "新易盛",
    "中际旭创",
    "旭创",
    "CPO板块",
    "A股贵金属个股",
)

TS_CODE_RE = re.compile(r"(?:\b\d{6}\.(?:SH|SZ|BJ)\b|\bBK\d{4}\.DC\b)")
PRICE_RE = re.compile(r"([0-9]+(?:\.[0-9]+)?)\s*元")
AUTHOR_RE = re.compile(
    r"([\u4e00-\u9fff·]{2,12})\s*[（(]\s*(?:首席分析师|分析师|研究员|作者)\s*[）)]"
)


def extract_raw_intents(chunks: Iterable[Tuple[int, str]], max_intents: int) -> List[AgentRawIntent]:
    intents: List[AgentRawIntent] = []
    seen = set()
    for chunk_index, text in chunks:
        for target in _targets_in_text(text):
            if target in seen:
                continue
            seen.add(target)
            price, note = _extract_reference_price(text)
            intents.append(
                AgentRawIntent(
                    intent_id=f"raw-{len(intents) + 1}",
                    raw_symbol=target,
                    direction=_direction_from_text(text),
                    reference_price=price,
                    reference_price_note=note,
                    thesis="source text contains an explicit investment target",
                    evidence=[EvidenceSpan(chunk_index=chunk_index, text=_evidence_text(text, target))],
                    risks=["source text requires downstream verification"],
                    confidence=0.78,
                )
            )
            if len(intents) >= max_intents:
                return intents
    return intents


def extract_first_author(chunks: Iterable[Tuple[int, str]]) -> str:
    for _, text in chunks:
        match = AUTHOR_RE.search(text)
        if match:
            return match.group(1).strip()
    return ""


def _targets_in_text(text: str) -> List[str]:
    targets = [target for target in KNOWN_TARGETS if target in text]
    targets.extend(match.group(0) for match in TS_CODE_RE.finditer(text))
    return targets


def _extract_reference_price(text: str) -> Tuple[float, str]:
    match = PRICE_RE.search(text)
    if not match:
        return 0.0, "price_missing_in_text"
    return float(match.group(1)), "explicit_price_mention"


def _direction_from_text(text: str) -> str:
    if "看空" in text or "做空" in text or "下调" in text:
        return "SHORT"
    return "LONG"


def _evidence_text(text: str, target: str) -> str:
    index = text.find(target)
    if index < 0:
        return text[:160]
    start = max(0, index - 60)
    end = min(len(text), index + len(target) + 100)
    return text[start:end].strip()
