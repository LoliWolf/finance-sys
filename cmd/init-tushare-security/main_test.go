package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSecurityMasterFromStockRecord(t *testing.T) {
	record := map[string]any{
		"ts_code":     "300502.SZ",
		"symbol":      "300502",
		"name":        "XYS",
		"fullname":    "Chengdu XYS Telecom Tech Co Ltd",
		"exchange":    "SZSE",
		"market":      "ChiNext",
		"industry":    "Telecom Equipment",
		"list_status": "L",
		"list_date":   "20160426",
	}

	model, aliases, ok := securityMasterFromRecord(record, "STOCK", "L")
	require.True(t, ok)
	require.Equal(t, "300502.SZ", model.TSCode)
	require.Equal(t, "300502", model.Symbol)
	require.Equal(t, "XYS", model.Name)
	require.Equal(t, "Chengdu XYS Telecom Tech Co Ltd", model.FullName)
	require.Equal(t, "SZSE", model.Exchange)
	require.Equal(t, "SZ", model.Market)
	require.Equal(t, "STOCK", model.AssetType)
	require.Equal(t, "L", model.ListStatus)
	require.True(t, model.IsActive)
	require.Equal(t, "Telecom Equipment", model.Industry)
	require.NotNil(t, model.ListDate)
	require.Equal(t, time.Date(2016, 4, 26, 0, 0, 0, 0, time.UTC), *model.ListDate)
	require.Len(t, aliases, 1)
	require.Equal(t, aliasTypeFullName, aliases[0].AliasType)
	require.Equal(t, "Chengdu XYS Telecom Tech Co Ltd", aliases[0].Alias)
}

func TestSecurityMasterFromETFRecordDerivesSymbolAndExchange(t *testing.T) {
	record := map[string]any{
		"ts_code":   "510050.SH",
		"name":      "SSE 50 ETF",
		"list_date": "2005-02-23T00:00:00.000",
	}

	model, aliases, ok := securityMasterFromRecord(record, "ETF", "L")
	require.True(t, ok)
	require.Equal(t, "510050.SH", model.TSCode)
	require.Equal(t, "510050", model.Symbol)
	require.Equal(t, "SSE", model.Exchange)
	require.Equal(t, "SH", model.Market)
	require.Equal(t, "ETF", model.AssetType)
	require.True(t, model.IsActive)
	require.Empty(t, aliases)
	require.NotNil(t, model.ListDate)
	require.Equal(t, time.Date(2005, 2, 23, 0, 0, 0, 0, time.UTC), *model.ListDate)
}

func TestSecurityMasterFromRecordSkipsIncompleteRows(t *testing.T) {
	_, _, ok := securityMasterFromRecord(map[string]any{"name": "missing code"}, "STOCK", "L")
	require.False(t, ok)

	_, _, ok = securityMasterFromRecord(map[string]any{"ts_code": "300502.SZ"}, "STOCK", "L")
	require.False(t, ok)
}

func TestStringValueHandlesNumericJSON(t *testing.T) {
	record := map[string]any{
		"symbol": float64(600519),
		"price":  float64(12.34),
	}

	require.Equal(t, "600519", stringValue(record, "symbol"))
	require.Equal(t, "12.34", stringValue(record, "price"))
}

func TestSplitCSVNormalizesValues(t *testing.T) {
	require.Equal(t, []string{"L", "D", "P"}, splitCSV(" l, D ,,p "))
}

func TestIsFundETFRecord(t *testing.T) {
	require.True(t, isFundETFRecord(map[string]any{
		"ts_code": "510050.SH",
		"name":    "SSE 50 ETF",
	}))
	require.False(t, isFundETFRecord(map[string]any{
		"ts_code": "160119.SZ",
		"name":    "CSI 500 LOF",
	}))
	require.False(t, isFundETFRecord(map[string]any{
		"ts_code": "513500.OF",
		"name":    "S&P 500 ETF feeder",
	}))
}
