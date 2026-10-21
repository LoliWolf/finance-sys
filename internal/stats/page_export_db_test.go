//go:build cgo

package stats

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"finance-sys/internal/config"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func pageReportFixture(t *testing.T, count int) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = connection.Close() })
	for _, ddl := range []string{
		`CREATE TABLE bloggers(id INTEGER PRIMARY KEY,name TEXT,normalized_name TEXT,institution TEXT)`,
		`CREATE TABLE documents(id INTEGER PRIMARY KEY,title TEXT,file_name TEXT,author TEXT,institution TEXT,status TEXT,config_version INTEGER,created_at DATETIME,updated_at DATETIME,raw_content TEXT)`,
		`CREATE TABLE recommendation_events(id INTEGER PRIMARY KEY,blogger_id INTEGER,source_document_id INTEGER,symbol TEXT,asset_type TEXT,market TEXT,direction TEXT,recommend_date DATE,thesis TEXT,status TEXT,reference_price REAL,confidence REAL)`,
		`CREATE TABLE security_master(ts_code TEXT PRIMARY KEY,name TEXT,sector_type TEXT)`,
		`CREATE TABLE recommendation_event_window_metrics(id INTEGER PRIMARY KEY,recommendation_event_id INTEGER,blogger_id INTEGER,security_master_id INTEGER,ts_code TEXT,symbol TEXT,security_name TEXT,asset_type TEXT,market TEXT,industry TEXT,sector_type TEXT,direction TEXT,recommend_date DATE,window_days INTEGER,status TEXT,reason_code TEXT,reason_message TEXT,quote_source TEXT,calc_version TEXT,entry_date DATE,entry_price REAL,exit_date DATE,exit_close_price REAL,raw_return_ratio REAL,direction_return_ratio REAL,max_favorable_return_ratio REAL,max_adverse_return_ratio REAL,max_drawdown_ratio REAL,win_flag INTEGER)`,
		`CREATE TABLE stock_daily_quotes(id INTEGER PRIMARY KEY,ts_code TEXT,source TEXT,trade_date DATE,open_price REAL,close_price REAL,high_price REAL,low_price REAL,volume REAL,pct_chg REAL)`,
		`CREATE TABLE recommendation_event_evidences(id INTEGER PRIMARY KEY,recommendation_event_id INTEGER,chunk_index INTEGER,evidence_text TEXT)`,
		`CREATE TABLE untrackable_targets(id INTEGER PRIMARY KEY,document_id INTEGER,is_active INTEGER)`,
		`CREATE TABLE recommendation_evaluation_runs(id INTEGER PRIMARY KEY,run_type TEXT,status TEXT,target_event_count INTEGER,evaluated_event_count INTEGER,window_metric_count INTEGER,pending_count INTEGER,incomplete_count INTEGER,failed_count INTEGER,queued_at DATETIME,request_params_json TEXT)`,
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	date := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		for i := 1; i <= count; i++ {
			symbol := fmt.Sprintf("%06d.SH", i)
			for _, query := range []struct {
				q    string
				args []any
			}{
				{`INSERT INTO bloggers VALUES(?,?,?,'机构')`, []any{i, fmt.Sprintf("博主%d", i), fmt.Sprintf("博主%d", i)}},
				{`INSERT INTO documents VALUES(?,?,'research.pdf','研究员','机构','PLANNED',1,?,?,'SECRET_RAW_CONTENT')`, []any{i, fmt.Sprintf("研报%d", i), date, date}},
				{`INSERT INTO recommendation_events VALUES(?,?,0,?,'A_SHARE','SH','LONG',?,'完整观点','ACTIVE',10,0.8)`, []any{i, i, symbol, date}},
				{`INSERT INTO security_master VALUES(?,?,'')`, []any{symbol, fmt.Sprintf("证券%d", i)}},
				{`INSERT INTO recommendation_event_window_metrics(id,recommendation_event_id,blogger_id,security_master_id,ts_code,symbol,security_name,asset_type,market,industry,sector_type,direction,recommend_date,window_days,status,reason_code,reason_message,quote_source,calc_version,raw_return_ratio,direction_return_ratio,win_flag) VALUES(?,?,?,?,?,?,?,'A_SHARE','SH','行业','','LONG',?,30,'READY','','','TUSHARE','v3',0.1,0.1,1)`, []any{i, i, i, i, symbol, symbol, fmt.Sprintf("证券%d", i), date}},
				{`INSERT INTO recommendation_evaluation_runs VALUES(?,'MANUAL','SUCCEEDED',1,1,4,0,0,0,?,'SECRET_REQUEST_PARAMS')`, []any{i, date}},
			} {
				if err := tx.Exec(query.q, query.args...).Error; err != nil {
					return err
				}
			}
		}
		return nil
	}))
	require.NoError(t, db.Exec(`INSERT INTO stock_daily_quotes VALUES(1,'000001.SH','TUSHARE',?,10,11,12,9,100,0.1)`, date).Error)
	cfg := &config.Config{}
	cfg.Evaluation.RecommendationPerformance.Windows = []int{5, 10, 30, 90}
	cfg.Evaluation.RecommendationPerformance.QuoteSource = "TUSHARE"
	return NewService(db, config.NewRuntime(&config.Snapshot{Config: cfg}))
}

