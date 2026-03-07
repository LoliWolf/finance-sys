package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

func Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	var errs []string

	require := func(ok bool, message string) {
		if !ok {
			errs = append(errs, message)
		}
	}

	require(cfg.Meta.ConfigVersion > 0, "meta.config_version must be positive")
	require(cfg.Meta.Timezone != "", "meta.timezone is required")
	require(cfg.Service.HTTP.Port > 0, "service.http.port must be positive")
	require(strings.HasPrefix(cfg.Service.HTTP.APIPrefix, "/"), "service.http.api_prefix must start with /")
	require(cfg.Service.Worker.Concurrency > 0, "service.worker.concurrency must be positive")
	require(cfg.Database.DSN != "", "database.dsn is required")
	require(cfg.Database.Driver == "mysql", "database.driver must be mysql")
	require(cfg.ObjectStorage.Endpoint != "", "object_storage.endpoint is required")
	require(cfg.Redis.Addr != "", "redis.addr is required")
	require(cfg.NacosClient.PollIntervalSeconds > 0, "nacos_client.poll_interval_seconds must be positive")
	require(cfg.DocumentIngestion.MaxFileSizeMB > 0, "document_ingestion.max_file_size_mb must be positive")
	require(len(cfg.DocumentIngestion.AllowedExtensions) > 0, "document_ingestion.allowed_extensions must not be empty")
	require(cfg.DocumentParsing.Chunking.TargetChars > 0, "document_parsing.chunking.target_chars must be positive")
	require(cfg.Market.PrimaryProvider != "", "market.primary_provider is required")
	require(cfg.Market.MaxRetries >= 0, "market.max_retries must be zero or positive")
	require(cfg.Market.CircuitBreaker.FailureThreshold > 0, "market.circuit_breaker.failure_threshold must be positive")
	require(cfg.Rules.Version != "", "rules.version is required")
	require(cfg.Rules.DefaultStrategy != "", "rules.default_strategy is required")
	require(cfg.Rules.Risk.MaxPositionPct > 0 && cfg.Rules.Risk.MaxPositionPct <= 1, "rules.risk.max_position_pct must be in (0,1]")
	require(cfg.Rules.Risk.DefaultStopATR > 0, "rules.risk.default_stop_atr must be positive")
	require(cfg.Rules.Risk.DefaultTakeProfitATR > 0, "rules.risk.default_take_profit_atr must be positive")
	require(cfg.Evaluation.EntryGraceMinutes >= 0, "evaluation.entry_grace_minutes must be zero or positive")
	require(cfg.Reporting.ReportBucketPrefix != "", "reporting.report_bucket_prefix is required")
	require(filepath.Clean(cfg.NacosClient.CacheDir) != ".", "nacos_client.cache_dir is required")

	allowed := map[string]struct{}{
		".pdf":  {},
		".docx": {},
		".txt":  {},
		".md":   {},
		".html": {},
		".eml":  {},
	}
	for _, ext := range cfg.DocumentIngestion.AllowedExtensions {
		_, ok := allowed[strings.ToLower(ext)]
		require(ok, fmt.Sprintf("unsupported document extension %q", ext))
	}

	if len(errs) > 0 {
		return fmt.Errorf("config validation failed: %s", strings.Join(errs, "; "))
	}
	return nil
}
