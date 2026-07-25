package evaluation_test

import (
	"testing"
	"time"

	"finance-sys/internal/evaluation"

	"github.com/stretchr/testify/require"
)

func TestEvaluateWindowLong(t *testing.T) {
	result := evaluation.EvaluateWindow(evaluation.Input{
		RecommendDate:         date("2026-01-02"),
		Direction:             "LONG",
		WindowDays:            5,
		Quotes:                longQuotes(),
		MarketTradingDates:    dates("2026-01-05", "2026-01-06", "2026-01-07", "2026-01-08", "2026-01-09"),
		LatestMarketDate:      date("2026-01-09"),
		WinThresholdRatio:     0,
		MinQuoteCoverageRatio: 1,
	})

	require.Equal(t, evaluation.StatusReady, result.Status)
	require.Equal(t, "2026-01-02", result.BaseDate.Format(time.DateOnly))
	require.Equal(t, "2026-01-05", result.EntryDate.Format(time.DateOnly))
	require.Equal(t, "2026-01-09", result.ExitDate.Format(time.DateOnly))
	require.InDelta(t, 10, *result.EntryPrice, 0.000001)
	require.InDelta(t, 0.2, *result.RawReturnRatio, 0.000001)
	require.InDelta(t, 0.2, *result.DirectionReturnRatio, 0.000001)
	require.InDelta(t, 0.3, *result.MaxFavorableReturnRatio, 0.000001)
	require.InDelta(t, -0.1, *result.MaxAdverseReturnRatio, 0.000001)
	require.InDelta(t, -0.0608695652, *result.MaxDrawdownRatio, 0.000001)
	require.True(t, *result.WinFlag)
	require.Equal(t, 5, result.ActualQuoteCount)
	require.Zero(t, result.MissingQuoteCount)
}

func TestEvaluateWindowShortUsesDirectionalFormula(t *testing.T) {
	quotes := []evaluation.Quote{
		quote("2026-01-02", 10, 10.4, 9.8, 10),
		quote("2026-01-05", 10, 10.2, 9.2, 9.5),
		quote("2026-01-06", 9.5, 9.7, 8.8, 9),
	}
	result := evaluation.EvaluateWindow(evaluation.Input{
		RecommendDate:      date("2026-01-02"),
		Direction:          "short",
		WindowDays:         2,
		Quotes:             quotes,
		MarketTradingDates: dates("2026-01-05", "2026-01-06"),
		LatestMarketDate:   date("2026-01-06"),
	})

	require.Equal(t, evaluation.StatusReady, result.Status)
	require.InDelta(t, -0.1, *result.RawReturnRatio, 0.000001)
	require.InDelta(t, 10.0/9.0-1, *result.DirectionReturnRatio, 0.000001)
	require.InDelta(t, 10.0/8.8-1, *result.MaxFavorableReturnRatio, 0.000001)
	require.InDelta(t, 10.0/10.2-1, *result.MaxAdverseReturnRatio, 0.000001)
	require.True(t, *result.WinFlag)
}

func TestEvaluateWindowPendingWhenWindowHasNotMatured(t *testing.T) {
	result := evaluation.EvaluateWindow(evaluation.Input{
		RecommendDate: date("2026-01-02"),
		Direction:     "LONG",
		WindowDays:    5,
		Quotes: []evaluation.Quote{
			quote("2026-01-02", 10, 10, 10, 10),
			quote("2026-01-05", 10, 11, 9, 10.5),
			quote("2026-01-06", 10.5, 11, 10, 10.8),
		},
		MarketTradingDates: dates("2026-01-05", "2026-01-06"),
		LatestMarketDate:   date("2026-01-06"),
	})

	require.Equal(t, evaluation.StatusPending, result.Status)
	require.Equal(t, evaluation.ReasonWindowNotMatured, result.ReasonCode)
	require.Equal(t, 2, result.ActualQuoteCount)
	require.Equal(t, 3, result.MissingQuoteCount)
}

func TestEvaluateWindowIncompleteWhenMarketMaturedButQuotesMissing(t *testing.T) {
	result := evaluation.EvaluateWindow(evaluation.Input{
		RecommendDate: date("2026-01-02"),
		Direction:     "LONG",
		WindowDays:    5,
		Quotes: []evaluation.Quote{
			quote("2026-01-02", 10, 10, 10, 10),
			quote("2026-01-05", 10, 11, 9, 10.5),
			quote("2026-01-06", 10.5, 11, 10, 10.8),
		},
		MarketTradingDates:    dates("2026-01-05", "2026-01-06", "2026-01-07", "2026-01-08", "2026-01-09"),
		LatestMarketDate:      date("2026-01-09"),
		MinQuoteCoverageRatio: 1,
	})

	require.Equal(t, evaluation.StatusIncomplete, result.Status)
	require.Equal(t, evaluation.ReasonQuoteGap, result.ReasonCode)
	require.Equal(t, 2, result.ActualQuoteCount)
	require.Equal(t, 3, result.MissingQuoteCount)
}

func TestEvaluateWindowRejectsInvalidOHLC(t *testing.T) {
	result := evaluation.EvaluateWindow(evaluation.Input{
		RecommendDate: date("2026-01-02"),
		Direction:     "LONG",
		WindowDays:    1,
		Quotes: []evaluation.Quote{
			quote("2026-01-02", 10, 10, 10, 10),
			quote("2026-01-05", 0, 11, 9, 10),
		},
		MarketTradingDates: dates("2026-01-05"),
		LatestMarketDate:   date("2026-01-05"),
	})

	require.Equal(t, evaluation.StatusFailed, result.Status)
	require.Equal(t, evaluation.ReasonInvalidPrice, result.ReasonCode)
}

func longQuotes() []evaluation.Quote {
	return []evaluation.Quote{
		quote("2026-01-02", 9.8, 10.2, 9.7, 10),
		quote("2026-01-05", 10, 11, 9, 10.5),
		quote("2026-01-06", 10.5, 12, 10.2, 11.5),
		quote("2026-01-07", 11.5, 11.8, 10.5, 10.8),
		quote("2026-01-08", 10.8, 13, 10.7, 12.5),
		quote("2026-01-09", 12.5, 12.8, 11.5, 12),
	}
}

func quote(day string, open, high, low, close float64) evaluation.Quote {
	return evaluation.Quote{TradeDate: date(day), Open: open, High: high, Low: low, Close: close}
}

func dates(values ...string) []time.Time {
	result := make([]time.Time, 0, len(values))
	for _, value := range values {
		result = append(result, date(value))
	}
	return result
}

func date(value string) time.Time {
	parsed, err := time.Parse(time.DateOnly, value)
	if err != nil {
		panic(err)
	}
	return parsed
}
