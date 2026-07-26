package service

import (
	"context"
	"log/slog"
	"os"
	"time"

	"finance-sys/internal/config"
)

type RecommendationEvaluationWorker struct {
	service  *RecommendationEvaluationService
	runtime  *config.Runtime
	logger   *slog.Logger
	workerID string
}

func NewRecommendationEvaluationWorker(service *RecommendationEvaluationService, runtime *config.Runtime, logger *slog.Logger) *RecommendationEvaluationWorker {
	host, _ := os.Hostname()
	if host == "" {
		host = "unknown-host"
	}
	return &RecommendationEvaluationWorker{
		service:  service,
		runtime:  runtime,
		logger:   logger,
		workerID: host + "-recommendation-evaluation-worker",
	}
}

func (w *RecommendationEvaluationWorker) Run(ctx context.Context) {
	if w == nil || w.service == nil || w.runtime == nil {
		return
	}
	interval := time.Second
	for {
		cfg := w.runtime.Config()
		if cfg != nil && cfg.Evaluation.Enabled && cfg.Evaluation.RecommendationPerformance.Enabled && cfg.Evaluation.RecommendationPerformance.AsyncWorker.Enabled {
			workerConfig := cfg.Evaluation.RecommendationPerformance.AsyncWorker
			if workerConfig.PollIntervalMS > 0 {
				interval = time.Duration(workerConfig.PollIntervalMS) * time.Millisecond
			}
			claimed, err := w.service.ClaimAndExecuteNext(ctx, w.workerID)
			if err != nil && w.logger != nil {
				w.logger.ErrorContext(ctx, "recommendation evaluation worker failed", "error", err.Error())
			}
			if claimed {
				continue
			}
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
