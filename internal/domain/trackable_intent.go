package domain

type InstrumentResolutionStatus string

const (
	InstrumentResolutionStatusResolved    InstrumentResolutionStatus = "RESOLVED"
	InstrumentResolutionStatusAmbiguous   InstrumentResolutionStatus = "AMBIGUOUS"
	InstrumentResolutionStatusUntrackable InstrumentResolutionStatus = "UNTRACKABLE"
	InstrumentResolutionStatusNotFound    InstrumentResolutionStatus = "NOT_FOUND"
)

type InstrumentTargetKind string

const (
	InstrumentTargetKindStock       InstrumentTargetKind = "STOCK"
	InstrumentTargetKindETF         InstrumentTargetKind = "ETF"
	InstrumentTargetKindSector      InstrumentTargetKind = "SECTOR"
	InstrumentTargetKindTheme       InstrumentTargetKind = "THEME"
	InstrumentTargetKindIndustry    InstrumentTargetKind = "INDUSTRY"
	InstrumentTargetKindIndex       InstrumentTargetKind = "INDEX"
	InstrumentTargetKindBroadPhrase InstrumentTargetKind = "BROAD_PHRASE"
	InstrumentTargetKindUnknown     InstrumentTargetKind = "UNKNOWN"
)

type InstrumentResolutionCandidate struct {
	TSCode      string    `json:"ts_code"`
	Symbol      string    `json:"symbol"`
	Name        string    `json:"name"`
	AssetType   AssetType `json:"asset_type"`
	Market      Market    `json:"market"`
	MatchSource string    `json:"match_source"`
}

type InstrumentResolution struct {
	RawSymbol       string                          `json:"raw_symbol"`
	NormalizedQuery string                          `json:"normalized_query"`
	Status          InstrumentResolutionStatus      `json:"status"`
	TargetKind      InstrumentTargetKind            `json:"target_kind"`
	Reason          string                          `json:"reason,omitempty"`
	Candidates      []InstrumentResolutionCandidate `json:"candidates,omitempty"`
}

type TrackablePlanIntent struct {
	Analyst            string             `json:"analyst"`
	Institution        string             `json:"institution"`
	RawSymbol          string             `json:"raw_symbol"`
	TSCode             string             `json:"ts_code"`
	Symbol             string             `json:"symbol"`
	SecurityName       string             `json:"security_name"`
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
