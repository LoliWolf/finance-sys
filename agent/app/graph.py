from time import monotonic
from typing import Any, Dict, List, Optional, TypedDict
from uuid import uuid4

from langgraph.graph import END, StateGraph

from app.config import get_settings
from app.llm_client import LLMClient
from app.nodes.classify import classify_untrackable_targets
from app.nodes.extract import extract_first_author, extract_raw_intents
from app.schemas import (
    AgentDebug,
    AgentResolveDocumentRequest,
    AgentResolveDocumentResponse,
    AgentStatus,
)
from app.security_client import SecurityClient, SecurityClientError, SecurityMatch
from app.skills import SkillSpec, load_instrument_resolution_skill
from app.tools.registry import load_instrument_resolution_tool_registry
from app.tools.tushare_tool import (
    OfficialTushareStockBasicTool,
    TushareSecurityCandidate,
    TushareToolError,
)


class AgentGraphState(TypedDict, total=False):
    request: AgentResolveDocumentRequest
    extracted_author: str
    raw_intents: List
    untrackable_targets: List
    candidate_plan_inputs: List
    warnings: List[str]
    unresolved_raw_intents: List
    external_candidates: List[Dict[str, Any]]
    tools_used: List[str]
    skill: SkillSpec
    tool_registry_count: int
    nodes: List[str]
    started_at: float
    graph_run_id: str


def build_graph():
    graph = StateGraph(AgentGraphState)
    graph.add_node("load_skill", _load_skill_node)
    graph.add_node("extract_raw_intents", _extract_raw_intents_node)
    graph.add_node("resolve_with_local_security", _resolve_with_local_security_node)
    graph.add_node("resolve_with_external_tools", _resolve_with_external_tools_node)
    graph.add_node("verify_external_candidates", _verify_external_candidates_node)
    graph.add_node("classify_untrackable_targets", _classify_untrackable_targets_node)
    graph.set_entry_point("load_skill")
    graph.add_edge("load_skill", "extract_raw_intents")
    graph.add_edge("extract_raw_intents", "resolve_with_local_security")
    graph.add_edge("resolve_with_local_security", "resolve_with_external_tools")
    graph.add_edge("resolve_with_external_tools", "verify_external_candidates")
    graph.add_edge("verify_external_candidates", "classify_untrackable_targets")
    graph.add_edge("classify_untrackable_targets", END)
    return graph.compile()


def resolve_document(request: AgentResolveDocumentRequest) -> AgentResolveDocumentResponse:
    started_at = monotonic()
    if any("AGENT_FAILED_SENTINEL" in chunk.text for chunk in request.chunks):
        skill = _load_skill_for_failed_response()
        return _failed_response(request, started_at, "forced failure sentinel", skill)

    try:
        state = _compiled_graph().invoke(
            {
                "request": request,
                "extracted_author": "",
                "raw_intents": [],
                "untrackable_targets": [],
                "candidate_plan_inputs": [],
                "warnings": [],
                "unresolved_raw_intents": [],
                "external_candidates": [],
                "tools_used": [],
                "nodes": [],
                "started_at": started_at,
                "graph_run_id": str(uuid4()),
            }
        )
    except Exception as exc:
        return _failed_response(request, started_at, str(exc), _load_skill_for_failed_response())
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
        return _failed_response(
            request,
            started_at,
            "no instrument intent extracted",
            state.get("skill"),
        )

    return AgentResolveDocumentResponse(
        agent_version=get_settings().agent_version,
        status=status,
        extracted_author=state.get("extracted_author", ""),
        raw_intents=raw_intents,
        candidate_plan_inputs=candidate_inputs,
        untrackable_targets=untrackable_targets,
        warnings=warnings,
        debug=AgentDebug(
            graph_run_id=state.get("graph_run_id", ""),
            nodes=state.get("nodes", []),
            tools_used=state.get("tools_used", []),
            duration_ms=_duration_ms(started_at),
            skill_name=state["skill"].name,
            skill_version=state["skill"].version,
            skill_hash=state["skill"].skill_hash,
        ),
    )


