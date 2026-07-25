package config

type Config struct {
	Meta        MetaConfig        `json:"meta"`
	Service     ServiceConfig     `json:"service"`
	NacosClient NacosClientConfig `json:"nacos_client"`
	Security    SecurityConfig    `json:"security"`
	Logging     LoggingConfig     `json:"logging"`
	Database    DatabaseConfig    `json:"database"`
	Document    DocumentConfig    `json:"document"`
	LLM         LLMConfig         `json:"llm"`
	Agent       AgentConfig       `json:"agent"`
	MarketData  MarketDataConfig  `json:"market_data"`
	Evaluation  EvaluationConfig  `json:"evaluation"`
	Rules       RulesConfig       `json:"rules"`
}

type MetaConfig struct {
	AppName       string `json:"app_name"`
	Env           string `json:"env"`
	Timezone      string `json:"timezone"`
	ConfigVersion int64  `json:"config_version"`
}

type ServiceConfig struct {
	HTTP HTTPServerConfig `json:"http"`
}

type HTTPServerConfig struct {
	Host                   string     `json:"host"`
	Port                   int        `json:"port"`
	APIPrefix              string     `json:"api_prefix"`
	ReadTimeoutMS          int        `json:"read_timeout_ms"`
	WriteTimeoutMS         int        `json:"write_timeout_ms"`
	IdleTimeoutMS          int        `json:"idle_timeout_ms"`
	ShutdownTimeoutSeconds int        `json:"shutdown_timeout_seconds"`
	MaxHeaderBytes         int        `json:"max_header_bytes"`
	CORS                   CORSConfig `json:"cors"`
}

type CORSConfig struct {
	Enabled      bool     `json:"enabled"`
	AllowOrigins []string `json:"allow_origins"`
}

type NacosClientConfig struct {
	PollIntervalSeconds     int    `json:"poll_interval_seconds"`
	CacheLastGoodConfig     bool   `json:"cache_last_good_config"`
	CacheDir                string `json:"cache_dir"`
	WriteConfigSnapshotToDB bool   `json:"write_config_snapshot_to_db"`
}

type SecurityConfig struct {
	Auth AuthConfig `json:"auth"`
}

type AuthConfig struct {
	Enabled      bool     `json:"enabled"`
	StaticTokens []string `json:"static_tokens"`
	HeaderName   string   `json:"header_name"`
	TokenPrefix  string   `json:"token_prefix"`
}

type LoggingConfig struct {
	Level LogLevel `json:"level"`
}

type DatabaseConfig struct {
	Driver                 DatabaseDriver `json:"driver"`
	DSN                    string         `json:"dsn"`
	MaxOpenConns           int            `json:"max_open_conns"`
	MaxIdleConns           int            `json:"max_idle_conns"`
	ConnMaxLifetimeMinutes int            `json:"conn_max_lifetime_minutes"`
	ConnMaxIdleTimeMinutes int            `json:"conn_max_idle_time_minutes"`
}

type DocumentConfig struct {
	APIUploadEnabled  bool                 `json:"api_upload_enabled"`
	AutoAnalyzeUpload bool                 `json:"auto_analyze_upload"`
	AllowedExtensions []string             `json:"allowed_extensions"`
	MaxFileSizeMB     int                  `json:"max_file_size_mb"`
	SHA256Dedup       bool                 `json:"sha256_dedup"`
	SourceDefaults    SourceDefaultsConfig `json:"source_defaults"`
	Chunking          ChunkingConfig       `json:"chunking"`
	PDFOCR            PDFOCRConfig         `json:"pdf_ocr"`
	PDFUseOCR         bool                 `json:"-"`
}

type SourceDefaultsConfig struct {
	Author      string `json:"author"`
	Institution string `json:"institution"`
}

type ChunkingConfig struct {
	Enabled      bool `json:"enabled"`
	TargetChars  int  `json:"target_chars"`
	OverlapChars int  `json:"overlap_chars"`
}

type PDFOCRConfig struct {
	Command              string   `json:"command"`
	Args                 []string `json:"args"`
	MinTextChars         int      `json:"min_text_chars"`
	TimeoutMS            int      `json:"timeout_ms"`
	TreatExitCodeOneAsOK bool     `json:"treat_exit_code_one_as_ok"`
}

type LLMConfig struct {
	Enabled      bool              `json:"enabled"`
	Provider     LLMProvider       `json:"provider"`
	Endpoint     string            `json:"endpoint"`
	APIKey       string            `json:"api_key"`
	Model        string            `json:"model"`
	TimeoutMS    int               `json:"timeout_ms"`
	MaxRetries   int               `json:"max_retries"`
	ExtraHeaders map[string]string `json:"extra_headers"`
}

