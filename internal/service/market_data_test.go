package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/domain/db_model"
	"finance-sys/internal/marketdata"

	"github.com/stretchr/testify/require"
)

func TestNormalizeStockDailySyncRequestRequiresTradeDate(t *testing.T) {
	_, err := normalizeStockDailySyncRequestFromAPI(StockDailySyncRequest{})
	require.ErrorContains(t, err, "trade_date is required")

	request, err := normalizeStockDailySyncRequestFromAPI(StockDailySyncRequest{TradeDate: "2026-06-26"})
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC), request.TradeDate)
}

func TestMarketDataAssetTypesMapStockToAShareAndETF(t *testing.T) {
	require.Equal(t, []string{"STOCK", "A_SHARE", "ETF"}, marketDataSecurityAssetTypes([]string{"STOCK", "ETF"}))
	require.Equal(t, []string{"STOCK", "A_SHARE", "ETF", "SECTOR"}, marketDataSecurityAssetTypes(nil))
}

func TestTushareTokenAliasFallbackKeepsDuplicateAliasesDistinct(t *testing.T) {
	tokens := enabledTushareTokens([]config.TushareTokenConfig{
		{Alias: "primary", Token: "token-a", Enabled: true, Weight: 1},
		{Alias: "primary", Token: "token-b", Enabled: true, Weight: 1},
		{Alias: "", Token: "token-c", Enabled: true, Weight: 1},
	})

	require.Equal(t, "primary", tokens[0].Alias)
	require.Equal(t, "primary#2", tokens[1].Alias)
	require.Equal(t, "token_3", tokens[2].Alias)
}

func TestRunConcurrentLimitsParallelism(t *testing.T) {
	items := make([]int, 40)
	var active int64
	var maxActive int64

	err := runConcurrent(context.Background(), items, defaultStockDailyTaskConcurrency, func(int) error {
		current := atomic.AddInt64(&active, 1)
		for {
			previous := atomic.LoadInt64(&maxActive)
			if current <= previous || atomic.CompareAndSwapInt64(&maxActive, previous, current) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		atomic.AddInt64(&active, -1)
		return nil
	})

	require.NoError(t, err)
	require.LessOrEqual(t, maxActive, int64(defaultStockDailyTaskConcurrency))
	require.Greater(t, maxActive, int64(1))
}

func TestRetryRetryableDBErrorRetriesDeadlock(t *testing.T) {
	attempts := 0

	err := retryRetryableDBError(context.Background(), 3, nil, func() error {
		attempts++
		if attempts < 3 {
			return errors.New("Error 1213 (40001): Deadlock found when trying to get lock; try restarting transaction")
		}
		return nil
	})

	require.NoError(t, err)
	require.Equal(t, 3, attempts)
}

func TestAssociateStockDailyProviderRowsPersistsOnlyMatchedLocalSecurities(t *testing.T) {
	tradeDate := time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC)
	securities := []db_model.SecurityMaster{
		{
			ID:         1,
			TSCode:     "000001.SZ",
			Symbol:     "000001",
			Name:       "Ping An Bank",
			Exchange:   "SZSE",
			Market:     "SZ",
			AssetType:  "A_SHARE",
			Industry:   "Bank",
			ListStatus: "L",
		},
		{
			ID:         2,
			TSCode:     "000002.SZ",
			Symbol:     "000002",
			Name:       "Vanke",
			Exchange:   "SZSE",
			Market:     "SZ",
			AssetType:  "A_SHARE",
			Industry:   "Real Estate",
			ListStatus: "L",
		},
	}
	rows := []stockDailyProviderRow{
		{
			assetType: "A_SHARE",
			row: marketdata.ProviderRow{Values: map[string]any{
				"ts_code":    "000001.SZ",
				"trade_date": "20260626",
				"open":       10.1,
				"high":       10.9,
				"low":        9.8,
				"close":      10.5,
			}},
		},
		{
			assetType: "A_SHARE",
			row: marketdata.ProviderRow{Values: map[string]any{
				"ts_code":    "999999.SZ",
				"trade_date": "20260626",
				"close":      99.9,
			}},
		},
	}

	quotes, missing, providerErrors, err := associateStockDailyProviderRows(context.Background(), 99, securities, tradeDate, rows, nil, 7, defaultStockDailyTaskConcurrency)

	require.NoError(t, err)
	require.Equal(t, 1, providerErrors)
	require.Len(t, quotes, 1)
	require.Equal(t, int64(1), quotes[0].SecurityMasterID)
	require.Equal(t, "000001.SZ", quotes[0].TSCode)
	require.Equal(t, 10.5, quotes[0].ClosePrice)

	require.Len(t, missing, 2)
	missingByCode := make(map[string]db_model.MarketDataSyncMissingItem, len(missing))
	for _, item := range missing {
		missingByCode[item.TSCode] = item
	}
	require.NotNil(t, missingByCode["000002.SZ"].SecurityMasterID)
	require.Equal(t, int64(2), *missingByCode["000002.SZ"].SecurityMasterID)
	require.Equal(t, MissingReasonNotReturned, missingByCode["000002.SZ"].Reason)
	require.Nil(t, missingByCode["999999.SZ"].SecurityMasterID)
	require.Equal(t, MissingReasonUnknownSymbol, missingByCode["999999.SZ"].Reason)
}

