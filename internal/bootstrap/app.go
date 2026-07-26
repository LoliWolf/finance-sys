package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"finance-sys/internal/agentclient"
	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	"finance-sys/internal/httpapi"
	"finance-sys/internal/llm"
	"finance-sys/internal/marketdata"
	"finance-sys/internal/nacoscfg"
	"finance-sys/internal/parser"
	"finance-sys/internal/rules"
	"finance-sys/internal/service"
	"finance-sys/internal/stats"
	"finance-sys/internal/telemetry"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type App struct {
	Runtime           *config.Runtime
	Logger            *slog.Logger
	DB                *gorm.DB
	DocumentService   *service.DocumentService
	SecurityService   *service.SecurityService
	MarketDataService *service.MarketDataService
	MarketDataWorker  *service.MarketDataWorker
	EvaluationService *service.RecommendationEvaluationService
	EvaluationWorker  *service.RecommendationEvaluationWorker
	ScheduledTasks    *service.ScheduledTaskService
	ExternalDocuments *service.ExternalDocumentIngestionService
	Scheduler         *service.AutomaticScheduler
	StatsService      *stats.Service
	HTTPServer        *httpapi.Server
	Watcher           *nacoscfg.Watcher
	Reloader          *nacoscfg.Reloader
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
	logger.Info("bootstrap db connected", "database_profile", snapshot.Config.SelectedDatabaseProfile)

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
	processingPools, err := service.NewProcessingPools(snapshot.Config.Processing.OCRMaxConcurrency, snapshot.Config.Processing.LLMMaxConcurrency)
	if err != nil {
		_ = dbConnectionClose(db)
		return nil, err
	}
	documentService := service.NewDocumentService(db, runtime, parserService, analyzer, candidateAssembler, ruleEngine, processingPools, logger)
	marketDataHTTPClient := &http.Client{}
	if snapshot.Config.MarketData.Tushare.TimeoutMS > 0 {
		marketDataHTTPClient.Timeout = time.Duration(snapshot.Config.MarketData.Tushare.TimeoutMS) * time.Millisecond
	}
	marketDataProvider := marketdata.NewTushareProvider(marketDataHTTPClient)
	marketDataService := service.NewMarketDataService(db, runtime, marketDataProvider, logger)
	marketDataWorker := service.NewMarketDataWorker(marketDataService, runtime, logger)
	evaluationService := service.NewRecommendationEvaluationService(db, runtime, logger)
	evaluationWorker := service.NewRecommendationEvaluationWorker(evaluationService, runtime, logger)
	scheduledTasks := service.NewScheduledTaskService(db, runtime, logger)
	externalDocuments := service.NewExternalDocumentIngestionService(db, runtime, documentService, logger)
	automaticScheduler, err := service.NewAutomaticScheduler(scheduledTasks, marketDataService, evaluationService, externalDocuments, runtime, logger)
	if err != nil {
		_ = dbConnectionClose(db)
		return nil, err
	}
	statsService := stats.NewService(db, runtime)

	var watcher *nacoscfg.Watcher
	var reloader *nacoscfg.Reloader
	if loader != nil {
		watcher = nacoscfg.NewWatcher(loader, runtime, db, logger)
		reloader = nacoscfg.NewReloader(loader, runtime, db, logger)
	}

	httpServer := httpapi.NewServer(db, runtime, documentService, securityService, marketDataService, evaluationService, statsService, reloader, logger)
	logger.Info("bootstrap build completed")
	return &App{
		Runtime:           runtime,
		Logger:            logger,
		DB:                db,
		DocumentService:   documentService,
		SecurityService:   securityService,
		MarketDataService: marketDataService,
		MarketDataWorker:  marketDataWorker,
		EvaluationService: evaluationService,
		EvaluationWorker:  evaluationWorker,
		ScheduledTasks:    scheduledTasks,
		ExternalDocuments: externalDocuments,
		Scheduler:         automaticScheduler,
		StatsService:      statsService,
		HTTPServer:        httpServer,
		Watcher:           watcher,
		Reloader:          reloader,
	}, nil
}

func dbConnectionClose(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func LoadInitialSnapshot(ctx context.Context, logger *slog.Logger) (*config.Snapshot, *nacoscfg.Loader, error) {
	bootstrapCfg, err := LoadNacosBootstrapFromEnv()
	if err == nil {
		loader := nacoscfg.NewLoader(bootstrapCfg, logger)
		snapshot, loadErr := loader.Load(ctx, false, false)
		if loadErr == nil {
			return snapshot, loader, nil
		}
		if logger != nil {
			logger.WarnContext(ctx, "load nacos config failed; using local example", "error", loadErr.Error())
		}
	} else if logger != nil {
		logger.InfoContext(ctx, "nacos address unavailable; using local example", "error", err.Error())
	}

	snapshot, fallbackErr := loadLocalExampleSnapshot()
	if fallbackErr != nil {
		if err == nil {
			return nil, nil, fmt.Errorf("load nacos config and local example fallback: %w", fallbackErr)
		}
		return nil, nil, fmt.Errorf("load local example fallback after nacos bootstrap error %v: %w", err, fallbackErr)
	}
	return snapshot, nil, nil
}

func loadLocalExampleSnapshot() (*config.Snapshot, error) {
	path, err := findLocalExampleConfig()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load local config fallback: %w", err)
	}
	var cfg config.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("decode local config fallback: %w", err)
	}
	if err := config.Validate(&cfg); err != nil {
		return nil, err
	}
	config.ApplyRuntimeEnvironment(&cfg, os.Getenv(config.FinanceSysEnvironmentVariable))
	return config.NewSnapshot(&cfg, raw, "local_example", time.Now())
}

func findLocalExampleConfig() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	for {
		candidate := filepath.Join(dir, "configs", "example_nacos_config.json")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("configs/example_nacos_config.json was not found from the working directory or its parents")
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
