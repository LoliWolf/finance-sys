package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"finance-sys/internal/bootstrap"
	"finance-sys/internal/telemetry"

	"gorm.io/driver/mysql"
	"gorm.io/gen"
	"gorm.io/gorm"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	logger := telemetry.NewLogger("INFO")
	snapshot, _, err := bootstrap.LoadInitialSnapshot(ctx, logger)
	if err != nil {
		fatal(logger, "load nacos config", err)
	}

	db, err := gorm.Open(mysql.Open(snapshot.Config.Database.DSN), &gorm.Config{})
	if err != nil {
		fatal(logger, "open mysql", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		fatal(logger, "unwrap sql db", err)
	}
	defer sqlDB.Close()

	sqlDB.SetMaxOpenConns(snapshot.Config.Database.MaxOpenConns)
	sqlDB.SetMaxIdleConns(snapshot.Config.Database.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(time.Duration(snapshot.Config.Database.ConnMaxLifetimeMinutes) * time.Minute)
	sqlDB.SetConnMaxIdleTime(time.Duration(snapshot.Config.Database.ConnMaxIdleTimeMinutes) * time.Minute)
	if err := sqlDB.PingContext(ctx); err != nil {
		fatal(logger, "ping mysql", err)
	}

	queryOutPath := filepath.Join(os.TempDir(), "finance_sys_gorm_gen_query")
	defer os.RemoveAll(queryOutPath)

	g := gen.NewGenerator(gen.Config{
		OutPath:           queryOutPath,
		ModelPkgPath:      filepath.Join("internal", "domain", "db_model"),
		FieldWithIndexTag: true,
		FieldWithTypeTag:  true,
		Mode:              gen.WithDefaultQuery | gen.WithQueryInterface,
	})
	g.UseDB(db)
	g.WithFileNameStrategy(func(tableName string) string {
		switch tableName {
		case "config_snapshots":
			return "config_snapshot"
		case "documents":
			return "document"
		case "parse_runs":
			return "parse_run"
		case "security_aliases":
			return "security_alias"
		case "trade_candidate_plans":
			return "trade_candidate_plan"
		case "instrument_resolution_runs":
			return "instrument_resolution_run"
		case "untrackable_targets":
			return "untrackable_target"
		case "bloggers":
			return "blogger"
		case "recommendation_events":
			return "recommendation_event"
		case "recommendation_event_evidences":
			return "recommendation_event_evidence"
		case "stock_daily_quotes":
			return "stock_daily_quote"
		case "market_data_sync_runs":
			return "market_data_sync_run"
		case "market_data_sync_missing_items":
			return "market_data_sync_missing_item"
		case "recommendation_evaluation_runs":
			return "recommendation_evaluation_run"
		case "recommendation_event_window_metrics":
			return "recommendation_event_window_metric"
		case "scheduled_task_runs":
			return "scheduled_task_run"
		case "external_document_ingestions":
			return "external_document_ingestion"
		case "trading_agent_runs":
			return "trading_agent_run"
		case "trading_intents":
			return "trading_intent"
		case "trading_risk_checks":
			return "trading_risk_check"
		case "trading_orders":
			return "trading_order"
		case "trading_order_events":
			return "trading_order_event"
		case "trading_fills":
			return "trading_fill"
		case "trading_account_snapshots":
			return "trading_account_snapshot"
		case "trading_position_snapshots":
			return "trading_position_snapshot"
		case "trading_reconciliation_runs":
			return "trading_reconciliation_run"
		case "trading_reconciliation_diffs":
			return "trading_reconciliation_diff"
		case "trading_runtime_controls":
			return "trading_runtime_control"
		case "trading_daily_sessions":
			return "trading_daily_session"
		case "trading_position_cycles":
			return "trading_position_cycle"
		case "trading_skill_decisions":
			return "trading_skill_decision"
		case "trading_board_capabilities":
			return "trading_board_capability"
		case "sector_trade_proxy_mappings":
			return "sector_trade_proxy_mapping"
		default:
			return tableName
		}
	})
	g.ApplyBasic(
		g.GenerateModelAs("config_snapshots", "ConfigSnapshot",
			gen.FieldRename("sha256", "Sha256"),
			gen.FieldRename("raw_json", "RawJSON"),
			gen.FieldType("raw_json", "[]byte"),
		),
		g.GenerateModelAs("documents", "Document",
			gen.FieldRename("sha256", "Sha256"),
			gen.FieldRename("pdf_ocr_enabled", "PdfOcrEnabled"),
		),
		g.GenerateModelAs("parse_runs", "ParseRun",
			gen.FieldRename("chunks_json", "ChunksJSON"),
			gen.FieldRename("raw_metadata_json", "RawMetadataJSON"),
			gen.FieldType("chunks_json", "[]byte"),
			gen.FieldType("raw_metadata_json", "[]byte"),
		),
		g.GenerateModelAs("security_master", "SecurityMaster",
			gen.FieldRename("ts_code", "TSCode"),
			gen.FieldRename("raw_json", "RawJSON"),
			gen.FieldType("list_date", "*time.Time"),
			gen.FieldType("delist_date", "*time.Time"),
			gen.FieldType("raw_json", "[]byte"),
		),
		g.GenerateModelAs("security_aliases", "SecurityAlias",
			gen.FieldRename("alias", "AliasName"),
		),
		g.GenerateModelAs("trade_candidate_plans", "TradeCandidatePlan",
			gen.FieldRename("risks_json", "RisksJSON"),
			gen.FieldRename("evidence_json", "EvidenceJSON"),
			gen.FieldType("risks_json", "[]byte"),
			gen.FieldType("evidence_json", "[]byte"),
		),
		g.GenerateModelAs("instrument_resolution_runs", "InstrumentResolutionRun",
			gen.FieldType("parse_run_id", "*int64"),
			gen.FieldType("finished_at", "*time.Time"),
			gen.FieldRename("targets_json", "TargetsJSON"),
			gen.FieldRename("tool_traces_json", "ToolTracesJSON"),
			gen.FieldRename("shadow_compare_json", "ShadowCompareJSON"),
			gen.FieldRename("raw_metadata_json", "RawMetadataJSON"),
			gen.FieldType("targets_json", "[]byte"),
			gen.FieldType("tool_traces_json", "[]byte"),
			gen.FieldType("shadow_compare_json", "[]byte"),
			gen.FieldType("raw_metadata_json", "[]byte"),
		),
		g.GenerateModelAs("untrackable_targets", "UntrackableTarget",
			gen.FieldType("parse_run_id", "*int64"),
			gen.FieldRename("evidence_json", "EvidenceJSON"),
			gen.FieldRename("candidates_json", "CandidatesJSON"),
			gen.FieldType("evidence_json", "[]byte"),
			gen.FieldType("candidates_json", "[]byte"),
		),
		g.GenerateModelAs("bloggers", "Blogger"),
		g.GenerateModelAs("recommendation_events", "RecommendationEvent",
			gen.FieldType("plan_id", "*int64"),
		),
		g.GenerateModelAs("recommendation_event_evidences", "RecommendationEventEvidence",
			gen.FieldType("plan_id", "*int64"),
		),
		g.GenerateModelAs("stock_daily_quotes", "StockDailyQuote",
			gen.FieldRename("ts_code", "TSCode"),
			gen.FieldRename("tushare_content", "TushareContent"),
			gen.FieldType("tushare_content", "[]byte"),
		),
		g.GenerateModelAs("market_data_sync_runs", "MarketDataSyncRun",
			gen.FieldType("started_at", "*time.Time"),
			gen.FieldType("finished_at", "*time.Time"),
			gen.FieldType("claimed_at", "*time.Time"),
			gen.FieldRename("request_params_json", "RequestParamsJSON"),
			gen.FieldType("request_params_json", "[]byte"),
		),
		g.GenerateModelAs("market_data_sync_missing_items", "MarketDataSyncMissingItem",
			gen.FieldRename("ts_code", "TSCode"),
			gen.FieldType("security_master_id", "*int64"),
		),
		g.GenerateModelAs("recommendation_evaluation_runs", "RecommendationEvaluationRun",
			gen.FieldRename("request_params_json", "RequestParamsJSON"),
			gen.FieldType("request_params_json", "[]byte"),
			gen.FieldType("started_at", "*time.Time"),
			gen.FieldType("finished_at", "*time.Time"),
		),
		g.GenerateModelAs("recommendation_event_window_metrics", "RecommendationEventWindowMetric",
			gen.FieldRename("ts_code", "TSCode"),
			gen.FieldType("base_date", "*time.Time"),
			gen.FieldType("base_close_price", "*float64"),
			gen.FieldType("entry_date", "*time.Time"),
			gen.FieldType("entry_price", "*float64"),
			gen.FieldType("exit_date", "*time.Time"),
			gen.FieldType("exit_close_price", "*float64"),
			gen.FieldType("raw_return_ratio", "*float64"),
			gen.FieldType("direction_return_ratio", "*float64"),
			gen.FieldType("max_favorable_return_ratio", "*float64"),
			gen.FieldType("max_adverse_return_ratio", "*float64"),
			gen.FieldType("max_drawdown_ratio", "*float64"),
			gen.FieldType("win_flag", "*bool"),
			gen.FieldType("best_trade_date", "*time.Time"),
			gen.FieldType("worst_trade_date", "*time.Time"),
		),
		g.GenerateModelAs("scheduled_task_runs", "ScheduledTaskRun",
			gen.FieldType("task_type", "uint16"),
			gen.FieldRename("input_json", "InputJSON"),
			gen.FieldRename("output_json", "OutputJSON"),
			gen.FieldType("input_json", "[]byte"),
			gen.FieldType("output_json", "[]byte"),
			gen.FieldType("started_at", "*time.Time"),
			gen.FieldType("finished_at", "*time.Time"),
		),
		g.GenerateModelAs("external_document_ingestions", "ExternalDocumentIngestion",
			gen.FieldRename("source_path_hash", "SourcePathHash"),
			gen.FieldRename("content_sha256", "ContentSHA256"),
			gen.FieldType("remote_modified_at", "*time.Time"),
			gen.FieldType("document_id", "*int64"),
			gen.FieldType("downloaded_at", "*time.Time"),
			gen.FieldType("analyzed_at", "*time.Time"),
		),
		g.GenerateModelAs("trading_agent_runs", "TradingAgentRun",
			gen.FieldRename("request_json", "RequestJSON"),
			gen.FieldRename("raw_output_json", "RawOutputJSON"),
			gen.FieldType("request_json", "[]byte"),
			gen.FieldType("raw_output_json", "[]byte"),
			gen.FieldType("started_at", "*time.Time"),
			gen.FieldType("finished_at", "*time.Time"),
			gen.FieldType("claimed_at", "*time.Time"),
			gen.FieldType("claim_deadline", "*time.Time"),
			gen.FieldType("decision_completed_at", "*time.Time"),
		),
		g.GenerateModelAs("trading_intents", "TradingIntent",
			gen.FieldType("recommendation_event_id", "*int64"),
			gen.FieldType("candidate_plan_id", "*int64"),
			gen.FieldType("position_cycle_id", "*int64"),
			gen.FieldRename("ts_code", "TSCode"),
			gen.FieldType("proposed_limit_price", "*string"),
			gen.FieldType("proposed_position_ratio", "*string"),
			gen.FieldType("proposed_volume", "*int64"),
			gen.FieldType("confidence", "string"),
			gen.FieldRename("evidence_refs_json", "EvidenceRefsJSON"),
			gen.FieldRename("raw_intent_json", "RawIntentJSON"),
			gen.FieldType("evidence_refs_json", "[]byte"),
			gen.FieldType("raw_intent_json", "[]byte"),
			gen.FieldType("next_execution_at", "*time.Time"),
			gen.FieldType("execution_claimed_at", "*time.Time"),
			gen.FieldType("execution_claim_deadline", "*time.Time"),
			gen.FieldType("executed_at", "*time.Time"),
		),
		g.GenerateModelAs("trading_risk_checks", "TradingRiskCheck",
			gen.FieldRename("observed_json", "ObservedJSON"),
			gen.FieldRename("limit_json", "LimitJSON"),
			gen.FieldType("observed_json", "[]byte"),
			gen.FieldType("limit_json", "[]byte"),
		),
		g.GenerateModelAs("trading_orders", "TradingOrder",
			gen.FieldType("limit_price", "*string"),
			gen.FieldType("filled_vwap", "*string"),
			gen.FieldType("filled_amount", "string"),
			gen.FieldType("filled_commission", "string"),
			gen.FieldType("next_dispatch_at", "*time.Time"),
			gen.FieldType("submitted_at", "*time.Time"),
			gen.FieldType("last_event_at", "*time.Time"),
			gen.FieldType("finished_at", "*time.Time"),
			gen.FieldRename("request_json", "RequestJSON"),
			gen.FieldRename("latest_provider_payload_json", "LatestProviderPayloadJSON"),
			gen.FieldType("request_json", "[]byte"),
			gen.FieldType("latest_provider_payload_json", "[]byte"),
		),
		g.GenerateModelAs("trading_order_events", "TradingOrderEvent",
			gen.FieldType("filled_vwap", "*string"),
			gen.FieldRename("raw_payload_json", "RawPayloadJSON"),
			gen.FieldType("raw_payload_json", "[]byte"),
		),
		g.GenerateModelAs("trading_fills", "TradingFill",
			gen.FieldType("price", "string"),
			gen.FieldType("amount", "string"),
			gen.FieldType("commission", "string"),
			gen.FieldRename("commission_evidence_json", "CommissionEvidenceJSON"),
			gen.FieldType("commission_evidence_json", "[]byte"),
			gen.FieldType("commission_reconciled_at", "*time.Time"),
			gen.FieldRename("raw_payload_json", "RawPayloadJSON"),
			gen.FieldType("raw_payload_json", "[]byte"),
		),
		g.GenerateModelAs("trading_account_snapshots", "TradingAccountSnapshot",
			gen.FieldType("nav", "string"),
			gen.FieldType("balance", "string"),
			gen.FieldType("available_cash", "string"),
			gen.FieldType("frozen_cash", "string"),
			gen.FieldType("market_value", "string"),
			gen.FieldType("floating_pnl", "string"),
			gen.FieldType("cumulative_inout", "string"),
			gen.FieldType("cumulative_trade", "string"),
			gen.FieldType("cumulative_pnl", "string"),
			gen.FieldType("cumulative_commission", "string"),
			gen.FieldType("last_trade", "string"),
			gen.FieldType("last_pnl", "string"),
			gen.FieldType("last_commission", "string"),
			gen.FieldRename("raw_payload_json", "RawPayloadJSON"),
			gen.FieldType("raw_payload_json", "[]byte"),
		),
		g.GenerateModelAs("trading_position_snapshots", "TradingPositionSnapshot",
			gen.FieldType("vwap", "string"),
			gen.FieldType("last_price", "string"),
			gen.FieldType("market_value", "string"),
			gen.FieldType("floating_pnl", "string"),
			gen.FieldRename("raw_payload_json", "RawPayloadJSON"),
			gen.FieldType("raw_payload_json", "[]byte"),
		),
		g.GenerateModelAs("trading_reconciliation_runs", "TradingReconciliationRun",
			gen.FieldRename("request_json", "RequestJSON"),
			gen.FieldRename("summary_json", "SummaryJSON"),
			gen.FieldType("request_json", "[]byte"),
			gen.FieldType("summary_json", "[]byte"),
			gen.FieldType("finished_at", "*time.Time"),
		),
		g.GenerateModelAs("trading_reconciliation_diffs", "TradingReconciliationDiff",
			gen.FieldRename("local_value_json", "LocalValueJSON"),
			gen.FieldRename("provider_value_json", "ProviderValueJSON"),
			gen.FieldType("local_value_json", "[]byte"),
			gen.FieldType("provider_value_json", "[]byte"),
			gen.FieldType("resolved_at", "*time.Time"),
		),
		g.GenerateModelAs("trading_runtime_controls", "TradingRuntimeControl"),
		g.GenerateModelAs("trading_daily_sessions", "TradingDailySession",
			gen.FieldType("decision_run_id", "*int64"),
			gen.FieldType("opened_at", "*time.Time"),
			gen.FieldType("closed_at", "*time.Time"),
			gen.FieldRename("preflight_json", "PreflightJSON"),
			gen.FieldRename("summary_json", "SummaryJSON"),
			gen.FieldType("preflight_json", "[]byte"),
			gen.FieldType("summary_json", "[]byte"),
		),
		g.GenerateModelAs("trading_position_cycles", "TradingPositionCycle",
			gen.FieldRename("ts_code", "TSCode"),
			gen.FieldType("source_recommendation_event_id", "*int64"),
			gen.FieldType("source_buy_intent_id", "*int64"),
			gen.FieldType("entry_order_id", "*int64"),
			gen.FieldType("exit_order_id", "*int64"),
			gen.FieldType("entry_price", "string"),
			gen.FieldType("stop_loss_price", "string"),
			gen.FieldType("take_profit_price", "string"),
			gen.FieldType("closed_at", "*time.Time"),
			gen.FieldType("last_evaluated_at", "*time.Time"),
		),
		g.GenerateModelAs("trading_skill_decisions", "TradingSkillDecision",
			gen.FieldType("trading_intent_id", "*int64"),
			gen.FieldType("position_cycle_id", "*int64"),
			gen.FieldType("score", "string"),
			gen.FieldRename("input_json", "InputJSON"),
			gen.FieldRename("output_json", "OutputJSON"),
			gen.FieldType("input_json", "[]byte"),
			gen.FieldType("output_json", "[]byte"),
		),
		g.GenerateModelAs("trading_board_capabilities", "TradingBoardCapability",
			gen.FieldType("verified_at", "*time.Time"),
			gen.FieldRename("evidence_json", "EvidenceJSON"),
			gen.FieldType("evidence_json", "[]byte"),
		),
		g.GenerateModelAs("sector_trade_proxy_mappings", "SectorTradeProxyMapping",
			gen.FieldType("effective_to", "*time.Time"),
		),
	)
	g.Execute()
	logger.Info("gorm gen completed", "source", snapshot.Source, "out_path", queryOutPath, "model_path", "internal/domain/db_model")
}

func fatal(logger *slog.Logger, step string, err error) {
	if logger != nil {
		logger.Error("generate failed", "step", step, "error", err.Error())
	}
	_, _ = fmt.Fprintf(os.Stderr, "generate failed at %s: %v\n", step, err)
	os.Exit(1)
}
