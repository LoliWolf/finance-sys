package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	tradingdomain "finance-sys/internal/trading/domain"

	"gorm.io/gorm"
)

func (s *Service) HandleBridgeEvent(ctx context.Context, event tradingdomain.BridgeEvent) error {
	return s.applyBridgeEvent(ctx, event, nil)
}

func (s *Service) applyBridgeEvent(ctx context.Context, event tradingdomain.BridgeEvent, applied func(bool, bool)) error {
	eventCreated, fillCreated := false, false
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
	if event.EventAt.IsZero() || event.FilledVolume < 0 || event.FillVolume < 0 {
		return fmt.Errorf("invalid Bridge event time or quantity")
	}
	raw, _ := json.Marshal(event.RawPayload)
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte(`{}`)
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := dal.TradingRuntimeControls.Lock(ctx, tx); err != nil {
			return err
		}
		order, err := dal.TradingOrders.QueryByClientOrderID(ctx, tx, event.ClientOrderID)
		if err != nil {
			return err
		}
		if order.AccountID != event.AccountID || (event.EastmoneySymbol != "" && event.EastmoneySymbol != order.EastmoneySymbol) || (event.Side != "" && event.Side != order.Side) || event.FilledVolume > order.Volume {
			return fmt.Errorf("Bridge event conflicts with local order")
		}
		filledVWAP := fixedDecimal(event.FilledVWAP, 6)
		updates := map[string]any{}
		if event.ClOrdID != "" {
			if order.ClOrdID != "" && order.ClOrdID != event.ClOrdID {
				return fmt.Errorf("provider order id conflict")
			}
			updates["cl_ord_id"] = event.ClOrdID
		}
		if order.LastEventAt == nil || !event.EventAt.Before(*order.LastEventAt) {
			updates["provider_status"] = event.ProviderStatus
			updates["last_event_at"] = event.EventAt
			updates["latest_provider_payload_json"] = raw
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
		eventCreated = created
		if createErr != nil || !created {
			return createErr
		}
		if event.EventType == "EXECUTION_REPORT" && event.ExecID != "" && event.FillVolume > 0 {
			if !isPositiveDecimal(event.FillPrice) {
				return fmt.Errorf("execution price is missing or invalid")
			}
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
			if fresh, fillErr := dal.TradingFills.CreateIdempotent(ctx, tx, &fill); fillErr != nil {
				return fillErr
			} else {
				fillCreated = fresh
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
		fills, err := dal.TradingFills.ByOrder(ctx, tx, order.ID)
		if err != nil {
			return err
		}
		if err := mergeOrderEvent(order, event, fills); err != nil {
			return err
		}
		updates["filled_volume"] = order.FilledVolume
		updates["filled_vwap"] = order.FilledVwap
		updates["filled_amount"] = order.FilledAmount
		updates["status"] = order.Status
		if tradingdomain.IsTerminalOrderStatus(order.Status) {
			updates["finished_at"] = event.EventAt
		}
		if code, ok := event.RawPayload["error_code"].(string); ok {
			updates["error_code"] = code
		}
		if err := dal.TradingOrders.Update(ctx, tx, order.ID, updates); err != nil {
			return err
		}
		return s.syncPositionCycleAfterEvent(ctx, tx, order, event)
	})
	if err != nil {
		return err
	}
	if applied != nil {
		applied(eventCreated, fillCreated)
	}
	return nil
}

func mergeOrderEvent(order *db_model.TradingOrder, event tradingdomain.BridgeEvent, fills []db_model.TradingFill) error {
	volume := int64(0)
	amount := new(big.Rat)
	for _, fill := range fills {
		if fill.Volume <= 0 || !isPositiveDecimal(fill.Amount) {
			return fmt.Errorf("invalid stored fill")
		}
		volume += fill.Volume
		amount.Add(amount, decimalRat(fill.Amount))
	}
	if volume > order.Volume || event.FilledVolume > order.Volume {
		return fmt.Errorf("cumulative fills exceed order volume")
	}
	if event.FilledVolume > order.FilledVolume {
		if !isPositiveDecimal(event.FilledVWAP) {
			return fmt.Errorf("cumulative fill VWAP missing")
		}
		order.FilledVolume = event.FilledVolume
		vwap := fixedDecimal(event.FilledVWAP, 6)
		order.FilledVwap = &vwap
		order.FilledAmount = multiplyDecimal(vwap, event.FilledVolume)
	}
	if volume >= order.FilledVolume && volume > 0 {
		order.FilledVolume = volume
		order.FilledAmount = amount.FloatString(6)
		vwap := new(big.Rat).Quo(amount, big.NewRat(volume, 1)).FloatString(6)
		order.FilledVwap = &vwap
	}
	if order.FilledVolume == order.Volume && order.Volume > 0 {
		order.Status = "FILLED"
		return nil
	}
	if event.NormalizedStatus != "" && event.NormalizedStatus != "UNKNOWN" && (order.LastEventAt == nil || !event.EventAt.Before(*order.LastEventAt)) && tradingdomain.CanTransitionOrder(order.Status, event.NormalizedStatus) {
		order.Status = event.NormalizedStatus
	}
	if order.FilledVolume > 0 && !tradingdomain.IsTerminalOrderStatus(order.Status) {
		order.Status = "PARTIALLY_FILLED"
	}
	if event.NormalizedStatus == "UNKNOWN" && !tradingdomain.IsTerminalOrderStatus(order.Status) && order.FilledVolume == 0 {
		order.Status = "UNKNOWN"
	}
	return nil
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
	now := s.now()
	deadline := now.Add(time.Minute)
	run := db_model.TradingReconciliationRun{
		RunKey: hashParts(runType, now.Truncate(time.Second).Format(time.RFC3339)), RunType: runType, Status: "RUNNING",
		AccountID: cfg.Trading.Bridge.ExpectedAccountID, RequestJSON: marshalJSON(map[string]any{"run_type": runType}),
		SummaryJSON: []byte(`{}`), StartedAt: now, ErrorMessage: "", ConfigVersion: cfg.Meta.ConfigVersion,
		ClaimToken: hashParts("manual-reconcile", now.String()), ClaimDeadline: &deadline,
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := dal.TradingRuntimeControls.Lock(ctx, tx); err != nil {
			return err
		}
		if err := s.requireNoRunningReconciliation(ctx, tx, run.AccountID); err != nil {
			return err
		}
		return dal.TradingReconciliationRuns.Create(ctx, tx, &run)
	}); err != nil {
		return nil, err
	}
	return s.performReconciliation(ctx, &run)
}

