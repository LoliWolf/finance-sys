package domain

type EvidenceSpan struct {
	ChunkIndex int    `json:"chunk_index"`
	Text       string `json:"text"`
}

type PlanIntent struct {
	Analyst            string             `json:"analyst"`
	Institution        string             `json:"institution"`
	Symbol             string             `json:"symbol"`
	AssetType          AssetType          `json:"asset_type"`
	Market             Market             `json:"market"`
	Direction          TradeDirection     `json:"direction"`
	ReferencePrice     float64            `json:"reference_price"`
	ReferencePriceNote ReferencePriceNote `json:"reference_price_note"`
	Thesis             string             `json:"thesis"`
	Evidence           []EvidenceSpan     `json:"evidence"`
	Risks              []string           `json:"risks"`
	Confidence         float64            `json:"confidence"`
}
