package rules

import (
	"fmt"
	"math"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/domain"
)

type Engine struct{}

func New() *Engine {
	return &Engine{}
}

func (e *Engine) Generate(signal domain.ExpertSignal, snapshot domain.MarketSnapshot, cfg config.RulesConfig, tradeDate time.Time) domain.TradePlan {
	atr := snapshot.ATR
	if atr <= 0 {
		atr = math.Max(snapshot.High-snapshot.Low, snapshot.Close*0.02)
	}
	direction := "LONG"
	sign := 1.0
	if signal.Sentiment == "BEARISH" {
		direction = "SHORT"
		sign = -1.0
	}

	entryOffset := math.Min(cfg.Risk.MaxGapPct/2, 0.01)
	entryPrice := snapshot.Close * (1 + sign*entryOffset)
	stopLoss := entryPrice - sign*(cfg.Risk.DefaultStopATR*atr)
	takeProfit := entryPrice + sign*(cfg.Risk.DefaultTakeProfitATR*atr)
	invalidation := snapshot.Low
	if direction == "SHORT" {
		invalidation = snapshot.High
	}

	positionPct := cfg.Risk.MaxPositionPct
	strategy := cfg.DefaultStrategy
	rationale := fmt.Sprintf(
		"%s by %s using close %.2f, ATR %.2f, turnover %.2f",
		strategy, signal.ExpertName, snapshot.Close, atr, snapshot.Turnover,
	)
	if snapshot.Turnover < cfg.Risk.MinAvgTurnoverCNY {
		positionPct = positionPct / 2
		rationale += "; liquidity haircut applied"
	}

	return domain.TradePlan{
		SignalID:          signal.ID,
		DocumentID:        signal.DocumentID,
		Symbol:            signal.Symbol,
		Strategy:          strategy,
		TradeDate:         tradeDate,
		Direction:         direction,
		EntryPrice:        round(entryPrice),
		StopLoss:          round(stopLoss),
		TakeProfit:        round(takeProfit),
		InvalidationPrice: round(invalidation),
		PositionPct:       round(positionPct),
		Status:            "PENDING_APPROVAL",
		Rationale:         rationale,
		ConfigVersion:     signal.ConfigVersion,
		RuleVersion:       cfg.Version,
		MarketSnapshotID:  snapshot.ID,
	}
}

func round(value float64) float64 {
	return math.Round(value*1000) / 1000
}
