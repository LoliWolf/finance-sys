package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	"gorm.io/gorm"
)

func (s *Service) QueueReconciliation(ctx context.Context, runType string, at time.Time) (*db_model.TradingReconciliationRun, error) {
	cfg := s.runtime.Config()
	if cfg == nil {
		return nil, fmt.Errorf("config unavailable")
	}
	if at.IsZero() {
		at = s.now()
	}
	if runType == "" {
		runType = "MANUAL"
	}
	key := hashParts("reconciliation", cfg.Trading.Bridge.ExpectedAccountID, runType, at.UTC().Truncate(time.Minute).Format(time.RFC3339), fmt.Sprint(cfg.Meta.ConfigVersion))
	if previous, e := dal.TradingReconciliationRuns.ByKey(ctx, s.db, key); e == nil {
		return previous, nil
	}
	run := db_model.TradingReconciliationRun{RunKey: key, RunType: runType, Status: "QUEUED", AccountID: cfg.Trading.Bridge.ExpectedAccountID, RequestJSON: marshalJSON(map[string]any{"run_type": runType, "at": at}), SummaryJSON: []byte(`{}`), StartedAt: at, ConfigVersion: cfg.Meta.ConfigVersion}
	if err := dal.TradingReconciliationRuns.Create(ctx, s.db, &run); err != nil {
		if previous, e := dal.TradingReconciliationRuns.ByKey(ctx, s.db, key); e == nil {
			return previous, nil
		}
		return nil, err
	}
	return &run, nil
}

func (s *Service) ProcessNextReconciliation(ctx context.Context, worker string) error {
	now := s.now()
	var run *db_model.TradingReconciliationRun
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := dal.TradingRuntimeControls.Lock(ctx, tx); e != nil {
			return e
		}
		var e error
		run, e = dal.TradingReconciliationRuns.Claim(ctx, tx, worker, hashParts(worker, now.String()), now, now.Add(90*time.Second))
		return e
	})
	if err != nil {
		return err
	}
	runCtx, cancel := context.WithTimeout(ctx, 80*time.Second)
	defer cancel()
	if run.RunType == "PREFLIGHT" || strings.HasPrefix(run.RunType, "CANCEL_") {
		var output any
		if run.RunType == "PREFLIGHT" {
			output, err = s.Preflight(runCtx, s.now())
		} else {
			output, err = s.CancelOpenOrders(runCtx, run.RunType)
		}
		status := "SUCCEEDED"
		message := ""
		if err != nil {
			status = "FAILED"
			message = err.Error()
		}
		ok, e := dal.TradingReconciliationRuns.FinishClaim(ctx, s.db, run.ID, run.ClaimToken, s.now(), map[string]any{"status": status, "summary_json": marshalJSON(output), "error_message": message, "finished_at": s.now()})
		if e != nil {
			return e
		}
		if !ok {
			return ErrClaimLost
		}
		return err
	}
	if run.RunType == "EOD" {
		_, err = s.CancelOpenOrders(runCtx, "EOD")
		if err != nil {
			_, _ = s.failReconciliation(ctx, run, "EOD_CANCEL_FAILED", err.Error())
			_ = s.closeTradingDay(ctx, now, false, err.Error())
			return err
		}
	}
	completed, err := s.performReconciliation(runCtx, run)
	if run.RunType == "EOD" {
		message := ""
		if err != nil {
			message = err.Error()
		}
		return s.closeTradingDay(ctx, now, err == nil && completed != nil && completed.Status == "SUCCEEDED", message)
	}
	return err
}

func (s *Service) GetReconciliation(ctx context.Context, id int64) (*db_model.TradingReconciliationRun, error) {
	return dal.TradingReconciliationRuns.ByID(ctx, s.db, id)
}

func (s *Service) closeTradingDay(ctx context.Context, at time.Time, success bool, message string) error {
	cfg := s.runtime.Config()
	if cfg == nil {
		return fmt.Errorf("config unavailable")
	}
	session, err := dal.TradingDailySessions.ByDate(ctx, s.db, cfg.Trading.Bridge.ExpectedAccountID, tradingDay(at))
	if err != nil {
		return err
	}
	open, err := dal.TradingOrders.QueryOpen(ctx, s.db)
	if err != nil {
		return err
	}
	status := "CLOSED"
	if !success || len(open) > 0 {
		status = "BLOCKED"
		if message == "" {
			message = "unresolved reconciliation or open orders"
		}
	}
	return dal.TradingDailySessions.Update(ctx, s.db, session.ID, map[string]any{"status": status, "closed_at": s.now(), "summary_json": marshalJSON(map[string]any{"open_orders": len(open), "reconciled": success}), "error_message": message})
}
