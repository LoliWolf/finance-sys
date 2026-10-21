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
	"finance-sys/internal/rules"
	tradingdomain "finance-sys/internal/trading/domain"
	"finance-sys/internal/trading/instrument"
	"gorm.io/gorm"
)

// Called inside the same account-locked transaction as the immutable event and
// cumulative order update. Recompute from all linked SELL orders; never subtract
// a single order's quantity from the original volume or close on a partial exit.
func (s *Service) syncPositionCycleAfterEvent(ctx context.Context, tx *gorm.DB, order *db_model.TradingOrder, event tradingdomain.BridgeEvent) error {
	if order == nil {
		return fmt.Errorf("missing order")
	}
	intent, err := dal.TradingIntents.QueryByID(ctx, tx, order.TradingIntentID)
	if err != nil {
		return err
	}
	var cycle *db_model.TradingPositionCycle
	if order.Side == "BUY" {
		if order.FilledVolume <= 0 {
			return nil
		}
		key := hashParts("position-cycle", order.AccountID, order.ClientOrderID)
		cycle, err = dal.TradingPositionCycles.ByKey(ctx, tx, key)
		if errors.Is(err, dal.ErrNotFound) {
			cfg := s.runtime.Config()
			if cfg == nil {
				return fmt.Errorf("config unavailable")
			}
			if order.FilledVwap == nil || !isPositiveDecimal(*order.FilledVwap) {
				return fmt.Errorf("fill price unavailable; cannot invent entry price")
			}
			security, parseErr := instrument.Parse(order.EastmoneySymbol, intent.Market, intent.AssetType)
			if parseErr != nil {
				return parseErr
			}
			var recipe tradingdomain.BridgeOrderRequest
			if err := json.Unmarshal(order.RequestJSON, &recipe); err != nil {
				return err
			}
			exit := recipe.ExitPolicy
			if exit.MaxHoldingTradeDays <= 0 {
				// Legacy fills predate frozen exit recipes. Keep their provenance in
				// the cycle's rule/config version; new orders always supply a recipe.
				exit = tradingdomain.ExitPolicy{StopLossRatio: cfg.Trading.Exit.StopLossRatio, TakeProfitRatio: cfg.Trading.Exit.TakeProfitRatio, MaxHoldingTradeDays: cfg.Trading.Exit.MaxHoldingTradeDays}
			}
			stop, take, levelErr := rules.TradingExitLevels(*order.FilledVwap, exit.StopLossRatio, exit.TakeProfitRatio)
			if levelErr != nil {
				return levelErr
			}
			date := tradingDay(event.EventAt)
			var sellable *time.Time
			if security.AssetType == "ETF" && recipe.TradeN == 0 {
				sellable = &date
			} else if next, nextErr := cfg.Trading.Calendar.Next(date); nextErr == nil {
				civil := tradingDay(next)
				sellable = &civil
			}
			active := hashParts(order.AccountID, order.EastmoneySymbol)
			cycle = &db_model.TradingPositionCycle{CycleKey: key, OpenSymbolKey: &active, Environment: "SIMULATION", Provider: order.Provider, AccountID: order.AccountID, Symbol: security.Symbol, TSCode: security.TSCode, EastmoneySymbol: security.EastmoneySymbol, Market: security.Market, AssetType: security.AssetType, BoardType: security.BoardType, Status: "OPEN", SourceRecommendationEventID: intent.RecommendationEventID, SourceBuyIntentID: &intent.ID, EntryOrderID: &order.ID, EntryTradeDate: date, SellableTradeDate: sellable, EntryPrice: *order.FilledVwap, InitialVolume: order.FilledVolume, CurrentVolume: order.FilledVolume, StopLossPrice: stop, TakeProfitPrice: take, MaxHoldingTradeDays: int32(exit.MaxHoldingTradeDays), OpenedAt: event.EventAt, StrategyVersion: intent.StrategyVersion, RuleVersion: cfg.Trading.Risk.TradingRuleVersion, ConfigVersion: order.ConfigVersion}
			if err := dal.TradingPositionCycles.Create(ctx, tx, cycle); err != nil {
				return err
			}
			cycle, err = dal.TradingPositionCycles.ByKey(ctx, tx, key)
		}
		if err != nil {
			return err
		}
		if err := dal.TradingIntents.Update(ctx, tx, intent.ID, map[string]any{"position_cycle_id": cycle.ID}); err != nil {
			return err
		}
	} else {
		if intent.PositionCycleID == nil {
			return nil
		} // Historical manual UAT SELL.
		cycle, err = dal.TradingPositionCycles.QueryByID(ctx, tx, *intent.PositionCycleID)
		if err != nil {
			return err
		}
		if cycle.AccountID != order.AccountID || cycle.EastmoneySymbol != order.EastmoneySymbol {
			return fmt.Errorf("cycle identity mismatch")
		}
	}
	if cycle.EntryOrderID == nil {
		return fmt.Errorf("cycle has no entry order")
	}
	entry, err := dal.TradingOrders.QueryByID(ctx, tx, *cycle.EntryOrderID)
	if err != nil {
		return err
	}
	exits, err := dal.TradingOrders.ByCycle(ctx, tx, cycle.ID)
	if err != nil {
		return err
	}
	remaining, status, err := cyclePosition(*entry, exits)
	if err != nil {
		return err
	}
	values := map[string]any{"initial_volume": entry.FilledVolume, "current_volume": remaining, "status": status, "available_volume": min64(cycle.AvailableVolume, remaining)}
	if entry.FilledVwap != nil {
		values["entry_price"] = *entry.FilledVwap
	}
	if status == "CLOSED" {
		values["open_symbol_key"] = nil
		values["closed_at"] = event.EventAt
		values["available_volume"] = 0
	} else {
		active := hashParts(cycle.AccountID, cycle.EastmoneySymbol)
		values["open_symbol_key"] = active
		values["closed_at"] = nil
	}
	if order.Side == "SELL" {
		values["exit_order_id"] = order.ID
		values["exit_reason"] = exitReason(intent.EvidenceRefsJSON)
	}
	return dal.TradingPositionCycles.Update(ctx, tx, cycle.ID, values)
}

