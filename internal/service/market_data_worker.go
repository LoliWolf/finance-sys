package service

import (
	"context"
	"log/slog"
	"os"
	"time"

	"finance-sys/internal/config"
)

type MarketDataWorker struct {
	service  *MarketDataService
	runtime  *config.Runtime
	logger   *slog.Logger
	workerID string
}

func NewMarketDataWorker(service *MarketDataService, runtime *config.Runtime, logger *slog.Logger) *MarketDataWorker {
	host, _ := os.Hostname()
	if host == "" {
		host = "unknown-host"
	}
	return &MarketDataWorker{
		service:  service,
		runtime:  runtime,
		logger:   logger,
		workerID: host + "-market-data-worker",
	}
}

func (w *MarketDataWorker) Run(ctx context.Context) {
	if w == nil || w.service == nil || w.runtime == nil {
		return
	}
	interval := time.Second
	for {
		cfg := w.runtime.Config()
		if cfg != nil && cfg.MarketData.Enabled && cfg.MarketData.AsyncWorker.Enabled {
			if cfg.MarketData.AsyncWorker.PollIntervalMS > 0 {
				interval = time.Duration(cfg.MarketData.AsyncWorker.PollIntervalMS) * time.Millisecond
			}
			claimed, err := w.service.ClaimAndExecuteNextStockDailyRun(ctx, w.workerID)
			if err != nil && w.logger != nil {
				w.logger.ErrorContext(ctx, "market data worker failed", "error", err.Error())
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
