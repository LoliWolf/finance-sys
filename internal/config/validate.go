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
	require(cfg.Database.Driver == DatabaseDriverMySQL, "database.driver must be mysql")
	require(cfg.NacosClient.PollIntervalSeconds > 0, "nacos_client.poll_interval_seconds must be positive")
	require(cfg.Document.MaxFileSizeMB > 0, "document.max_file_size_mb must be positive")
	require(len(cfg.Document.AllowedExtensions) > 0, "document.allowed_extensions must not be empty")
	require(cfg.Document.Chunking.TargetChars > 0, "document.chunking.target_chars must be positive")
	require(strings.TrimSpace(cfg.Document.PDFOCR.Command) != "", "document.pdf_ocr.command is required")
	require(len(cfg.Document.PDFOCR.Args) > 0, "document.pdf_ocr.args must not be empty")
	require(cfg.Document.PDFOCR.MinTextChars >= 0, "document.pdf_ocr.min_text_chars must be zero or positive")
	require(cfg.Document.PDFOCR.TimeoutMS > 0, "document.pdf_ocr.timeout_ms must be positive")
	require(cfg.LLM.TimeoutMS > 0, "llm.timeout_ms must be positive")
	require(cfg.LLM.MaxRetries >= 0, "llm.max_retries must be zero or positive")
	for headerName := range cfg.LLM.ExtraHeaders {
		trimmedHeaderName := strings.TrimSpace(headerName)
		require(trimmedHeaderName != "", "llm.extra_headers must not contain empty header names")
		switch strings.ToLower(trimmedHeaderName) {
		case "authorization":
			require(false, "llm.extra_headers must not override Authorization")
		case "content-type":
			require(false, "llm.extra_headers must not override Content-Type")
		}
	}
	require(cfg.Rules.Version != "", "rules.version is required")
	require(cfg.Rules.Strategy != "", "rules.strategy is required")
	require(cfg.Rules.Strategy == RuleStrategyTextReferencePrice, "rules.strategy must be TEXT_REFERENCE_PRICE")
	require(cfg.Rules.TradeDateOffsetDays > 0, "rules.trade_date_offset_days must be positive")
	require(cfg.Rules.MaxPositionPct > 0 && cfg.Rules.MaxPositionPct <= 1, "rules.max_position_pct must be in (0,1]")
	require(cfg.Rules.DefaultStopLossPct > 0, "rules.default_stop_loss_pct must be positive")
	require(cfg.Rules.DefaultTakeProfitPct > 0, "rules.default_take_profit_pct must be positive")
	require(cfg.Rules.MinConfidence > 0 && cfg.Rules.MinConfidence <= 1, "rules.min_confidence must be in (0,1]")
	require(filepath.Clean(cfg.NacosClient.CacheDir) != ".", "nacos_client.cache_dir is required")
	if cfg.LLM.Enabled {
		require(cfg.LLM.Provider != "", "llm.provider is required when llm.enabled is true")
		require(cfg.LLM.Provider == LLMProviderOpenAICompatible, "llm.provider must be openai_compatible")
		require(cfg.LLM.Endpoint != "", "llm.endpoint is required when llm.enabled is true")
		require(cfg.LLM.Model != "", "llm.model is required when llm.enabled is true")
	}
	if cfg.Agent.Enabled {
		require(cfg.Agent.Mode == AgentModePrimary || cfg.Agent.Mode == AgentModeShadow, "agent.mode must be primary or shadow when agent.enabled is true")
		require(strings.TrimSpace(cfg.Agent.Endpoint) != "", "agent.endpoint is required when agent.enabled is true")
		if strings.TrimSpace(cfg.Agent.InternalAPIBaseURL) != "" {
			require(strings.HasPrefix(cfg.Agent.InternalAPIBaseURL, "http://") || strings.HasPrefix(cfg.Agent.InternalAPIBaseURL, "https://"), "agent.internal_api_base_url must start with http:// or https:// when set")
		}
		if cfg.Agent.Tushare.Enabled {
			require(strings.TrimSpace(cfg.Agent.Tushare.Token) != "", "agent.tushare.token is required when agent.tushare.enabled is true")
			require(strings.HasPrefix(cfg.Agent.Tushare.Endpoint, "http://") || strings.HasPrefix(cfg.Agent.Tushare.Endpoint, "https://"), "agent.tushare.endpoint must start with http:// or https:// when agent.tushare.enabled is true")
			require(cfg.Agent.Tushare.TimeoutMS > 0 && cfg.Agent.Tushare.TimeoutMS <= 60000, "agent.tushare.timeout_ms must be in (0,60000] when agent.tushare.enabled is true")
		}
		require(cfg.Agent.TimeoutMS > 0, "agent.timeout_ms must be positive when agent.enabled is true")
		require(cfg.Agent.MaxRetries >= 0, "agent.max_retries must be zero or positive")
		require(strings.TrimSpace(cfg.Agent.SchemaVersion) != "", "agent.schema_version is required when agent.enabled is true")
		if cfg.Agent.Auth.Enabled {
			require(strings.TrimSpace(cfg.Agent.Auth.HeaderName) != "", "agent.auth.header_name is required when agent.auth.enabled is true")
			require(strings.TrimSpace(cfg.Agent.Auth.StaticToken) != "", "agent.auth.static_token is required when agent.auth.enabled is true")
		}
	}
	if cfg.Agent.Observation.Enabled {
		require(cfg.Agent.Observation.ShadowSampleRate >= 0 && cfg.Agent.Observation.ShadowSampleRate <= 1, "agent.observation.shadow_sample_rate must be in [0,1]")
		require(cfg.Agent.Observation.MaxTargetsPerRun > 0, "agent.observation.max_targets_per_run must be positive when agent.observation.enabled is true")
		require(cfg.Agent.Observation.MaxJSONBytes > 0, "agent.observation.max_json_bytes must be positive when agent.observation.enabled is true")
		require(cfg.Agent.Observation.RetentionDays > 0, "agent.observation.retention_days must be positive when agent.observation.enabled is true")
	}
	if cfg.MarketData.Enabled {
		require(cfg.MarketData.Provider == "tushare", "market_data.provider must be tushare when market_data.enabled is true")
		require(cfg.MarketData.Tushare.Enabled, "market_data.tushare.enabled must be true when market_data.enabled is true")
		if cfg.MarketData.Tushare.Enabled {
			validateMarketDataTushare(cfg.MarketData.Tushare, require)
		}
		if cfg.MarketData.AsyncWorker.Enabled {
			require(cfg.MarketData.AsyncWorker.PollIntervalMS > 0, "market_data.async_worker.poll_interval_ms must be positive when enabled")
			require(cfg.MarketData.AsyncWorker.ClaimTimeoutMS > 0, "market_data.async_worker.claim_timeout_ms must be positive when enabled")
			require(cfg.MarketData.AsyncWorker.MaxConcurrentRuns > 0, "market_data.async_worker.max_concurrent_runs must be positive when enabled")
			require(cfg.MarketData.AsyncWorker.BatchSize > 0, "market_data.async_worker.batch_size must be positive when enabled")
		}
		if cfg.MarketData.StockDaily.Enabled {
			validateStockDailySync(cfg.MarketData.StockDaily, require)
		}
	}
	if cfg.Evaluation.Enabled {
		require(cfg.Evaluation.RecommendationPerformance.Enabled, "evaluation.recommendation_performance.enabled must be true when evaluation.enabled is true")
		if cfg.Evaluation.RecommendationPerformance.Enabled {
			validateRecommendationPerformance(cfg.Evaluation.RecommendationPerformance, require)
		}
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

func validateMarketDataTushare(cfg MarketDataTushareConfig, require func(bool, string)) {
	require(strings.TrimSpace(cfg.SDKPackage) != "", "market_data.tushare.sdk_package is required when enabled")
	require(cfg.TimeoutMS > 0 && cfg.TimeoutMS <= 120000, "market_data.tushare.timeout_ms must be in (0,120000] when enabled")
	require(cfg.MaxRetries >= 0, "market_data.tushare.max_retries must be zero or positive")
	require(cfg.TokenCooldownMS >= 0, "market_data.tushare.token_cooldown_ms must be zero or positive")
	require(len(cfg.Tokens) > 0, "market_data.tushare.tokens must not be empty when enabled")

	enabledTokens := 0
	for _, token := range cfg.Tokens {
		require(token.Weight > 0, "market_data.tushare.tokens weight must be positive")
		if token.Enabled {
			enabledTokens++
			require(strings.TrimSpace(token.Token) != "", "market_data.tushare.tokens token is required when token is enabled")
		}
	}
	require(enabledTokens > 0, "market_data.tushare.tokens must contain at least one enabled token when enabled")
}

func validateStockDailySync(cfg StockDailySyncConfig, require func(bool, string)) {
	require(len(cfg.SyncAssetTypes) > 0, "market_data.stock_daily.sync_asset_types must not be empty when enabled")
	allowedAssetTypes := map[string]struct{}{
		"STOCK": {},
		"ETF":   {},
	}
	for _, assetType := range cfg.SyncAssetTypes {
		_, ok := allowedAssetTypes[strings.ToUpper(strings.TrimSpace(assetType))]
		require(ok, fmt.Sprintf("unsupported market_data.stock_daily.sync_asset_types value %q", assetType))
	}
	require(len(cfg.Fields) > 0, "market_data.stock_daily.fields must not be empty when enabled")
}

func validateRecommendationPerformance(cfg RecommendationPerformanceConfig, require func(bool, string)) {
	require(len(cfg.Windows) > 0, "evaluation.recommendation_performance.windows must not be empty when enabled")
	seenWindows := make(map[int]struct{}, len(cfg.Windows))
	for _, window := range cfg.Windows {
		require(window > 0, "evaluation.recommendation_performance.windows must contain only positive values")
		_, duplicated := seenWindows[window]
		require(!duplicated, "evaluation.recommendation_performance.windows must not contain duplicates")
		seenWindows[window] = struct{}{}
	}
	require(strings.TrimSpace(cfg.QuoteSource) != "", "evaluation.recommendation_performance.quote_source is required when enabled")
	require(cfg.EntryPriceRule == "NEXT_TRADE_OPEN", "evaluation.recommendation_performance.entry_price_rule must be NEXT_TRADE_OPEN")
	require(cfg.BasePriceRule == "RECOMMEND_DATE_CLOSE_OR_NEXT_CLOSE", "evaluation.recommendation_performance.base_price_rule must be RECOMMEND_DATE_CLOSE_OR_NEXT_CLOSE")
	require(cfg.WinThresholdRatio > -1 && cfg.WinThresholdRatio < 1, "evaluation.recommendation_performance.win_threshold_ratio must be in (-1,1)")
	require(cfg.MinQuoteCoverageRatio > 0 && cfg.MinQuoteCoverageRatio <= 1, "evaluation.recommendation_performance.min_quote_coverage_ratio must be in (0,1]")
	require(strings.TrimSpace(cfg.CalcVersion) != "", "evaluation.recommendation_performance.calc_version is required when enabled")
	if cfg.AsyncWorker.Enabled {
		require(cfg.AsyncWorker.PollIntervalMS > 0, "evaluation.recommendation_performance.async_worker.poll_interval_ms must be positive when enabled")
		require(cfg.AsyncWorker.ClaimTimeoutMS > 0, "evaluation.recommendation_performance.async_worker.claim_timeout_ms must be positive when enabled")
		require(cfg.AsyncWorker.MaxConcurrentRuns > 0, "evaluation.recommendation_performance.async_worker.max_concurrent_runs must be positive when enabled")
		require(cfg.AsyncWorker.BatchSize > 0, "evaluation.recommendation_performance.async_worker.batch_size must be positive when enabled")
	}
	_, defaultWindowExists := seenWindows[cfg.Ranking.DefaultWindowDays]
	require(defaultWindowExists, "evaluation.recommendation_performance.ranking.default_window_days must be present in windows")
	require(cfg.Ranking.DefaultMinSampleCount >= 0, "evaluation.recommendation_performance.ranking.default_min_sample_count must be zero or positive")
	allowedSorts := map[string]struct{}{
		"win_rate":          {},
		"avg_return":        {},
		"sample_count":      {},
		"performance_score": {},
	}
	_, sortAllowed := allowedSorts[cfg.Ranking.DefaultSort]
	require(sortAllowed, "evaluation.recommendation_performance.ranking.default_sort is unsupported")
}
