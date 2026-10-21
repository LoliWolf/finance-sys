package service

import (
	"context"
	"reflect"
	"testing"
	"time"

	"finance-sys/internal/config"
)

func recommendationCalendar() config.TradingCalendarConfig {
	// Synthetic calendar for boundary tests, not an official holiday schedule.
	return config.TradingCalendarConfig{Source: "fixture", Version: "1", CoverageFrom: "2023-12-01", CoverageTo: "2027-01-31", OpenDates: []string{"2023-12-29", "2024-01-01", "2024-02-29", "2024-03-01", "2026-05-29", "2026-06-01", "2026-09-18", "2026-09-21", "2026-09-22", "2026-09-23", "2026-09-24", "2026-09-25", "2026-09-28", "2026-09-30", "2026-10-09", "2026-12-31", "2027-01-01"}}
}

func TestPreviousTradingDayWindow(t *testing.T) {
	for _, tc := range []struct{ at, want string }{
		{"2026-09-24T10:32:00+08:00", "2026-09-23"},
		{"2026-09-23T16:00:00Z", "2026-09-23"},
		{"2026-09-23T15:59:59Z", "2026-09-22"},
		{"2026-09-22T10:30:00+08:00", "2026-09-21"},
		{"2026-10-09T10:30:00+08:00", "2026-09-30"},
		{"2027-01-01T10:30:00+08:00", "2026-12-31"},
		{"2024-03-01T10:30:00+08:00", "2024-02-29"},
	} {
		at, err := time.Parse(time.RFC3339, tc.at)
		if err != nil {
			t.Fatal(err)
		}
		for _, mode := range []string{"PREVIOUS_TRADING_DAY_TO_NOW"} {
			// An old Nacos lookback value cannot override the new default mode.
			from, through, err := tradingRecommendationDateWindow(at, config.TradingDecisionConfig{RecommendationDateMode: mode, RecommendationLookbackDays: 5}, recommendationCalendar())
			if err != nil || from != tc.want || through != config.ChinaDate(at) {
				t.Fatalf("%s/%s: %s..%s %v", tc.at, mode, from, through, err)
			}
		}
	}
	if _, _, err := tradingRecommendationDateWindow(time.Now(), config.TradingDecisionConfig{RecommendationDateMode: "TYPO"}, recommendationCalendar()); err == nil {
		t.Fatal("unknown mode accepted")
	}
	if _, _, err := tradingRecommendationDateWindow(time.Time{}, config.TradingDecisionConfig{}, recommendationCalendar()); err == nil {
		t.Fatal("zero clock accepted")
	}
}

func TestMondayIncludesWeekendWindow(t *testing.T) {
	for _, tc := range []struct{ at, from string }{
		{"2026-09-21T10:30:00+08:00", "2026-09-18"},
		{"2026-09-20T16:00:00Z", "2026-09-18"},
		{"2026-09-21T15:59:59Z", "2026-09-18"},
		{"2024-01-01T10:30:00+08:00", "2023-12-29"},
		{"2026-06-01T10:30:00+08:00", "2026-05-29"},
	} {
		at, err := time.Parse(time.RFC3339, tc.at)
		if err != nil {
			t.Fatal(err)
		}
		for _, mode := range []string{"PREVIOUS_TRADING_DAY_TO_NOW"} {
			from, through, err := tradingRecommendationDateWindow(at, config.TradingDecisionConfig{RecommendationDateMode: mode}, recommendationCalendar())
			if err != nil || from != tc.from || through != config.ChinaDate(at) {
				t.Fatalf("%s: %s..%s %v", tc.at, from, through, err)
			}
		}
	}
}

func TestRecommendationCalendarFailsClosed(t *testing.T) {
	for _, date := range []string{"2023-12-29", "2026-09-20", "2027-02-01"} {
		at, err := time.ParseInLocation(time.DateOnly, date, config.ChinaLocation)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := tradingRecommendationDateWindow(at, config.TradingDecisionConfig{RecommendationDateMode: "PREVIOUS_TRADING_DAY_TO_NOW"}, recommendationCalendar()); err == nil {
			t.Fatalf("missing previous day, closed day or missing coverage accepted: %s", date)
		}
	}
	if _, _, err := tradingRecommendationDateWindow(time.Now(), config.TradingDecisionConfig{RecommendationDateMode: "PREVIOUS_TRADING_DAY_TO_NOW"}, config.TradingCalendarConfig{}); err == nil {
		t.Fatal("missing calendar accepted")
	}
}

