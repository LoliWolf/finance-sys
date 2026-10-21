package service

import (
	"context"
	"finance-sys/internal/config"
	"testing"
	"time"
)

func TestDailyFeaturesExcludeTodayAndStaleData(t *testing.T) {
	s := ledgerService(t)
	at := time.Date(2026, 9, 10, 10, 0, 0, 0, config.ChinaLocation)
	if err := s.db.Exec(`CREATE TABLE stock_daily_quotes (id INTEGER PRIMARY KEY, symbol TEXT, ts_code TEXT, source TEXT, trade_date DATETIME, open_price REAL, high_price REAL, low_price REAL, close_price REAL, pre_close_price REAL, volume REAL)`).Error; err != nil {
		t.Fatal(err)
	}
	for i, date := range []string{"2026-09-09", "2026-09-10"} {
		day, _ := time.Parse(time.DateOnly, date)
		if err := s.db.Exec("INSERT INTO stock_daily_quotes VALUES (?,?,?,?,?,?,?,?,?,?,?)", i+1, "600000", "600000.SH", "TUSHARE", day, 10, 11, 9, 10+i, 10, 1000).Error; err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.DailyHistory(context.Background(), []string{"600000.SH"}, at, 20)
	if err != nil || len(rows) != 1 || rows[0].TradeDate != "2026-09-09" || rows[0].ClosePrice != 10 || rows[0].PreClosePrice != 10 {
		t.Fatalf("future or missing bar %+v %v", rows, err)
	}
	if err := s.db.Exec("DELETE FROM stock_daily_quotes WHERE id = 1").Error; err != nil {
		t.Fatal(err)
	}
	rows, err = s.DailyHistory(context.Background(), []string{"600000.SH"}, at, 20)
	if err != nil || len(rows) != 0 {
		t.Fatalf("incomplete/stale quote admitted %+v %v", rows, err)
	}
}

func TestBloggerPerformanceIsPointInTime(t *testing.T) {
	s := ledgerService(t)
	at := s.now()
	if err := s.db.Exec(`CREATE TABLE recommendation_event_window_metrics (id INTEGER PRIMARY KEY, blogger_id INTEGER, window_days INTEGER, quote_source TEXT, status TEXT, win_flag INTEGER, direction_return_ratio REAL, exit_date TEXT, calculated_at DATETIME, updated_at DATETIME)`).Error; err != nil {
		t.Fatal(err)
	}
	for i, kind := range []string{"past", "future-update", "future-calc", "today-exit", "wrong-source"} {
		updated, calculated := at.Add(-time.Hour), at.Add(-time.Hour)
		exit := "2026-09-09"
		source := "TUSHARE"
		switch kind {
		case "future-update":
			updated = at.Add(time.Hour)
		case "future-calc":
			calculated = at.Add(time.Hour)
		case "today-exit":
			exit = "2026-09-10"
		case "wrong-source":
			source = "OTHER"
		}
		if err := s.db.Exec("INSERT INTO recommendation_event_window_metrics VALUES (?,?,30,?,'READY',1,0.1,?,?,?)", i+1, 1, source, exit, calculated, updated).Error; err != nil {
			t.Fatal(err)
		}
	}
	result, err := s.BloggerPerformance(context.Background(), 1, 30, at)
	if err != nil || result.EvaluableCount != 1 || result.WinCount != 1 {
		t.Fatalf("future statistics leaked %+v %v", result, err)
	}
}