func (s *Service) performReconciliation(ctx context.Context, run *db_model.TradingReconciliationRun) (*db_model.TradingReconciliationRun, error) {
	cfg := s.runtime.Config()
	if cfg == nil {
		return s.failReconciliation(ctx, run, "CONFIG_UNAVAILABLE", "config unavailable")
	}
	maxAttempts, interval, budget := reconciliationConfirmationSettings(cfg.Trading.Reconciliation)
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	if err := s.lockReconciliationActivity(ctx); err != nil {
		return s.failReconciliation(ctx, run, "RECONCILIATION_BUSY", err.Error())
	}
	defer s.accountActivity.Unlock()
	if s.bridge == nil {
		return s.failReconciliation(ctx, run, "BRIDGE_UNAVAILABLE", "Bridge client unavailable")
	}
	var snapshot *tradingdomain.ReconciliationSnapshot
	var diffs []db_model.TradingReconciliationDiff
	var commissionSummary map[string]any
	var err error
	repairedEvents, repairedFills := int(run.RepairedEventCount), int(run.RepairedFillCount)
	var prior struct {
		Attempts []reconciliationSnapshotAttempt `json:"snapshot_attempts"`
	}
	if err := json.Unmarshal(run.SummaryJSON, &prior); err != nil {
		return s.failReconciliation(ctx, run, "ATTEMPT_AUDIT_INVALID", "cannot decode previous confirmation evidence")
	}
	attempts := prior.Attempts
	if attempts == nil {
		attempts = []reconciliationSnapshotAttempt{}
	}
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return s.failReconciliation(ctx, run, "SNAPSHOT_CONFIRMATION_FAILED", err.Error())
		}
		if current := s.runtime.Config(); current == nil || current.Meta.ConfigVersion != cfg.Meta.ConfigVersion {
			return s.failReconciliation(ctx, run, "CONFIG_CHANGED", "configuration changed during reconciliation")
		}
		snapshot, err = s.freshBridgeSnapshot(ctx, fmt.Sprintf("reconciliation-%d-%d", run.ID, attempt))
		if err != nil {
			return s.failReconciliation(ctx, run, "SNAPSHOT_CONFIRMATION_FAILED", err.Error())
		}
		if snapshot.Account.AccountID != cfg.Trading.Bridge.ExpectedAccountID {
			_ = dal.TradingRuntimeControls.SetKillSwitch(ctx, s.db, true, "reconciliation account mismatch", "system", cfg.Meta.ConfigVersion)
			return s.failReconciliation(ctx, run, "ACCOUNT_MISMATCH", "Bridge account differs from expected simulation account")
		}
		for _, previous := range attempts {
			if previous.SnapshotVersion == snapshot.SnapshotVersion || snapshot.Account.SnapshotAt.Before(previous.SnapshotAt) {
				return s.failReconciliation(ctx, run, "SNAPSHOT_CONFIRMATION_FAILED", "repeated or regressed snapshot cannot confirm consistency")
			}
		}
		for _, items := range [][]map[string]any{snapshot.Orders, snapshot.Executions} {
			for _, item := range items {
				if raw, exists := item["bridge_event"]; exists {
					var event tradingdomain.BridgeEvent
					if e := json.Unmarshal(marshalJSON(raw), &event); e != nil {
						return s.failReconciliation(ctx, run, "RECOVERY_SCHEMA_INVALID", e.Error())
					}
					if (event.EventType == "EXECUTION_REPORT" && cfg.Trading.Reconciliation.AutoRepairMissingFills) || (event.EventType != "EXECUTION_REPORT" && cfg.Trading.Reconciliation.AutoRepairMissingEvents) {
						if e := s.applyBridgeEvent(ctx, event, func(newEvent, newFill bool) {
							if newEvent {
								repairedEvents++
							}
							if newFill {
								repairedFills++
							}
						}); e != nil {
							return s.failReconciliation(ctx, run, "RECOVERY_FAILED", e.Error())
						}
					}
				}
			}
		}
		if err := s.persistSnapshot(ctx, snapshot.Account, snapshot.Positions); err != nil {
			return s.failReconciliation(ctx, run, "SNAPSHOT_PERSIST_FAILED", err.Error())
		}
		var commissionDiff *db_model.TradingReconciliationDiff
		commissionSummary, commissionDiff, err = s.reconcileCommissions(ctx, snapshot.Account)
		if err != nil {
			return s.failReconciliation(ctx, run, "COMMISSION_RECONCILIATION_FAILED", err.Error())
		}
		providerOrders := make(map[string]struct{}, len(snapshot.Orders))
		for _, item := range snapshot.Orders {
			if value, ok := item["client_order_id"].(string); ok && value != "" {
				providerOrders[value] = struct{}{}
			}
		}
		localOpen, err := dal.TradingOrders.QueryOpen(ctx, s.db)
		if err != nil {
			return s.failReconciliation(ctx, run, "LOCAL_QUERY_FAILED", err.Error())
		}
		diffs, err = s.ledgerDiffs(ctx, snapshot.Account, snapshot.Positions)
		if err != nil {
			return s.failReconciliation(ctx, run, "LEDGER_UNAVAILABLE", err.Error())
		}
		for i := range diffs {
			diffs[i].ReconciliationRunID = run.ID
		}
		for _, item := range snapshot.Orders {
			if id, ok := item["client_order_id"].(string); !ok || id == "" {
				diffs = append(diffs, db_model.TradingReconciliationDiff{ReconciliationRunID: run.ID, DiffType: "UNTRACKED_PROVIDER_ORDER", Severity: "P0", EntityType: "ORDER", EntityKey: fmt.Sprint(item["cl_ord_id"]), LocalValueJSON: []byte(`null`), ProviderValueJSON: marshalJSON(item), Resolution: "OPEN", ResolutionMessage: "provider order has no local client-order binding"})
			}
		}
		if commissionDiff != nil {
			commissionDiff.ReconciliationRunID = run.ID
			diffs = append(diffs, *commissionDiff)
		}
		for _, order := range localOpen {
			if _, ok := providerOrders[order.ClientOrderID]; !ok {
				diffs = append(diffs, db_model.TradingReconciliationDiff{
					ReconciliationRunID: run.ID, DiffType: "MISSING_PROVIDER_ORDER", Severity: "P0", EntityType: "ORDER",
					EntityKey: order.ClientOrderID, LocalValueJSON: marshalJSON(order), ProviderValueJSON: []byte(`null`),
					Resolution: "OPEN", ResolutionMessage: "provider snapshot does not contain a non-terminal local order",
				})
			}
		}
		attempts = append(attempts, newReconciliationSnapshotAttempt(snapshot, diffs))
		run.SummaryJSON = marshalJSON(map[string]any{"snapshot_attempts": attempts, "confirmation_max_attempts": maxAttempts, "confirmation_budget_ms": budget.Milliseconds()})
		run.RepairedEventCount = int32(repairedEvents)
		run.RepairedFillCount = int32(repairedFills)
		ok, checkpointErr := dal.TradingReconciliationRuns.UpdateClaim(ctx, s.db, run.ID, run.ClaimToken, s.now(), map[string]any{
			"summary_json": run.SummaryJSON, "repaired_event_count": repairedEvents, "repaired_fill_count": repairedFills,
		})
		if checkpointErr != nil {
			return s.failReconciliation(ctx, run, "ATTEMPT_PERSIST_FAILED", checkpointErr.Error())
		}
		if !ok {
			return nil, ErrClaimLost
		}
		if !valuationOnlyDiffs(diffs) || attempt >= maxAttempts {
			break
		}
		if err := waitReconciliationRetry(ctx, interval); err != nil {
			return s.failReconciliation(ctx, run, "SNAPSHOT_CONFIRMATION_FAILED", err.Error())
		}
	}
	if err := ctx.Err(); err != nil {
		return s.failReconciliation(ctx, run, "SNAPSHOT_CONFIRMATION_FAILED", err.Error())
	}
	if err := dal.TradingReconciliationDiffs.CreateBatch(ctx, s.db, diffs); err != nil {
		return s.failReconciliation(ctx, run, "DIFF_PERSIST_FAILED", err.Error())
	}
	status := "SUCCEEDED"
	if len(diffs) > 0 {
		status = "FAILED"
		if err := s.haltForReconciliation(ctx, run, "reconciliation ledger difference"); err != nil {
			return s.failReconciliation(ctx, run, "KILL_SWITCH_FAILED", err.Error())
		}
	} else if err := s.syncPositionCycles(ctx, snapshot.Account, snapshot.Positions); err != nil {
		return s.failReconciliation(ctx, run, "POSITION_CYCLE_SYNC_FAILED", err.Error())
	}
	finished := s.now()
	summary := marshalJSON(map[string]any{
		"snapshot_version": snapshot.SnapshotVersion, "order_count": len(snapshot.Orders),
		"fill_count": len(snapshot.Executions), "position_count": len(snapshot.Positions),
		"diff_count": len(diffs), "commission": commissionSummary,
		"snapshot_attempts": attempts, "confirmation_max_attempts": maxAttempts, "confirmation_budget_ms": budget.Milliseconds(),
	})
	ok, err := dal.TradingReconciliationRuns.FinishClaim(ctx, s.db, run.ID, run.ClaimToken, finished, map[string]any{
		"status": status, "snapshot_version": snapshot.SnapshotVersion, "bridge_cursor": snapshot.Cursor,
		"order_count": len(snapshot.Orders), "fill_count": len(snapshot.Executions), "position_count": len(snapshot.Positions),
		"diff_count": len(diffs), "summary_json": summary, "finished_at": finished,
		"repaired_event_count": repairedEvents, "repaired_fill_count": repairedFills,
		"error_code": "", "error_message": "",
	})
	if err != nil {
		return s.failReconciliation(ctx, run, "RUN_FINISH_FAILED", err.Error())
	}
	if !ok {
		return nil, ErrClaimLost
	}
	run.Status = status
	run.ErrorCode, run.ErrorMessage = "", ""
	run.SnapshotVersion = snapshot.SnapshotVersion
	run.BridgeCursor = snapshot.Cursor
	run.OrderCount = int32(len(snapshot.Orders))
	run.FillCount = int32(len(snapshot.Executions))
	run.PositionCount = int32(len(snapshot.Positions))
	run.DiffCount = int32(len(diffs))
	run.SummaryJSON = summary
	run.FinishedAt = &finished
	return run, nil
}

