package domain

import "time"

type EvidenceSpan struct {
	ChunkIndex int    `json:"chunk_index"`
	Text       string `json:"text"`
}

type ExpertSignal struct {
	ID            int64          `json:"id"`
	DocumentID    int64          `json:"document_id"`
	ParseRunID    int64          `json:"parse_run_id"`
	ExpertName    string         `json:"expert_name"`
	ExpertOrg     string         `json:"expert_org"`
	Symbol        string         `json:"symbol"`
	AssetType     string         `json:"asset_type"`
	Market        string         `json:"market"`
	Sentiment     string         `json:"sentiment"`
	Thesis        string         `json:"thesis"`
	Evidence      []EvidenceSpan `json:"evidence"`
	Risks         []string       `json:"risks"`
	Confidence    float64        `json:"confidence"`
	ConfigVersion int64          `json:"config_version"`
	RuleVersion   string         `json:"rule_version"`
	SignalDate    time.Time      `json:"signal_date"`
	CreatedAt     time.Time      `json:"created_at"`
}

type ExtractedSignal struct {
	ExpertName string         `json:"expert_name"`
	ExpertOrg  string         `json:"expert_org"`
	Symbol     string         `json:"symbol"`
	AssetType  string         `json:"asset_type"`
	Market     string         `json:"market"`
	Sentiment  string         `json:"sentiment"`
	Thesis     string         `json:"thesis"`
	Evidence   []EvidenceSpan `json:"evidence"`
	Risks      []string       `json:"risks"`
	Confidence float64        `json:"confidence"`
}
