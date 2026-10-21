package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	tradingdomain "finance-sys/internal/trading/domain"
)

type reconciliationDiffEvidence struct {
	Type     string          `json:"type"`
	Key      string          `json:"key"`
	Field    string          `json:"field"`
	Local    json.RawMessage `json:"local"`
	Provider json.RawMessage `json:"provider"`
}

type reconciliationSnapshotAttempt struct {
	SnapshotVersion string                       `json:"snapshot_version"`
	SnapshotAt      time.Time                    `json:"snapshot_at"`
	Diffs           []reconciliationDiffEvidence `json:"diffs"`
}

func newReconciliationSnapshotAttempt(snapshot *tradingdomain.ReconciliationSnapshot, diffs []db_model.TradingReconciliationDiff) reconciliationSnapshotAttempt {
	a := reconciliationSnapshotAttempt{SnapshotVersion: snapshot.SnapshotVersion, SnapshotAt: snapshot.Account.SnapshotAt, Diffs: []reconciliationDiffEvidence{}}
	for _, d := range diffs {
		a.Diffs = append(a.Diffs, reconciliationDiffEvidence{Type: d.DiffType, Key: d.EntityKey, Field: d.FieldName, Local: json.RawMessage(d.LocalValueJSON), Provider: json.RawMessage(d.ProviderValueJSON)})
	}
	return a
}

// Valuations come from separate provider reads, not an atomic source snapshot.
// Never widen tolerances, synthesize totals or retry a cash/quantity/fee/order fault.
func valuationOnlyDiffs(diffs []db_model.TradingReconciliationDiff) bool {
	if len(diffs) == 0 {
		return false
	}
	for _, d := range diffs {
		if d.DiffType != "MARKET_VALUE_DIFF" && d.DiffType != "NAV_DIFF" {
			return false
		}
	}
	return true
}

func reconciliationConfirmationSettings(cfg config.TradingReconciliationConfig) (int, time.Duration, time.Duration) {
	attempts, interval, seconds := cfg.ValuationMaxAttempts, cfg.ValuationRetryIntervalMS, cfg.RefreshTimeoutSeconds
	if attempts <= 0 {
		attempts = 3
	}
	if interval <= 0 {
		interval = 1000
	}
	if seconds <= 0 {
		seconds = 10
	}
	// Hard caps protect the 60s manual / 90s worker lease even with unvalidated config.
	if attempts > 5 {
		attempts = 5
	}
	if interval > 5000 {
		interval = 5000
	}
	if seconds > 40 {
		seconds = 40
	}
	return attempts, time.Duration(interval) * time.Millisecond, time.Duration(seconds) * time.Second
}

func waitReconciliationRetry(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}

func (s *Service) lockReconciliationActivity(ctx context.Context) error {
	for !s.accountActivity.TryLock() {
		if err := waitReconciliationRetry(ctx, 100*time.Millisecond); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) haltForReconciliation(ctx context.Context, run *db_model.TradingReconciliationRun, reason string) error {
	// Attempt both latches; neither failure may be silently reported as success.
	dbErr := dal.TradingRuntimeControls.SetKillSwitch(ctx, s.db, true, reason, "system", run.ConfigVersion)
	var bridgeErr error
	if s.bridge != nil {
		bridgeErr = s.bridge.SetKillSwitch(ctx, true, reason, hashParts("reconciliation-kill", run.RunKey))
	}
	return errors.Join(dbErr, bridgeErr)
}
