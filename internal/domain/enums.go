package domain

// DocumentStatus 表示文档在上传分析链路中的处理状态。
type DocumentStatus string

const (
	// DocumentStatusIngested 表示文档已上传、完成去重并写入原始文件内容。
	DocumentStatusIngested DocumentStatus = "INGESTED"
	// DocumentStatusParsed 表示文档已成功解析出纯文本并完成切块。
	DocumentStatusParsed DocumentStatus = "PARSED"
	// DocumentStatusPlanned 表示文档已完成 LLM 抽取和规则生成，候选计划已落库。
	DocumentStatusPlanned DocumentStatus = "PLANNED"
	// DocumentStatusFailed 表示文档在解析、抽取、校验或规则生成阶段失败。
	DocumentStatusFailed DocumentStatus = "FAILED"
	// DocumentStatusInvalid 表示文档已完成解析/抽取，但没有可进入规则层的有效交易标的。
	DocumentStatusInvalid DocumentStatus = "INVALID"
)

// ParseRunStatus 表示一次文档解析运行的结果状态。
type ParseRunStatus string

const (
	// ParseRunStatusParsed 表示本次解析运行已成功生成纯文本内容。
	ParseRunStatusParsed ParseRunStatus = "PARSED"
	// ParseRunStatusFailed 表示本次解析运行失败，未产出可进入 LLM 的文本。
	ParseRunStatusFailed ParseRunStatus = "FAILED"
)

// ParserName 表示实际承担纯文本提取的解析器名称。
type ParserName string

const (
	// ParserNameTextNative 表示使用内置文本解析器处理 txt、md、csv 等纯文本文件。
	ParserNameTextNative ParserName = "text-native"
	// ParserNamePDFNative 表示通过内置纯 Go 解析器从 PDF 中提取文本。
	ParserNamePDFNative ParserName = "pdf-native"
	// ParserNamePDFKit 表示在 macOS 上通过系统 PDFKit 框架从 PDF 中提取文本。
	ParserNamePDFKit ParserName = "pdfkit-native"
	// ParserNamePDFCLI 表示通过外部命令行工具从 PDF 中提取文本。
	ParserNamePDFCLI ParserName = "pdf-cli"
	// ParserNamePDFOCR 表示通过 OCR 命令从扫描型 PDF 中提取文本。
	ParserNamePDFOCR ParserName = "pdf-ocr"
	// ParserNameDOCCLI 表示通过外部命令行工具从 doc 文件中提取文本。
	ParserNameDOCCLI ParserName = "doc-cli"
	// ParserNameDOCXNative 表示使用内置解析逻辑从 docx 文件中提取文本。
	ParserNameDOCXNative ParserName = "docx-native"
)

// TradeDirection 表示结构化交易意图中的交易方向。
type TradeDirection string

const (
	// TradeDirectionLong 表示看多方向，用于生成做多候选计划。
	TradeDirectionLong TradeDirection = "LONG"
	// TradeDirectionShort 表示看空方向，用于生成做空候选计划。
	TradeDirectionShort TradeDirection = "SHORT"
)

// AssetType 表示候选计划标的的资产类型。
type AssetType string

const (
	// AssetTypeAShare 表示 A 股股票标的。
	AssetTypeAShare AssetType = "A_SHARE"
	// AssetTypeETF 表示交易型开放式指数基金标的。
	AssetTypeETF AssetType = "ETF"
	// AssetTypeSector 表示东方财富 BK 编码板块指数，仅用于持续跟踪，不进入交易规则层。
	AssetTypeSector AssetType = "SECTOR"
)

// Market 表示标的所在的交易市场。
type Market string

const (
	// MarketSH 表示上海证券交易所市场。
	MarketSH Market = "SH"
	// MarketSZ 表示深圳证券交易所市场。
	MarketSZ Market = "SZ"
	// MarketBJ 表示北京证券交易所市场。
	MarketBJ Market = "BJ"
	// MarketDC 表示东方财富板块指数市场。
	MarketDC Market = "DC"
)

// CandidatePlanStatus 表示规则引擎生成的候选计划状态。
type CandidatePlanStatus string

const (
	// CandidatePlanStatusReady 表示候选计划已由确定性规则完整生成，可供查询使用。
	CandidatePlanStatusReady CandidatePlanStatus = "READY"
	// CandidatePlanStatusNeedsReview 表示候选计划缺少明确价格或置信度不足，需要人工复核。
	CandidatePlanStatusNeedsReview CandidatePlanStatus = "NEEDS_REVIEW"
)

// RecommendationEventStatus 表示推荐事实是否可进入后续评估。
type RecommendationEventStatus string

const (
	// RecommendationEventStatusActive 表示推荐事件可进入后续行情评估。
	RecommendationEventStatusActive RecommendationEventStatus = "ACTIVE"
	// RecommendationEventStatusNeedsReview 表示推荐事件需要人工复核后再评估。
	RecommendationEventStatusNeedsReview RecommendationEventStatus = "NEEDS_REVIEW"
	// RecommendationEventStatusSuperseded 表示推荐事件已被后续分析结果替代。
	RecommendationEventStatusSuperseded RecommendationEventStatus = "SUPERSEDED"
)

// RuleStrategy 表示生成交易参数时使用的确定性规则策略。
type RuleStrategy string

const (
	// RuleStrategyTextReferencePrice 表示以文本中抽取的参考价为基准生成入场、止损、止盈和仓位。
	RuleStrategyTextReferencePrice RuleStrategy = "TEXT_REFERENCE_PRICE"
)

// ReferencePriceNote 表示 LLM 对参考价来源的结构化说明。
type ReferencePriceNote string

const (
	// ReferencePriceNoteExplicitPriceMention 表示原文中明确出现了可作为参考价的价格。
	ReferencePriceNoteExplicitPriceMention ReferencePriceNote = "explicit_price_mention"
	// ReferencePriceNotePriceMissingInText 表示原文中没有明确可用的参考价。
	ReferencePriceNotePriceMissingInText ReferencePriceNote = "price_missing_in_text"
)
