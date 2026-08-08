package service

import (
	"testing"
	"time"

	"finance-sys/internal/marketdata"

	"github.com/stretchr/testify/require"
)

func TestSecurityMasterFromProviderRowMapsCurrentETFFields(t *testing.T) {
	row := marketdata.ProviderRow{Values: map[string]any{
		"ts_code":     "158006.SZ",
		"csname":      "创业板ETF样例",
		"cname":       "创业板交易型开放式指数证券投资基金",
		"extname":     "创业板ETF基金完整名称",
		"exchange":    "SZSE",
		"list_status": "L",
		"list_date":   "20260810",
	}}
	asOfDate := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)

	model, _, ok := securityMasterFromProviderRow(row, "ETF", "L", SecurityMasterSourceTushare, asOfDate)

	require.True(t, ok)
	require.Equal(t, "创业板ETF样例", model.Name)
	require.Equal(t, "创业板ETF基金完整名称", model.FullName)
	require.Equal(t, "SZ", model.Market)
	require.False(t, model.IsActive, "future-listing ETF must remain inactive")
}

func TestSecurityMasterFromProviderRowMapsDCSectorAndAliases(t *testing.T) {
	row := marketdata.ProviderRow{Values: map[string]any{
		"ts_code":   "BK1128.DC",
		"name":      "CPO概念",
		"fullname":  "CPO概念板块指数",
		"idx_type":  "概念板块",
		"list_date": "20200101",
	}}

	model, aliases, ok := securityMasterFromProviderRow(row, "SECTOR", "L", SecurityMasterSourceTushareDC, time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC))

	require.True(t, ok)
	require.Equal(t, "BK1128", model.Symbol)
	require.Equal(t, "DC", model.Market)
	require.Equal(t, "SECTOR", model.AssetType)
	require.Equal(t, "CONCEPT", model.SectorType)
	require.True(t, model.IsActive)
	aliasValues := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		aliasValues = append(aliasValues, alias.Alias)
	}
	require.Contains(t, aliasValues, "CPO板块")
	require.Contains(t, aliasValues, "CPO概念板块")
}

func TestStockDailyQuoteFromProviderRowUsesDCPctChange(t *testing.T) {
	model, _, ok := securityMasterFromProviderRow(marketdata.ProviderRow{Values: map[string]any{
		"ts_code":  "BK1128.DC",
		"name":     "CPO概念",
		"idx_type": "概念板块",
	}}, "SECTOR", "L", SecurityMasterSourceTushareDC, time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC))
	require.True(t, ok)
	model.ID = 7
	quote, err := stockDailyQuoteFromProviderRow(*model, time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC), marketdata.ProviderRow{Values: map[string]any{
		"ts_code":    "BK1128.DC",
		"open":       1000.0,
		"high":       1020.0,
		"low":        990.0,
		"close":      1015.0,
		"pct_change": 1.5,
	}}, 12)

	require.NoError(t, err)
	require.Equal(t, "SECTOR", quote.AssetType)
	require.Equal(t, "CONCEPT", quote.SectorType)
	require.Equal(t, 1.5, quote.PctChg)
}
