package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"finance-sys/internal/approval"
	"finance-sys/internal/config"
	"finance-sys/internal/domain"
	"finance-sys/internal/evaluation"
	"finance-sys/internal/httpapi"
	"finance-sys/internal/llm"
	"finance-sys/internal/market"
	"finance-sys/internal/nacoscfg"
	"finance-sys/internal/parser"
	"finance-sys/internal/report"
	"finance-sys/internal/repository"
	"finance-sys/internal/rules"
	"finance-sys/internal/scheduler"
	"finance-sys/internal/service"
	"finance-sys/internal/storage"
	"finance-sys/internal/telemetry"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type App struct {
	Runtime           *config.Runtime
	Logger            *slog.Logger
	DB                *pgxpool.Pool
	Redis             *redis.Client
	Repository        *repository.Repository
	ObjectStorage     storage.ObjectStorage
	DocumentService   *service.DocumentService
	EvaluationService *service.EvaluationService
	ApprovalService   *approval.Service
	ReportService     *report.Service
	HTTPServer        *httpapi.Server
	Watcher           *nacoscfg.Watcher
	Reloader          *nacoscfg.Reloader
	Scheduler         *scheduler.Scheduler
}

func Build(ctx context.Context) (*App, error) {
	bootstrapLogger := telemetry.NewLogger("INFO")
	snapshot, loader, err := loadInitialSnapshot(ctx, bootstrapLogger)
	if err != nil {
		return nil, err
	}

	logger := telemetry.NewLogger(snapshot.Config.Logging.Level)
	runtime := config.NewRuntime(snapshot)

	db, err := openDB(ctx, snapshot.Config)
	if err != nil {
		return nil, err
	}
	repo := repository.New(db)

	redisClient := openRedis(snapshot.Config)

	objectStorage, err := storage.NewMinIOStorage(snapshot.Config.ObjectStorage)
	if err != nil {
		return nil, err
	}
	if err := objectStorage.EnsureBuckets(ctx); err != nil {
		logger.Warn("ensure object storage buckets", "error", err.Error())
	}

	if snapshot.Config.NacosClient.WriteConfigSnapshotToDB {
		_, _ = repo.InsertConfigSnapshot(ctx, &domain.ConfigSnapshot{
			ConfigVersion: snapshot.Config.Meta.ConfigVersion,
			Source:        snapshot.Source,
			SHA256:        snapshot.SHA256,
			RawJSON:       string(snapshot.Raw),
		})
	}

	parserService := parser.New()
	extractor := llm.NewMockExtractor()
	marketChain := market.NewChain(snapshot.Config.Market, snapshot.Config.ObjectStorage, objectStorage)
	ruleEngine := rules.New()
	documentService := service.NewDocumentService(repo, runtime, parserService, extractor, marketChain, ruleEngine, objectStorage, logger)
	evaluationService := service.NewEvaluationService(repo, runtime, marketChain, evaluation.New(), logger)
	approvalService := approval.NewService(repo)
	reportService := report.NewService(repo)
	var watcher *nacoscfg.Watcher
	var reloader *nacoscfg.Reloader
	if loader != nil {
		watcher = nacoscfg.NewWatcher(loader, runtime, repo, logger)
		reloader = nacoscfg.NewReloader(loader, runtime, repo)
	}
	httpServer := httpapi.NewServer(repo, runtime, documentService, evaluationService, approvalService, reportService, reloader)
	jobScheduler := scheduler.New(runtime, documentService, evaluationService, logger)
	if err := jobScheduler.Register(); err != nil {
		return nil, err
	}

	return &App{
		Runtime:           runtime,
		Logger:            logger,
		DB:                db,
		Redis:             redisClient,
		Repository:        repo,
		ObjectStorage:     objectStorage,
		DocumentService:   documentService,
		EvaluationService: evaluationService,
		ApprovalService:   approvalService,
		ReportService:     reportService,
		HTTPServer:        httpServer,
		Watcher:           watcher,
		Reloader:          reloader,
		Scheduler:         jobScheduler,
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

func openDB(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.Database.DSN)
	if err != nil {
		return nil, err
	}
	poolConfig.MaxConns = int32(cfg.Database.MaxOpenConns)
	poolConfig.MinConns = int32(cfg.Database.MaxIdleConns)
	poolConfig.MaxConnLifetime = time.Duration(cfg.Database.ConnMaxLifetimeMinutes) * time.Minute
	poolConfig.MaxConnIdleTime = time.Duration(cfg.Database.ConnMaxIdleTimeMinutes) * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, err
	}
	return pool, nil
}

func openRedis(cfg *config.Config) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:         cfg.Redis.Addr,
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		DialTimeout:  time.Duration(cfg.Redis.DialTimeoutMS) * time.Millisecond,
		ReadTimeout:  time.Duration(cfg.Redis.ReadTimeoutMS) * time.Millisecond,
		WriteTimeout: time.Duration(cfg.Redis.WriteTimeoutMS) * time.Millisecond,
	})
}
