package config

type Config struct {
	Meta              MetaConfig              `json:"meta"`
	Service           ServiceConfig           `json:"service"`
	Runtime           RuntimeConfig           `json:"runtime"`
	NacosClient       NacosClientConfig       `json:"nacos_client"`
	Security          SecurityConfig          `json:"security"`
	Logging           LoggingConfig           `json:"logging"`
	Metrics           MetricsConfig           `json:"metrics"`
	Database          DatabaseConfig          `json:"database"`
	Redis             RedisConfig             `json:"redis"`
	ObjectStorage     ObjectStorageConfig     `json:"object_storage"`
	DistributedLock   DistributedLockConfig   `json:"distributed_lock"`
	HTTPClients       HTTPClientsConfig       `json:"http_clients"`
	DocumentIngestion DocumentIngestionConfig `json:"document_ingestion"`
	DocumentParsing   DocumentParsingConfig   `json:"document_parsing"`
	LLM               LLMConfig               `json:"llm"`
	Market            MarketConfig            `json:"market"`
	Rules             RulesConfig             `json:"rules"`
	Approval          ApprovalConfig          `json:"approval"`
	Evaluation        EvaluationConfig        `json:"evaluation"`
	Reporting         ReportingConfig         `json:"reporting"`
}

type MetaConfig struct {
	AppName       string `json:"app_name"`
	AppCode       string `json:"app_code"`
	Env           string `json:"env"`
	Version       string `json:"version"`
	Timezone      string `json:"timezone"`
	ConfigVersion int64  `json:"config_version"`
	Owner         string `json:"owner"`
	Description   string `json:"description"`
}

type ServiceConfig struct {
	Name     string           `json:"name"`
	Env      string           `json:"env"`
	Timezone string           `json:"timezone"`
	HTTP     HTTPServerConfig `json:"http"`
	Worker   WorkerConfig     `json:"worker"`
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
	AllowMethods []string `json:"allow_methods"`
	AllowHeaders []string `json:"allow_headers"`
}

type WorkerConfig struct {
	Concurrency             int    `json:"concurrency"`
	GracefulShutdownSeconds int    `json:"graceful_shutdown_seconds"`
	JobPollIntervalSeconds  int    `json:"job_poll_interval_seconds"`
	TemporaryDir            string `json:"temporary_dir"`
}

type RuntimeConfig struct {
	TradeCalendar                  string   `json:"trade_calendar"`
	DefaultAssetUniverse           []string `json:"default_asset_universe"`
	MaxParallelJobs                int      `json:"max_parallel_jobs"`
	IdempotencyEnabled             bool     `json:"idempotency_enabled"`
	RawResponseArchiveEnabled      bool     `json:"raw_response_archive_enabled"`
	DefaultBenchmarkSymbol         string   `json:"default_benchmark_symbol"`
	AllowAutoApproval              bool     `json:"allow_auto_approval"`
	SkillsRuntimeDependencyAllowed bool     `json:"skills_runtime_dependency_allowed"`
}

type NacosClientConfig struct {
	RefreshMode             string `json:"refresh_mode"`
	PollIntervalSeconds     int    `json:"poll_interval_seconds"`
	FailFast                bool   `json:"fail_fast"`
	CacheLastGoodConfig     bool   `json:"cache_last_good_config"`
	CacheDir                string `json:"cache_dir"`
	ValidateSchemaOnRefresh bool   `json:"validate_schema_on_refresh"`
	WriteConfigSnapshotToDB bool   `json:"write_config_snapshot_to_db"`
}

type SecurityConfig struct {
	Auth             AuthConfig             `json:"auth"`
	WebhookSignature WebhookSignatureConfig `json:"webhook_signature"`
}

type AuthConfig struct {
	Enabled      bool     `json:"enabled"`
	Mode         string   `json:"mode"`
	StaticTokens []string `json:"static_tokens"`
	HeaderName   string   `json:"header_name"`
	TokenPrefix  string   `json:"token_prefix"`
}

type WebhookSignatureConfig struct {
	Enabled      bool   `json:"enabled"`
	SharedSecret string `json:"shared_secret"`
}

type LoggingConfig struct {
	Level             string   `json:"level"`
	Format            string   `json:"format"`
	IncludeCaller     bool     `json:"include_caller"`
	IncludeStacktrace bool     `json:"include_stacktrace"`
	Fields            []string `json:"fields"`
}

