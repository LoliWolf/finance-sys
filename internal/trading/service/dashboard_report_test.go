package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"finance-sys/internal/domain/db_model"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedReportFills(t *testing.T, count int) *Service {
	t.Helper()
	s := ledgerService(t)
	require.NoError(t, s.db.Exec(`CREATE TABLE security_master(id INTEGER PRIMARY KEY,ts_code TEXT,name TEXT)`).Error)
	require.NoError(t, s.db.Exec(`INSERT INTO security_master VALUES(1,'600000.SH','测试证券')`).Error)
	require.NoError(t, s.db.Transaction(func(tx *gorm.DB) error {
		for i := 0; i < count; i++ {
			fill := db_model.TradingFill{AccountID: "fixture", ExecID: fmt.Sprint(i), Symbol: "600000", EastmoneySymbol: "SHSE.600000", Side: "BUY", Price: "10", Volume: 10, Amount: "100", Commission: "1", CommissionStatus: "REPORTED", TradedAt: time.Date(2026, 9, 10, 2, 0, 0, 0, time.UTC)}
			if err := tx.Create(&fill).Error; err != nil {
				return err
			}
		}
		return nil
	}))
	return s
}

func TestTradingReportReturnsAllDailyFillsWithoutBridgeOrWrites(t *testing.T) {
	s := seedReportFills(t, 230)
	// Both remote clients are nil: report generation must never depend on them.
	s.bridge = nil
	s.agent = nil
	normal, err := s.TradingDashboard(context.Background(), "2026-09-10")
	require.NoError(t, err)
	require.Len(t, normal.Fills, 200)
	report, err := s.TradingDashboardReport(context.Background(), "2026-09-10")
	require.NoError(t, err)
	require.Len(t, report.Trading.Fills, 230)
	require.Equal(t, 230, report.Trading.DailySummary.FillCount)
	require.Nil(t, report.Trading.Account)
	require.Equal(t, 200, report.CycleLimit)
	for _, table := range []string{"trading_orders", "trading_reconciliation_runs", "trading_agent_runs"} {
		var count int64
		require.NoError(t, s.db.Table(table).Count(&count).Error)
		require.Zero(t, count)
	}
	var count int64
	require.NoError(t, s.db.Table("trading_fills").Count(&count).Error)
	require.EqualValues(t, 230, count)
	empty, err := s.TradingDashboardReport(context.Background(), "2026-09-11")
	require.NoError(t, err)
	require.Empty(t, empty.Trading.Fills)
}
func TestTradingReportRejectsOverflowAndInvalidDate(t *testing.T) {
	s := seedReportFills(t, TradingReportItemLimit+1)
	report, err := s.TradingDashboardReport(context.Background(), "2026-09-10")
	var tooLarge *TradingReportLimitError
	require.ErrorAs(t, err, &tooLarge)
	require.Nil(t, report)
	_, err = s.TradingDashboardReport(context.Background(), "wrong")
	require.Error(t, err)
}
