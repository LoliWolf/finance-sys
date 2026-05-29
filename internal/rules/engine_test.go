package rules_test

import (
	"testing"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/domain"
	"finance-sys/internal/rules"

	"github.com/stretchr/testify/require"
)

func TestGeneratePlan(t *testing.T) {
	engine := rules.New(nil)
	plan := engine.Generate(domain.TrackablePlanIntent{
		Analyst:        "Alice",
		RawSymbol:      "贵州茅台",
		TSCode:         "600519.SH",
		Symbol:         "600519",
		SecurityName:   "贵州茅台",
		AssetType:      domain.AssetTypeAShare,
		Market:         domain.MarketSH,
		Direction:      domain.TradeDirectionLong,
		ReferencePrice: 100,
		Confidence:     0.8,
		Thesis:         "渠道改善",
	}, config.RulesConfig{
		Version:              "rules-v2",
		Strategy:             config.RuleStrategyTextReferencePrice,
		TradeDateOffsetDays:  1,
		MaxPositionPct:       0.1,
		DefaultStopLossPct:   0.03,
		DefaultTakeProfitPct: 0.06,
		MinConfidence:        0.65,
	}, time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC), 2)
	require.Equal(t, domain.TradeDirectionLong, plan.Direction)
	require.Equal(t, domain.RuleStrategyTextReferencePrice, plan.Strategy)
	require.Equal(t, 100.0, plan.EntryPrice)
	require.Equal(t, 97.0, plan.StopLoss)
	require.Equal(t, 106.0, plan.TakeProfit)
	require.Equal(t, domain.CandidatePlanStatusReady, plan.Status)
}

func TestGenerateShortPlan(t *testing.T) {
	engine := rules.New(nil)
	plan := engine.Generate(domain.TrackablePlanIntent{
		Analyst:        "Alice",
		RawSymbol:      "平安银行",
		TSCode:         "000001.SZ",
		Symbol:         "000001",
		SecurityName:   "平安银行",
		AssetType:      domain.AssetTypeAShare,
		Market:         domain.MarketSZ,
		Direction:      domain.TradeDirectionShort,
		ReferencePrice: 10,
		Confidence:     0.9,
		Thesis:         "weak demand",
	}, config.RulesConfig{
		Version:              "rules-v2",
		Strategy:             config.RuleStrategyTextReferencePrice,
		MaxPositionPct:       0.1,
		DefaultStopLossPct:   0.03,
		DefaultTakeProfitPct: 0.06,
		MinConfidence:        0.65,
	}, time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC), 2)

	require.Equal(t, domain.TradeDirectionShort, plan.Direction)
	require.Equal(t, 10.0, plan.EntryPrice)
	require.Equal(t, 10.3, plan.StopLoss)
	require.Equal(t, 9.4, plan.TakeProfit)
	require.Equal(t, domain.CandidatePlanStatusReady, plan.Status)
}

func TestGeneratePlanNeedsReviewWithoutReferencePrice(t *testing.T) {
	engine := rules.New(nil)
	plan := engine.Generate(domain.TrackablePlanIntent{
		Analyst:        "Alice",
		RawSymbol:      "贵州茅台",
		TSCode:         "600519.SH",
		Symbol:         "600519",
		SecurityName:   "贵州茅台",
		AssetType:      domain.AssetTypeAShare,
		Market:         domain.MarketSH,
		Direction:      domain.TradeDirectionLong,
		ReferencePrice: 0,
		Confidence:     0.8,
		Thesis:         "recommended but no explicit price",
	}, config.RulesConfig{
		Version:              "rules-v2",
		Strategy:             config.RuleStrategyTextReferencePrice,
		MaxPositionPct:       0.1,
		DefaultStopLossPct:   0.03,
		DefaultTakeProfitPct: 0.06,
		MinConfidence:        0.65,
	}, time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC), 2)

	require.Equal(t, domain.CandidatePlanStatusNeedsReview, plan.Status)
	require.Equal(t, 0.0, plan.EntryPrice)
	require.Contains(t, plan.PricingNote, "missing explicit price")
}
