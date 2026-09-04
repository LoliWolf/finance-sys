package config

type Config struct {
	Meta                    MetaConfig              `json:"meta"`
	Service                 ServiceConfig           `json:"service"`
	NacosClient             NacosClientConfig       `json:"nacos_client"`
	Security                SecurityConfig          `json:"security"`
	Logging                 LoggingConfig           `json:"logging"`
	DatabaseProduction      DatabaseConfig          `json:"database"`
	DatabaseTest            DatabaseConfig          `json:"database_test"`
	Database                DatabaseConfig          `json:"-"`
	SelectedDatabaseProfile DatabaseProfile         `json:"-"`
	Processing              ProcessingConfig        `json:"processing"`
	Document                DocumentConfig          `json:"document"`
	ExternalDocuments       ExternalDocumentsConfig `json:"external_documents"`
	LLM                     LLMConfig               `json:"llm"`
	Agent                   AgentConfig             `json:"agent"`
	MarketData              MarketDataConfig        `json:"market_data"`
	Evaluation              EvaluationConfig        `json:"evaluation"`
	Scheduler               SchedulerConfig         `json:"scheduler"`
	Trading                 TradingConfig           `json:"trading"`
	Rules                   RulesConfig             `json:"rules"`
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
	PortProduction         int        `json:"port"`
	PortTest               int        `json:"port_test"`
	Port                   int        `json:"-"`
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

// ProcessingConfig controls process-wide resource pools shared by every
// document entry point, including HTTP uploads, historical replay and external
// source ingestion.
type ProcessingConfig struct {
	OCRMaxConcurrency int `json:"ocr_max_concurrency"`
	LLMMaxConcurrency int `json:"llm_max_concurrency"`
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

type ExternalDocumentsConfig struct {
	OpenList OpenListDocumentSourceConfig `json:"openlist"`
}

type OpenListDocumentSourceConfig struct {
	Enabled          bool   `json:"enabled"`
	BaseURL          string `json:"base_url"`
	Username         string `json:"username"`
	Password         string `json:"password"`
	RootPath         string `json:"root_path"`
	Institution      string `json:"institution"`
	RequestTimeoutMS int    `json:"request_timeout_ms"`
	ScanLookbackDays int    `json:"scan_lookback_days"`
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
	Enabled        bool                        `json:"enabled"`
	Provider       string                      `json:"provider"`
	Tushare        MarketDataTushareConfig     `json:"tushare"`
	AsyncWorker    MarketDataWorkerConfig      `json:"async_worker"`
	SecurityMaster SecurityMasterRefreshConfig `json:"security_master"`
	StockDaily     StockDailySyncConfig        `json:"stock_daily"`
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
	SectorFields           []string `json:"sector_fields"`
	PreserveRawUnits       bool     `json:"preserve_raw_units"`
	StoreRawContent        bool     `json:"store_raw_content"`
	MissingItemMarkEnabled bool     `json:"missing_item_mark_enabled"`
}

type SecurityMasterRefreshConfig struct {
	Enabled      bool     `json:"enabled"`
	StockFields  []string `json:"stock_fields"`
	ETFFields    []string `json:"etf_fields"`
	SectorFields []string `json:"sector_fields"`
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

type SchedulerConfig struct {
	Enabled                        bool                                   `json:"enabled"`
	PollIntervalMS                 int                                    `json:"poll_interval_ms"`
	ClaimTimeoutMS                 int                                    `json:"claim_timeout_ms"`
	SecurityMasterRefresh          DailyTaskScheduleConfig                `json:"security_master_refresh"`
	StockDailyPreviousDay          DailyTaskScheduleConfig                `json:"stock_daily_previous_day"`
	RecommendationEvaluationRecent RecommendationEvaluationScheduleConfig `json:"recommendation_evaluation_recent"`
	OpenListDocumentIngestion      HourlyTaskScheduleConfig               `json:"openlist_document_ingestion"`
}

type DailyTaskScheduleConfig struct {
	Enabled bool `json:"enabled"`
	Hour    int  `json:"hour"`
	Minute  int  `json:"minute"`
}

type RecommendationEvaluationScheduleConfig struct {
	DailyTaskScheduleConfig
	LookbackDays int `json:"lookback_days"`
}

type HourlyTaskScheduleConfig struct {
	Enabled bool `json:"enabled"`
	Minute  int  `json:"minute"`
}

type TradingConfig struct {
	Enabled        bool                        `json:"enabled"`
	Environment    string                      `json:"environment"`
	Provider       string                      `json:"provider"`
	AllowLive      bool                        `json:"allow_live"`
	KillSwitch     bool                        `json:"kill_switch"`
	Agent          TradingAgentConfig          `json:"agent"`
	Decision       TradingDecisionConfig       `json:"decision"`
	Risk           TradingRiskConfig           `json:"risk"`
	Execution      TradingExecutionConfig      `json:"execution"`
	Exit           TradingExitConfig           `json:"exit"`
	Bridge         TradingBridgeConfig         `json:"bridge"`
	Eastmoney      TradingEastmoneyConfig      `json:"eastmoney"`
	Reconciliation TradingReconciliationConfig `json:"reconciliation"`
	Scheduler      TradingSchedulerConfig      `json:"scheduler"`
}

type TradingAgentConfig struct {
	Enabled        bool   `json:"enabled"`
	Endpoint       string `json:"endpoint"`
	HealthEndpoint string `json:"health_endpoint"`
	TimeoutMS      int    `json:"timeout_ms"`
	MaxRetries     int    `json:"max_retries"`
	SchemaVersion  string `json:"schema_version"`
	InternalToken  string `json:"internal_token"`
}

type TradingDecisionConfig struct {
	Provider                    string  `json:"provider"`
	StrategyName                string  `json:"strategy_name"`
	StrategyVersion             string  `json:"strategy_version"`
	ToolContractVersion         string  `json:"tool_contract_version"`
	MinRecommendationConfidence float64 `json:"min_recommendation_confidence"`
	MinBloggerSampleCount       int     `json:"min_blogger_sample_count"`
	MinBloggerWinRate           float64 `json:"min_blogger_win_rate"`
	MaxCandidatesPerRun         int     `json:"max_candidates_per_run"`
	MaxIntentsPerRun            int     `json:"max_intents_per_run"`
}

type TradingRiskConfig struct {
	Version                 string   `json:"version"`
	AllowedAssetTypes       []string `json:"allowed_asset_types"`
	AllowedMarkets          []string `json:"allowed_markets"`
	AllowedBoards           []string `json:"allowed_boards"`
	AllowedSides            []string `json:"allowed_sides"`
	TradingRuleVersion      string   `json:"trading_rule_version"`
	ExcludeNoPriceLimit     bool     `json:"exclude_no_price_limit_period"`
	MaxTotalPositionRatio   float64  `json:"max_total_position_ratio"`
	MaxSymbolPositionRatio  float64  `json:"max_symbol_position_ratio"`
	MaxSingleOrderRatio     float64  `json:"max_single_order_ratio"`
	MaxPositionCount        int      `json:"max_position_count"`
	MaxNewOrdersPerDay      int      `json:"max_new_orders_per_day"`
	MaxDailyTurnoverRatio   float64  `json:"max_daily_turnover_ratio"`
	DailyLossKillRatio      float64  `json:"daily_loss_kill_ratio"`
	MaxPriceDeviationRatio  float64  `json:"max_price_deviation_ratio"`
	MinCashReserveRatio     float64  `json:"min_cash_reserve_ratio"`
	IntentCooldownTradeDays int      `json:"intent_cooldown_trade_days"`
	MaxSnapshotAgeSeconds   int      `json:"max_snapshot_age_seconds"`
}

type TradingExecutionConfig struct {
	DefaultOrderType             string `json:"default_order_type"`
	AllowMarketOrder             bool   `json:"allow_market_order"`
	CommandPollIntervalMS        int    `json:"command_poll_interval_ms"`
	CommandClaimTimeoutMS        int    `json:"command_claim_timeout_ms"`
	DispatchTimeoutMS            int    `json:"dispatch_timeout_ms"`
	DispatchMaxRetries           int    `json:"dispatch_max_retries"`
	CancelOpenOrdersAtSessionEnd bool   `json:"cancel_open_orders_at_session_end"`
}

type TradingExitConfig struct {
	Enabled                bool    `json:"enabled"`
	StopLossRatio          float64 `json:"stop_loss_ratio"`
	TakeProfitRatio        float64 `json:"take_profit_ratio"`
	MaxHoldingTradeDays    int     `json:"max_holding_trade_days"`
	MonitorIntervalSeconds int     `json:"monitor_interval_seconds"`
	SellLimitDiscountRatio float64 `json:"sell_limit_discount_ratio"`
}

type TradingBridgeConfig struct {
	BaseURL           string                  `json:"base_url"`
	CallbackURL       string                  `json:"callback_url"`
	HealthPath        string                  `json:"health_path"`
	RequestTimeoutMS  int                     `json:"request_timeout_ms"`
	ExpectedAccountID string                  `json:"expected_account_id"`
	StrategyID        string                  `json:"strategy_id"`
	SimulationOnly    bool                    `json:"simulation_only"`
	HMAC              TradingBridgeHMACConfig `json:"hmac"`
	TLS               TradingBridgeTLSConfig  `json:"tls"`
}

type TradingBridgeHMACConfig struct {
	KeyID               string `json:"key_id"`
	Secret              string `json:"secret"`
	MaxClockSkewSeconds int    `json:"max_clock_skew_seconds"`
	NonceTTLSeconds     int    `json:"nonce_ttl_seconds"`
}

type TradingBridgeTLSConfig struct {
	Verify   bool   `json:"verify"`
	CAFile   string `json:"ca_file"`
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
}

type TradingEastmoneyConfig struct {
	Token                string                      `json:"token"`
	StrategyID           string                      `json:"strategy_id"`
	ExpectedAccountID    string                      `json:"expected_account_id"`
	Mode                 string                      `json:"mode"`
	MaxSubscribedSymbols int                         `json:"max_subscribed_symbols"`
	SQLitePath           string                      `json:"sqlite_path"`
	TokenHealth          TradingTokenHealthConfig    `json:"token_health"`
	HistoricalData       TradingHistoricalDataConfig `json:"historical_data"`
	AccountPolicy        TradingAccountPolicyConfig  `json:"account_policy"`
}

type TradingTokenHealthConfig struct {
	ProbeIntervalSeconds        int   `json:"probe_interval_seconds"`
	TransientFailureThreshold   int   `json:"transient_failure_threshold"`
	InvalidTokenErrorCodes      []int `json:"invalid_token_error_codes"`
	AuthServiceErrorCodes       []int `json:"auth_service_error_codes"`
	KillOnInvalidToken          bool  `json:"kill_on_invalid_token"`
	RequireRestartAfterRotation bool  `json:"require_restart_after_rotation"`
}

type TradingHistoricalDataConfig struct {
	BulkBackfillEnabled        bool `json:"bulk_backfill_enabled"`
	MaxRecordsPerRequest       int  `json:"max_records_per_request"`
	MaxWaitTimeSeconds         int  `json:"max_wait_time_seconds"`
	LocalCacheEnabled          bool `json:"local_cache_enabled"`
	ProviderDailyQuotaVerified bool `json:"provider_daily_quota_verified"`
}

type TradingAccountPolicyConfig struct {
	MaxActiveAccounts                 int      `json:"max_active_accounts"`
	AllowedAccountIDs                 []string `json:"allowed_account_ids"`
	VerifiedBoards                    []string `json:"verified_boards"`
	ProviderAccountLimitVerified      bool     `json:"provider_account_limit_verified"`
	MaxSimulationStrategiesPerAccount int      `json:"max_simulation_strategies_per_account"`
}

type TradingReconciliationConfig struct {
	Enabled                   bool `json:"enabled"`
	IntervalSeconds           int  `json:"interval_seconds"`
	RefreshTimeoutSeconds     int  `json:"refresh_timeout_seconds"`
	AutoRepairMissingEvents   bool `json:"auto_repair_missing_events"`
	AutoRepairMissingFills    bool `json:"auto_repair_missing_fills"`
	KillOnMoneyOrPositionDiff bool `json:"kill_on_money_or_position_diff"`
}

type TradingSchedulerConfig struct {
	Enabled           bool                    `json:"enabled"`
	Preflight         DailyTaskScheduleConfig `json:"preflight"`
	PreOpenDecision   DailyTaskScheduleConfig `json:"pre_open_decision"`
	MorningWindow     TradingSessionWindow    `json:"morning_window"`
	AfternoonWindow   TradingSessionWindow    `json:"afternoon_window"`
	MorningCancel     DailyTaskScheduleConfig `json:"morning_cancel"`
	AfternoonCancel   DailyTaskScheduleConfig `json:"afternoon_cancel"`
	EndOfDayReconcile DailyTaskScheduleConfig `json:"end_of_day_reconcile"`
}

type TradingSessionWindow struct {
	Start string `json:"start"`
	End   string `json:"end"`
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