func cyclePosition(entry db_model.TradingOrder, linked []db_model.TradingOrder) (int64, string, error) {
	sold := int64(0)
	pending := false
	for _, o := range linked {
		if o.Side != "SELL" || o.Status == "DRY_RUN" {
			continue
		}
		if o.FilledVolume < 0 || o.FilledVolume > o.Volume {
			return 0, "", fmt.Errorf("invalid cumulative exit quantity")
		}
		sold += o.FilledVolume
		if !tradingdomain.IsTerminalOrderStatus(o.Status) {
			pending = true
		}
	}
	remaining := entry.FilledVolume - sold
	if remaining < 0 {
		return 0, "", fmt.Errorf("sold volume exceeds confirmed entry fills")
	}
	if remaining == 0 && tradingdomain.IsTerminalOrderStatus(entry.Status) {
		return 0, "CLOSED", nil
	}
	if pending {
		return remaining, "EXIT_PENDING", nil
	}
	return remaining, "OPEN", nil
}

// Provider snapshots may update sellable quantity but never overwrite the local
// confirmed-fill ledger. A quantity discrepancy is an error, not an auto-repair.
func (s *Service) syncPositionCycles(ctx context.Context, account tradingdomain.AccountSnapshot, positions []tradingdomain.PositionSnapshot) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := dal.TradingRuntimeControls.Lock(ctx, tx); err != nil {
			return err
		}
		cycles, err := dal.TradingPositionCycles.ListOpen(ctx, tx, account.AccountID)
		if err != nil {
			return err
		}
		bySymbol := make(map[string]tradingdomain.PositionSnapshot, len(positions))
		for _, p := range positions {
			bySymbol[p.EastmoneySymbol] = p
		}
		for _, c := range cycles {
			p := bySymbol[c.EastmoneySymbol]
			if p.Volume != c.CurrentVolume {
				return fmt.Errorf("POSITION_DIFF: %s local=%d provider=%d", c.EastmoneySymbol, c.CurrentVolume, p.Volume)
			}
			values := map[string]any{"available_volume": p.AvailableVolume, "last_evaluated_at": account.SnapshotAt}
			cfg := s.runtime.Config()
			days, err := cfg.Trading.Calendar.Elapsed(c.EntryTradeDate, account.SnapshotAt)
			if err != nil {
				return err
			}
			values["holding_trade_days"] = days
			if c.SellableTradeDate == nil {
				if next, e := cfg.Trading.Calendar.Next(c.EntryTradeDate); e == nil {
					values["sellable_trade_date"] = next
				}
			}
			if err := dal.TradingPositionCycles.Update(ctx, tx, c.ID, values); err != nil {
				return err
			}
		}
		return nil
	})
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func exitReason(raw []byte) string {
	text := string(raw)
	for _, reason := range []string{"STOP_LOSS", "TAKE_PROFIT", "MAX_HOLDING_DAYS", "UAT_EXIT"} {
		if strings.Contains(text, reason) {
			return reason
		}
	}
	return "AGENT_EXIT"
}

// Keep civil dates in the exchange zone even when the server process uses UTC.
func tradingDay(at time.Time) time.Time {
	d, _ := time.ParseInLocation(time.DateOnly, config.ChinaDate(at), time.UTC)
	return d
}
