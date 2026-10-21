package service

import (
	"finance-sys/internal/marketdata"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestETFTradingSymbolIsNotBenchmarkIndex(t *testing.T) {
	for _, code := range []string{"510300.SH", "159919.SZ"} {
		model, _, ok := securityMasterFromProviderRow(marketdata.ProviderRow{Values: map[string]any{"ts_code": code, "index_code": "000300.SH", "csname": "ETF", "list_status": "L", "list_date": "20120528"}}, "ETF", "L", SecurityMasterSourceTushare, time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC))
		require.True(t, ok)
		require.Equal(t, code[:6], model.Symbol)
		require.Equal(t, code, model.TSCode)
	}
}
