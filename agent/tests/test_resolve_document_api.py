from fastapi.testclient import TestClient

from app.main import app
from app.schemas import REQUEST_SCHEMA_VERSION, RESPONSE_SCHEMA_VERSION


client = TestClient(app)


def test_healthz():
    response = client.get("/healthz")
    assert response.status_code == 200
    assert response.json()["status"] == "ok"


def test_resolve_document_api_returns_stable_response():
    response = client.post("/v1/resolve-document", json=_request_payload("推荐新易盛和CPO板块"))
    assert response.status_code == 200
    payload = response.json()
    assert payload["schema_version"] == RESPONSE_SCHEMA_VERSION
    assert payload["status"] == "RESOLVED"
    assert payload["extracted_author"] == ""
    assert [item["raw_symbol"] for item in payload["raw_intents"]] == ["新易盛", "CPO板块"]
    assert payload["untrackable_targets"][0]["raw_symbol"] == "CPO板块"


def test_resolve_document_api_rejects_invalid_request_schema():
    payload = _request_payload("推荐新易盛")
    payload["schema_version"] = "agent.resolve_document.request.v0"
    response = client.post("/v1/resolve-document", json=payload)
    assert response.status_code == 422


def _request_payload(text: str):
    return {
        "schema_version": REQUEST_SCHEMA_VERSION,
        "request_id": "api-test",
        "document": {
            "document_id": 1,
            "parse_run_id": 2,
            "title": "M4 API test",
            "author": "M4 Tester",
            "institution": "Integration",
        },
        "trade_date": "2026-05-24",
        "chunks": [{"chunk_index": 0, "text": text}],
        "limits": {
            "max_intents": 10,
            "max_evidence_per_intent": 4,
            "max_risks_per_intent": 5,
            "max_untrackable_targets": 10,
        },
    }
