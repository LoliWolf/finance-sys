package service

import (
	"testing"
	"time"

	"finance-sys/internal/domain/db_model"
	tradingdomain "finance-sys/internal/trading/domain"
	"github.com/stretchr/testify/require"
)

func TestDashboardDateRangeUsesConfiguredTimezone(t *testing.T) {
	date, from, to, err := dashboardDateRange("2026-09-04", "Asia/Shanghai")
	require.NoError(t, err)
	require.Equal(t, "2026-09-04", date)
	require.Equal(t, 24*time.Hour, to.Sub(from))
	_, offset := from.Zone()
	require.Equal(t, 8*60*60, offset)
}

func TestBuildDashboardDailyUsesFillsAndCommission(t *testing.T) {
	result := buildDashboardDaily([]db_model.TradingFill{
		{Side: "BUY", Volume: 100, Amount: "918.000000", Commission: "5.000000"},
		{Side: "SELL", Volume: 100, Amount: "896.000000", Commission: "5.000000"},
	})
	require.Equal(t, 2, result.FillCount)
	require.Equal(t, 1, result.BuyCount)
	require.Equal(t, 1, result.SellCount)
	require.Equal(t, int64(100), result.BuyVolume)
	require.Equal(t, int64(100), result.SellVolume)
	require.Equal(t, "918.000000", result.BuyAmount)
	require.Equal(t, "896.000000", result.SellAmount)
	require.Equal(t, "10.000000", result.Commission)
	require.Equal(t, "-32.000000", result.NetCashFlow)
}

func TestBuildDashboardCyclesCalculatesRealizedPnLAfterCommission(t *testing.T) {
	entryID := int64(10)
	exitID := int64(11)
	exitPrice := "8.960000"
	cycles := []db_model.TradingPositionCycle{{
		ID: 1, Symbol: "600000", TSCode: "600000.SH", EastmoneySymbol: "SHSE.600000",
		Status: "CLOSED", EntryOrderID: &entryID, ExitOrderID: &exitID, EntryPrice: "9.180000", InitialVolume: 100,
	}}
	orders := map[int64]db_model.TradingOrder{
		entryID: {ID: entryID, FilledAmount: "918.000000", FilledCommission: "5.000000"},
		exitID:  {ID: exitID, FilledAmount: "896.000000", FilledCommission: "5.000000", FilledVwap: &exitPrice},
	}
	result := buildDashboardCycles(cycles, orders, nil)
	require.Len(t, result, 1)
	require.NotNil(t, result[0].RealizedPnL)
	require.Equal(t, "-32.000000", *result[0].RealizedPnL)
	require.Equal(t, &exitPrice, result[0].ExitPrice)
}

func TestDashboardSnapshotAdvancedRequiresANewSnapshot(t *testing.T) {
	baselineTime := time.Date(2026, 9, 4, 9, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	baseline := &tradingdomain.ReconciliationSnapshot{
		SnapshotVersion: "old",
		Account:         tradingdomain.AccountSnapshot{SnapshotAt: baselineTime},
	}
	require.False(t, dashboardSnapshotAdvanced(baseline, baseline, baselineTime.Add(time.Second), 15*time.Second))
	require.False(t, dashboardSnapshotAdvanced(baseline, &tradingdomain.ReconciliationSnapshot{
		SnapshotVersion: "new",
		Account:         tradingdomain.AccountSnapshot{SnapshotAt: baselineTime},
	}, baselineTime.Add(time.Second), 15*time.Second))
	require.True(t, dashboardSnapshotAdvanced(baseline, &tradingdomain.ReconciliationSnapshot{
		SnapshotVersion: "new",
		Account:         tradingdomain.AccountSnapshot{SnapshotAt: baselineTime.Add(time.Second)},
	}, baselineTime.Add(time.Second), 15*time.Second))
}
