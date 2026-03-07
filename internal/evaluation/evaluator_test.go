package evaluation_test

import (
	"testing"
	"time"

	"finance-sys/internal/domain"
	"finance-sys/internal/evaluation"

	"github.com/stretchr/testify/require"
)

func TestEvaluateLongPlan(t *testing.T) {
	evaluator := evaluation.New()
	result := evaluator.Evaluate(domain.TradePlan{
		ID:         1,
		Direction:  "LONG",
		EntryPrice: 100,
		StopLoss:   95,
		TakeProfit: 108,
	}, []domain.MinuteBar{
		{TradeTime: time.Now(), Low: 99.5, High: 101},
		{TradeTime: time.Now(), Low: 100.5, High: 108.5},
	}, domain.DailyBar{
		TradeDate: time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC),
		Close:     107,
	}, 1.2, 3)
	require.Equal(t, "SUCCESS", result.Status)
	require.Greater(t, result.PNLPct, 0.0)
}