func (s *Service) failReconciliation(ctx context.Context, run *db_model.TradingReconciliationRun, code, message string) (*db_model.TradingReconciliationRun, error) {
	// A canceled request must neither leave a false success nor skip the safety latch.
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	haltErr := s.haltForReconciliation(cleanup, run, "reconciliation failed: "+code)
	finished := s.now()
	values := map[string]any{
		"error_code": code, "error_message": message,
		"summary_json": run.SummaryJSON, "repaired_event_count": run.RepairedEventCount, "repaired_fill_count": run.RepairedFillCount,
	}
	// If either safety latch cannot be confirmed, retain the durable RUNNING
	// execution barrier so a recovery worker (not a new order) is the next action.
	if haltErr == nil {
		values["status"] = "FAILED"
		values["finished_at"] = finished
	}
	ok, persistErr := dal.TradingReconciliationRuns.UpdateClaim(cleanup, s.db, run.ID, run.ClaimToken, finished, values)
	if !ok && persistErr == nil {
		persistErr = ErrClaimLost
	}
	if haltErr == nil && persistErr == nil {
		run.Status = "FAILED"
		run.FinishedAt = &finished
	}
	run.ErrorCode = code
	run.ErrorMessage = message
	return run, errors.Join(fmt.Errorf("%s: %s", code, message), haltErr, persistErr)
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
