package domain

import (
	"testing"
	"time"
)

func TestRecommendationDateForArticleUsesArticleCalendarDate(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	articleDate := time.Date(2026, time.July, 29, 18, 30, 0, 0, location)

	got := RecommendationDateForArticle(articleDate)
	want := time.Date(2026, time.July, 29, 0, 0, 0, 0, location)
	if !got.Equal(want) || got.Location() != location {
		t.Fatalf("RecommendationDateForArticle(%v) = %v, want %v", articleDate, got, want)
	}
}

func TestRecommendationDateForArticlePreservesZeroValue(t *testing.T) {
	if got := RecommendationDateForArticle(time.Time{}); !got.IsZero() {
		t.Fatalf("RecommendationDateForArticle(zero) = %v, want zero", got)
	}
}