def _compiled_graph():
    if not hasattr(_compiled_graph, "_graph"):
        _compiled_graph._graph = build_graph()
    return _compiled_graph._graph


def _load_skill_node(state: AgentGraphState) -> Dict:
    skill = load_instrument_resolution_skill()
    tool_registry = load_instrument_resolution_tool_registry()
    return {
        "skill": skill,
        "tool_registry_count": len(tool_registry),
        "nodes": state.get("nodes", []) + ["load_skill"],
    }


def _extract_raw_intents_node(state: AgentGraphState) -> Dict:
    request = state["request"]
    chunks = [(chunk.chunk_index, chunk.text) for chunk in request.chunks]
    llm_client = LLMClient()
    if llm_client.enabled():
        extraction = llm_client.extract_document(
            "\n\n".join(chunk.text for chunk in request.chunks),
            request.limits.max_intents,
            state["skill"],
        )
        raw_intents = extraction.raw_intents
        extracted_author = extraction.author
    else:
        raw_intents = extract_raw_intents(chunks, request.limits.max_intents)
        extracted_author = extract_first_author(chunks)
    return {
        "extracted_author": extracted_author,
        "raw_intents": raw_intents,
        "nodes": state.get("nodes", []) + ["extract_raw_intents"],
    }


def _resolve_with_local_security_node(state: AgentGraphState) -> Dict:
    client = SecurityClient()
    candidate_inputs = list(state.get("candidate_plan_inputs", []))
    unresolved = []
    warnings = list(state.get("warnings", []))
    tools_used = list(state.get("tools_used", []))
    if not client.enabled():
        warnings.append("local security endpoint is not enabled; returning raw intents for Go M3 resolution")
        return {
            "candidate_plan_inputs": candidate_inputs,
            "unresolved_raw_intents": list(state.get("raw_intents", [])),
            "warnings": warnings,
            "tools_used": tools_used,
            "nodes": state.get("nodes", []) + ["resolve_with_local_security"],
        }
    for intent in state.get("raw_intents", []):
        tools_used = _append_tool(tools_used, "local_security_lookup_tool")
        try:
            matches = client.lookup(intent.raw_symbol)
        except SecurityClientError as exc:
            warnings.append(str(exc))
            unresolved.append(intent)
            continue
        if len(matches) == 1:
            candidate_inputs.append(_candidate_input_from_match(intent, matches[0]))
            continue
        if len(matches) > 1:
            warnings.append(f"local security lookup for {intent.raw_symbol} returned multiple candidates")
        unresolved.append(intent)
    return {
        "candidate_plan_inputs": candidate_inputs,
        "unresolved_raw_intents": unresolved,
        "warnings": warnings,
        "tools_used": tools_used,
        "nodes": state.get("nodes", []) + ["resolve_with_local_security"],
    }


def _resolve_with_external_tools_node(state: AgentGraphState) -> Dict:
    warnings = list(state.get("warnings", []))
    external_candidates = []
    tools_used = list(state.get("tools_used", []))
    unresolved = state.get("unresolved_raw_intents", [])
    if not unresolved:
        return {
            "external_candidates": external_candidates,
            "warnings": warnings,
            "tools_used": tools_used,
            "nodes": state.get("nodes", []) + ["resolve_with_external_tools"],
        }

    tushare_tool = OfficialTushareStockBasicTool()
    if not tushare_tool.enabled():
        warnings.append("tushare_stock_basic_tool is not enabled; external candidate recall skipped")
        return {
            "external_candidates": external_candidates,
            "warnings": warnings,
            "tools_used": tools_used,
            "nodes": state.get("nodes", []) + ["resolve_with_external_tools"],
        }

    for intent in unresolved:
        tools_used = _append_tool(tools_used, "tushare_stock_basic_tool")
        try:
            candidates = tushare_tool.search(intent.raw_symbol)
        except TushareToolError as exc:
            warnings.append(str(exc))
            continue
        if not candidates:
            continue
        external_candidates.append({"intent": intent, "candidates": candidates})

    return {
        "external_candidates": external_candidates,
        "warnings": warnings,
        "tools_used": tools_used,
        "nodes": state.get("nodes", []) + ["resolve_with_external_tools"],
    }


