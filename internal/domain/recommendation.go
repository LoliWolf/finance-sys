package domain

import "time"

type Blogger struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	NormalizedName string    `json:"normalized_name"`
	Institution    string    `json:"institution"`
	SourceType     string    `json:"source_type"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type RecommendationEvent struct {
	ID               int64                     `json:"id"`
	BloggerID        int64                     `json:"blogger_id"`
	BloggerName      string                    `json:"blogger_name"`
	SourceDocumentID int64                     `json:"source_document_id"`
	PlanID           int64                     `json:"plan_id"`
	ParseRunID       int64                     `json:"parse_run_id"`
	Symbol           string                    `json:"symbol"`
	AssetType        AssetType                 `json:"asset_type"`
	Market           Market                    `json:"market"`
	Direction        TradeDirection            `json:"direction"`
	RecommendDate    time.Time                 `json:"recommend_date"`
	ReferencePrice   float64                   `json:"reference_price"`
	Confidence       float64                   `json:"confidence"`
	Status           RecommendationEventStatus `json:"status"`
	Thesis           string                    `json:"thesis"`
	Evidence         []EvidenceSpan            `json:"evidence,omitempty"`
	DedupeKey        string                    `json:"dedupe_key"`
	ConfigVersion    int64                     `json:"config_version"`
	RuleVersion      string                    `json:"rule_version"`
	CreatedAt        time.Time                 `json:"created_at"`
	UpdatedAt        time.Time                 `json:"updated_at"`
}

type RecommendationEventQuery struct {
	Limit     int
	Symbol    string
	Direction TradeDirection
	Status    RecommendationEventStatus
}
