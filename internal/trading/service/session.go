package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	"finance-sys/internal/trading/instrument"
)

func (s *Service) Preflight(ctx context.Context, at time.Time) (*db_model.TradingDailySession, error) {
	cfg := s.runtime.Config()
	if cfg == nil {
		return nil, fmt.Errorf("runtime config unavailable")
	}
	if at.IsZero() {
		at = time.Now()
	}
	tradeDate := startOfDayInLocation(at, cfg.Meta.Timezone)
	health := map[string]any{"status": "UNAVAILABLE", "kill_switch": true}
	authState := "UNKNOWN"
	bridgeState := "UNAVAILABLE"
	bridgeReady := false
	if s.bridge != nil {
		if value, err := s.bridge.Health(ctx); err == nil && value != nil {
			health = map[string]any{"status": value.Status, "runner": value.Runner, "terminal": value.Terminal, "account": value.Account, "auth_state": value.AuthState, "kill_switch": value.KillSwitch, "config_version": value.ConfigVersion}
			authState = value.AuthState
			bridgeState = value.Status
			bridgeReady = value.Status == "READY" && value.AuthState == "AUTH_OK" && !value.KillSwitch
		}
	}
	runtimeKill := true
	if control, err := dal.TradingRuntimeControls.KillSwitch(ctx, s.db); err == nil {
		runtimeKill = control.Enabled
	}
	untrackedPositions := make([]string, 0)
	if s.bridge != nil {
		_, account, positions := s.currentBridgeState(ctx, cfg)
		for _, position := range positions {
			if position.Volume <= 0 {
				continue
			}
			if _, err := dal.TradingPositionCycles.QueryOpenBySymbol(ctx, s.db, account.AccountID, position.EastmoneySymbol); errors.Is(err, dal.ErrNotFound) {
				untrackedPositions = append(untrackedPositions, position.EastmoneySymbol)
			}
		}
	}
	openOrders, openOrderErr := dal.TradingOrders.QueryOpen(ctx, s.db)
	ready := cfg.Trading.Enabled && !cfg.Trading.KillSwitch && !runtimeKill && bridgeReady && cfg.Trading.Environment == "SIMULATION" && !cfg.Trading.AllowLive && len(untrackedPositions) == 0 && openOrderErr == nil && len(openOrders) == 0
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
	if err := s.persistBoardCapabilities(ctx, cfg.Meta.ConfigVersion); err != nil {
		return nil, err
	}
	if !ready {
		return &session, fmt.Errorf("trading preflight blocked")
	}
	return &session, nil
}

func (s *Service) persistBoardCapabilities(ctx context.Context, configVersion int64) error {
	cfg := s.runtime.Config()
	if cfg == nil {
		return fmt.Errorf("runtime config unavailable")
	}
	for _, board := range []string{instrument.BoardSHMain, instrument.BoardSZMain, instrument.BoardChiNext, instrument.BoardSTAR, instrument.BoardETF} {
		rule, err := instrument.UnitRule(board)
		if err != nil {
			return err
		}
		verified := instrument.ContainsBoard(cfg.Trading.Risk.AllowedBoards, board) && instrument.ContainsBoard(cfg.Trading.Eastmoney.AccountPolicy.VerifiedBoards, board)
		status := "UNVERIFIED"
		var verifiedAt *time.Time
		if verified {
			status = "VERIFIED"
			now := time.Now()
			verifiedAt = &now
		}
		assetType := "STOCK"
		if board == instrument.BoardETF {
			assetType = "ETF"
		}
		model := db_model.TradingBoardCapability{
			CapabilityKey: hashParts(cfg.Trading.Provider, cfg.Trading.Bridge.ExpectedAccountID, board, assetType), Provider: cfg.Trading.Provider,
			Environment: "SIMULATION", AccountID: cfg.Trading.Bridge.ExpectedAccountID, BoardType: board, AssetType: assetType,
			BuyEnabled: verified, SellEnabled: verified, MinimumBuyVolume: rule.MinimumBuyVolume, BuyStep: rule.BuyStep,
			MinimumSellVolume: rule.MinimumSellVolume, SellStep: rule.SellStep, ResidualSellSupported: true,
			VerificationStatus: status, VerifiedAt: verifiedAt, EvidenceJSON: marshalJSON(map[string]any{"risk_allowed": instrument.ContainsBoard(cfg.Trading.Risk.AllowedBoards, board), "account_verified": instrument.ContainsBoard(cfg.Trading.Eastmoney.AccountPolicy.VerifiedBoards, board)}),
			TradingRuleVersion: cfg.Trading.Risk.TradingRuleVersion, ConfigVersion: configVersion,
		}
		if err := dal.TradingBoardCapabilities.Upsert(ctx, s.db, &model); err != nil {
			return err
		}
	}
	return nil
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