type MetricsConfig struct {
	Enabled   bool   `json:"enabled"`
	Namespace string `json:"namespace"`
	Path      string `json:"path"`
}

type DatabaseConfig struct {
	Driver                 string `json:"driver"`
	DSN                    string `json:"dsn"`
	MaxOpenConns           int    `json:"max_open_conns"`
	MaxIdleConns           int    `json:"max_idle_conns"`
	ConnMaxLifetimeMinutes int    `json:"conn_max_lifetime_minutes"`
	ConnMaxIdleTimeMinutes int    `json:"conn_max_idle_time_minutes"`
	StatementTimeoutMS     int    `json:"statement_timeout_ms"`
	MigrationsDir          string `json:"migrations_dir"`
}

type RedisConfig struct {
	Addr           string `json:"addr"`
	Password       string `json:"password"`
	DB             int    `json:"db"`
	KeyPrefix      string `json:"key_prefix"`
	DialTimeoutMS  int    `json:"dial_timeout_ms"`
	ReadTimeoutMS  int    `json:"read_timeout_ms"`
	WriteTimeoutMS int    `json:"write_timeout_ms"`
}

type ObjectStorageConfig struct {
	Backend           string `json:"backend"`
	Endpoint          string `json:"endpoint"`
	AccessKey         string `json:"access_key"`
	SecretKey         string `json:"secret_key"`
	Secure            bool   `json:"secure"`
	BucketDocuments   string `json:"bucket_documents"`
	BucketRawMarket   string `json:"bucket_raw_market"`
	BucketReports     string `json:"bucket_reports"`
	BucketDeadLetters string `json:"bucket_dead_letters"`
}

type DistributedLockConfig struct {
	Backend              string `json:"backend"`
	TTLSeconds           int    `json:"ttl_seconds"`
	RenewIntervalSeconds int    `json:"renew_interval_seconds"`
}

type HTTPClientsConfig struct {
	DefaultTimeoutMS    int         `json:"default_timeout_ms"`
	MaxIdleConns        int         `json:"max_idle_conns"`
	MaxIdleConnsPerHost int         `json:"max_idle_conns_per_host"`
	IdleConnTimeoutMS   int         `json:"idle_conn_timeout_ms"`
	Retry               RetryConfig `json:"retry"`
}

type RetryConfig struct {
	MaxAttempts   int `json:"max_attempts"`
	BaseBackoffMS int `json:"base_backoff_ms"`
	MaxBackoffMS  int `json:"max_backoff_ms"`
}

type DocumentIngestionConfig struct {
	WatchLocalDirs     []string             `json:"watch_local_dirs"`
	WatchMinioPrefixes []string             `json:"watch_minio_prefixes"`
	APIUploadEnabled   bool                 `json:"api_upload_enabled"`
	AllowedExtensions  []string             `json:"allowed_extensions"`
	MaxFileSizeMB      int                  `json:"max_file_size_mb"`
	SHA256Dedup        bool                 `json:"sha256_dedup"`
	SourceDefaults     SourceDefaultsConfig `json:"source_defaults"`
}

type SourceDefaultsConfig struct {
	SourceType  string `json:"source_type"`
	SourceName  string `json:"source_name"`
	Author      string `json:"author"`
	Institution string `json:"institution"`
}

type DocumentParsingConfig struct {
	PDF             PDFParsingConfig      `json:"pdf"`
	OCR             OCRConfig             `json:"ocr"`
	TableExtraction TableExtractionConfig `json:"table_extraction"`
	DOCX            SimpleToggleConfig    `json:"docx"`
	HTML            HTMLParsingConfig     `json:"html"`
	Email           EmailParsingConfig    `json:"email"`
	Cleaning        CleaningConfig        `json:"cleaning"`
	Chunking        ChunkingConfig        `json:"chunking"`
	DeadLetter      DeadLetterConfig      `json:"dead_letter"`
}

type PDFParsingConfig struct {
	TextExtractors                   []string `json:"text_extractors"`
	PreferTextExtractionFirst        bool     `json:"prefer_text_extraction_first"`
	OCRFallbackWhenTextDensityBelow  float64  `json:"ocr_fallback_when_text_density_below"`
	OCRFallbackWhenGarbledRatioAbove float64  `json:"ocr_fallback_when_garbled_ratio_above"`
	KeepPageText                     bool     `json:"keep_page_text"`
	ExtractTables                    bool     `json:"extract_tables"`
}

