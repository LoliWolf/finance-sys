package domain

import "time"

const (
	SchemaVersionIntent      = "trading-intent/v2"
	SchemaVersionBridgeEvent = "eastmoney-bridge-event/v1"
)

type TradeIntent struct {
	IntentKey             string    `json:"intent_key"`
	RecommendationEventID *int64    `json:"recommendation_event_id,omitempty"`
	CandidatePlanID       *int64    `json:"candidate_plan_id,omitempty"`
	PositionCycleID       *int64    `json:"position_cycle_id,omitempty"`
	Symbol                string    `json:"symbol"`
	TSCode                string    `json:"ts_code"`
	Market                string    `json:"market"`
	AssetType             string    `json:"asset_type"`
	BoardType             string    `json:"board_type"`
	Action                string    `json:"action"`
	ProposedOrderType     string    `json:"proposed_order_type"`
	ProposedLimitPrice    string    `json:"proposed_limit_price"`
	ProposedPositionRatio string    `json:"proposed_position_ratio"`
	ProposedVolume        *int64    `json:"proposed_volume,omitempty"`
	ValidFrom             time.Time `json:"valid_from"`
	ValidUntil            time.Time `json:"valid_until"`
	Confidence            string    `json:"confidence"`
	EvidenceRefs          []string  `json:"evidence_refs"`
	Reason                string    `json:"reason"`
}

type AgentRunRequest struct {
	RunKey           string    `json:"run_key"`
	AsOfTime         time.Time `json:"as_of_time"`
	TriggerType      string    `json:"trigger_type"`
	StrategyName     string    `json:"strategy_name"`
	StrategyVersion  string    `json:"strategy_version"`
	DecisionProvider string    `json:"decision_provider"`
	ToolBaseURL      string    `json:"tool_base_url"`
	ConfigVersion    int64     `json:"config_version"`
	DryRun           bool      `json:"dry_run"`
}

type AgentRunResponse struct {
	SchemaVersion       string               `json:"schema_version"`
	RunKey              string               `json:"run_key"`
	AsOfTime            time.Time            `json:"as_of_time"`
	StrategyName        string               `json:"strategy_name"`
	StrategyVersion     string               `json:"strategy_version"`
	PromptVersion       string               `json:"prompt_version"`
	ToolContractVersion string               `json:"tool_contract_version"`
	CandidateCount      int                  `json:"candidate_count"`
	Intents             []TradeIntent        `json:"intents"`
	SkillDecisions      []AgentSkillDecision `json:"skill_decisions"`
}

type AgentSkillDecision struct {
	DecisionKey     string         `json:"decision_key"`
	IntentKey       string         `json:"intent_key,omitempty"`
	PositionCycleID *int64         `json:"position_cycle_id,omitempty"`
	Stage           string         `json:"stage"`
	SkillName       string         `json:"skill_name"`
	SkillVersion    string         `json:"skill_version"`
	Decision        string         `json:"decision"`
	Score           string         `json:"score"`
	Reason          string         `json:"reason"`
	Input           map[string]any `json:"input"`
	Output          map[string]any `json:"output"`
	EvaluatedAt     time.Time      `json:"evaluated_at"`
}

type RecommendationCandidate struct {
	RecommendationEventID int64     `json:"recommendation_event_id"`
	CandidatePlanID       *int64    `json:"candidate_plan_id,omitempty"`
	BloggerID             int64     `json:"blogger_id"`
	Symbol                string    `json:"symbol"`
	TSCode                string    `json:"ts_code"`
	Market                string    `json:"market"`
	AssetType             string    `json:"asset_type"`
	BoardType             string    `json:"board_type"`
	EastmoneySymbol       string    `json:"eastmoney_symbol"`
	Direction             string    `json:"direction"`
	RecommendDate         string    `json:"recommend_date"`
	ReferencePrice        string    `json:"reference_price"`
	EntryPrice            string    `json:"entry_price"`
	PositionRatio         string    `json:"position_ratio"`
	Confidence            string    `json:"confidence"`
	RuleVersion           string    `json:"rule_version"`
	EvidenceRefs          []string  `json:"evidence_refs"`
	ObservedAt            time.Time `json:"observed_at"`
	ListingTradingDays    int       `json:"listing_trading_days"`
	NoPriceLimitPeriod    bool      `json:"no_price_limit_period"`
}

type CandidateList struct {
	SchemaVersion string                    `json:"schema_version"`
	AsOfTime      time.Time                 `json:"as_of_time"`
	NextCursor    string                    `json:"next_cursor"`
	Items         []RecommendationCandidate `json:"items"`
}

type AccountSnapshot struct {
	SnapshotVersion      string    `json:"snapshot_version"`
	Environment          string    `json:"environment"`
	AccountID            string    `json:"account_id"`
	AccountName          string    `json:"account_name"`
	NAV                  string    `json:"nav"`
	Balance              string    `json:"balance"`
	AvailableCash        string    `json:"available_cash"`
	FrozenCash           string    `json:"frozen_cash"`
	MarketValue          string    `json:"market_value"`
	FloatingPnL          string    `json:"floating_pnl"`
	CumulativeInOut      string    `json:"cumulative_inout"`
	CumulativeTrade      string    `json:"cumulative_trade"`
	CumulativePnL        string    `json:"cumulative_pnl"`
	CumulativeCommission string    `json:"cumulative_commission"`
	LastTrade            string    `json:"last_trade"`
	LastPnL              string    `json:"last_pnl"`
	LastCommission       string    `json:"last_commission"`
	CommissionDataStatus string    `json:"commission_data_status"`
	TerminalState        string    `json:"terminal_state"`
	AccountState         string    `json:"account_state"`
	SnapshotAt           time.Time `json:"snapshot_at"`
}

