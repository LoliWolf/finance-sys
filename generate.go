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
