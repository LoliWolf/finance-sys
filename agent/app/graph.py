from time import monotonic
from typing import Dict, List, TypedDict
from uuid import uuid4

from langgraph.graph import END, StateGraph

from app.config import get_settings
from app.llm_client import LLMClient
from app.nodes.classify import classify_untrackable_targets
from app.nodes.extract import extract_raw_intents
from app.schemas import (
    AgentDebug,
    AgentResolveDocumentRequest,
    AgentResolveDocumentResponse,
    AgentStatus,
)
from app.security_client import SecurityClient


class AgentGraphState(TypedDict, total=False):
    request: AgentResolveDocumentRequest
    raw_intents: List
    untrackable_targets: List
    candidate_plan_inputs: List
    warnings: List[str]
    nodes: List[str]
    started_at: float
    graph_run_id: str


def build_graph():
    graph = StateGraph(AgentGraphState)
    graph.add_node("extract_raw_intents", _extract_raw_intents_node)
    graph.add_node("resolve_candidates", _resolve_candidates_node)
    graph.add_node("classify_untrackable_targets", _classify_untrackable_targets_node)
    graph.set_entry_point("extract_raw_intents")
    graph.add_edge("extract_raw_intents", "resolve_candidates")
    graph.add_edge("resolve_candidates", "classify_untrackable_targets")
    graph.add_edge("classify_untrackable_targets", END)
    return graph.compile()


def resolve_document(request: AgentResolveDocumentRequest) -> AgentResolveDocumentResponse:
    started_at = monotonic()
    if any("AGENT_FAILED_SENTINEL" in chunk.text for chunk in request.chunks):
        return _failed_response(request, started_at, "forced failure sentinel")

    try:
        state = _compiled_graph().invoke(
            {
                "request": request,
                "raw_intents": [],
                "untrackable_targets": [],
                "candidate_plan_inputs": [],
                "warnings": [],
                "nodes": [],
                "started_at": started_at,
                "graph_run_id": str(uuid4()),
            }
        )
    except Exception as exc:
        return _failed_response(request, started_at, str(exc))
    raw_intents = state.get("raw_intents", [])
    candidate_inputs = state.get("candidate_plan_inputs", [])
    untrackable_targets = state.get("untrackable_targets", [])
    warnings = state.get("warnings", [])
    if candidate_inputs and untrackable_targets:
        status = AgentStatus.partial
    elif raw_intents or candidate_inputs:
        status = AgentStatus.resolved
    elif untrackable_targets:
        status = AgentStatus.partial
    else:
        return _failed_response(request, started_at, "no instrument intent extracted")

    return AgentResolveDocumentResponse(
        agent_version=get_settings().agent_version,
        status=status,
        raw_intents=raw_intents,
        candidate_plan_inputs=candidate_inputs,
        untrackable_targets=untrackable_targets,
        warnings=warnings,
        debug=AgentDebug(
            graph_run_id=state.get("graph_run_id", ""),
            nodes=state.get("nodes", []),
            tools_used=[],
            duration_ms=_duration_ms(started_at),
        ),
    )


def _compiled_graph():
    if not hasattr(_compiled_graph, "_graph"):
        _compiled_graph._graph = build_graph()
    return _compiled_graph._graph


def _extract_raw_intents_node(state: AgentGraphState) -> Dict:
    request = state["request"]
    chunks = [(chunk.chunk_index, chunk.text) for chunk in request.chunks]
    llm_client = LLMClient()
    if llm_client.enabled():
        raw_intents = llm_client.extract_raw_intents(
            "\n\n".join(chunk.text for chunk in request.chunks),
            request.limits.max_intents,
        )
    else:
        raw_intents = extract_raw_intents(chunks, request.limits.max_intents)
    return {
        "raw_intents": raw_intents,
        "nodes": state.get("nodes", []) + ["extract_raw_intents"],
    }


def _resolve_candidates_node(state: AgentGraphState) -> Dict:
    client = SecurityClient()
    candidate_inputs = []
    warnings = list(state.get("warnings", []))
    for intent in state.get("raw_intents", []):
        match = client.lookup(intent.raw_symbol)
        if match is None:
            continue
        candidate_inputs.append(
            {
                **intent.model_dump(),
                "security": {
                    "ts_code": match.ts_code,
                    "symbol": match.symbol,
                    "name": match.name,
                    "asset_type": match.asset_type,
                    "market": match.market,
                },
            }
        )
    if not candidate_inputs:
        warnings.append("security lookup endpoint is not enabled; returning raw intents for Go M3 resolution")
    return {
        "candidate_plan_inputs": candidate_inputs,
        "warnings": warnings,
        "nodes": state.get("nodes", []) + ["resolve_candidates"],
    }


def _classify_untrackable_targets_node(state: AgentGraphState) -> Dict:
    request = state["request"]
    targets = classify_untrackable_targets(
        state.get("raw_intents", []),
        request.limits.max_untrackable_targets,
    )
    return {
        "untrackable_targets": targets,
        "nodes": state.get("nodes", []) + ["classify_untrackable_targets"],
    }


def _failed_response(
    request: AgentResolveDocumentRequest,
    started_at: float,
    reason: str,
) -> AgentResolveDocumentResponse:
    return AgentResolveDocumentResponse(
        agent_version=get_settings().agent_version,
        status=AgentStatus.failed,
        raw_intents=[],
        candidate_plan_inputs=[],
        untrackable_targets=[],
        warnings=[reason],
        debug=AgentDebug(
            graph_run_id=f"failed-{request.request_id}",
            nodes=[],
            tools_used=[],
            duration_ms=_duration_ms(started_at),
        ),
    )


def _duration_ms(started_at: float) -> int:
    return int((monotonic() - started_at) * 1000)