func TestAssociateStockDailyProviderRowsAcceptsStockSecurityForAShareProviderRows(t *testing.T) {
	tradeDate := time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC)
	securities := []db_model.SecurityMaster{{
		ID:         1,
		TSCode:     "000001.SZ",
		Symbol:     "000001",
		Name:       "Ping An Bank",
		Exchange:   "SZSE",
		Market:     "SZ",
		AssetType:  "STOCK",
		Industry:   "Bank",
		ListStatus: "L",
	}}
	rows := []stockDailyProviderRow{{
		assetType: "A_SHARE",
		row: marketdata.ProviderRow{Values: map[string]any{
			"ts_code":    "000001.SZ",
			"trade_date": "20260626",
			"close":      10.5,
		}},
	}}

	quotes, missing, providerErrors, err := associateStockDailyProviderRows(context.Background(), 99, securities, tradeDate, rows, nil, 7, defaultStockDailyTaskConcurrency)

	require.NoError(t, err)
	require.Zero(t, providerErrors)
	require.Empty(t, missing)
	require.Len(t, quotes, 1)
	require.Equal(t, "000001.SZ", quotes[0].TSCode)
	require.Equal(t, "STOCK", quotes[0].AssetType)
	require.Equal(t, 10.5, quotes[0].ClosePrice)
}

func TestStockDailyQuoteFromProviderRowMapsFieldsAndRawContent(t *testing.T) {
	tradeDate := time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC)
	security := db_model.SecurityMaster{
		ID:         12,
		TSCode:     "000001.SZ",
		Symbol:     "000001",
		Name:       "平安银行",
		Exchange:   "SZSE",
		Market:     "SZ",
		AssetType:  "A_SHARE",
		Industry:   "银行",
		ListStatus: "L",
	}
	row := marketdata.ProviderRow{
		Fields: []string{"ts_code", "trade_date", "open", "high", "low", "close", "pre_close", "change", "pct_chg", "vol", "amount"},
		Values: map[string]any{
			"ts_code":    "000001.SZ",
			"trade_date": "20260626",
			"open":       10.1,
			"high":       10.9,
			"low":        9.8,
			"close":      10.5,
			"pre_close":  10.0,
			"change":     0.5,
			"pct_chg":    5.0,
			"vol":        12345.6,
			"amount":     78901.2,
		},
	}

	quote, err := stockDailyQuoteFromProviderRow(security, tradeDate, row, 7)

	require.NoError(t, err)
	require.Equal(t, int64(12), quote.SecurityMasterID)
	require.Equal(t, "000001.SZ", quote.TSCode)
	require.Equal(t, "平安银行", quote.SecurityName)
	require.Equal(t, 10.1, quote.OpenPrice)
	require.Equal(t, 10.9, quote.HighPrice)
	require.Equal(t, 9.8, quote.LowPrice)
	require.Equal(t, 10.5, quote.ClosePrice)
	require.Equal(t, 10.0, quote.PreClosePrice)
	require.Equal(t, 0.5, quote.ChangeAmount)
	require.Equal(t, 5.0, quote.PctChg)
	require.Equal(t, 12345.6, quote.Volume)
	require.Equal(t, 78901.2, quote.Amount)
	require.Equal(t, int64(7), quote.ConfigVersion)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(quote.TushareContent, &raw))
	require.Equal(t, "000001.SZ", raw["ts_code"])
	require.Equal(t, float64(10.5), raw["close"])
}
