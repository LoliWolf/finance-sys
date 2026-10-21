package service

import (
	"context"
	"finance-sys/internal/config"
	"testing"
	"time"
)

func TestWeeklyDefaultOverridesLegacyLookback(t *testing.T) {
	for _, input := range []struct{ at, from, through string }{
		{"2026-09-30T10:30:00+08:00", "2026-09-24", "2026-09-30"},
		{"2026-09-20T16:00:00Z", "2026-09-15", "2026-09-21"},
		{"2027-01-01T10:30:00+08:00", "2026-12-26", "2027-01-01"},
	} {
		at, _ := time.Parse(time.RFC3339, input.at)
		for _, mode := range []string{"", "WEEK_NATURAL_DAYS"} {
			from, through, err := tradingRecommendationDateWindow(at, config.TradingDecisionConfig{RecommendationDateMode: mode, RecommendationLookbackDays: 5}, config.TradingCalendarConfig{})
			if err != nil || from != input.from || through != input.through {
				t.Fatalf("%s: %s..%s %v", input.at, from, through, err)
			}
		}
	}
}

func TestWeeklyKeysetAdvancesInvalidPageAndLateDates(t *testing.T) {
	s := ledgerService(t)
	for _, ddl := range []string{
		`CREATE TABLE recommendation_events (id INTEGER PRIMARY KEY, symbol TEXT, market TEXT, asset_type TEXT, direction TEXT, status TEXT, confidence REAL, recommend_date DATE, created_at DATETIME, updated_at DATETIME, plan_id INTEGER, reference_price REAL)`,
		`CREATE TABLE trade_candidate_plans (id INTEGER PRIMARY KEY, status TEXT, reference_price REAL, pricing_note TEXT)`,
	} {
		if err := s.db.Exec(ddl).Error; err != nil {
			t.Fatal(err)
		}
	}
	cfg := s.runtime.Config()
	cfg.Trading.Decision.MaxCandidatesPerRun = 1
	cfg.Trading.Decision.MinRecommendationConfidence = .65
	at := time.Date(2026, 9, 30, 10, 30, 0, 0, config.ChinaLocation)
	for _, row := range []struct {
		id           int
		symbol, date string
	}{{1, "600000", "2026-09-30"}, {2, "600000", "2026-09-24"}, {3, "INVALID", "2026-09-30"}, {4, "600000", "2026-09-23"}} {
		if err := s.db.Exec("INSERT INTO recommendation_events VALUES (?,?,?,?,?,?,?,?,?,?,NULL,0)", row.id, row.symbol, "SH", "STOCK", "LONG", "ACTIVE", .8, row.date, at.Add(-time.Hour), at.Add(-time.Hour)).Error; err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.RecommendationCandidates(context.Background(), at, 0, 1)
	if err != nil || len(first.Items) != 0 || first.NextCursor != "3" {
		t.Fatalf("invalid page %+v %v", first, err)
	}
	second, err := s.RecommendationCandidates(context.Background(), at, 3, 1)
	if err != nil || len(second.Items) != 1 || second.Items[0].RecommendationEventID != 2 {
		t.Fatalf("late older date %+v %v", second, err)
	}
	third, err := s.RecommendationCandidates(context.Background(), at, 2, 1)
	if err != nil || len(third.Items) != 1 || third.Items[0].RecommendationEventID != 1 {
		t.Fatalf("skipped newer recommendation %+v %v", third, err)
	}
}
