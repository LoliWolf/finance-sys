package domain

import "time"

// RecommendationDateForArticle keeps the source article's calendar date as
// the recommendation fact date. T+1 offsets belong to evaluation, not ingest.
func RecommendationDateForArticle(articleDate time.Time) time.Time {
	if articleDate.IsZero() {
		return time.Time{}
	}
	return time.Date(
		articleDate.Year(),
		articleDate.Month(),
		articleDate.Day(),
		0,
		0,
		0,
		0,
		articleDate.Location(),
	)
}