def _verify_external_candidates_node(state: AgentGraphState) -> Dict:
    candidate_inputs = list(state.get("candidate_plan_inputs", []))
    warnings = list(state.get("warnings", []))
    tools_used = list(state.get("tools_used", []))
    external_candidates = state.get("external_candidates", [])
    if not external_candidates:
        return {
            "candidate_plan_inputs": candidate_inputs,
            "warnings": warnings,
            "tools_used": tools_used,
            "nodes": state.get("nodes", []) + ["verify_external_candidates"],
        }

    client = SecurityClient()
    if not client.enabled():
        warnings.append("local security verify endpoint is not enabled; external candidates ignored")
        return {
            "candidate_plan_inputs": candidate_inputs,
            "warnings": warnings,
            "tools_used": tools_used,
            "nodes": state.get("nodes", []) + ["verify_external_candidates"],
        }

    for item in external_candidates:
        intent = item["intent"]
        candidates: List[TushareSecurityCandidate] = item["candidates"]
        if len(candidates) != 1:
            warnings.append(f"tushare_stock_basic_tool for {intent.raw_symbol} returned multiple candidates")
            continue
        tools_used = _append_tool(tools_used, "local_security_verify_tool")
        try:
            verified = client.verify(candidates[0].ts_code, intent.raw_symbol)
        except SecurityClientError as exc:
            warnings.append(str(exc))
            continue
        if verified is None:
            warnings.append(f"external candidate {candidates[0].ts_code} for {intent.raw_symbol} was not verified locally")
            continue
        candidate_inputs.append(_candidate_input_from_match(intent, verified))

    return {
        "candidate_plan_inputs": candidate_inputs,
        "warnings": warnings,
        "tools_used": tools_used,
        "nodes": state.get("nodes", []) + ["verify_external_candidates"],
    }


def _classify_untrackable_targets_node(state: AgentGraphState) -> Dict:
    request = state["request"]
    resolved_intent_ids = {
        (item.get("intent_id", "") if isinstance(item, dict) else item.intent_id)
        for item in state.get("candidate_plan_inputs", [])
    }
    unresolved = [
        item
        for item in state.get("raw_intents", [])
        if item.intent_id not in resolved_intent_ids
    ]
    targets = classify_untrackable_targets(
        unresolved,
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
    skill: Optional[SkillSpec] = None,
) -> AgentResolveDocumentResponse:
    return AgentResolveDocumentResponse(
        agent_version=get_settings().agent_version,
        status=AgentStatus.failed,
        extracted_author="",
        raw_intents=[],
        candidate_plan_inputs=[],
        untrackable_targets=[],
        warnings=[reason],
        debug=AgentDebug(
            graph_run_id=f"failed-{request.request_id}",
            nodes=["load_skill"] if skill else [],
            tools_used=[],
            duration_ms=_duration_ms(started_at),
            skill_name=skill.name if skill else "",
            skill_version=skill.version if skill else "",
            skill_hash=skill.skill_hash if skill else "",
        ),
    )


def _duration_ms(started_at: float) -> int:
    return int((monotonic() - started_at) * 1000)


def _load_skill_for_failed_response() -> Optional[SkillSpec]:
    try:
        return load_instrument_resolution_skill()
    except Exception:
        return None


def _candidate_input_from_match(intent, match: SecurityMatch) -> Dict:
    return {
        **intent.model_dump(),
        "security": {
            "ts_code": match.ts_code,
            "symbol": match.symbol,
            "name": match.name,
            "asset_type": match.asset_type,
            "market": match.market,
        },
    }


def _append_tool(tools_used: List[str], tool_name: str) -> List[str]:
    if tool_name not in tools_used:
        tools_used.append(tool_name)
    return tools_used
