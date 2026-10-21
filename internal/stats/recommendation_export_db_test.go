//go:build cgo

package stats

import (
	"context"
	"fmt"
	"testing"
	"time"

	"finance-sys/internal/config"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func exportTestService(t *testing.T, count int) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = connection.Close() })
	for _, ddl := range []string{
		`CREATE TABLE bloggers(id INTEGER PRIMARY KEY,name TEXT,normalized_name TEXT,institution TEXT)`,
		`CREATE TABLE recommendation_events(id INTEGER PRIMARY KEY,blogger_id INTEGER,symbol TEXT,asset_type TEXT,market TEXT,direction TEXT,recommend_date DATE,thesis TEXT)`,
		`CREATE TABLE security_master(ts_code TEXT PRIMARY KEY,name TEXT,sector_type TEXT)`,
		`CREATE TABLE recommendation_event_window_metrics(id INTEGER PRIMARY KEY,recommendation_event_id INTEGER,ts_code TEXT,symbol TEXT,security_name TEXT,asset_type TEXT,market TEXT,sector_type TEXT,window_days INTEGER,status TEXT,reason_code TEXT,reason_message TEXT,raw_return_ratio REAL,quote_source TEXT)`,
		`CREATE TABLE stock_daily_quotes(trade_date DATE,source TEXT)`,
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	require.NoError(t, db.Exec(`INSERT INTO bloggers VALUES(1,'研究员甲','研究员甲','机构')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO security_master VALUES('600001.SH','示例标的','')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO stock_daily_quotes VALUES('2026-09-11','TUSHARE')`).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		for i := 1; i <= count; i++ {
			if err := tx.Exec(`INSERT INTO recommendation_events VALUES(?,1,'600001.SH','A_SHARE','SH','SHORT',?,?)`, i, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), fmt.Sprintf("完整观点-%d", i)).Error; err != nil {
				return err
			}
		}
		return nil
	}))
	cfg := &config.Config{}
	cfg.Evaluation.RecommendationPerformance.Windows = []int{5, 10, 30, 90}
	cfg.Evaluation.RecommendationPerformance.QuoteSource = "TUSHARE"
	return NewService(db, config.NewRuntime(&config.Snapshot{Config: cfg}))
}

func TestExportReturnsAllFilteredRowsNotCurrentPage(t *testing.T) {
	s := exportTestService(t, 230)
	date := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	filter := Filter{BloggerName: "研究员", Symbol: "600001.SH", Direction: "SHORT", DateFrom: &date, DateTo: &date, Limit: 1, Offset: 200}
	page, err := s.RecommendationLedger(context.Background(), filter)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	exported, err := s.RecommendationExport(context.Background(), filter)
	require.NoError(t, err)
	require.EqualValues(t, 230, exported.Total)
	require.Len(t, exported.Items, 230)
	require.EqualValues(t, 230, exported.Items[0].RecommendationEventID)
	require.EqualValues(t, 1, exported.Items[229].RecommendationEventID)
	require.Equal(t, "完整观点-230", exported.Items[0].Thesis)
	require.Len(t, exported.Items[0].Windows, 4)
	require.Equal(t, "2026-09-11", exported.DataAsOf.Format(time.DateOnly))
	require.Nil(t, exported.Summary[0].AvgReturnRatio)
	filter.BloggerName = "不存在"
	empty, err := s.RecommendationExport(context.Background(), filter)
	require.NoError(t, err)
	require.Empty(t, empty.Items)
	require.Zero(t, empty.Total)
}

func TestExportCapRejectsInsteadOfTruncating(t *testing.T) {
	s := exportTestService(t, MaxRecommendationExportItems+1)
	exported, err := s.RecommendationExport(context.Background(), Filter{Limit: 1})
	var tooLarge *RecommendationExportLimitError
	require.ErrorAs(t, err, &tooLarge)
	require.Nil(t, exported)
	require.EqualValues(t, MaxRecommendationExportItems+1, tooLarge.Total)
}

func TestExportReusesAnyWindowFilterAndRawMetric(t *testing.T) {
	s := exportTestService(t, 3)
	require.NoError(t, s.db.Exec(`INSERT INTO recommendation_event_window_metrics VALUES(1,2,'600001.SH','600001','示例标的','A_SHARE','SH','',5,'READY','','',-0.2,'TUSHARE')`).Error)
	result, err := s.RecommendationExport(context.Background(), Filter{Status: "READY"})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.EqualValues(t, 2, result.Items[0].RecommendationEventID)
	require.InDelta(t, -0.2, *result.Items[0].Windows[0].ReturnRatio, 1e-9)
	require.Equal(t, 1, result.Summary[1].UnavailableCount)
}
