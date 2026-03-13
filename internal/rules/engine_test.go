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
	engine := rules.New()
	plan := engine.Generate(domain.PlanIntent{
		Analyst:        "Alice",
		Symbol:         "600519.SH",
		Direction:      "LONG",
		ReferencePrice: 100,
		Confidence:     0.8,
		Thesis:         "渠道改善",
	}, config.RulesConfig{
		Version:              "rules-v2",
		Strategy:             "TEXT_REFERENCE_PRICE",
		TradeDateOffsetDays:  1,
		MaxPositionPct:       0.1,
		DefaultStopLossPct:   0.03,
		DefaultTakeProfitPct: 0.06,
		MinConfidence:        0.65,
	}, time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC), 2)
	require.Equal(t, "LONG", plan.Direction)
	require.Equal(t, "TEXT_REFERENCE_PRICE", plan.Strategy)
	require.Equal(t, 100.0, plan.EntryPrice)
	require.Equal(t, 97.0, plan.StopLoss)
	require.Equal(t, 106.0, plan.TakeProfit)
	require.Equal(t, "READY", plan.Status)
}