type AgentConfig struct {
	Enabled                bool              `json:"enabled"`
	Mode                   AgentMode         `json:"mode"`
	Endpoint               string            `json:"endpoint"`
	HealthEndpoint         string            `json:"health_endpoint"`
	InternalAPIBaseURL     string            `json:"internal_api_base_url"`
	Tushare                TushareConfig     `json:"tushare"`
	Observation            ObservationConfig `json:"observation"`
	TimeoutMS              int               `json:"timeout_ms"`
	MaxRetries             int               `json:"max_retries"`
	SchemaVersion          string            `json:"schema_version"`
	Auth                   AgentAuthConfig   `json:"auth"`
	AllowLegacyLLMFallback bool              `json:"allow_legacy_llm_fallback"`
}

type TushareConfig struct {
	Enabled   bool   `json:"enabled"`
	Token     string `json:"token"`
	Endpoint  string `json:"endpoint"`
	TimeoutMS int    `json:"timeout_ms"`
}

type ObservationConfig struct {
	Enabled           bool    `json:"enabled"`
	PersistSuccess    bool    `json:"persist_success"`
	PersistFailure    bool    `json:"persist_failure"`
	PersistToolTraces bool    `json:"persist_tool_traces"`
	ShadowSampleRate  float64 `json:"shadow_sample_rate"`
	MaxTargetsPerRun  int     `json:"max_targets_per_run"`
	MaxJSONBytes      int     `json:"max_json_bytes"`
	RetentionDays     int     `json:"retention_days"`
}

type AgentAuthConfig struct {
	Enabled     bool   `json:"enabled"`
	HeaderName  string `json:"header_name"`
	StaticToken string `json:"static_token"`
}

type MarketDataConfig struct {
	Enabled     bool                    `json:"enabled"`
	Provider    string                  `json:"provider"`
	Tushare     MarketDataTushareConfig `json:"tushare"`
	AsyncWorker MarketDataWorkerConfig  `json:"async_worker"`
	StockDaily  StockDailySyncConfig    `json:"stock_daily"`
}

type MarketDataTushareConfig struct {
	Enabled         bool                 `json:"enabled"`
	SDKPackage      string               `json:"sdk_package"`
	Tokens          []TushareTokenConfig `json:"tokens"`
	TimeoutMS       int                  `json:"timeout_ms"`
	MaxRetries      int                  `json:"max_retries"`
	TokenCooldownMS int                  `json:"token_cooldown_ms"`
}

type TushareTokenConfig struct {
	Alias   string `json:"alias"`
	Token   string `json:"token"`
	Enabled bool   `json:"enabled"`
	Weight  int    `json:"weight"`
}

type MarketDataWorkerConfig struct {
	Enabled           bool `json:"enabled"`
	PollIntervalMS    int  `json:"poll_interval_ms"`
	ClaimTimeoutMS    int  `json:"claim_timeout_ms"`
	MaxConcurrentRuns int  `json:"max_concurrent_runs"`
	BatchSize         int  `json:"batch_size"`
}

type StockDailySyncConfig struct {
	Enabled                bool     `json:"enabled"`
	SyncAssetTypes         []string `json:"sync_asset_types"`
	Fields                 []string `json:"fields"`
	PreserveRawUnits       bool     `json:"preserve_raw_units"`
	StoreRawContent        bool     `json:"store_raw_content"`
	MissingItemMarkEnabled bool     `json:"missing_item_mark_enabled"`
}

type EvaluationConfig struct {
	Enabled                   bool                            `json:"enabled"`
	RecommendationPerformance RecommendationPerformanceConfig `json:"recommendation_performance"`
}

type RecommendationPerformanceConfig struct {
	Enabled               bool                    `json:"enabled"`
	Windows               []int                   `json:"windows"`
	QuoteSource           string                  `json:"quote_source"`
	EntryPriceRule        string                  `json:"entry_price_rule"`
	BasePriceRule         string                  `json:"base_price_rule"`
	WinThresholdRatio     float64                 `json:"win_threshold_ratio"`
	MinQuoteCoverageRatio float64                 `json:"min_quote_coverage_ratio"`
	CalcVersion           string                  `json:"calc_version"`
	AsyncWorker           EvaluationWorkerConfig  `json:"async_worker"`
	Ranking               EvaluationRankingConfig `json:"ranking"`
}

type EvaluationWorkerConfig struct {
	Enabled           bool `json:"enabled"`
	PollIntervalMS    int  `json:"poll_interval_ms"`
	ClaimTimeoutMS    int  `json:"claim_timeout_ms"`
	MaxConcurrentRuns int  `json:"max_concurrent_runs"`
	BatchSize         int  `json:"batch_size"`
}

type EvaluationRankingConfig struct {
	DefaultWindowDays     int    `json:"default_window_days"`
	DefaultMinSampleCount int    `json:"default_min_sample_count"`
	DefaultSort           string `json:"default_sort"`
}

type RulesConfig struct {
	Version              string       `json:"version"`
	Strategy             RuleStrategy `json:"strategy"`
	TradeDateOffsetDays  int          `json:"trade_date_offset_days"`
	MaxPositionPct       float64      `json:"max_position_pct"`
	DefaultStopLossPct   float64      `json:"default_stop_loss_pct"`
	DefaultTakeProfitPct float64      `json:"default_take_profit_pct"`
	MinConfidence        float64      `json:"min_confidence"`
}
