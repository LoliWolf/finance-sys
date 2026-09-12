package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	tradingdomain "finance-sys/internal/trading/domain"
)

func (s *Service) Preflight(ctx context.Context, at time.Time) (*db_model.TradingDailySession, error) {
	cfg := s.runtime.Config()
	if cfg == nil {
		return nil, fmt.Errorf("runtime config unavailable")
	}
	if at.IsZero() {
		at = time.Now()
	}
	tradeDate := tradingDay(at)
	open, calendarErr := cfg.Trading.Calendar.IsOpen(at)
	if calendarErr != nil {
		return nil, calendarErr
	}
	if !open {
		return nil, fmt.Errorf("not a trading day")
	}
	health := map[string]any{"status": "UNAVAILABLE", "kill_switch": true}
	authState := "UNKNOWN"
	bridgeState := "UNAVAILABLE"
	bridgeReady := false
	var snapshotPositions []tradingdomain.PositionSnapshot
	if s.bridge != nil {
		if snapshot, err := s.freshBridgeSnapshot(ctx, "preflight"); err == nil && snapshot != nil {
			value := snapshot.Health
			health = map[string]any{"status": value.Status, "runner": value.Runner, "terminal": value.Terminal, "account": value.Account, "auth_state": value.AuthState, "kill_switch": value.KillSwitch, "config_version": value.ConfigVersion}
			authState = value.AuthState
			bridgeState = value.Status
			bridgeReady = value.Runner == "READY" && value.Terminal == "CONNECTED" && value.Account == "READY" && value.AuthState == "AUTH_OK" && value.AccountID == cfg.Trading.Bridge.ExpectedAccountID
			snapshotPositions = snapshot.Positions
			if err := s.persistSnapshot(ctx, snapshot.Account, snapshot.Positions); err != nil {
				return nil, err
			}
		}
	}
	runtimeKill := true
	if control, err := dal.TradingRuntimeControls.KillSwitch(ctx, s.db); err == nil {
		runtimeKill = control.Enabled
	}
	untrackedPositions := make([]string, 0)
	if s.bridge != nil {
		for _, position := range snapshotPositions {
			if position.Volume <= 0 {
				continue
			}
			if _, err := dal.TradingPositionCycles.QueryOpenBySymbol(ctx, s.db, cfg.Trading.Bridge.ExpectedAccountID, position.EastmoneySymbol); errors.Is(err, dal.ErrNotFound) {
				untrackedPositions = append(untrackedPositions, position.EastmoneySymbol)
			} else if err != nil {
				return nil, err
			}
		}
	}
	openOrders, openOrderErr := dal.TradingOrders.QueryOpen(ctx, s.db)
	_, nextErr := cfg.Trading.Calendar.Next(at)
	ready := bridgeReady && cfg.Trading.Environment == "SIMULATION" && !cfg.Trading.AllowLive && len(untrackedPositions) == 0 && openOrderErr == nil && len(openOrders) == 0 && nextErr == nil
	status := "BLOCKED"
	if ready {
		status = "READY"
	}
	session := db_model.TradingDailySession{
		SessionKey:  hashParts("trading-session", cfg.Trading.Provider, cfg.Trading.Bridge.ExpectedAccountID, tradeDate.Format(time.DateOnly)),
		Environment: "SIMULATION", Provider: cfg.Trading.Provider, AccountID: cfg.Trading.Bridge.ExpectedAccountID,
		TradeDate: tradeDate, Status: status, PreflightStatus: status, AuthState: authState, BridgeState: bridgeState,
		PreflightJSON: marshalJSON(map[string]any{"bridge": health, "nacos_enabled": cfg.Trading.Enabled, "nacos_kill_switch": cfg.Trading.KillSwitch, "runtime_kill_switch": runtimeKill, "allowed_boards": cfg.Trading.Risk.AllowedBoards, "verified_boards": cfg.Trading.Eastmoney.AccountPolicy.VerifiedBoards, "untracked_positions": untrackedPositions, "open_order_count": len(openOrders)}),
		SummaryJSON:   []byte(`{}`), ConfigVersion: cfg.Meta.ConfigVersion,
	}
	if !ready {
		session.ErrorCode = "PREFLIGHT_BLOCKED"
		session.ErrorMessage = "one or more fail-closed trading prerequisites are not ready"
	} else {
		openedAt := at
		session.OpenedAt = &openedAt
	}
	if err := dal.TradingDailySessions.Upsert(ctx, s.db, &session); err != nil {
		return nil, err
	}
	// Preflight never turns an allowlist into fabricated UAT evidence.
	if !ready {
		return &session, fmt.Errorf("trading preflight blocked")
	}
	return &session, nil
}

func (s *Service) CancelOpenOrders(ctx context.Context, reason string) (map[string]any, error) {
	if s.bridge == nil {
		return nil, fmt.Errorf("trading Bridge unavailable")
	}
	orders, err := dal.TradingOrders.QueryOpen(ctx, s.db)
	if err != nil {
		return nil, err
	}
	canceled := 0
	errorsByOrder := make(map[string]string)
	for _, order := range orders {
		if reason != "EOD" && order.ValidUntil.After(s.now()) {
			continue
		}
		if _, err := s.bridge.CancelOrder(ctx, order.ClientOrderID, hashParts("scheduled-cancel", reason, order.ClientOrderID)); err != nil {
			errorsByOrder[order.ClientOrderID] = err.Error()
			continue
		}
		canceled++
	}
	result := map[string]any{"requested": len(orders), "accepted": canceled, "errors": errorsByOrder, "reason": reason}
	if len(errorsByOrder) > 0 {
		return result, fmt.Errorf("%d open-order cancellations failed", len(errorsByOrder))
	}
	return result, nil
}