func TestPreviousDayCandidatesAllowOnlyMissingPriceReview(t *testing.T) {
	s := ledgerService(t)
	for _, ddl := range []string{
		`CREATE TABLE recommendation_events (id INTEGER PRIMARY KEY, symbol TEXT, market TEXT, asset_type TEXT, direction TEXT, status TEXT, confidence REAL, recommend_date DATE, created_at DATETIME, updated_at DATETIME, plan_id INTEGER, reference_price REAL)`,
		`CREATE TABLE trade_candidate_plans (id INTEGER PRIMARY KEY, status TEXT, reference_price REAL, pricing_note TEXT, entry_price REAL, position_pct REAL)`,
	} {
		if err := s.db.Exec(ddl).Error; err != nil {
			t.Fatal(err)
		}
	}
	at := time.Date(2026, 9, 24, 10, 32, 0, 0, config.ChinaLocation)
	cfg := s.runtime.Config()
	cfg.Trading.Calendar = recommendationCalendar()
	cfg.Trading.Decision.MaxCandidatesPerRun = 20
	cfg.Trading.Decision.MinRecommendationConfidence = .65
	cfg.Trading.Decision.RecommendationLookbackDays = 5
	cfg.Trading.Decision.RecommendationDateMode = "PREVIOUS_TRADING_DAY_TO_NOW"
	for _, row := range []struct {
		id                 int
		date, status, note string
		confidence         float64
		future             bool
	}{
		{1, "2026-09-23", "ACTIVE", "explicit_price_mention", .8, false},
		{2, "2026-09-23", "NEEDS_REVIEW", "missing explicit price in source text", .8, false},
		{3, "2026-09-23", "NEEDS_REVIEW", "confidence below threshold", .8, false},
		{4, "2026-09-23", "NEEDS_REVIEW", "missing explicit price in source text", .64, false},
		{5, "2026-09-23", "SUPERSEDED", "missing explicit price in source text", .8, false},
		{6, "2026-09-22", "ACTIVE", "explicit_price_mention", .8, false},
		{7, "2026-09-24", "ACTIVE", "explicit_price_mention", .8, false},
		{8, "2026-09-24", "NEEDS_REVIEW", "missing explicit price in source text", .8, true},
		{9, "2026-09-23", "NEEDS_REVIEW", "unknown review reason", .8, false},
		{10, "2026-09-29", "ACTIVE", "explicit_price_mention", .8, false},
	} {
		created := at.Add(-time.Minute)
		if row.future {
			created = at.Add(time.Second)
		}
		if err := s.db.Exec("INSERT INTO recommendation_events VALUES (?,?,?,?,?,?,?,?,?,?,?,0)", row.id, "600000", "SH", "STOCK", "LONG", row.status, row.confidence, row.date, created, created, row.id).Error; err != nil {
			t.Fatal(err)
		}
		if err := s.db.Exec("INSERT INTO trade_candidate_plans VALUES (?,?,0,?,0,0)", row.id, row.status, row.note).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Orphan review records must not be admitted merely because their price is zero.
	if err := s.db.Exec("DELETE FROM trade_candidate_plans WHERE id = 9").Error; err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{1, 20} {
		got, err := s.RecommendationCandidates(context.Background(), at, 0, limit)
		if err != nil {
			t.Fatal(err)
		}
		ids := []int64{}
		for _, item := range got.Items {
			ids = append(ids, item.RecommendationEventID)
		}
		want := []int64{7, 2, 1}
		if limit == 1 {
			want = []int64{7}
		}
		if !reflect.DeepEqual(ids, want) {
			t.Fatalf("got %v want %v", ids, want)
		}
		// Missing-price plans have zero sizing: retain the safe candidate default,
		// do not overwrite it with an unusable source plan or invent a source price.
		for _, item := range got.Items {
			if item.RecommendationEventID == 2 && (item.PositionRatio != "0.03000000" || item.ReferencePrice != "0.000000") {
				t.Fatalf("invalid missing-price candidate: %+v", item)
			}
		}
	}
	got, err := s.RecommendationCandidates(context.Background(), at.AddDate(0, 0, 4), 0, 20)
	if err != nil || len(got.Items) != 0 {
		t.Fatalf("must not fall back before Friday on Monday: %+v %v", got, err)
	}
	var reviews int64
	// Exercise Monday boundaries: Thursday excluded, Friday through Monday included.
	for i, date := range []string{"2026-09-24", "2026-09-25", "2026-09-26", "2026-09-27", "2026-09-28"} {
		if err := s.db.Exec("INSERT INTO recommendation_events VALUES (?,?,?,?,?,?,?,?,?,?,NULL,0)", 20+i, "600000", "SH", "STOCK", "LONG", "ACTIVE", .8, date, at, at).Error; err != nil {
			t.Fatal(err)
		}
	}
	got, err = s.RecommendationCandidates(context.Background(), at.AddDate(0, 0, 4), 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	ids := []int64{}
	for _, item := range got.Items {
		ids = append(ids, item.RecommendationEventID)
	}
	if !reflect.DeepEqual(ids, []int64{24, 23, 22, 21}) {
		t.Fatalf("Monday candidates: %v", ids)
	}
	if err := s.db.Table("recommendation_events").Where("status = ?", "NEEDS_REVIEW").Count(&reviews).Error; err != nil || reviews != 5 {
		t.Fatalf("source status mutated: %d %v", reviews, err)
	}
}
