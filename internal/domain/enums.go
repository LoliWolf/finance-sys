package domain

type DocumentStatus string

const (
	DocumentStatusIngested DocumentStatus = "INGESTED"
	DocumentStatusParsed   DocumentStatus = "PARSED"
	DocumentStatusPlanned  DocumentStatus = "PLANNED"
	DocumentStatusFailed   DocumentStatus = "FAILED"
)

type ParseRunStatus string

const (
	ParseRunStatusParsed ParseRunStatus = "PARSED"
	ParseRunStatusFailed ParseRunStatus = "FAILED"
)

type ParserName string

const (
	ParserNameTextNative ParserName = "text-native"
	ParserNamePDFCLI     ParserName = "pdf-cli"
	ParserNamePDFOCR     ParserName = "pdf-ocr"
	ParserNameDOCCLI     ParserName = "doc-cli"
	ParserNameDOCXNative ParserName = "docx-native"
)

type TradeDirection string

const (
	TradeDirectionLong  TradeDirection = "LONG"
	TradeDirectionShort TradeDirection = "SHORT"
)

type AssetType string

const (
	AssetTypeAShare AssetType = "A_SHARE"
	AssetTypeETF    AssetType = "ETF"
)

type Market string

const (
	MarketSH Market = "SH"
	MarketSZ Market = "SZ"
)

type CandidatePlanStatus string

const (
	CandidatePlanStatusReady       CandidatePlanStatus = "READY"
	CandidatePlanStatusNeedsReview CandidatePlanStatus = "NEEDS_REVIEW"
)

type RuleStrategy string

const (
	RuleStrategyTextReferencePrice RuleStrategy = "TEXT_REFERENCE_PRICE"
)

type ReferencePriceNote string

const (
	ReferencePriceNoteExplicitPriceMention ReferencePriceNote = "explicit_price_mention"
	ReferencePriceNotePriceMissingInText   ReferencePriceNote = "price_missing_in_text"
)
