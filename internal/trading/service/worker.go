package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	"gorm.io/gorm"
)

var ErrExecutionDeferred = errors.New("execution deferred")
var ErrClaimLost = errors.New("claim lost")

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
	return &Worker{service: service, runtime: runtime, logger: logger, workerID: host + "-trading", now: time.Now}
}

func (w *Worker) Run(ctx context.Context) {
	if w == nil || w.service == nil || w.runtime == nil {
		return
	}
	var wg sync.WaitGroup
	for _, lane := range []string{"DECISION", "EXECUTION", "RECONCILIATION", "EXIT"} {
		wg.Add(1)
		go func(lane string) { defer wg.Done(); w.loop(ctx, lane) }(lane)
	}
	wg.Wait()
}

func (w *Worker) loop(ctx context.Context, lane string) {
	for ctx.Err() == nil {
		cfg := w.runtime.Config()
		interval := 500 * time.Millisecond
		if cfg == nil {
			return
		}
		var err error
		switch lane {
		case "DECISION":
			if cfg.Trading.Agent.Enabled {
				err = w.service.ProcessNextDecision(ctx, w.workerID+"-decision")
			}
		case "RECONCILIATION":
			err = w.service.ProcessNextReconciliation(ctx, w.workerID+"-reconcile")
		case "EXECUTION":
			if cfg.Trading.Scheduler.Enabled {
				err = dal.TradingIntents.Expire(ctx, w.service.db, w.now())
				if err == nil {
					err = w.service.ExecuteNextReadyIntent(ctx, w.workerID+"-execute", w.now())
				}
			}
		case "EXIT":
			if cfg.Trading.Exit.MonitorIntervalSeconds > 0 {
				interval = time.Duration(cfg.Trading.Exit.MonitorIntervalSeconds) * time.Second
			}
			if cfg.Trading.Scheduler.Enabled && cfg.Trading.Exit.Enabled && tradingSessionOpen(w.now(), cfg.Trading) {
				_, err = w.service.ScanExits(ctx, w.now())
			}
		}
		if err != nil && !errors.Is(err, dal.ErrNotFound) && !errors.Is(err, ErrExecutionDeferred) && w.logger != nil {
			w.logger.ErrorContext(ctx, "trading worker lane failed", "lane", lane, "error", err.Error())
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

func (s *Service) ProcessNextDecision(ctx context.Context, worker string) error {
	cfg := s.runtime.Config()
	if cfg == nil {
		return fmt.Errorf("config unavailable")
	}
	now := s.now()
	lease := time.Duration((cfg.Trading.Agent.TimeoutMS+5000)*(cfg.Trading.Agent.MaxRetries+1)) * time.Millisecond
	if lease < time.Minute {
		lease = time.Minute
	}
	var run *db_model.TradingAgentRun
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := dal.TradingRuntimeControls.Lock(ctx, tx); e != nil {
			return e
		}
		var e error
		run, e = dal.TradingAgentRuns.Claim(ctx, tx, worker, hashParts(worker, now.String()), now, now.Add(lease))
		return e
	})
	if err != nil {
		return err
	}
	runCtx, cancel := context.WithTimeout(ctx, lease-5*time.Second)
	defer cancel()
	_, err = s.decideRun(runCtx, run)
	if err != nil {
		_, _ = dal.TradingAgentRuns.FinishClaim(ctx, s.db, run.ID, run.ClaimToken, s.now(), map[string]any{"status": "FAILED", "finished_at": s.now(), "error_code": "DECISION_FAILED", "error_message": err.Error()})
	}
	return err
}

func (s *Service) ExecuteNextReadyIntent(ctx context.Context, worker string, now time.Time) error {
	cfg := s.runtime.Config()
	if cfg == nil {
		return fmt.Errorf("config unavailable")
	}
	if !cfg.Trading.Scheduler.Enabled || !cfg.Trading.Enabled || cfg.Trading.KillSwitch || !tradingSessionOpen(now, cfg.Trading) {
		return dal.ErrNotFound
	}
	if err := s.requireReadySession(ctx, now); err != nil {
		return fmt.Errorf("%w: %v", ErrExecutionDeferred, err)
	}
	control, err := dal.TradingRuntimeControls.KillSwitch(ctx, s.db)
	if err != nil {
		return err
	}
	if control.Enabled {
		return dal.ErrNotFound
	}
	lease := time.Duration(cfg.Trading.Execution.CommandClaimTimeoutMS) * time.Millisecond
	if lease < time.Minute {
		lease = time.Minute
	}
	var intent *db_model.TradingIntent
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := dal.TradingRuntimeControls.Lock(ctx, tx); e != nil {
			return e
		}
		var e error
		intent, e = dal.TradingIntents.ClaimNextReady(ctx, tx, worker, hashParts(worker, now.String()), now, now.Add(lease))
		return e
	})
	if err != nil {
		return err
	}
	executeCtx, cancel := context.WithTimeout(ctx, lease-5*time.Second)
	defer cancel()
	err = s.evaluateIntent(executeCtx, intent, false)
	values := map[string]any{"execution_status": "COMPLETED", "execution_attempt_count": intent.ExecutionAttemptCount + 1, "executed_at": s.now(), "execution_claim_token": "", "execution_claimed_by": "", "execution_claim_deadline": nil}
	if err != nil {
		if errors.Is(err, ErrClaimLost) {
			return err
		}
		values["execution_status"] = "RETRY"
		values["next_execution_at"] = s.now().Add(5 * time.Second)
		values["rejection_message"] = err.Error()
		values["rejection_code"] = "EXECUTION_DEFERRED"
	}
	ok, finishErr := dal.TradingIntents.FinishClaim(ctx, s.db, intent.ID, intent.ExecutionClaimToken, s.now(), values)
	if finishErr != nil {
		return finishErr
	}
	if !ok {
		return ErrClaimLost
	}
	return err
}

func (s *Service) HasOpenPositionCycles(ctx context.Context) (bool, error) {
	cfg := s.runtime.Config()
	if cfg == nil {
		return false, nil
	}
	c, e := dal.TradingPositionCycles.ListOpen(ctx, s.db, cfg.Trading.Bridge.ExpectedAccountID)
	return len(c) > 0, e
}

func tradingSessionOpen(now time.Time, cfg config.TradingConfig) bool {
	open, err := cfg.Calendar.IsOpen(now)
	return err == nil && open && sessionWindowOpen(now.In(config.ChinaLocation), cfg.Scheduler)
}

func (s *Service) requireReadySession(ctx context.Context, now time.Time) error {
	cfg := s.runtime.Config()
	if cfg == nil {
		return fmt.Errorf("config unavailable")
	}
	session, err := dal.TradingDailySessions.ByDate(ctx, s.db, cfg.Trading.Bridge.ExpectedAccountID, tradingDay(now))
	if err != nil {
		return err
	}
	if session.PreflightStatus != "READY" || session.Status == "BLOCKED" || session.Status == "CLOSED" || session.ConfigVersion != cfg.Meta.ConfigVersion {
		return fmt.Errorf("daily session is not ready for current config")
	}
	return nil
}
