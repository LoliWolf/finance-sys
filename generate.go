package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
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

	g := gen.NewGenerator(gen.Config{
		OutPath:           "internal/dal/query",
		ModelPkgPath:      "internal/domain/db_model",
		FieldWithIndexTag: true,
		FieldWithTypeTag:  true,
		Mode:              gen.WithDefaultQuery | gen.WithQueryInterface,
	})
	g.UseDB(db)
	g.ApplyBasic(
		g.GenerateModelAs("config_snapshots", "ConfigSnapshot",
			gen.FieldRename("sha256", "Sha256"),
			gen.FieldRename("raw_json", "RawJson"),
			gen.FieldType("raw_json", "[]byte"),
		),
		g.GenerateModelAs("documents", "Document",
			gen.FieldRename("sha256", "Sha256"),
			gen.FieldRename("pdf_ocr_enabled", "PdfOcrEnabled"),
		),
		g.GenerateModelAs("parse_runs", "ParseRun",
			gen.FieldRename("chunks_json", "ChunksJson"),
			gen.FieldRename("raw_metadata_json", "RawMetadataJson"),
			gen.FieldType("chunks_json", "[]byte"),
			gen.FieldType("raw_metadata_json", "[]byte"),
		),
		g.GenerateModelAs("trade_candidate_plans", "TradeCandidatePlan",
			gen.FieldRename("risks_json", "RisksJson"),
			gen.FieldRename("evidence_json", "EvidenceJson"),
			gen.FieldType("risks_json", "[]byte"),
			gen.FieldType("evidence_json", "[]byte"),
		),
	)
	g.Execute()
	logger.Info("gorm gen completed", "source", snapshot.Source, "out_path", "internal/dal/query", "model_path", "internal/domain/db_model")
}

func fatal(logger *slog.Logger, step string, err error) {
	if logger != nil {
		logger.Error("generate failed", "step", step, "error", err.Error())
	}
	_, _ = fmt.Fprintf(os.Stderr, "generate failed at %s: %v\n", step, err)
	os.Exit(1)
}
