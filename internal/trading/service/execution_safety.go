package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	tradingdomain "finance-sys/internal/trading/domain"
	"gorm.io/gorm"
)

func (s *Service) fenceIntent(ctx context.Context, tx *gorm.DB, intent *db_model.TradingIntent) error {
	if err := dal.TradingRuntimeControls.Lock(ctx, tx); err != nil {
		return err
	}
	stored, err := dal.TradingIntents.QueryByID(ctx, tx, intent.ID)
	if err != nil {
		return err
	}
	if intent.ExecutionClaimToken != "" && (stored.ExecutionClaimToken != intent.ExecutionClaimToken || stored.ExecutionClaimDeadline == nil || !stored.ExecutionClaimDeadline.After(s.now())) {
		return ErrClaimLost
	}
	if stored.Status != "READY_FOR_EXECUTION" && stored.Status != "PROPOSED" {
		return ErrClaimLost
	}
	return nil
}

func uatKeyFromIntent(intent *db_model.TradingIntent) string {
	var refs []string
	_ = json.Unmarshal(intent.EvidenceRefsJSON, &refs)
	for _, ref := range refs {
		if strings.HasPrefix(ref, "uat:") {
			return strings.TrimPrefix(ref, "uat:")
		}
	}
	return ""
}

func uatAllowed(cfg config.TradingConfig, key, symbol string, now time.Time) bool {
	if !cfg.UAT.Enabled || cfg.Scheduler.Enabled || key == "" || key != cfg.UAT.Key {
		return false
	}
	expires, err := time.Parse(time.RFC3339, cfg.UAT.ExpiresAt)
	if err != nil || !expires.After(now) {
		return false
	}
	for _, allowed := range cfg.UAT.Symbols {
		if allowed == symbol {
			return true
		}
	}
	return false
}

func (s *Service) freshBridgeSnapshot(ctx context.Context, reason string) (*tradingdomain.ReconciliationSnapshot, error) {
	if s.bridge == nil {
		return nil, fmt.Errorf("Bridge unavailable")
	}
	cfg := s.runtime.Config()
	if cfg == nil {
		return nil, fmt.Errorf("config unavailable")
	}
	health, err := s.bridge.Health(ctx)
	if err != nil {
		return nil, err
	}
	if health.Runner != "READY" || health.Terminal != "CONNECTED" || health.AuthState != "AUTH_OK" || health.AccountID != cfg.Trading.Bridge.ExpectedAccountID {
		return nil, fmt.Errorf("Bridge prerequisites not ready")
	}
	baseline, _ := s.bridge.ReconciliationSnapshot(ctx, "")
	requested := s.now()
	if _, err := s.bridge.RefreshSnapshot(ctx, hashParts(reason, requested.String())); err != nil {
		return nil, err
	}
	wait, maxAge := bridgeSnapshotWaitSettings(cfg)
	timer := time.NewTimer(wait)
	defer timer.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		value, err := s.bridge.ReconciliationSnapshot(ctx, "")
		if err == nil && value != nil && value.Account.AccountID == cfg.Trading.Bridge.ExpectedAccountID && value.Account.Environment == "SIMULATION" && !value.Account.SnapshotAt.IsZero() && s.now().Sub(value.Account.SnapshotAt) <= maxAge && !value.Account.SnapshotAt.After(s.now().Add(time.Second)) && (baseline == nil || value.SnapshotVersion != baseline.SnapshotVersion) {
			value.Health = *health
			return value, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return nil, errors.New("fresh account snapshot timed out")
		case <-ticker.C:
		}
	}
}

func (s *Service) requireNoRunningReconciliation(ctx context.Context, db *gorm.DB, account string) error {
	running, err := dal.TradingReconciliationRuns.HasRunning(ctx, db, account)
	if err != nil {
		return err
	}
	if running {
		return fmt.Errorf("%w: account reconciliation in progress", ErrExecutionDeferred)
	}
	return nil
}
