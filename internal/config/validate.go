package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
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
	_, timezoneErr := time.LoadLocation(cfg.Meta.Timezone)
	require(timezoneErr == nil, "meta.timezone must be a valid IANA timezone")
	require(cfg.Service.HTTP.PortProduction > 0 && cfg.Service.HTTP.PortProduction <= 65535, "service.http.port must be in (0,65535]")
	require(cfg.Service.HTTP.PortTest > 0 && cfg.Service.HTTP.PortTest <= 65535, "service.http.port_test must be in (0,65535]")
	require(cfg.Service.HTTP.PortProduction != cfg.Service.HTTP.PortTest, "service.http.port and service.http.port_test must be different")
	require(strings.HasPrefix(cfg.Service.HTTP.APIPrefix, "/"), "service.http.api_prefix must start with /")
	validateDatabase("database", cfg.DatabaseProduction, require)
	validateDatabase("database_test", cfg.DatabaseTest, require)
	require(cfg.Processing.OCRMaxConcurrency > 0, "processing.ocr_max_concurrency must be positive")
	require(cfg.Processing.LLMMaxConcurrency > 0, "processing.llm_max_concurrency must be positive")
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
		require(!cfg.LLM.Enabled || cfg.Agent.TimeoutMS > cfg.LLM.TimeoutMS, "agent.timeout_ms must be greater than llm.timeout_ms when both agent and llm are enabled")
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
	if cfg.ExternalDocuments.OpenList.Enabled {
		validateOpenListDocumentSource(cfg.ExternalDocuments.OpenList, require)
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
		if cfg.MarketData.SecurityMaster.Enabled {
			validateSecurityMasterRefresh(cfg.MarketData.SecurityMaster, require)
		}
	}
	if cfg.Evaluation.Enabled {
		require(cfg.Evaluation.RecommendationPerformance.Enabled, "evaluation.recommendation_performance.enabled must be true when evaluation.enabled is true")
		if cfg.Evaluation.RecommendationPerformance.Enabled {
			validateRecommendationPerformance(cfg.Evaluation.RecommendationPerformance, require)
		}
	}
	if cfg.Scheduler.Enabled {
		require(cfg.Scheduler.PollIntervalMS > 0, "scheduler.poll_interval_ms must be positive when enabled")
		require(cfg.Scheduler.ClaimTimeoutMS > 0, "scheduler.claim_timeout_ms must be positive when enabled")
		if cfg.Scheduler.StockDailyPreviousDay.Enabled {
			validateDailySchedule("scheduler.stock_daily_previous_day", cfg.Scheduler.StockDailyPreviousDay, require)
			require(cfg.MarketData.Enabled && cfg.MarketData.StockDaily.Enabled, "market_data.stock_daily must be enabled when scheduler.stock_daily_previous_day is enabled")
			require(cfg.MarketData.AsyncWorker.Enabled, "market_data.async_worker must be enabled when scheduler.stock_daily_previous_day is enabled")
		}
		if cfg.Scheduler.SecurityMasterRefresh.Enabled {
			validateDailySchedule("scheduler.security_master_refresh", cfg.Scheduler.SecurityMasterRefresh, require)
			require(cfg.MarketData.Enabled && cfg.MarketData.SecurityMaster.Enabled, "market_data.security_master must be enabled when scheduler.security_master_refresh is enabled")
		}
		if cfg.Scheduler.RecommendationEvaluationRecent.Enabled {
			validateDailySchedule("scheduler.recommendation_evaluation_recent", cfg.Scheduler.RecommendationEvaluationRecent.DailyTaskScheduleConfig, require)
			require(cfg.Scheduler.RecommendationEvaluationRecent.LookbackDays > 0, "scheduler.recommendation_evaluation_recent.lookback_days must be positive when enabled")
			require(cfg.Evaluation.Enabled && cfg.Evaluation.RecommendationPerformance.Enabled, "evaluation.recommendation_performance must be enabled when scheduler.recommendation_evaluation_recent is enabled")
			require(cfg.Evaluation.RecommendationPerformance.AsyncWorker.Enabled, "evaluation.recommendation_performance.async_worker must be enabled when scheduler.recommendation_evaluation_recent is enabled")
		}
		if cfg.Scheduler.OpenListDocumentIngestion.Enabled {
			validateHourlySchedule("scheduler.openlist_document_ingestion", cfg.Scheduler.OpenListDocumentIngestion, require)
			require(cfg.ExternalDocuments.OpenList.Enabled, "external_documents.openlist must be enabled when scheduler.openlist_document_ingestion is enabled")
		}
	}
	if cfg.Trading.Enabled || strings.TrimSpace(cfg.Trading.Provider) != "" {
		validateTrading(cfg.Trading, require)
		if cfg.Trading.Scheduler.Enabled {
			require(cfg.Scheduler.Enabled, "scheduler.enabled must be true when trading.scheduler.enabled is true")
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

func validateTrading(cfg TradingConfig, require func(bool, string)) {
	require(cfg.Environment == "SIMULATION", "trading.environment must be SIMULATION")
	require(cfg.Provider == "EASTMONEY_GM", "trading.provider must be EASTMONEY_GM")
	require(!cfg.AllowLive, "trading.allow_live must be false")
	require(cfg.Bridge.SimulationOnly, "trading.bridge.simulation_only must be true")
	require(cfg.Eastmoney.Mode == "MODE_LIVE", "trading.eastmoney.mode must be MODE_LIVE")
	require(cfg.Eastmoney.MaxSubscribedSymbols > 0 && cfg.Eastmoney.MaxSubscribedSymbols <= 50, "trading.eastmoney.max_subscribed_symbols must be in (0,50]")
	require(strings.TrimSpace(cfg.Eastmoney.SQLitePath) != "", "trading.eastmoney.sqlite_path is required")
	require(cfg.Eastmoney.TokenHealth.ProbeIntervalSeconds > 0, "trading.eastmoney.token_health.probe_interval_seconds must be positive")
	require(cfg.Eastmoney.TokenHealth.TransientFailureThreshold > 0, "trading.eastmoney.token_health.transient_failure_threshold must be positive")
	require(len(cfg.Eastmoney.TokenHealth.InvalidTokenErrorCodes) > 0, "trading.eastmoney.token_health.invalid_token_error_codes must not be empty")
	require(cfg.Eastmoney.HistoricalData.MaxRecordsPerRequest > 0 && cfg.Eastmoney.HistoricalData.MaxRecordsPerRequest <= 33000, "trading.eastmoney.historical_data.max_records_per_request must be in (0,33000]")
	require(!cfg.Eastmoney.HistoricalData.BulkBackfillEnabled || cfg.Eastmoney.HistoricalData.ProviderDailyQuotaVerified, "trading bulk backfill requires a verified provider daily quota")
	require(cfg.Eastmoney.AccountPolicy.MaxActiveAccounts == 1, "trading.eastmoney.account_policy.max_active_accounts must be 1")
	require(cfg.Eastmoney.AccountPolicy.MaxSimulationStrategiesPerAccount > 0 && cfg.Eastmoney.AccountPolicy.MaxSimulationStrategiesPerAccount <= 10, "trading.eastmoney.account_policy.max_simulation_strategies_per_account must be in (0,10]")

	require(cfg.Decision.Provider == "DETERMINISTIC" || cfg.Decision.Provider == "SHADOW", "trading.decision.provider must be DETERMINISTIC or SHADOW")
	require(strings.TrimSpace(cfg.Decision.StrategyName) != "", "trading.decision.strategy_name is required")
	require(strings.TrimSpace(cfg.Decision.StrategyVersion) != "", "trading.decision.strategy_version is required")
	require(strings.TrimSpace(cfg.Decision.ToolContractVersion) != "", "trading.decision.tool_contract_version is required")
	require(cfg.Decision.MinRecommendationConfidence >= 0 && cfg.Decision.MinRecommendationConfidence <= 1, "trading.decision.min_recommendation_confidence must be in [0,1]")
	require(cfg.Decision.MinBloggerWinRate >= 0 && cfg.Decision.MinBloggerWinRate <= 1, "trading.decision.min_blogger_win_rate must be in [0,1]")
	require(cfg.Decision.MaxCandidatesPerRun > 0, "trading.decision.max_candidates_per_run must be positive")
	require(cfg.Decision.MaxIntentsPerRun > 0 && cfg.Decision.MaxIntentsPerRun <= cfg.Decision.MaxCandidatesPerRun, "trading.decision.max_intents_per_run must be positive and not exceed max_candidates_per_run")

	require(strings.TrimSpace(cfg.Risk.Version) != "", "trading.risk.version is required")
	require(sameStringSet(cfg.Risk.AllowedAssetTypes, []string{"STOCK", "ETF"}), "trading.risk.allowed_asset_types must contain exactly STOCK and ETF")
	require(sameStringSet(cfg.Risk.AllowedMarkets, []string{"SH", "SZ"}), "trading.risk.allowed_markets must contain exactly SH and SZ")
	require(sameStringSet(cfg.Risk.AllowedSides, []string{"BUY", "SELL"}), "trading.risk.allowed_sides must contain exactly BUY and SELL")
	validBoards := map[string]bool{"SH_MAIN": true, "SZ_MAIN": true, "CHINEXT": true, "STAR": true, "ETF": true}
	for _, board := range cfg.Risk.AllowedBoards {
		require(validBoards[strings.ToUpper(strings.TrimSpace(board))], fmt.Sprintf("unsupported trading.risk.allowed_boards value %q", board))
	}
	for _, board := range cfg.Eastmoney.AccountPolicy.VerifiedBoards {
		require(validBoards[strings.ToUpper(strings.TrimSpace(board))], fmt.Sprintf("unsupported trading.eastmoney.account_policy.verified_boards value %q", board))
	}
	if cfg.Scheduler.Enabled {
		require(len(cfg.Risk.AllowedBoards) > 0, "trading.risk.allowed_boards must not be empty when trading scheduler is enabled")
		require(len(cfg.Eastmoney.AccountPolicy.VerifiedBoards) > 0, "trading.eastmoney.account_policy.verified_boards must not be empty when trading scheduler is enabled")
		require(strings.TrimSpace(cfg.Risk.TradingRuleVersion) != "", "trading.risk.trading_rule_version is required when trading scheduler is enabled")
		require(cfg.Agent.Enabled, "trading.agent.enabled must be true when trading scheduler is enabled")
	}
	for name, value := range map[string]float64{
		"max_total_position_ratio":  cfg.Risk.MaxTotalPositionRatio,
		"max_symbol_position_ratio": cfg.Risk.MaxSymbolPositionRatio,
		"max_single_order_ratio":    cfg.Risk.MaxSingleOrderRatio,
		"max_daily_turnover_ratio":  cfg.Risk.MaxDailyTurnoverRatio,
		"daily_loss_kill_ratio":     cfg.Risk.DailyLossKillRatio,
		"max_price_deviation_ratio": cfg.Risk.MaxPriceDeviationRatio,
		"min_cash_reserve_ratio":    cfg.Risk.MinCashReserveRatio,
	} {
		require(value > 0 && value <= 1, "trading.risk."+name+" must be in (0,1]")
	}
	require(cfg.Risk.MaxSingleOrderRatio <= cfg.Risk.MaxSymbolPositionRatio, "trading.risk.max_single_order_ratio must not exceed max_symbol_position_ratio")
	require(cfg.Risk.MaxSymbolPositionRatio <= cfg.Risk.MaxTotalPositionRatio, "trading.risk.max_symbol_position_ratio must not exceed max_total_position_ratio")
	require(cfg.Risk.MaxTotalPositionRatio+cfg.Risk.MinCashReserveRatio <= 1, "trading.risk total position and cash reserve ratios conflict")
	require(cfg.Risk.MaxPositionCount > 0, "trading.risk.max_position_count must be positive")
	require(cfg.Risk.MaxNewOrdersPerDay > 0, "trading.risk.max_new_orders_per_day must be positive")
	require(cfg.Risk.IntentCooldownTradeDays > 0, "trading.risk.intent_cooldown_trade_days must be positive")
	require(cfg.Risk.MaxSnapshotAgeSeconds > 0, "trading.risk.max_snapshot_age_seconds must be positive")

	require(cfg.Execution.DefaultOrderType == "LIMIT", "trading.execution.default_order_type must be LIMIT")
	require(!cfg.Execution.AllowMarketOrder, "trading.execution.allow_market_order must be false")
	require(cfg.Execution.CommandPollIntervalMS > 0, "trading.execution.command_poll_interval_ms must be positive")
	require(cfg.Execution.CommandClaimTimeoutMS > 0, "trading.execution.command_claim_timeout_ms must be positive")
	require(cfg.Execution.DispatchTimeoutMS > 0, "trading.execution.dispatch_timeout_ms must be positive")
	require(cfg.Execution.DispatchMaxRetries >= 0, "trading.execution.dispatch_max_retries must be zero or positive")
	require(cfg.Exit.StopLossRatio > 0 && cfg.Exit.StopLossRatio < 1, "trading.exit.stop_loss_ratio must be in (0,1)")
	require(cfg.Exit.TakeProfitRatio > 0 && cfg.Exit.TakeProfitRatio < 1, "trading.exit.take_profit_ratio must be in (0,1)")
	require(cfg.Exit.MaxHoldingTradeDays > 0, "trading.exit.max_holding_trade_days must be positive")
	require(cfg.Exit.MonitorIntervalSeconds > 0, "trading.exit.monitor_interval_seconds must be positive")
	require(cfg.Exit.SellLimitDiscountRatio >= 0 && cfg.Exit.SellLimitDiscountRatio <= cfg.Risk.MaxPriceDeviationRatio, "trading.exit.sell_limit_discount_ratio must be non-negative and not exceed max_price_deviation_ratio")
	require(strings.HasPrefix(cfg.Bridge.CallbackURL, "http://") || strings.HasPrefix(cfg.Bridge.CallbackURL, "https://"), "trading.bridge.callback_url must start with http:// or https://")
	require(cfg.Bridge.RequestTimeoutMS > 0, "trading.bridge.request_timeout_ms must be positive")
	require(cfg.Bridge.HMAC.MaxClockSkewSeconds > 0, "trading.bridge.hmac.max_clock_skew_seconds must be positive")
	require(cfg.Bridge.HMAC.NonceTTLSeconds > 0, "trading.bridge.hmac.nonce_ttl_seconds must be positive")
	if cfg.Bridge.TLS.Verify {
		require(strings.TrimSpace(cfg.Bridge.TLS.CAFile) != "", "trading.bridge.tls.ca_file is required when verification is enabled")
		require(strings.TrimSpace(cfg.Bridge.TLS.CertFile) != "", "trading.bridge.tls.cert_file is required when verification is enabled")
		require(strings.TrimSpace(cfg.Bridge.TLS.KeyFile) != "", "trading.bridge.tls.key_file is required when verification is enabled")
	}

	if cfg.Agent.Enabled {
		require(strings.HasPrefix(cfg.Agent.Endpoint, "http://") || strings.HasPrefix(cfg.Agent.Endpoint, "https://"), "trading.agent.endpoint must start with http:// or https:// when enabled")
		require(strings.HasPrefix(cfg.Agent.HealthEndpoint, "http://") || strings.HasPrefix(cfg.Agent.HealthEndpoint, "https://"), "trading.agent.health_endpoint must start with http:// or https:// when enabled")
		require(cfg.Agent.TimeoutMS > 0, "trading.agent.timeout_ms must be positive when enabled")
		require(cfg.Agent.MaxRetries >= 0, "trading.agent.max_retries must be zero or positive")
		require(strings.TrimSpace(cfg.Agent.SchemaVersion) != "", "trading.agent.schema_version is required when enabled")
		require(strings.TrimSpace(cfg.Agent.InternalToken) != "", "trading.agent.internal_token is required when enabled")
	}

	if cfg.TradingReadyForExecution() {
		require(strings.TrimSpace(cfg.Bridge.BaseURL) != "", "trading.bridge.base_url is required when trading.enabled is true")
		require(strings.TrimSpace(cfg.Bridge.ExpectedAccountID) != "", "trading.bridge.expected_account_id is required when trading.enabled is true")
		require(strings.TrimSpace(cfg.Bridge.StrategyID) != "", "trading.bridge.strategy_id is required when trading.enabled is true")
		require(strings.TrimSpace(cfg.Bridge.HMAC.KeyID) != "", "trading.bridge.hmac.key_id is required when trading.enabled is true")
		require(strings.TrimSpace(cfg.Bridge.HMAC.Secret) != "", "trading.bridge.hmac.secret is required when trading.enabled is true")
		require(strings.TrimSpace(cfg.Eastmoney.Token) != "", "trading.eastmoney.token is required when trading.enabled is true")
		require(cfg.Bridge.ExpectedAccountID == cfg.Eastmoney.ExpectedAccountID, "trading Bridge and Eastmoney expected_account_id must match")
		require(cfg.Bridge.StrategyID == cfg.Eastmoney.StrategyID, "trading Bridge and Eastmoney strategy_id must match")
		require(len(cfg.Eastmoney.AccountPolicy.AllowedAccountIDs) == 1 && cfg.Eastmoney.AccountPolicy.AllowedAccountIDs[0] == cfg.Eastmoney.ExpectedAccountID, "trading allowed_account_ids must contain only expected_account_id")
	}

	if cfg.Reconciliation.Enabled {
		require(cfg.Reconciliation.IntervalSeconds > 0, "trading.reconciliation.interval_seconds must be positive when enabled")
		require(cfg.Reconciliation.RefreshTimeoutSeconds > 0, "trading.reconciliation.refresh_timeout_seconds must be positive when enabled")
	}
	if cfg.Scheduler.Enabled {
		if cfg.Scheduler.PreOpenDecision.Enabled {
			validateDailySchedule("trading.scheduler.pre_open_decision", cfg.Scheduler.PreOpenDecision, require)
		}
		if cfg.Scheduler.EndOfDayReconcile.Enabled {
			validateDailySchedule("trading.scheduler.end_of_day_reconcile", cfg.Scheduler.EndOfDayReconcile, require)
		}
		validateTradingSession("trading.scheduler.morning_window", cfg.Scheduler.MorningWindow, require)
		validateTradingSession("trading.scheduler.afternoon_window", cfg.Scheduler.AfternoonWindow, require)
	}
}

func (cfg TradingConfig) TradingReadyForExecution() bool {
	return cfg.Enabled
}

func validateTradingSession(name string, cfg TradingSessionWindow, require func(bool, string)) {
	start, startErr := time.Parse("15:04:05", cfg.Start)
	end, endErr := time.Parse("15:04:05", cfg.End)
	require(startErr == nil, name+".start must use HH:MM:SS")
	require(endErr == nil, name+".end must use HH:MM:SS")
	if startErr == nil && endErr == nil {
		require(start.Before(end), name+" start must be before end")
	}
}

func sameStringSet(actual, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	values := make(map[string]struct{}, len(actual))
	for _, item := range actual {
		values[strings.ToUpper(strings.TrimSpace(item))] = struct{}{}
	}
	for _, item := range expected {
		if _, ok := values[item]; !ok {
			return false
		}
	}
	return true
}

func validateDailySchedule(name string, cfg DailyTaskScheduleConfig, require func(bool, string)) {
	require(cfg.Hour >= 0 && cfg.Hour <= 23, name+".hour must be in [0,23]")
	require(cfg.Minute >= 0 && cfg.Minute <= 59, name+".minute must be in [0,59]")
}

func validateHourlySchedule(name string, cfg HourlyTaskScheduleConfig, require func(bool, string)) {
	require(cfg.Minute >= 0 && cfg.Minute <= 59, name+".minute must be in [0,59]")
}

func validateOpenListDocumentSource(cfg OpenListDocumentSourceConfig, require func(bool, string)) {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	require(strings.HasPrefix(baseURL, "http://") || strings.HasPrefix(baseURL, "https://"), "external_documents.openlist.base_url must start with http:// or https:// when enabled")
	require(strings.TrimSpace(cfg.Username) != "", "external_documents.openlist.username is required when enabled")
	require(strings.TrimSpace(cfg.Password) != "", "external_documents.openlist.password is required when enabled")
	require(strings.HasPrefix(strings.TrimSpace(cfg.RootPath), "/"), "external_documents.openlist.root_path must start with / when enabled")
	require(strings.TrimSpace(cfg.Institution) != "", "external_documents.openlist.institution is required when enabled")
	require(cfg.RequestTimeoutMS > 0, "external_documents.openlist.request_timeout_ms must be positive when enabled")
	require(cfg.ScanLookbackDays > 0, "external_documents.openlist.scan_lookback_days must be positive when enabled")
}

func validateDatabase(name string, cfg DatabaseConfig, require func(bool, string)) {
	require(cfg.DSN != "", name+".dsn is required")
	require(cfg.Driver == DatabaseDriverMySQL, name+".driver must be mysql")
	require(cfg.MaxOpenConns > 0, name+".max_open_conns must be positive")
	require(cfg.MaxIdleConns >= 0, name+".max_idle_conns must be zero or positive")
	require(cfg.MaxIdleConns <= cfg.MaxOpenConns, name+".max_idle_conns must not exceed max_open_conns")
	require(cfg.ConnMaxLifetimeMinutes > 0, name+".conn_max_lifetime_minutes must be positive")
	require(cfg.ConnMaxIdleTimeMinutes > 0, name+".conn_max_idle_time_minutes must be positive")
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
		"STOCK":  {},
		"ETF":    {},
		"SECTOR": {},
	}
	for _, assetType := range cfg.SyncAssetTypes {
		_, ok := allowedAssetTypes[strings.ToUpper(strings.TrimSpace(assetType))]
		require(ok, fmt.Sprintf("unsupported market_data.stock_daily.sync_asset_types value %q", assetType))
	}
	require(len(cfg.Fields) > 0, "market_data.stock_daily.fields must not be empty when enabled")
	if _, enabled := configuredAssetTypes(cfg.SyncAssetTypes)["SECTOR"]; enabled {
		require(len(cfg.SectorFields) > 0, "market_data.stock_daily.sector_fields must not be empty when SECTOR sync is enabled")
	}
}

func validateSecurityMasterRefresh(cfg SecurityMasterRefreshConfig, require func(bool, string)) {
	require(len(cfg.StockFields) > 0, "market_data.security_master.stock_fields must not be empty when enabled")
	require(len(cfg.ETFFields) > 0, "market_data.security_master.etf_fields must not be empty when enabled")
	require(len(cfg.SectorFields) > 0, "market_data.security_master.sector_fields must not be empty when enabled")
	sectorFields := configuredFields(cfg.SectorFields)
	for _, field := range []string{"ts_code", "trade_date", "name", "idx_type"} {
		_, ok := sectorFields[field]
		require(ok, fmt.Sprintf("market_data.security_master.sector_fields must contain %q when enabled", field))
	}
}

func configuredFields(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}
	return result
}

func configuredAssetTypes(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[strings.ToUpper(strings.TrimSpace(value))] = struct{}{}
	}
	return result
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
