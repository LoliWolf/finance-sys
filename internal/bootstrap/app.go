package bootstrap

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/domain"
	"finance-sys/internal/httpapi"
	"finance-sys/internal/llm"
	"finance-sys/internal/nacoscfg"
	"finance-sys/internal/parser"
	"finance-sys/internal/repository"
	"finance-sys/internal/rules"
	"finance-sys/internal/service"
	"finance-sys/internal/telemetry"

	_ "github.com/go-sql-driver/mysql"
)

type App struct {
	Runtime         *config.Runtime
	Logger          *slog.Logger
	DB              *sql.DB
	Repository      *repository.Repository
	DocumentService *service.DocumentService
	HTTPServer      *httpapi.Server
	Watcher         *nacoscfg.Watcher
	Reloader        *nacoscfg.Reloader
}

func Build(ctx context.Context) (*App, error) {
	bootstrapLogger := telemetry.NewLogger("INFO")
	bootstrapLogger.Info("bootstrap build start")
	snapshot, loader, err := loadInitialSnapshot(ctx, bootstrapLogger)
	if err != nil {
		bootstrapLogger.Error("bootstrap load initial snapshot failed", "error", err.Error())
		return nil, err
	}

	logger := telemetry.NewLogger(snapshot.Config.Logging.Level)
	runtime := config.NewRuntime(snapshot)
	logger.Info("bootstrap runtime initialized", "config_version", snapshot.Config.Meta.ConfigVersion, "config_source", snapshot.Source)

	db, err := openDB(ctx, snapshot.Config)
	if err != nil {
		logger.Error("bootstrap open db failed", "error", err.Error())
		return nil, err
	}
	logger.Info("bootstrap db connected")
	repo := repository.New(db, logger)

	if snapshot.Config.NacosClient.WriteConfigSnapshotToDB {
		_, _ = repo.InsertConfigSnapshot(ctx, &domain.ConfigSnapshot{
			ConfigVersion: snapshot.Config.Meta.ConfigVersion,
			Source:        snapshot.Source,
			SHA256:        snapshot.SHA256,
			RawJSON:       string(snapshot.Raw),
		})
	}

	parserService := parser.New(logger)
	analyzer := llm.NewModelAnalyzer(runtime, logger)
	ruleEngine := rules.New(logger)
	documentService := service.NewDocumentService(repo, runtime, parserService, analyzer, ruleEngine, logger)

	var watcher *nacoscfg.Watcher
	var reloader *nacoscfg.Reloader
	if loader != nil {
		watcher = nacoscfg.NewWatcher(loader, runtime, repo, logger)
		reloader = nacoscfg.NewReloader(loader, runtime, repo, logger)
	}

	httpServer := httpapi.NewServer(repo, runtime, documentService, reloader, logger)
	logger.Info("bootstrap build completed")
	return &App{
		Runtime:         runtime,
		Logger:          logger,
		DB:              db,
		Repository:      repo,
		DocumentService: documentService,
		HTTPServer:      httpServer,
		Watcher:         watcher,
		Reloader:        reloader,
	}, nil
}

func loadInitialSnapshot(ctx context.Context, logger *slog.Logger) (*config.Snapshot, *nacoscfg.Loader, error) {
	bootstrapCfg, err := LoadNacosBootstrapFromEnv()
	if err == nil {
		loader := nacoscfg.NewLoader(bootstrapCfg, logger)
		snapshot, loadErr := loader.Load(ctx, true, false)
		if loadErr == nil {
			return snapshot, loader, nil
		}
		logger.Warn("falling back to local example config", "error", loadErr.Error())
	}

	raw, err := os.ReadFile("configs/example_nacos_config.json")
	if err != nil {
		return nil, nil, fmt.Errorf("load local config fallback: %w", err)
	}
	var cfg config.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, nil, err
	}
	if err := config.Validate(&cfg); err != nil {
		return nil, nil, err
	}
	snapshot, err := config.NewSnapshot(&cfg, raw, "local_example", time.Now())
	return snapshot, nil, err
}

func openDB(ctx context.Context, cfg *config.Config) (*sql.DB, error) {
	db, err := sql.Open("mysql", cfg.Database.DSN)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	db.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	db.SetConnMaxLifetime(time.Duration(cfg.Database.ConnMaxLifetimeMinutes) * time.Minute)
	db.SetConnMaxIdleTime(time.Duration(cfg.Database.ConnMaxIdleTimeMinutes) * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}