type PositionSnapshot struct {
	AccountID       string `json:"account_id"`
	Symbol          string `json:"symbol"`
	EastmoneySymbol string `json:"eastmoney_symbol"`
	PositionSide    string `json:"position_side"`
	Volume          int64  `json:"volume"`
	AvailableVolume int64  `json:"available_volume"`
	TodayVolume     int64  `json:"today_volume"`
	VWAP            string `json:"vwap"`
	LastPrice       string `json:"last_price"`
	MarketValue     string `json:"market_value"`
	FloatingPnL     string `json:"floating_pnl"`
}

type QuoteSnapshot struct {
	Symbol          string    `json:"symbol"`
	TSCode          string    `json:"ts_code"`
	EastmoneySymbol string    `json:"eastmoney_symbol"`
	BoardType       string    `json:"board_type"`
	Price           string    `json:"price"`
	Bid1            string    `json:"bid1"`
	Ask1            string    `json:"ask1"`
	PreClose        string    `json:"pre_close"`
	UpperLimit      string    `json:"upper_limit"`
	LowerLimit      string    `json:"lower_limit"`
	SecurityStatus  string    `json:"security_status"`
	ListingDays     int       `json:"listing_days"`
	ObservedAt      time.Time `json:"observed_at"`
	Source          string    `json:"source"`
}

type BridgeHealth struct {
	Status            string    `json:"status"`
	API               string    `json:"api"`
	SQLite            string    `json:"sqlite"`
	Runner            string    `json:"runner"`
	Terminal          string    `json:"terminal"`
	Account           string    `json:"account"`
	AuthState         string    `json:"auth_state"`
	KillSwitch        bool      `json:"kill_switch"`
	AccountID         string    `json:"account_id"`
	StrategyID        string    `json:"strategy_id"`
	ConfigVersion     int64     `json:"config_version"`
	RunnerHeartbeatAt time.Time `json:"runner_heartbeat_at"`
	LastAuthSuccessAt time.Time `json:"last_auth_success_at"`
	TokenFingerprint  string    `json:"token_fingerprint"`
}

type BridgeOrderRequest struct {
	ClientOrderID      string    `json:"client_order_id"`
	Environment        string    `json:"environment"`
	ExpectedAccountID  string    `json:"expected_account_id"`
	StrategyID         string    `json:"strategy_id"`
	Symbol             string    `json:"symbol"`
	TSCode             string    `json:"ts_code"`
	AssetType          string    `json:"asset_type"`
	BoardType          string    `json:"board_type"`
	TradingRuleVersion string    `json:"trading_rule_version"`
	Side               string    `json:"side"`
	OrderType          string    `json:"order_type"`
	PositionEffect     string    `json:"position_effect"`
	Volume             int64     `json:"volume"`
	Price              string    `json:"price"`
	ValidUntil         time.Time `json:"valid_until"`
	SourceIntentID     int64     `json:"source_intent_id"`
	RiskSnapshotHash   string    `json:"risk_snapshot_hash"`
}

type BridgeCommandResponse struct {
	RequestID        string    `json:"request_id"`
	ClientOrderID    string    `json:"client_order_id"`
	CommandID        int64     `json:"command_id"`
	Status           string    `json:"status"`
	IdempotentReplay bool      `json:"idempotent_replay"`
	AcceptedAt       time.Time `json:"accepted_at"`
}

type BridgeEvent struct {
	SchemaVersion    string         `json:"schema_version"`
	EventHash        string         `json:"event_hash"`
	EventType        string         `json:"event_type"`
	ClientOrderID    string         `json:"client_order_id"`
	AccountID        string         `json:"account_id"`
	ClOrdID          string         `json:"cl_ord_id"`
	ExecID           string         `json:"exec_id,omitempty"`
	ProviderStatus   string         `json:"provider_status"`
	NormalizedStatus string         `json:"normalized_status"`
	Symbol           string         `json:"symbol,omitempty"`
	EastmoneySymbol  string         `json:"eastmoney_symbol,omitempty"`
	Side             string         `json:"side,omitempty"`
	FilledVolume     int64          `json:"filled_volume"`
	FilledVWAP       string         `json:"filled_vwap,omitempty"`
	FillPrice        string         `json:"fill_price,omitempty"`
	FillVolume       int64          `json:"fill_volume,omitempty"`
	Commission       string         `json:"commission,omitempty"`
	ExecType         string         `json:"exec_type,omitempty"`
	EventAt          time.Time      `json:"event_at"`
	RawPayload       map[string]any `json:"raw_payload"`
}

type ReconciliationSnapshot struct {
	SnapshotVersion string             `json:"snapshot_version"`
	Cursor          string             `json:"cursor"`
	Health          BridgeHealth       `json:"health"`
	Account         AccountSnapshot    `json:"account"`
	Positions       []PositionSnapshot `json:"positions"`
	Orders          []map[string]any   `json:"orders"`
	Executions      []map[string]any   `json:"executions"`
}