func TestPageExportIncludesAllRankingsNotScreenLimit(t *testing.T) {
	s := pageReportFixture(t, 230)
	for _, kind := range []string{"dashboard", "securities"} {
		report, err := s.PageExport(context.Background(), PageExportRequest{Kind: kind, Filter: Filter{WindowDays: 30, Limit: 1, Offset: 200, MinSampleCount: 1}})
		require.NoError(t, err)
		if kind == "dashboard" {
			require.Len(t, report.Dashboard.Items, 230)
			require.EqualValues(t, 1, report.Dashboard.Items[0].BloggerID)
		} else {
			require.Len(t, report.Securities.Items, 230)
			require.Equal(t, "000001.SH", report.Securities.Items[0].TSCode)
		}
	}
	selected, err := s.PageExport(context.Background(), PageExportRequest{Kind: "securities", Search: "证券230", SelectedTSCode: "000001.SH", Filter: Filter{WindowDays: 30, MinSampleCount: 1}})
	require.NoError(t, err)
	require.Len(t, selected.Securities.Items, 1)
	require.Nil(t, selected.SelectedSecurity)
	require.Nil(t, selected.Recommendations)
}

func TestPageExportBloggerIncludesFullFilteredPerformance(t *testing.T) {
	s := pageReportFixture(t, 230)
	require.NoError(t, s.db.Exec("UPDATE recommendation_events SET blogger_id=1").Error)
	require.NoError(t, s.db.Exec("UPDATE recommendation_event_window_metrics SET blogger_id=1").Error)
	report, err := s.PageExport(context.Background(), PageExportRequest{Kind: "blogger", Filter: Filter{BloggerID: 1, WindowDays: 30, Limit: 1}})
	require.NoError(t, err)
	require.Len(t, report.Recommendations.Items, 230)
	require.EqualValues(t, 230, report.Recommendations.Total)
	require.Len(t, report.Blogger.Windows, 4)
	require.Len(t, report.Timeseries.Items, 1)
	require.Equal(t, 230, report.Blogger.Windows[2].EvaluatedCount)
}

func TestPageExportDocumentListAndWorkbenchDoNotExposeRawData(t *testing.T) {
	s := pageReportFixture(t, 230)
	report, err := s.PageExport(context.Background(), PageExportRequest{Kind: "documents", Documents: DocumentReportListFilter{Limit: 1}})
	require.NoError(t, err)
	require.Len(t, report.Documents.Items, 230)
	jsonBytes, err := json.Marshal(report)
	require.NoError(t, err)
	require.NotContains(t, string(jsonBytes), "SECRET_RAW_CONTENT")
	workbench, err := s.PageExport(context.Background(), PageExportRequest{Kind: "workbench"})
	require.NoError(t, err)
	require.Len(t, workbench.Runs, 30)
	require.Len(t, workbench.RecentDocuments, 20)
	jsonBytes, err = json.Marshal(workbench)
	require.NoError(t, err)
	require.NotContains(t, string(jsonBytes), "SECRET_REQUEST_PARAMS")
	require.NotContains(t, string(jsonBytes), "SECRET_RAW_CONTENT")
}

func TestPageExportCapsAllListKinds(t *testing.T) {
	s := pageReportFixture(t, MaxPageExportItems+1)
	for _, kind := range []string{"dashboard", "securities", "documents"} {
		report, err := s.PageExport(context.Background(), PageExportRequest{Kind: kind, Filter: Filter{WindowDays: 30, MinSampleCount: 1}})
		var tooLarge *PageExportLimitError
		require.ErrorAs(t, err, &tooLarge)
		require.Nil(t, report)
		require.Equal(t, MaxPageExportItems+1, tooLarge.Total)
	}
}

func TestPageExportRecommendationReadsPriceAndEvidence(t *testing.T) {
	s := pageReportFixture(t, 1)
	require.NoError(t, s.db.Exec(`INSERT INTO recommendation_event_evidences VALUES(1,1,2,'完整证据')`).Error)
	report, err := s.PageExport(context.Background(), PageExportRequest{Kind: "recommendation", EventID: 1})
	require.NoError(t, err)
	require.Equal(t, "完整证据", report.Recommendation.Recommendation.Evidence[0].Text)
	require.Len(t, report.Prices.Items, 1)
}
