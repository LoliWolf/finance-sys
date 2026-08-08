package main

import (
	"testing"
	"time"
)

func TestM9ArticleDateFromTitle(t *testing.T) {
	got, err := m9ArticleDateFromTitle("M9_REAL_HISTORY|20260729|文章标题")
	if err != nil {
		t.Fatal(err)
	}
	if got.Format(time.DateOnly) != "2026-07-29" {
		t.Fatalf("date = %s, want 2026-07-29", got.Format(time.DateOnly))
	}
}

func TestM9ArticleDateFromTitleRejectsMalformedTitle(t *testing.T) {
	if _, err := m9ArticleDateFromTitle("M9_REAL_HISTORY|not-a-date|文章标题"); err == nil {
		t.Fatal("expected malformed title error")
	}
}

func TestDateBounds(t *testing.T) {
	dates := map[int64]time.Time{
		1: time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC),
		2: time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC),
		3: time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC),
	}
	from, to := dateBounds(dates)
	if from != "2026-02-02" || to != "2026-08-04" {
		t.Fatalf("bounds = %s..%s", from, to)
	}
}
