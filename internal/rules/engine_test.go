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
	plan := engine.Generate(domain.ExpertSignal{
		ID:            10,
		DocumentID:    11,
		ExpertName:    "Alice",
		Symbol:        "600519.SH",
		Sentiment:     "BULLISH",
		ConfigVersion: 2,
	}, domain.MarketSnapshot{
		ID:       20,
		Symbol:   "600519.SH",
		Close:    100,
		High:     102,
		Low:      98,
		Turnover: 100000000,
		ATR:      3,
	}, config.RulesConfig{
		Version:         "rules-v1",
		DefaultStrategy: "OPEN_GAP_FILTER",
		Risk: config.RulesRiskConfig{
			MaxPositionPct:       0.1,
			MaxGapPct:            0.03,
			DefaultStopATR:       1.5,
			DefaultTakeProfitATR: 2.5,
			MinAvgTurnoverCNY:    50000000,
		},
	}, time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC))
	require.Equal(t, "LONG", plan.Direction)
	require.Equal(t, "OPEN_GAP_FILTER", plan.Strategy)
	require.Greater(t, plan.TakeProfit, plan.EntryPrice)
	require.Less(t, plan.StopLoss, plan.EntryPrice)
}
