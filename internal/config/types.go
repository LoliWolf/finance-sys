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
	Level string `json:"level"`
}

type DatabaseConfig struct {
	Driver                 string `json:"driver"`
	DSN                    string `json:"dsn"`
	MaxOpenConns           int    `json:"max_open_conns"`
	MaxIdleConns           int    `json:"max_idle_conns"`
	ConnMaxLifetimeMinutes int    `json:"conn_max_lifetime_minutes"`
	ConnMaxIdleTimeMinutes int    `json:"conn_max_idle_time_minutes"`
}

type DocumentConfig struct {
	APIUploadEnabled  bool                 `json:"api_upload_enabled"`
	AutoAnalyzeUpload bool                 `json:"auto_analyze_upload"`
	AllowedExtensions []string             `json:"allowed_extensions"`
	MaxFileSizeMB     int                  `json:"max_file_size_mb"`
	SHA256Dedup       bool                 `json:"sha256_dedup"`
	SourceDefaults    SourceDefaultsConfig `json:"source_defaults"`
	Chunking          ChunkingConfig       `json:"chunking"`
}

type SourceDefaultsConfig struct {
	SourceType  string `json:"source_type"`
	SourceName  string `json:"source_name"`
	Author      string `json:"author"`
	Institution string `json:"institution"`
}

type ChunkingConfig struct {
	Enabled      bool `json:"enabled"`
	TargetChars  int  `json:"target_chars"`
	OverlapChars int  `json:"overlap_chars"`
}

type LLMConfig struct {
	Enabled    bool   `json:"enabled"`
	Provider   string `json:"provider"`
	Endpoint   string `json:"endpoint"`
	APIKey     string `json:"api_key"`
	Model      string `json:"model"`
	TimeoutMS  int    `json:"timeout_ms"`
	MaxRetries int    `json:"max_retries"`
}

type RulesConfig struct {
	Version              string  `json:"version"`
	Strategy             string  `json:"strategy"`
	TradeDateOffsetDays  int     `json:"trade_date_offset_days"`
	MaxPositionPct       float64 `json:"max_position_pct"`
	DefaultStopLossPct   float64 `json:"default_stop_loss_pct"`
	DefaultTakeProfitPct float64 `json:"default_take_profit_pct"`
	MinConfidence        float64 `json:"min_confidence"`
}
