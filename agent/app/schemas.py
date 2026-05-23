from enum import Enum
from typing import List, Literal

from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator


REQUEST_SCHEMA_VERSION = "agent.resolve_document.request.v1"
RESPONSE_SCHEMA_VERSION = "agent.resolve_document.response.v1"


class StrictBaseModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class AgentStatus(str, Enum):
    resolved = "RESOLVED"
    partial = "PARTIAL"
    failed = "FAILED"


class EvidenceSpan(StrictBaseModel):
    chunk_index: int = Field(ge=0)
    text: str = Field(min_length=1)

    @field_validator("text")
    @classmethod
    def text_must_not_be_blank(cls, value: str) -> str:
        value = value.strip()
        if not value:
            raise ValueError("text is required")
        return value


class AgentDocument(StrictBaseModel):
    document_id: int = Field(ge=0)
    parse_run_id: int = Field(ge=0)
    title: str = ""
    author: str = ""
    institution: str = ""


class AgentDocumentChunk(StrictBaseModel):
    chunk_index: int = Field(ge=0)
    text: str = Field(min_length=1)

    @field_validator("text")
    @classmethod
    def text_must_not_be_blank(cls, value: str) -> str:
        value = value.strip()
        if not value:
            raise ValueError("text is required")
        return value


class AgentLimits(StrictBaseModel):
    max_intents: int = Field(default=20, ge=1, le=100)
    max_evidence_per_intent: int = Field(default=4, ge=1, le=20)
    max_risks_per_intent: int = Field(default=5, ge=0, le=20)
    max_untrackable_targets: int = Field(default=20, ge=0, le=100)


class AgentResolveDocumentRequest(StrictBaseModel):
    schema_version: Literal["agent.resolve_document.request.v1"]
    request_id: str = Field(min_length=1)
    document: AgentDocument
    trade_date: str = Field(min_length=1)
    chunks: List[AgentDocumentChunk] = Field(min_length=1)
    limits: AgentLimits = Field(default_factory=AgentLimits)


class AgentRawIntent(StrictBaseModel):
    intent_id: str = Field(min_length=1)
    raw_symbol: str = Field(min_length=1)
    direction: Literal["LONG", "SHORT"]
    reference_price: float = Field(ge=0)
    reference_price_note: Literal["explicit_price_mention", "price_missing_in_text"]
    thesis: str = Field(min_length=1)
    evidence: List[EvidenceSpan] = Field(min_length=1)
    risks: List[str] = Field(default_factory=list)
    confidence: float = Field(gt=0, le=1)

    @field_validator("raw_symbol", "thesis")
    @classmethod
    def string_must_not_be_blank(cls, value: str) -> str:
        value = value.strip()
        if not value:
            raise ValueError("value is required")
        return value


class AgentSecurity(StrictBaseModel):
    ts_code: str = Field(pattern=r"^\d{6}\.(SH|SZ|BJ)$")
    symbol: str = Field(min_length=1)
    name: str = Field(min_length=1)
    asset_type: Literal["STOCK", "ETF"]
    market: Literal["SH", "SZ", "BJ"]


class AgentCandidatePlanInput(AgentRawIntent):
    security: AgentSecurity


class AgentUntrackableTarget(StrictBaseModel):
    raw_symbol: str = Field(min_length=1)
    target_kind: Literal["SECTOR", "INDUSTRY", "INDEX", "THEME", "BROAD_PHRASE", "UNKNOWN"]
    reason: str = Field(min_length=1)
    evidence: List[EvidenceSpan] = Field(default_factory=list)


class AgentDebug(StrictBaseModel):
    graph_run_id: str = ""
    nodes: List[str] = Field(default_factory=list)
    tools_used: List[str] = Field(default_factory=list)
    duration_ms: int = Field(default=0, ge=0)


class AgentResolveDocumentResponse(StrictBaseModel):
    schema_version: Literal["agent.resolve_document.response.v1"] = RESPONSE_SCHEMA_VERSION
    agent_version: str = Field(min_length=1)
    status: AgentStatus
    raw_intents: List[AgentRawIntent] = Field(default_factory=list)
    candidate_plan_inputs: List[AgentCandidatePlanInput] = Field(default_factory=list)
    untrackable_targets: List[AgentUntrackableTarget] = Field(default_factory=list)
    warnings: List[str] = Field(default_factory=list)
    debug: AgentDebug = Field(default_factory=AgentDebug)

    @model_validator(mode="after")
    def successful_status_requires_output(self) -> "AgentResolveDocumentResponse":
        if self.status in {AgentStatus.resolved, AgentStatus.partial}:
            has_output = bool(self.raw_intents or self.candidate_plan_inputs or self.untrackable_targets)
            if not has_output:
                raise ValueError("successful agent response requires at least one output item")
        return self
