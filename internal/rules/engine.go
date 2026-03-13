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

func (e *Engine) Generate(intent domain.PlanIntent, cfg config.RulesConfig, tradeDate time.Time, configVersion int64) domain.CandidatePlan {
	plan := domain.CandidatePlan{
		DocumentID:     0,
		ParseRunID:     0,
		Analyst:        intent.Analyst,
		Institution:    intent.Institution,
		Symbol:         intent.Symbol,
		AssetType:      intent.AssetType,
		Market:         intent.Market,
		Strategy:       cfg.Strategy,
		Direction:      intent.Direction,
		TradeDate:      tradeDate,
		ReferencePrice: round(intent.ReferencePrice),
		Confidence:     round(intent.Confidence),
		Status:         "READY",
		Thesis:         intent.Thesis,
		Risks:          intent.Risks,
		Evidence:       intent.Evidence,
		PricingNote:    intent.ReferencePriceNote,
		ConfigVersion:  configVersion,
		RuleVersion:    cfg.Version,
	}

	if intent.ReferencePrice <= 0 {
		plan.Status = "NEEDS_REVIEW"
		plan.PricingNote = "missing explicit price in source text"
		return plan
	}

	entry := intent.ReferencePrice
	stopFactor := 1 - cfg.DefaultStopLossPct
	takeFactor := 1 + cfg.DefaultTakeProfitPct
	if intent.Direction == "SHORT" {
		stopFactor = 1 + cfg.DefaultStopLossPct
		takeFactor = 1 - cfg.DefaultTakeProfitPct
	}

	position := cfg.MaxPositionPct * math.Max(intent.Confidence, cfg.MinConfidence)
	if position > cfg.MaxPositionPct {
		position = cfg.MaxPositionPct
	}

	plan.EntryPrice = round(entry)
	plan.StopLoss = round(entry * stopFactor)
	plan.TakeProfit = round(entry * takeFactor)
	plan.PositionPct = round(position)
	if intent.Confidence < cfg.MinConfidence {
		plan.Status = "NEEDS_REVIEW"
		plan.PricingNote = fmt.Sprintf("confidence %.2f below threshold %.2f", intent.Confidence, cfg.MinConfidence)
	}
	return plan
}

func round(value float64) float64 {
	return math.Round(value*1000) / 1000
}
