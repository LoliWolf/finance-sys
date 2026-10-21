package service

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	"finance-sys/internal/rules"
	tradingdomain "finance-sys/internal/trading/domain"
	"finance-sys/internal/trading/instrument"
	"gorm.io/gorm"
)

// ScanExits has no Agent dependency. It persists a sticky EXIT_PENDING cycle
// and DEFER/T1_PENDING audit before shares become sellable, then creates at
// most one active SELL intent per cycle using a fresh provider snapshot.
func (s *Service) ScanExits(ctx context.Context, at time.Time) (map[string]any, error) {
	cfg := s.runtime.Config()
	if cfg == nil {
		return nil, fmt.Errorf("config unavailable")
	}
	if !tradingSessionOpen(at, cfg.Trading) {
		return map[string]any{"skipped": "NOT_TRADING_SESSION"}, nil
	}
	cycles, err := dal.TradingPositionCycles.ListOpen(ctx, s.db, cfg.Trading.Bridge.ExpectedAccountID)
	if err != nil {
		return nil, err
	}
	if len(cycles) == 0 {
		return map[string]any{"cycles": 0, "intents": 0}, nil
	}
	snapshot, err := s.freshBridgeSnapshot(ctx, "exit-monitor")
	if err != nil {
		return nil, err
	}
	symbols := make([]string, 0, len(cycles))
	for _, c := range cycles {
		symbols = append(symbols, c.EastmoneySymbol)
	}
	quotes, err := s.refreshBridgeQuotes(ctx, symbols, s.now())
	if err != nil {
		return nil, err
	}
	quoteBySymbol := map[string]tradingdomain.QuoteSnapshot{}
	for _, q := range quotes {
		quoteBySymbol[q.EastmoneySymbol] = q
	}
	positionBySymbol := map[string]tradingdomain.PositionSnapshot{}
	for _, p := range snapshot.Positions {
		positionBySymbol[p.EastmoneySymbol] = p
	}
	now := s.now()
	bucket := at.UTC().Truncate(30 * time.Second)
	key := hashParts("GO_EXIT", cfg.Trading.Bridge.ExpectedAccountID, cfg.Trading.Decision.StrategyVersion, bucket.Format(time.RFC3339))
	if prior, e := dal.TradingAgentRuns.QueryByRunKey(ctx, s.db, key); e == nil {
		return map[string]any{"run_id": prior.ID, "intents": prior.IntentCount, "replay": true}, nil
	}
	control, err := dal.TradingRuntimeControls.KillSwitch(ctx, s.db)
	if err != nil {
		return nil, err
	}
	dry := !cfg.Trading.Enabled || cfg.Trading.KillSwitch || control.Enabled || snapshot.Health.KillSwitch || cfg.Trading.Decision.Provider == "SHADOW"
	run := db_model.TradingAgentRun{RunKey: key, TriggerType: "EXIT_MONITOR", Status: "DECISION_SUCCEEDED", DecisionProvider: "GO_RULES", StrategyName: "deterministic-exit", StrategyVersion: cfg.Trading.Decision.StrategyVersion, SchemaVersion: tradingdomain.SchemaVersionIntent, ToolContractVersion: cfg.Trading.Decision.ToolContractVersion, PromptVersion: "go-exit-v1", AsOfTime: now, RequestJSON: marshalJSON(map[string]any{"cycles": cycles, "snapshot": snapshot.Account, "positions": snapshot.Positions, "quotes": quotes}), RawOutputJSON: []byte(`{}`), DryRun: dry, QueuedAt: now, StartedAt: &now, DecisionCompletedAt: &now, FinishedAt: &now, ConfigVersion: cfg.Meta.ConfigVersion}
	run.InputSnapshotHash = hashParts(string(run.RequestJSON))
	created := 0
	deferred := 0
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := dal.TradingRuntimeControls.Lock(ctx, tx); e != nil {
			return e
		}
		if e := dal.TradingAgentRuns.Create(ctx, tx, &run); e != nil {
			return e
		}
		for _, observed := range cycles {
			cycle, e := dal.TradingPositionCycles.QueryByID(ctx, tx, observed.ID)
			if e != nil {
				return e
			}
			if cycle.Status == "CLOSED" {
				continue
			}
			position := positionBySymbol[cycle.EastmoneySymbol]
			q := quoteBySymbol[cycle.EastmoneySymbol]
			if position.Volume != cycle.CurrentVolume {
				return fmt.Errorf("POSITION_DIFF for %s", cycle.EastmoneySymbol)
			}
			days, e := cfg.Trading.Calendar.Elapsed(cycle.EntryTradeDate, now)
			if e != nil {
				return e
			}
			reason, e := rules.TradingExitReason(q.Price, cycle.StopLossPrice, cycle.TakeProfitPrice, days, int(cycle.MaxHoldingTradeDays))
			if e != nil {
				return fmt.Errorf("exit quote/rule missing for %s: %w", cycle.EastmoneySymbol, e)
			}
			if cycle.ExitReason != "" {
				reason = cycle.ExitReason
			}
			if reason == "" {
				continue
			}
			values := map[string]any{"status": "EXIT_PENDING", "exit_reason": reason, "available_volume": position.AvailableVolume, "holding_trade_days": days, "last_evaluated_at": now}
			if e := dal.TradingPositionCycles.Update(ctx, tx, cycle.ID, values); e != nil {
				return e
			}
			decision := "SELL"
			message := reason
			var intentID *int64
			active, e := dal.TradingIntents.HasActiveExit(ctx, tx, cycle.ID)
			if e != nil {
				return e
			}
			if position.AvailableVolume <= 0 {
				decision = "DEFER"
				message = "T1_PENDING"
			} else if active {
				decision = "DEFER"
				message = "EXIT_ALREADY_PENDING"
			} else if created >= cfg.Trading.Decision.MaxIntentsPerRun {
				decision = "DEFER"
				message = "BATCH_LIMIT"
			}
			if decision == "SELL" {
				price, priceErr := rules.TradingPrice("SELL", q, now, time.Duration(cfg.Trading.Risk.MaxSnapshotAgeSeconds)*time.Second)
				unit, unitErr := instrument.UnitRule(cycle.BoardType)
				volume, volumeErr := instrument.RoundSellVolume(min64(position.AvailableVolume, cycle.CurrentVolume), position.AvailableVolume, unit)
				if priceErr != nil || unitErr != nil || volumeErr != nil {
					decision = "DEFER"
					message = "EXIT_PRICE_OR_UNIT_UNAVAILABLE"
				} else {
					until := validUntilForWindow(now, 25*time.Minute, cfg.Trading.Scheduler)
					d := tradingdomain.TradeIntent{PositionCycleID: &cycle.ID, Symbol: cycle.Symbol, TSCode: cycle.TSCode, Market: cycle.Market, AssetType: cycle.AssetType, BoardType: cycle.BoardType, Action: "SELL", ProposedOrderType: "LIMIT", ProposedLimitPrice: price, ProposedPositionRatio: "1", ProposedVolume: &volume, ValidFrom: now, ValidUntil: until, Confidence: "1", EvidenceRefs: []string{"position_cycle:" + strconv.FormatInt(cycle.ID, 10), "exit_rule:" + reason}, Reason: reason}
					d.IntentKey = intentKey(cfg.Trading.Decision.StrategyVersion, now, d)
					executionStatus := "READY"
					if dry {
						executionStatus = "SHADOW_READY"
					}
					i := db_model.TradingIntent{TradingAgentRunID: run.ID, IntentKey: d.IntentKey, PositionCycleID: &cycle.ID, Symbol: d.Symbol, TSCode: d.TSCode, Market: d.Market, AssetType: d.AssetType, BoardType: d.BoardType, EastmoneySymbol: cycle.EastmoneySymbol, TradeAction: "SELL", Status: "READY_FOR_EXECUTION", ExecutionStatus: executionStatus, ProposedOrderType: "LIMIT", ProposedLimitPrice: &price, ProposedPositionRatio: &d.ProposedPositionRatio, ProposedVolume: &volume, Confidence: "1", ValidFrom: now, ValidUntil: until, Reason: reason, EvidenceRefsJSON: marshalJSON(d.EvidenceRefs), RawIntentJSON: marshalJSON(d), StrategyVersion: cfg.Trading.Decision.StrategyVersion, ConfigVersion: cfg.Meta.ConfigVersion}
					if e := dal.TradingIntents.Create(ctx, tx, &i); e != nil {
						return e
					}
					intentID = &i.ID
					created++
				}
			}
			if decision == "DEFER" {
				deferred++
			}
			audit := db_model.TradingSkillDecision{DecisionKey: hashParts(key, strconv.FormatInt(cycle.ID, 10)), TradingAgentRunID: run.ID, TradingIntentID: intentID, PositionCycleID: &cycle.ID, Stage: "EXIT", SkillName: "go_position_exit", SkillVersion: "1", Decision: decision, Score: "1", Reason: message, InputJSON: marshalJSON(map[string]any{"cycle": cycle, "quote": q, "available_volume": position.AvailableVolume, "holding_trade_days": days}), OutputJSON: marshalJSON(map[string]any{"exit_reason": reason, "pending": decision == "DEFER"}), EvaluatedAt: now}
			if e := dal.TradingSkillDecisions.CreateBatch(ctx, tx, []db_model.TradingSkillDecision{audit}); e != nil {
				return e
			}
		}
		return dal.TradingAgentRuns.Update(ctx, tx, run.ID, map[string]any{"candidate_count": len(cycles), "intent_count": created, "raw_output_json": marshalJSON(map[string]any{"created": created, "deferred": deferred})})
	})
	return map[string]any{"run_id": run.ID, "intents": created, "deferred": deferred, "dry_run": dry}, err
}

func validUntilForWindow(now time.Time, ttl time.Duration, cfg config.TradingSchedulerConfig) time.Time {
	local := now.In(config.ChinaLocation)
	until := local.Add(ttl)
	clock := local.Format("15:04:05")
	for _, window := range []config.TradingSessionWindow{cfg.MorningWindow, cfg.AfternoonWindow} {
		if clock >= window.Start && clock < window.End {
			end, err := time.ParseInLocation("2006-01-02 15:04:05", local.Format(time.DateOnly)+" "+window.End, config.ChinaLocation)
			if err == nil && end.Before(until) {
				return end
			}
		}
	}
	return until
}
