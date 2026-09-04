package service

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/dal"
)

type Worker struct {
	service  *Service
	runtime  *config.Runtime
	logger   *slog.Logger
	workerID string
	now      func() time.Time
}

func NewWorker(service *Service, runtime *config.Runtime, logger *slog.Logger) *Worker {
	host, _ := os.Hostname()
	if host == "" {
		host = "unknown-host"
	}
	return &Worker{service: service, runtime: runtime, logger: logger, workerID: host + "-trading-worker", now: time.Now}
}

func (w *Worker) Run(ctx context.Context) {
	if w == nil || w.service == nil || w.runtime == nil {
		return
	}
	lastExitRun := time.Time{}
	for {
		cfg := w.runtime.Config()
		interval := 500 * time.Millisecond
		if cfg != nil && cfg.Trading.Execution.CommandPollIntervalMS > 0 {
			interval = time.Duration(cfg.Trading.Execution.CommandPollIntervalMS) * time.Millisecond
		}
		now := w.now()
		if cfg != nil && cfg.Trading.Scheduler.Enabled && cfg.Trading.Enabled && !cfg.Trading.KillSwitch {
			if sessionWindowOpen(now, cfg.Trading.Scheduler) {
				if err := w.service.ExecuteNextReadyIntent(ctx, w.workerID, now); err != nil && !errors.Is(err, dal.ErrNotFound) && w.logger != nil {
					w.logger.ErrorContext(ctx, "execute claimed trading intent failed", "error", err.Error())
				}
				if cfg.Trading.Exit.Enabled && (lastExitRun.IsZero() || now.Sub(lastExitRun) >= time.Duration(cfg.Trading.Exit.MonitorIntervalSeconds)*time.Second) {
					lastExitRun = now
					if hasCycles, err := w.service.HasOpenPositionCycles(ctx); err == nil && hasCycles {
						if _, err := w.service.StartRun(ctx, RunRequest{TriggerType: "EXIT_MONITOR", AsOfTime: now}); err != nil && w.logger != nil {
							w.logger.ErrorContext(ctx, "trading exit monitor decision failed", "error", err.Error())
						}
					}
				}
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

func (s *Service) ExecuteNextReadyIntent(ctx context.Context, workerID string, now time.Time) error {
	cfg := s.runtime.Config()
	if cfg == nil {
		return errors.New("runtime config unavailable")
	}
	if !sessionWindowOpen(now, cfg.Trading.Scheduler) {
		return dal.ErrNotFound
	}
	control, err := dal.TradingRuntimeControls.KillSwitch(ctx, s.db)
	if err != nil || control.Enabled {
		return dal.ErrNotFound
	}
	claimToken := hashParts(workerID, now.Format(time.RFC3339Nano))
	deadline := now.Add(time.Duration(cfg.Trading.Execution.CommandClaimTimeoutMS) * time.Millisecond)
	intent, err := dal.TradingIntents.ClaimNextReady(ctx, s.db, workerID, claimToken, now, deadline)
	if err != nil {
		return err
	}
	err = s.evaluateIntent(ctx, intent, false)
	executedAt := time.Now()
	if err == nil {
		return dal.TradingIntents.Update(ctx, s.db, intent.ID, map[string]any{
			"execution_status": "COMPLETED", "execution_attempt_count": int(intent.ExecutionAttemptCount) + 1, "executed_at": executedAt,
			"execution_claim_token": "", "execution_claimed_by": "", "execution_claim_deadline": nil,
		})
	}
	attempt := int(intent.ExecutionAttemptCount) + 1
	status := "RETRY"
	next := executedAt.Add(time.Duration(attempt) * time.Second)
	if attempt > cfg.Trading.Execution.DispatchMaxRetries {
		status = "FAILED"
	}
	updateErr := dal.TradingIntents.Update(ctx, s.db, intent.ID, map[string]any{
		"execution_status": status, "execution_attempt_count": attempt, "next_execution_at": next,
		"execution_claim_token": "", "execution_claimed_by": "", "execution_claim_deadline": nil,
		"rejection_code": "EXECUTION_FAILED", "rejection_message": err.Error(),
	})
	if updateErr != nil {
		return updateErr
	}
	return err
}

func (s *Service) HasOpenPositionCycles(ctx context.Context) (bool, error) {
	cfg := s.runtime.Config()
	if cfg == nil || cfg.Trading.Bridge.ExpectedAccountID == "" {
		return false, nil
	}
	cycles, err := dal.TradingPositionCycles.ListOpen(ctx, s.db, cfg.Trading.Bridge.ExpectedAccountID)
	return len(cycles) > 0, err
}
