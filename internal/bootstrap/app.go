package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"finance-sys/internal/agentclient"
	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	"finance-sys/internal/httpapi"
	"finance-sys/internal/llm"
	"finance-sys/internal/nacoscfg"
	"finance-sys/internal/parser"
	"finance-sys/internal/rules"
	"finance-sys/internal/service"
	"finance-sys/internal/telemetry"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type App struct {
	Runtime         *config.Runtime
	Logger          *slog.Logger
	DB              *gorm.DB
	DocumentService *service.DocumentService
	SecurityService *service.SecurityService
	HTTPServer      *httpapi.Server
	Watcher         *nacoscfg.Watcher
	Reloader        *nacoscfg.Reloader
}

func (a *App) Close() error {
	if a == nil || a.DB == nil {
		return nil
	}
	sqlDB, err := a.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func Build(ctx context.Context) (*App, error) {
	bootstrapLogger := telemetry.NewLogger(string(config.LogLevelInfo))
	bootstrapLogger.Info("bootstrap build start")
	snapshot, loader, err := LoadInitialSnapshot(ctx, bootstrapLogger)
	if err != nil {
		bootstrapLogger.Error("bootstrap load initial snapshot failed", "error", err.Error())
		return nil, err
	}

	logger := telemetry.NewLogger(string(snapshot.Config.Logging.Level))
	runtime := config.NewRuntime(snapshot)
	logger.Info("bootstrap runtime initialized", "config_version", snapshot.Config.Meta.ConfigVersion, "config_source", snapshot.Source)

	db, err := openDB(ctx, snapshot.Config)
	if err != nil {
		logger.Error("bootstrap open db failed", "error", err.Error())
		return nil, err
	}
	logger.Info("bootstrap db connected")

	if snapshot.Config.NacosClient.WriteConfigSnapshotToDB {
		_ = dal.ConfigSnapshots.Create(ctx, db, &db_model.ConfigSnapshot{
			ConfigVersion: snapshot.Config.Meta.ConfigVersion,
			Source:        snapshot.Source,
			Sha256:        snapshot.SHA256,
			RawJSON:       snapshot.Raw,
		})
	}

	parserService := parser.New(logger)
	llmAnalyzer := llm.NewModelAnalyzer(runtime, logger)
	agentAnalyzer := agentclient.NewAnalyzer(runtime, logger)
	analyzer := service.NewAnalysisRouter(runtime, llmAnalyzer, agentAnalyzer, logger)
	ruleEngine := rules.New(logger)
	securityService := service.NewSecurityService(db, logger)
	candidateAssembler := service.NewCandidateAssembler(securityService, logger)
	documentService := service.NewDocumentService(db, runtime, parserService, analyzer, candidateAssembler, ruleEngine, logger)

	var watcher *nacoscfg.Watcher
	var reloader *nacoscfg.Reloader
	if loader != nil {
		watcher = nacoscfg.NewWatcher(loader, runtime, db, logger)
		reloader = nacoscfg.NewReloader(loader, runtime, db, logger)
	}

	httpServer := httpapi.NewServer(db, runtime, documentService, securityService, reloader, logger)
	logger.Info("bootstrap build completed")
	return &App{
		Runtime:         runtime,
		Logger:          logger,
		DB:              db,
		DocumentService: documentService,
		SecurityService: securityService,
		HTTPServer:      httpServer,
		Watcher:         watcher,
		Reloader:        reloader,
	}, nil
}

func LoadInitialSnapshot(ctx context.Context, logger *slog.Logger) (*config.Snapshot, *nacoscfg.Loader, error) {
	bootstrapCfg, err := LoadNacosBootstrapFromEnv()
	if err == nil {
		loader := nacoscfg.NewLoader(bootstrapCfg, logger)
		snapshot, loadErr := loader.Load(ctx, false, false)
		if loadErr == nil {
			return snapshot, loader, nil
		}
		return nil, nil, fmt.Errorf("load nacos config: %w", loadErr)
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

func openDB(ctx context.Context, cfg *config.Config) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(cfg.Database.DSN), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(time.Duration(cfg.Database.ConnMaxLifetimeMinutes) * time.Minute)
	sqlDB.SetConnMaxIdleTime(time.Duration(cfg.Database.ConnMaxIdleTimeMinutes) * time.Minute)
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}
