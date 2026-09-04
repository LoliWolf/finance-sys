package agentclient

import "finance-sys/internal/domain"

const (
	RequestSchemaVersion  = "agent.resolve_document.request.v1"
	ResponseSchemaVersion = "agent.resolve_document.response.v1"
)

type ResolveDocumentRequest struct {
	SchemaVersion string               `json:"schema_version"`
	RequestID     string               `json:"request_id"`
	Document      AgentDocument        `json:"document"`
	TradeDate     string               `json:"trade_date"`
	Chunks        []AgentDocumentChunk `json:"chunks"`
	Limits        AgentLimits          `json:"limits"`
}

type AgentDocument struct {
	DocumentID  int64  `json:"document_id"`
	ParseRunID  int64  `json:"parse_run_id"`
	Title       string `json:"title"`
	Author      string `json:"author"`
	Institution string `json:"institution"`
}

type AgentDocumentChunk struct {
	ChunkIndex int    `json:"chunk_index"`
	Text       string `json:"text"`
}

type AgentLimits struct {
	MaxIntents            int `json:"max_intents"`
	MaxEvidencePerIntent  int `json:"max_evidence_per_intent"`
	MaxRisksPerIntent     int `json:"max_risks_per_intent"`
	MaxUntrackableTargets int `json:"max_untrackable_targets"`
}

type ResolveDocumentResponse struct {
	SchemaVersion      string                    `json:"schema_version"`
	AgentVersion       string                    `json:"agent_version"`
	Status             AgentStatus               `json:"status"`
	ExtractedAuthor    string                    `json:"extracted_author"`
	RawIntents         []AgentRawIntent          `json:"raw_intents"`
	CandidatePlanInput []AgentCandidatePlanInput `json:"candidate_plan_inputs"`
	UntrackableTargets []AgentUntrackableTarget  `json:"untrackable_targets"`
	Warnings           []string                  `json:"warnings"`
	Debug              AgentDebug                `json:"debug"`
}

type AgentStatus string

const (
	AgentStatusResolved AgentStatus = "RESOLVED"
	AgentStatusPartial  AgentStatus = "PARTIAL"
	AgentStatusFailed   AgentStatus = "FAILED"
)

type AgentRawIntent struct {
	IntentID           string                    `json:"intent_id"`
	RawSymbol          string                    `json:"raw_symbol"`
	Direction          domain.TradeDirection     `json:"direction"`
	ReferencePrice     float64                   `json:"reference_price"`
	ReferencePriceNote domain.ReferencePriceNote `json:"reference_price_note"`
	Thesis             string                    `json:"thesis"`
	Evidence           []domain.EvidenceSpan     `json:"evidence"`
	Risks              []string                  `json:"risks"`
	Confidence         float64                   `json:"confidence"`
}

type AgentCandidatePlanInput struct {
	IntentID           string                    `json:"intent_id"`
	RawSymbol          string                    `json:"raw_symbol"`
	Security           AgentSecurity             `json:"security"`
	Direction          domain.TradeDirection     `json:"direction"`
	ReferencePrice     float64                   `json:"reference_price"`
	ReferencePriceNote domain.ReferencePriceNote `json:"reference_price_note"`
	Thesis             string                    `json:"thesis"`
	Evidence           []domain.EvidenceSpan     `json:"evidence"`
	Risks              []string                  `json:"risks"`
	Confidence         float64                   `json:"confidence"`
}

type AgentSecurity struct {
	TSCode    string `json:"ts_code"`
	Symbol    string `json:"symbol"`
	Name      string `json:"name"`
	AssetType string `json:"asset_type"`
	Market    string `json:"market"`
}

type AgentUntrackableTarget struct {
	RawSymbol  string                `json:"raw_symbol"`
	TargetKind string                `json:"target_kind"`
	Reason     string                `json:"reason"`
	Evidence   []domain.EvidenceSpan `json:"evidence"`
}

type AgentDebug struct {
	GraphRunID   string   `json:"graph_run_id"`
	Nodes        []string `json:"nodes"`
	ToolsUsed    []string `json:"tools_used"`
	DurationMS   int64    `json:"duration_ms"`
	SkillName    string   `json:"skill_name"`
	SkillVersion string   `json:"skill_version"`
	SkillHash    string   `json:"skill_hash"`
}
