from app.graph import resolve_document
from app.schemas import (
    REQUEST_SCHEMA_VERSION,
    AgentDocument,
    AgentDocumentChunk,
    AgentLimits,
    AgentResolveDocumentRequest,
    AgentStatus,
)


def test_resolve_document_extracts_raw_intents_and_untrackable_targets():
    response = resolve_document(
        AgentResolveDocumentRequest(
            schema_version=REQUEST_SCHEMA_VERSION,
            request_id="graph-test",
            document=AgentDocument(document_id=1, parse_run_id=2),
            trade_date="2026-05-24",
            chunks=[
                AgentDocumentChunk(
                    chunk_index=0,
                    text="推荐新易盛和旭创，参考价88.8元，同时关注CPO板块。",
                )
            ],
            limits=AgentLimits(max_intents=10),
        )
    )

    assert response.status == AgentStatus.resolved
    assert [item.raw_symbol for item in response.raw_intents] == ["新易盛", "旭创", "CPO板块"]
    assert [item.raw_symbol for item in response.untrackable_targets] == ["CPO板块"]
    assert response.candidate_plan_inputs == []
    assert "extract_raw_intents" in response.debug.nodes
    assert "entry_price" not in response.model_dump()


def test_resolve_document_extracts_first_joint_author_in_local_fallback():
    response = resolve_document(
        AgentResolveDocumentRequest(
            schema_version=REQUEST_SCHEMA_VERSION,
            request_id="graph-author-test",
            document=AgentDocument(document_id=1, parse_run_id=2),
            trade_date="2026-05-24",
            chunks=[
                AgentDocumentChunk(
                    chunk_index=0,
                    text="张豪杰（分析师） 韩笑（分析师） 推荐新易盛。",
                )
            ],
            limits=AgentLimits(max_intents=10),
        )
    )

    assert response.extracted_author == "张豪杰"


def test_resolve_document_failed_sentinel_returns_failed_status():
    response = resolve_document(
        AgentResolveDocumentRequest(
            schema_version=REQUEST_SCHEMA_VERSION,
            request_id="failed-test",
            document=AgentDocument(document_id=1, parse_run_id=2),
            trade_date="2026-05-24",
            chunks=[AgentDocumentChunk(chunk_index=0, text="AGENT_FAILED_SENTINEL")],
            limits=AgentLimits(),
        )
    )

    assert response.status == AgentStatus.failed
    assert response.raw_intents == []
