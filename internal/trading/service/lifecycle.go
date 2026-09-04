package service

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	tradingdomain "finance-sys/internal/trading/domain"
	"finance-sys/internal/trading/instrument"
)

func (s *Service) syncPositionCycleAfterEvent(ctx context.Context, order *db_model.TradingOrder, event tradingdomain.BridgeEvent) error {
	if event.FilledVolume <= 0 || order == nil {
		return nil
	}
	intent, err := dal.TradingIntents.QueryByID(ctx, s.db, order.TradingIntentID)
	if err != nil {
		return err
	}
	if strings.EqualFold(order.Side, "BUY") {
		if event.NormalizedStatus != "FILLED" {
			return nil
		}
		if existing, queryErr := dal.TradingPositionCycles.QueryOpenBySymbol(ctx, s.db, order.AccountID, order.EastmoneySymbol); queryErr == nil {
			return dal.TradingPositionCycles.Update(ctx, s.db, existing.ID, map[string]any{"current_volume": event.FilledVolume, "available_volume": 0})
		} else if !errors.Is(queryErr, dal.ErrNotFound) {
			return queryErr
		}
		cfg := s.runtime.Config()
		if cfg == nil {
			return fmt.Errorf("runtime config unavailable")
		}
		security, err := instrument.Parse(order.EastmoneySymbol, intent.Market, intent.AssetType)
		if err != nil {
			return err
		}
		entryPrice := event.FilledVWAP
		if entryPrice == "" && order.FilledVwap != nil {
			entryPrice = *order.FilledVwap
		}
		if entryPrice == "" && order.LimitPrice != nil {
			entryPrice = *order.LimitPrice
		}
		location, _ := time.LoadLocation(cfg.Meta.Timezone)
		if location == nil {
			location = time.FixedZone("CST", 8*3600)
		}
		entryDate := startOfDayInLocation(event.EventAt.In(location), cfg.Meta.Timezone)
		cycle := db_model.TradingPositionCycle{
			CycleKey: hashParts("position-cycle", order.AccountID, order.ClientOrderID), Environment: "SIMULATION", Provider: order.Provider,
			AccountID: order.AccountID, Symbol: security.Symbol, TSCode: security.TSCode, EastmoneySymbol: security.EastmoneySymbol,
			Market: security.Market, AssetType: security.AssetType, BoardType: security.BoardType, Status: "OPEN",
			SourceRecommendationEventID: intent.RecommendationEventID, SourceBuyIntentID: &intent.ID, EntryOrderID: &order.ID,
			EntryTradeDate: entryDate, SellableTradeDate: nextWeekday(entryDate), EntryPrice: fixedDecimal(entryPrice, 6),
			InitialVolume: event.FilledVolume, CurrentVolume: event.FilledVolume, AvailableVolume: 0,
			StopLossPrice: ratioPrice(entryPrice, 1-cfg.Trading.Exit.StopLossRatio), TakeProfitPrice: ratioPrice(entryPrice, 1+cfg.Trading.Exit.TakeProfitRatio),
			MaxHoldingTradeDays: int32(cfg.Trading.Exit.MaxHoldingTradeDays), OpenedAt: event.EventAt,
			StrategyVersion: intent.StrategyVersion, RuleVersion: cfg.Trading.Risk.TradingRuleVersion, ConfigVersion: cfg.Meta.ConfigVersion,
		}
		if err := dal.TradingPositionCycles.Create(ctx, s.db, &cycle); err != nil {
			return err
		}
		stored, err := dal.TradingPositionCycles.QueryOpenBySymbol(ctx, s.db, order.AccountID, order.EastmoneySymbol)
		if err == nil {
			return dal.TradingIntents.Update(ctx, s.db, intent.ID, map[string]any{"position_cycle_id": stored.ID})
		}
		return err
	}

	if intent.PositionCycleID == nil {
		return nil
	}
	cycle, err := dal.TradingPositionCycles.QueryByID(ctx, s.db, *intent.PositionCycleID)
	if err != nil {
		return err
	}
	remaining := cycle.InitialVolume - event.FilledVolume
	if remaining < 0 {
		remaining = 0
	}
	values := map[string]any{"current_volume": remaining, "exit_order_id": order.ID, "exit_reason": exitReason(intent.EvidenceRefsJSON)}
	if event.NormalizedStatus == "FILLED" || remaining == 0 {
		values["status"] = "CLOSED"
		values["available_volume"] = 0
		values["closed_at"] = event.EventAt
	} else {
		values["status"] = "EXIT_PENDING"
	}
	return dal.TradingPositionCycles.Update(ctx, s.db, cycle.ID, values)
}

func (s *Service) syncPositionCycles(ctx context.Context, account tradingdomain.AccountSnapshot, positions []tradingdomain.PositionSnapshot) error {
	cycles, err := dal.TradingPositionCycles.ListOpen(ctx, s.db, account.AccountID)
	if err != nil {
		return err
	}
	bySymbol := make(map[string]tradingdomain.PositionSnapshot, len(positions))
	for _, position := range positions {
		bySymbol[strings.ToUpper(position.EastmoneySymbol)] = position
	}
	for _, cycle := range cycles {
		position, exists := bySymbol[strings.ToUpper(cycle.EastmoneySymbol)]
		values := map[string]any{"last_evaluated_at": account.SnapshotAt}
		if exists {
			values["current_volume"] = position.Volume
			values["available_volume"] = position.AvailableVolume
		} else if cycle.ExitOrderID != nil {
			values["status"] = "CLOSED"
			values["current_volume"] = 0
			values["available_volume"] = 0
			values["closed_at"] = account.SnapshotAt
		}
		quotes, quoteErr := dal.StockDailyQuotes.QueryTradingDates(ctx, s.db, "TUSHARE", cycle.EntryTradeDate, account.SnapshotAt)
		if quoteErr == nil && len(quotes) > 0 {
			values["holding_trade_days"] = max(len(quotes)-1, 0)
		}
		if err := dal.TradingPositionCycles.Update(ctx, s.db, cycle.ID, values); err != nil {
			return err
		}
	}
	return nil
}

func ratioPrice(price string, multiplier float64) string {
	value, ok := new(big.Rat).SetString(zeroDefault(price))
	if !ok || value.Sign() <= 0 {
		return "0.000000"
	}
	factor, ok := new(big.Rat).SetString(fmt.Sprintf("%.8f", multiplier))
	if !ok {
		return "0.000000"
	}
	return new(big.Rat).Mul(value, factor).FloatString(6)
}

func nextWeekday(value time.Time) time.Time {
	next := value.AddDate(0, 0, 1)
	for next.Weekday() == time.Saturday || next.Weekday() == time.Sunday {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

func exitReason(raw []byte) string {
	text := string(raw)
	for _, reason := range []string{"STOP_LOSS", "TAKE_PROFIT", "MAX_HOLDING_DAYS"} {
		if strings.Contains(text, reason) {
			return reason
		}
	}
	return "AGENT_EXIT"
}
