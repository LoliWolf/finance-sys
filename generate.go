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
		g.GenerateModelAs("recommendation_events", "RecommendationEvent"),
		g.GenerateModelAs("recommendation_event_evidences", "RecommendationEventEvidence"),
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
