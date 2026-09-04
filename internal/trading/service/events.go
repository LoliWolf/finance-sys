package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	tradingdomain "finance-sys/internal/trading/domain"

	"gorm.io/gorm"
)

func (s *Service) HandleBridgeEvent(ctx context.Context, event tradingdomain.BridgeEvent) error {
	cfg := s.runtime.Config()
	if cfg == nil {
		return fmt.Errorf("runtime config unavailable")
	}
	if event.SchemaVersion != tradingdomain.SchemaVersionBridgeEvent || len(event.EventHash) != 64 || event.ClientOrderID == "" {
		return fmt.Errorf("invalid Bridge event schema or identity")
	}
	if event.AccountID != cfg.Trading.Bridge.ExpectedAccountID {
		_ = dal.TradingRuntimeControls.SetKillSwitch(ctx, s.db, true, "Bridge callback account mismatch", "system", cfg.Meta.ConfigVersion)
		return fmt.Errorf("Bridge account mismatch")
	}
	order, err := dal.TradingOrders.QueryByClientOrderID(ctx, s.db, event.ClientOrderID)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(event.RawPayload)
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte(`{}`)
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		filledVWAP := fixedDecimal(event.FilledVWAP, 6)
		updates := map[string]any{
			"provider_status": event.ProviderStatus, "cl_ord_id": event.ClOrdID,
			"last_event_at": event.EventAt, "latest_provider_payload_json": raw,
		}
		model := db_model.TradingOrderEvent{
			TradingOrderID: order.ID, EventHash: event.EventHash, EventType: event.EventType, Source: "BRIDGE_CALLBACK",
			AccountID: event.AccountID, ClOrdID: event.ClOrdID, ProviderStatus: event.ProviderStatus,
			NormalizedStatus: event.NormalizedStatus, FilledVolume: event.FilledVolume, EventAt: event.EventAt, RawPayloadJSON: raw,
		}
		if event.FilledVWAP != "" {
			model.FilledVwap = &filledVWAP
		}
		created, createErr := dal.TradingOrderEvents.CreateIdempotent(ctx, tx, &model)
		if createErr != nil || !created {
			return createErr
		}
		if event.EventType == "EXECUTION_REPORT" && event.ExecID != "" && event.FillVolume > 0 {
			fillPrice := fixedDecimal(event.FillPrice, 6)
			amount := multiplyDecimal(fillPrice, event.FillVolume)
			commission := fixedDecimal(event.Commission, 6)
			commissionStatus, commissionSource := reportedCommissionState(commission)
			fill := db_model.TradingFill{
				TradingOrderID: order.ID, AccountID: event.AccountID, ExecID: event.ExecID, ClOrdID: event.ClOrdID,
				Symbol: order.Symbol, EastmoneySymbol: order.EastmoneySymbol, Side: order.Side,
				Price: fillPrice, Volume: event.FillVolume, Amount: amount,
				Commission: commission, CommissionStatus: commissionStatus, CommissionSource: commissionSource,
				ExecType: event.ExecType, TradedAt: event.EventAt, RawPayloadJSON: raw,
			}
			if _, fillErr := dal.TradingFills.CreateIdempotent(ctx, tx, &fill); fillErr != nil {
				return fillErr
			}
			if isPositiveDecimal(commission) {
				evidence := marshalJSON(map[string]any{"method": "PROVIDER_EXECUTION", "event_hash": event.EventHash})
				reconciledAt := time.Now()
				if fillErr := dal.TradingFills.UpdateCommissionByExec(ctx, tx, event.AccountID, event.ExecID, map[string]any{
					"commission": commission, "commission_status": "VERIFIED", "commission_source": "PROVIDER_EXECUTION",
					"commission_evidence_json": evidence, "commission_reconciled_at": reconciledAt,
				}); fillErr != nil {
					return fillErr
				}
			}
			orderCommission, commissionErr := dal.TradingFills.SumCommissionByOrder(ctx, tx, order.ID)
			if commissionErr != nil {
				return commissionErr
			}
			updates["filled_commission"] = fixedDecimal(orderCommission, 6)
		}
		if event.FilledVolume >= order.FilledVolume {
			updates["filled_volume"] = event.FilledVolume
			if event.FilledVWAP != "" {
				updates["filled_vwap"] = filledVWAP
				updates["filled_amount"] = multiplyDecimal(filledVWAP, event.FilledVolume)
			}
		}
		if event.NormalizedStatus != "" && tradingdomain.CanTransitionOrder(order.Status, event.NormalizedStatus) {
			updates["status"] = event.NormalizedStatus
			if tradingdomain.IsTerminalOrderStatus(event.NormalizedStatus) {
				updates["finished_at"] = event.EventAt
			}
		}
		return dal.TradingOrders.Update(ctx, tx, order.ID, updates)
	})
	if err != nil {
		return err
	}
	return s.syncPositionCycleAfterEvent(ctx, order, event)
}

