package domain

import "time"

type ResolutionRunStatus string

const (
	ResolutionRunStatusRunning   ResolutionRunStatus = "RUNNING"
	ResolutionRunStatusSucceeded ResolutionRunStatus = "SUCCEEDED"
	ResolutionRunStatusFailed    ResolutionRunStatus = "FAILED"
	ResolutionRunStatusPartial   ResolutionRunStatus = "PARTIAL"
)

type ResolutionRoute string

const (
	ResolutionRouteLegacyLLM    ResolutionRoute = "legacy_llm"
	ResolutionRouteAgentPrimary ResolutionRoute = "agent_primary"
	ResolutionRouteAgentShadow  ResolutionRoute = "agent_shadow"
	ResolutionRouteLocalOnly    ResolutionRoute = "local_only"
)

type ResolutionTargetDecision string

const (
	ResolutionTargetAccepted    ResolutionTargetDecision = "ACCEPTED"
	ResolutionTargetRejected    ResolutionTargetDecision = "REJECTED"
	ResolutionTargetUntrackable ResolutionTargetDecision = "UNTRACKABLE"
	ResolutionTargetAmbiguous   ResolutionTargetDecision = "AMBIGUOUS"
)

type UntrackableReasonCode string

const (
	UntrackableReasonSectorNotTradable           UntrackableReasonCode = "SECTOR_NOT_TRADABLE"
	UntrackableReasonThemeNotTradable            UntrackableReasonCode = "THEME_NOT_TRADABLE"
	UntrackableReasonIndexNotSupported           UntrackableReasonCode = "INDEX_NOT_SUPPORTED"
	UntrackableReasonCommodityNotSupported       UntrackableReasonCode = "COMMODITY_NOT_SUPPORTED"
	UntrackableReasonSecurityNotFound            UntrackableReasonCode = "SECURITY_NOT_FOUND"
	UntrackableReasonSecurityNotActive           UntrackableReasonCode = "SECURITY_NOT_ACTIVE"
	UntrackableReasonAmbiguousSecurity           UntrackableReasonCode = "AMBIGUOUS_SECURITY"
	UntrackableReasonExternalCandidateUnverified UntrackableReasonCode = "EXTERNAL_CANDIDATE_UNVERIFIED"
	UntrackableReasonToolTimeout                 UntrackableReasonCode = "TOOL_TIMEOUT"
	UntrackableReasonSchemaInvalid               UntrackableReasonCode = "SCHEMA_INVALID"
	UntrackableReasonUnknown                     UntrackableReasonCode = "UNKNOWN"
)

type ResolutionRun struct {
	ID                      int64                 `json:"id"`
	DocumentID              int64                 `json:"document_id"`
	ParseRunID              int64                 `json:"parse_run_id,omitempty"`
	ConfigVersion           int64                 `json:"config_version"`
	AgentMode               string                `json:"agent_mode"`
	Route                   string                `json:"route"`
	Status                  ResolutionRunStatus   `json:"status"`
	SchemaVersion           string                `json:"schema_version"`
	AgentVersion            string                `json:"agent_version"`
	SkillName               string                `json:"skill_name"`
	SkillVersion            string                `json:"skill_version"`
	SkillHash               string                `json:"skill_hash"`
	FallbackUsed            bool                  `json:"fallback_used"`
	RawTargetCount          int                   `json:"raw_target_count"`
	CandidatePlanInputCount int                   `json:"candidate_plan_input_count"`
	CandidatePlanCount      int                   `json:"candidate_plan_count"`
	UntrackableCount        int                   `json:"untrackable_count"`
	ToolCallCount           int                   `json:"tool_call_count"`
	ErrorCode               string                `json:"error_code,omitempty"`
	ErrorMessage            string                `json:"error_message,omitempty"`
	StartedAt               time.Time             `json:"started_at"`
	FinishedAt              *time.Time            `json:"finished_at,omitempty"`
	DurationMS              int                   `json:"duration_ms"`
	Targets                 []ResolutionTarget    `json:"targets,omitempty"`
	ToolTraces              []ResolutionToolTrace `json:"tool_traces,omitempty"`
	ShadowCompare           map[string]any        `json:"shadow_compare,omitempty"`
	RawMetadata             map[string]any        `json:"raw_metadata,omitempty"`
	CreatedAt               time.Time             `json:"created_at"`
	UpdatedAt               time.Time             `json:"updated_at"`
}

type ResolutionTarget struct {
	RawTarget        string                          `json:"raw_target"`
	NormalizedTarget string                          `json:"normalized_target,omitempty"`
	Decision         ResolutionTargetDecision        `json:"decision"`
	ReasonCode       string                          `json:"reason_code,omitempty"`
	ReasonMessage    string                          `json:"reason_message,omitempty"`
	TargetKind       InstrumentTargetKind            `json:"target_kind,omitempty"`
	Security         *InstrumentResolutionCandidate  `json:"security,omitempty"`
	Candidates       []InstrumentResolutionCandidate `json:"candidates,omitempty"`
	MatchSource      string                          `json:"match_source,omitempty"`
	Source           string                          `json:"source,omitempty"`
	Evidence         []EvidenceSpan                  `json:"evidence,omitempty"`
	ToolName         string                          `json:"tool_name,omitempty"`
}

type ResolutionToolTrace struct {
	ToolName       string         `json:"tool_name"`
	ToolSource     string         `json:"tool_source,omitempty"`
	Input          map[string]any `json:"input,omitempty"`
	Status         string         `json:"status"`
	CandidateCount int            `json:"candidate_count,omitempty"`
	DurationMS     int            `json:"duration_ms,omitempty"`
	ErrorCode      string         `json:"error_code,omitempty"`
}

type UntrackableTarget struct {
	ID               int64                           `json:"id"`
	ResolutionRunID  int64                           `json:"resolution_run_id"`
	DocumentID       int64                           `json:"document_id"`
	ParseRunID       int64                           `json:"parse_run_id,omitempty"`
	RawTarget        string                          `json:"raw_target"`
	NormalizedTarget string                          `json:"normalized_target"`
	TargetKind       InstrumentTargetKind            `json:"target_kind"`
	ReasonCode       string                          `json:"reason_code"`
	ReasonMessage    string                          `json:"reason_message"`
	Source           string                          `json:"source"`
	Evidence         []EvidenceSpan                  `json:"evidence,omitempty"`
	Candidates       []InstrumentResolutionCandidate `json:"candidates,omitempty"`
	ConfigVersion    int64                           `json:"config_version"`
	IsActive         bool                            `json:"is_active"`
	CreatedAt        time.Time                       `json:"created_at"`
	UpdatedAt        time.Time                       `json:"updated_at"`
}
