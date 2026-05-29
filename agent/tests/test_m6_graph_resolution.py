from app.graph import resolve_document
from app.schemas import (
    REQUEST_SCHEMA_VERSION,
    AgentDocument,
    AgentDocumentChunk,
    AgentLimits,
    AgentResolveDocumentRequest,
)
from app.tools.local_security import SecurityMatch
from app.tools.tushare_tool import TushareSecurityCandidate


def test_m6_graph_outputs_candidate_plan_inputs_from_local_security(monkeypatch):
    class FakeSecurityClient:
        def enabled(self):
            return True

        def lookup(self, raw_symbol):
            return [
                SecurityMatch(
                    ts_code="300502.SZ",
                    symbol="300502",
                    name="Sample",
                    asset_type="STOCK",
                    market="SZ",
                    match_source="direct",
                )
            ]

    monkeypatch.setattr("app.graph.SecurityClient", FakeSecurityClient)

    response = resolve_document(_request("M6 local 300502.SZ"))

    assert response.candidate_plan_inputs[0].security.ts_code == "300502.SZ"
    assert response.debug.tools_used == ["local_security_lookup_tool"]
    assert "resolve_with_local_security" in response.debug.nodes


def test_m6_graph_uses_tushare_candidate_only_after_local_verify(monkeypatch):
    class FakeSecurityClient:
        def enabled(self):
            return True

        def lookup(self, raw_symbol):
            return []

        def verify(self, ts_code, raw_symbol=""):
            return SecurityMatch(
                ts_code=ts_code,
                symbol="300502",
                name="Sample",
                asset_type="STOCK",
                market="SZ",
                match_source="direct",
            )

    class FakeTushareTool:
        def enabled(self):
            return True

        def search(self, raw_symbol):
            return [
                TushareSecurityCandidate(
                    ts_code="300502.SZ",
                    symbol="300502",
                    name="Sample",
                    asset_type="STOCK",
                    market="SZ",
                    list_status="L",
                )
            ]

    monkeypatch.setattr("app.graph.SecurityClient", FakeSecurityClient)
    monkeypatch.setattr("app.graph.OfficialTushareStockBasicTool", FakeTushareTool)

    response = resolve_document(_request("M6 external 300502.SZ"))

    assert response.candidate_plan_inputs[0].security.ts_code == "300502.SZ"
    assert response.debug.tools_used == [
        "local_security_lookup_tool",
        "tushare_stock_basic_tool",
        "local_security_verify_tool",
    ]


def test_m6_graph_rejects_unverified_tushare_candidate(monkeypatch):
    class FakeSecurityClient:
        def enabled(self):
            return True

        def lookup(self, raw_symbol):
            return []

        def verify(self, ts_code, raw_symbol=""):
            return None

    class FakeTushareTool:
        def enabled(self):
            return True

        def search(self, raw_symbol):
            return [
                TushareSecurityCandidate(
                    ts_code="300502.SZ",
                    symbol="300502",
                    name="Sample",
                    asset_type="STOCK",
                    market="SZ",
                    list_status="L",
                )
            ]

    monkeypatch.setattr("app.graph.SecurityClient", FakeSecurityClient)
    monkeypatch.setattr("app.graph.OfficialTushareStockBasicTool", FakeTushareTool)

    response = resolve_document(_request("M6 unverified 300502.SZ"))

    assert response.candidate_plan_inputs == []
    assert any("was not verified locally" in warning for warning in response.warnings)


def _request(text: str) -> AgentResolveDocumentRequest:
    return AgentResolveDocumentRequest(
        schema_version=REQUEST_SCHEMA_VERSION,
        request_id="m6-graph-test",
        document=AgentDocument(document_id=1, parse_run_id=2),
        trade_date="2026-05-24",
        chunks=[AgentDocumentChunk(chunk_index=0, text=text)],
        limits=AgentLimits(max_intents=10),
    )