func reportedCommissionState(commission string) (string, string) {
	if isPositiveDecimal(commission) {
		return "VERIFIED", "PROVIDER_EXECUTION"
	}
	return "PENDING", ""
}

func isPositiveDecimal(value string) bool {
	r, ok := new(big.Rat).SetString(zeroDefault(value))
	return ok && r.Sign() > 0
}

func (s *Service) Reconcile(ctx context.Context, runType string) (*db_model.TradingReconciliationRun, error) {
	cfg := s.runtime.Config()
	if cfg == nil {
		return nil, fmt.Errorf("runtime config unavailable")
	}
	if runType == "" {
		runType = "MANUAL"
	}
	now := time.Now()
	run := db_model.TradingReconciliationRun{
		RunKey: hashParts(runType, now.Truncate(time.Second).Format(time.RFC3339)), RunType: runType, Status: "RUNNING",
		AccountID: cfg.Trading.Bridge.ExpectedAccountID, RequestJSON: marshalJSON(map[string]any{"run_type": runType}),
		SummaryJSON: []byte(`{}`), StartedAt: now, ErrorMessage: "", ConfigVersion: cfg.Meta.ConfigVersion,
	}
	if err := dal.TradingReconciliationRuns.Create(ctx, s.db, &run); err != nil {
		return nil, err
	}
	if s.bridge == nil {
		return s.failReconciliation(ctx, &run, "BRIDGE_UNAVAILABLE", "Bridge client unavailable")
	}
	requestedAt := time.Now()
	if _, err := s.bridge.RefreshSnapshot(ctx, hashParts("refresh", run.RunKey)); err != nil {
		return s.failReconciliation(ctx, &run, "BRIDGE_UNAVAILABLE", err.Error())
	}
	wait, maxAge := bridgeSnapshotWaitSettings(cfg)
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var snapshot *tradingdomain.ReconciliationSnapshot
	for {
		value, snapshotErr := s.bridge.ReconciliationSnapshot(ctx, "")
		if snapshotErr == nil && value != nil && !value.Account.SnapshotAt.Before(requestedAt.Add(-maxAge)) {
			snapshot = value
			break
		}
		select {
		case <-ctx.Done():
			return s.failReconciliation(ctx, &run, "BRIDGE_SNAPSHOT_STALE", ctx.Err().Error())
		case <-deadline.C:
			return s.failReconciliation(ctx, &run, "BRIDGE_SNAPSHOT_STALE", "timed out waiting for a fresh Bridge snapshot")
		case <-ticker.C:
		}
	}
	if snapshot.Account.AccountID != cfg.Trading.Bridge.ExpectedAccountID {
		_ = dal.TradingRuntimeControls.SetKillSwitch(ctx, s.db, true, "reconciliation account mismatch", "system", cfg.Meta.ConfigVersion)
		return s.failReconciliation(ctx, &run, "ACCOUNT_MISMATCH", "Bridge account differs from expected simulation account")
	}
	if err := s.persistSnapshot(ctx, snapshot.Account, snapshot.Positions); err != nil {
		return s.failReconciliation(ctx, &run, "SNAPSHOT_PERSIST_FAILED", err.Error())
	}
	if err := s.syncPositionCycles(ctx, snapshot.Account, snapshot.Positions); err != nil {
		return s.failReconciliation(ctx, &run, "POSITION_CYCLE_SYNC_FAILED", err.Error())
	}
	commissionSummary, commissionDiff, err := s.reconcileCommissions(ctx, snapshot.Account)
	if err != nil {
		return s.failReconciliation(ctx, &run, "COMMISSION_RECONCILIATION_FAILED", err.Error())
	}
	providerOrders := make(map[string]struct{}, len(snapshot.Orders))
	for _, item := range snapshot.Orders {
		if value, ok := item["client_order_id"].(string); ok && value != "" {
			providerOrders[value] = struct{}{}
		}
	}
	localOpen, err := dal.TradingOrders.QueryOpen(ctx, s.db)
	if err != nil {
		return s.failReconciliation(ctx, &run, "LOCAL_QUERY_FAILED", err.Error())
	}
	diffs := make([]db_model.TradingReconciliationDiff, 0)
	if commissionDiff != nil {
		commissionDiff.ReconciliationRunID = run.ID
		diffs = append(diffs, *commissionDiff)
	}
	for _, order := range localOpen {
		if order.Status == "DISPATCH_PENDING" || order.Status == "BRIDGE_QUEUED" {
			continue
		}
		if _, ok := providerOrders[order.ClientOrderID]; !ok {
			diffs = append(diffs, db_model.TradingReconciliationDiff{
				ReconciliationRunID: run.ID, DiffType: "MISSING_PROVIDER_ORDER", Severity: "P0", EntityType: "ORDER",
				EntityKey: order.ClientOrderID, LocalValueJSON: marshalJSON(order), ProviderValueJSON: []byte(`null`),
				Resolution: "OPEN", ResolutionMessage: "provider snapshot does not contain a non-terminal local order",
			})
		}
	}
	if err := dal.TradingReconciliationDiffs.CreateBatch(ctx, s.db, diffs); err != nil {
		return s.failReconciliation(ctx, &run, "DIFF_PERSIST_FAILED", err.Error())
	}
	status := "SUCCEEDED"
	if len(diffs) > 0 {
		status = "FAILED"
		_ = dal.TradingRuntimeControls.SetKillSwitch(ctx, s.db, true, "reconciliation found high-severity differences", "system", cfg.Meta.ConfigVersion)
	}
	finished := time.Now()
	summary := marshalJSON(map[string]any{
		"snapshot_version": snapshot.SnapshotVersion, "order_count": len(snapshot.Orders),
		"fill_count": len(snapshot.Executions), "position_count": len(snapshot.Positions),
		"diff_count": len(diffs), "commission": commissionSummary,
	})
	err = dal.TradingReconciliationRuns.Update(ctx, s.db, run.ID, map[string]any{
		"status": status, "snapshot_version": snapshot.SnapshotVersion, "bridge_cursor": snapshot.Cursor,
		"order_count": len(snapshot.Orders), "fill_count": len(snapshot.Executions), "position_count": len(snapshot.Positions),
		"diff_count": len(diffs), "summary_json": summary, "finished_at": finished,
	})
	if err != nil {
		return nil, err
	}
	run.Status = status
	run.SnapshotVersion = snapshot.SnapshotVersion
	run.BridgeCursor = snapshot.Cursor
	run.OrderCount = int32(len(snapshot.Orders))
	run.FillCount = int32(len(snapshot.Executions))
	run.PositionCount = int32(len(snapshot.Positions))
	run.DiffCount = int32(len(diffs))
	run.SummaryJSON = summary
	run.FinishedAt = &finished
	return &run, nil
}

func (s *Service) failReconciliation(ctx context.Context, run *db_model.TradingReconciliationRun, code, message string) (*db_model.TradingReconciliationRun, error) {
	finished := time.Now()
	_ = dal.TradingReconciliationRuns.Update(ctx, s.db, run.ID, map[string]any{"status": "FAILED", "error_code": code, "error_message": message, "finished_at": finished})
	run.Status = "FAILED"
	run.ErrorCode = code
	run.ErrorMessage = message
	run.FinishedAt = &finished
	return run, fmt.Errorf("%s: %s", code, message)
}

func multiplyDecimal(value string, volume int64) string {
	r, ok := new(big.Rat).SetString(zeroDefault(value))
	if !ok {
		return "0"
	}
	r.Mul(r, big.NewRat(volume, 1))
	return r.FloatString(6)
}

func fixedDecimal(value string, scale int) string {
	r, ok := new(big.Rat).SetString(zeroDefault(value))
	if !ok {
		return new(big.Rat).FloatString(scale)
	}
	return r.FloatString(scale)
}
