import pytest
from pydantic import ValidationError

from app.schemas import (
    AgentCandidatePlanInput,
    AgentRawIntent,
    AgentResolveDocumentResponse,
    AgentSecurity,
    AgentStatus,
    EvidenceSpan,
)


def test_raw_intent_rejects_invalid_direction():
    with pytest.raises(ValidationError):
        _raw_intent(direction="BUY")


def test_raw_intent_rejects_invalid_confidence():
    with pytest.raises(ValidationError):
        _raw_intent(confidence=1.5)


def test_candidate_input_rejects_invalid_ts_code():
    with pytest.raises(ValidationError):
        AgentCandidatePlanInput(
            **_raw_intent().model_dump(),
            security=AgentSecurity(
                ts_code="CPO板块.SZ",
                symbol="CPO板块",
                name="CPO板块",
                asset_type="STOCK",
                market="SZ",
            ),
        )


def test_candidate_input_accepts_eastmoney_sector():
    item = AgentCandidatePlanInput(
        **_raw_intent(raw_symbol="CPO板块").model_dump(),
        security=AgentSecurity(
            ts_code="BK1128.DC",
            symbol="BK1128",
            name="CPO概念",
            asset_type="SECTOR",
            market="DC",
        ),
    )

    assert item.security.asset_type == "SECTOR"


def test_successful_response_requires_output():
    with pytest.raises(ValidationError):
        AgentResolveDocumentResponse(agent_version="test", status=AgentStatus.resolved)


def _raw_intent(**overrides):
    values = {
        "intent_id": "intent-1",
        "raw_symbol": "新易盛",
        "direction": "LONG",
        "reference_price": 88.8,
        "reference_price_note": "explicit_price_mention",
        "thesis": "source text supports recommendation",
        "evidence": [EvidenceSpan(chunk_index=0, text="source evidence")],
        "risks": ["volatility"],
        "confidence": 0.8,
    }
    values.update(overrides)
    return AgentRawIntent(**values)
