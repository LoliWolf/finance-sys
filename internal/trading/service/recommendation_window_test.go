package service

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestRecommendationDateWindow(t *testing.T) {
	for _, tc := range []struct {
		name, asOf    string
		days          int
		from, through string
	}{
		{"five-days", "2026-09-16T13:30:00+08:00", 5, "2026-09-12", "2026-09-16"},
		{"utc-crosses-chinese-midnight", "2026-09-15T16:00:00Z", 5, "2026-09-12", "2026-09-16"},
		{"before-chinese-midnight", "2026-09-15T15:59:59Z", 5, "2026-09-11", "2026-09-15"},
		{"weekends-count", "2026-09-14T09:35:00+08:00", 5, "2026-09-10", "2026-09-14"},
		{"year-boundary", "2027-01-01T09:35:00+08:00", 5, "2026-12-28", "2027-01-01"},
		{"leap-year", "2024-03-01T09:35:00+08:00", 5, "2024-02-26", "2024-03-01"},
		{"only-today", "2026-09-16T13:30:00+08:00", 1, "2026-09-16", "2026-09-16"},
		{"legacy-is-bounded", "2026-09-16T13:30:00+08:00", 0, "2026-09-12", "2026-09-16"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at, err := time.Parse(time.RFC3339, tc.asOf)
			if err != nil {
				t.Fatal(err)
			}
			from, through, err := recommendationDateWindow(at, tc.days)
			if err != nil || from != tc.from || through != tc.through {
				t.Fatalf("got %s..%s err=%v", from, through, err)
			}
		})
	}
	for _, days := range []int{-1, 366} {
		if _, _, err := recommendationDateWindow(time.Now(), days); err == nil {
			t.Fatalf("invalid days accepted: %d", days)
		}
	}
	if _, _, err := recommendationDateWindow(time.Time{}, 5); err == nil {
		t.Fatal("zero reference time accepted")
	}
}

func TestRecommendationCandidatesFiltersDateBeforeLimit(t *testing.T) {
	s := ledgerService(t)
	// Exercise the real service + DAL against isolated SQLite, not a real schema.
	err := s.db.Exec(`CREATE TABLE recommendation_events (
		id INTEGER PRIMARY KEY, symbol TEXT, market TEXT, asset_type TEXT,
		direction TEXT, status TEXT, confidence REAL, recommend_date DATE,
		created_at DATETIME, updated_at DATETIME, plan_id INTEGER, reference_price REAL
	)`).Error
	if err != nil {
		t.Fatal(err)
	}
	asOf := time.Date(2026, 9, 15, 16, 30, 0, 0, time.UTC) // Sep 16 00:30 in China
	if err := s.db.Exec(`CREATE TABLE trade_candidate_plans (id INTEGER PRIMARY KEY, status TEXT, reference_price REAL, pricing_note TEXT)`).Error; err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return asOf }
	cfg := s.runtime.Config()
	cfg.Trading.Decision.RecommendationLookbackDays = 5
	cfg.Trading.Decision.RecommendationDateMode = "ROLLING_NATURAL_DAYS"
	cfg.Trading.Decision.MaxCandidatesPerRun = 20
	cfg.Trading.Decision.MinRecommendationConfidence = .65
	for _, row := range []struct {
		id         int
		date       string
		created    time.Time
		confidence float64
		status     string
	}{
		{1, "2026-09-11", asOf.Add(-time.Minute), .9, "ACTIVE"},  // late ingestion cannot revive an old recommendation
		{2, "2026-09-12", asOf.Add(-time.Hour), .9, "ACTIVE"},    // inclusive lower boundary
		{3, "2026-09-16", asOf.Add(-time.Minute), .9, "ACTIVE"},  // inclusive Chinese today
		{4, "2026-09-17", asOf.Add(-time.Minute), .9, "ACTIVE"},  // future recommendation
		{5, "2026-09-16", asOf.Add(time.Second), .9, "ACTIVE"},   // future ingestion
		{6, "2026-09-16", asOf.Add(-time.Minute), .64, "ACTIVE"}, // existing confidence guard
		{7, "2026-09-16", asOf.Add(-time.Minute), .9, "SUPERSEDED"},
	} {
		if err := s.db.Exec("INSERT INTO recommendation_events VALUES (?,?,?,?,?,?,?,?,?,?,NULL,0)", row.id, "600000", "SH", "STOCK", "LONG", row.status, row.confidence, row.date, row.created, row.created).Error; err != nil {
			t.Fatal(err)
		}
	}
	check := func(at time.Time, after int64, limit int, want []int64) {
		t.Helper()
		got, err := s.RecommendationCandidates(context.Background(), at, after, limit)
		if err != nil {
			t.Fatal(err)
		}
		ids := []int64{}
		for _, item := range got.Items {
			ids = append(ids, item.RecommendationEventID)
		}
		if !reflect.DeepEqual(ids, want) {
			t.Fatalf("got IDs=%v want=%v", ids, want)
		}
	}
	check(asOf, 0, 20, []int64{3, 2})
	check(time.Time{}, 0, 20, []int64{3, 2}) // same clock/window for unspecified as-of
	check(asOf, 0, 1, []int64{3})
	check(asOf, 3, 20, []int64{2}) // cursor must not bring back expired records
	cfg.Trading.Decision.RecommendationLookbackDays = 1
	check(asOf, 0, 20, []int64{3})
	cfg.Trading.Decision.RecommendationLookbackDays = 0
	check(asOf, 0, 20, []int64{3, 2})
	check(asOf.AddDate(0, 0, 15), 0, 20, []int64{}) // empty recent window never falls back to old recommendations
	cfg.Trading.Decision.RecommendationLookbackDays = -1
	if _, err := s.RecommendationCandidates(context.Background(), asOf, 0, 20); err == nil {
		t.Fatal("invalid runtime window must fail closed")
	}
	// The rule is scoped to candidate selection; no event history is removed.
	var count int64
	if err := s.db.Table("recommendation_events").Count(&count).Error; err != nil || count != 7 {
		t.Fatalf("history changed: %d %v", count, err)
	}
}
