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
	require(cfg.Database.DSN != "", "database.dsn is required")
	require(cfg.Database.Driver == "mysql", "database.driver must be mysql")
	require(cfg.NacosClient.PollIntervalSeconds > 0, "nacos_client.poll_interval_seconds must be positive")
	require(cfg.Document.MaxFileSizeMB > 0, "document.max_file_size_mb must be positive")
	require(len(cfg.Document.AllowedExtensions) > 0, "document.allowed_extensions must not be empty")
	require(cfg.Document.Chunking.TargetChars > 0, "document.chunking.target_chars must be positive")
	require(cfg.LLM.TimeoutMS > 0, "llm.timeout_ms must be positive")
	require(cfg.LLM.MaxRetries >= 0, "llm.max_retries must be zero or positive")
	require(cfg.Rules.Version != "", "rules.version is required")
	require(cfg.Rules.Strategy != "", "rules.strategy is required")
	require(cfg.Rules.TradeDateOffsetDays > 0, "rules.trade_date_offset_days must be positive")
	require(cfg.Rules.MaxPositionPct > 0 && cfg.Rules.MaxPositionPct <= 1, "rules.max_position_pct must be in (0,1]")
	require(cfg.Rules.DefaultStopLossPct > 0, "rules.default_stop_loss_pct must be positive")
	require(cfg.Rules.DefaultTakeProfitPct > 0, "rules.default_take_profit_pct must be positive")
	require(cfg.Rules.MinConfidence > 0 && cfg.Rules.MinConfidence <= 1, "rules.min_confidence must be in (0,1]")
	require(filepath.Clean(cfg.NacosClient.CacheDir) != ".", "nacos_client.cache_dir is required")
	if cfg.LLM.Enabled {
		require(cfg.LLM.Provider != "", "llm.provider is required when llm.enabled is true")
		require(cfg.LLM.Endpoint != "", "llm.endpoint is required when llm.enabled is true")
		require(cfg.LLM.Model != "", "llm.model is required when llm.enabled is true")
	}

	allowed := map[string]struct{}{
		".pdf":  {},
		".doc":  {},
		".docx": {},
		".txt":  {},
		".md":   {},
		".csv":  {},
	}
	for _, ext := range cfg.Document.AllowedExtensions {
		_, ok := allowed[strings.ToLower(ext)]
		require(ok, fmt.Sprintf("unsupported document extension %q", ext))
	}

	if len(errs) > 0 {
		return fmt.Errorf("config validation failed: %s", strings.Join(errs, "; "))
	}
	return nil
}
