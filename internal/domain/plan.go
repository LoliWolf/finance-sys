package domain

import "time"

type CandidatePlan struct {
	ID             int64          `json:"id"`
	DocumentID     int64          `json:"document_id"`
	ParseRunID     int64          `json:"parse_run_id"`
	Analyst        string         `json:"analyst"`
	Institution    string         `json:"institution"`
	Symbol         string         `json:"symbol"`
	AssetType      string         `json:"asset_type"`
	Market         string         `json:"market"`
	Strategy       string         `json:"strategy"`
	Direction      string         `json:"direction"`
	TradeDate      time.Time      `json:"trade_date"`
	ReferencePrice float64        `json:"reference_price"`
	EntryPrice     float64        `json:"entry_price"`
	StopLoss       float64        `json:"stop_loss"`
	TakeProfit     float64        `json:"take_profit"`
	PositionPct    float64        `json:"position_pct"`
	Confidence     float64        `json:"confidence"`
	Status         string         `json:"status"`
	Thesis         string         `json:"thesis"`
	Risks          []string       `json:"risks"`
	Evidence       []EvidenceSpan `json:"evidence"`
	PricingNote    string         `json:"pricing_note"`
	ConfigVersion  int64          `json:"config_version"`
	RuleVersion    string         `json:"rule_version"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}