type OCRConfig struct {
	Enabled             bool     `json:"enabled"`
	Mode                string   `json:"mode"`
	Provider            string   `json:"provider"`
	ServiceURL          string   `json:"service_url"`
	TimeoutMS           int      `json:"timeout_ms"`
	MaxPagesPerDocument int      `json:"max_pages_per_document"`
	DPI                 int      `json:"dpi"`
	Languages           []string `json:"languages"`
}

type TableExtractionConfig struct {
	Enabled             bool   `json:"enabled"`
	Mode                string `json:"mode"`
	AllowExternalBridge bool   `json:"allow_external_bridge"`
	StoreRawBlocks      bool   `json:"store_raw_blocks"`
}

type SimpleToggleConfig struct {
	Enabled bool `json:"enabled"`
}

type HTMLParsingConfig struct {
	Enabled       bool `json:"enabled"`
	RemoveScripts bool `json:"remove_scripts"`
	RemoveStyles  bool `json:"remove_styles"`
}

type EmailParsingConfig struct {
	Enabled         bool `json:"enabled"`
	PreferPlainText bool `json:"prefer_plain_text"`
}

type CleaningConfig struct {
	RemoveHeadersFooters   bool `json:"remove_headers_footers"`
	RemoveDisclaimerBlocks bool `json:"remove_disclaimer_blocks"`
	RemoveDuplicateLines   bool `json:"remove_duplicate_lines"`
	NormalizeWhitespace    bool `json:"normalize_whitespace"`
}

type ChunkingConfig struct {
	Enabled      bool `json:"enabled"`
	TargetChars  int  `json:"target_chars"`
	OverlapChars int  `json:"overlap_chars"`
}

type DeadLetterConfig struct {
	Enabled bool   `json:"enabled"`
	Prefix  string `json:"prefix"`
}

type LLMConfig struct {
	Enabled              bool   `json:"enabled"`
	Provider             string `json:"provider"`
	Endpoint             string `json:"endpoint"`
	APIKey               string `json:"api_key"`
	Model                string `json:"model"`
	TimeoutMS            int    `json:"timeout_ms"`
	MaxRetries           int    `json:"max_retries"`
	JSONSchemaValidation bool   `json:"json_schema_validation"`
}

type MarketConfig struct {
	PrimaryProvider   string               `json:"primary_provider"`
	FallbackProviders []string             `json:"fallback_providers"`
	CacheTTLSeconds   int                  `json:"cache_ttl_seconds"`
	ProviderTimeoutMS int                  `json:"provider_timeout_ms"`
	MaxRetries        int                  `json:"max_retries"`
	CircuitBreaker    CircuitBreakerConfig `json:"circuit_breaker"`
	RateLimit         RateLimitConfig      `json:"rate_limit"`
}

type CircuitBreakerConfig struct {
	FailureThreshold     int `json:"failure_threshold"`
	HalfOpenAfterSeconds int `json:"half_open_after_seconds"`
}

type RateLimitConfig struct {
	RequestsPerSecond int `json:"requests_per_second"`
	Burst             int `json:"burst"`
}

type RulesConfig struct {
	Version         string          `json:"version"`
	DefaultStrategy string          `json:"default_strategy"`
	Risk            RulesRiskConfig `json:"risk"`
}

type RulesRiskConfig struct {
	MaxPositionPct       float64 `json:"max_position_pct"`
	MaxGapPct            float64 `json:"max_gap_pct"`
	DefaultStopATR       float64 `json:"default_stop_atr"`
	DefaultTakeProfitATR float64 `json:"default_take_profit_atr"`
	MinAvgTurnoverCNY    float64 `json:"min_avg_turnover_cny"`
}

type ApprovalConfig struct {
	ManualRequired  bool   `json:"manual_required"`
	DefaultApprover string `json:"default_approver"`
}

type EvaluationConfig struct {
	EntryGraceMinutes int    `json:"entry_grace_minutes"`
	BenchmarkSymbol   string `json:"benchmark_symbol"`
	MinuteBarInterval string `json:"minute_bar_interval"`
}

type ReportingConfig struct {
	DailyEnabled       bool   `json:"daily_enabled"`
	WeeklyEnabled      bool   `json:"weekly_enabled"`
	ReportBucketPrefix string `json:"report_bucket_prefix"`
}
