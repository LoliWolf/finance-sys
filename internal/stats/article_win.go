package stats

import "finance-sys/internal/evaluation"

// articleKey identifies one blogger's recommendations inside one source
// document. A document that mentions several bloggers yields one article per
// blogger. Rows without a source document (not expected in production) fall
// back to their own recommendation event so they never merge into a fake
// shared article.
type articleKey struct {
	bloggerID             int64
	sourceDocumentID      int64
	recommendationEventID int64
}

type articleTally struct {
	evaluated  bool
	profitable int
	losing     int
}

func articleKeyFor(row metricRow) articleKey {
	if row.SourceDocumentID > 0 {
		return articleKey{bloggerID: row.BloggerID, sourceDocumentID: row.SourceDocumentID}
	}
	return articleKey{bloggerID: row.BloggerID, recommendationEventID: row.RecommendationEventID}
}

// addArticleMetric records one window metric in its article. Only READY
// recommendations take part. A recommendation is profitable when it is a win
// under the configured threshold, and losing only when it is not a win and its
// direction return is strictly negative; a zero return is neither.
func addArticleMetric(values *aggregate, row metricRow) {
	if values.articles == nil {
		values.articles = make(map[articleKey]*articleTally)
	}
	key := articleKeyFor(row)
	tally := values.articles[key]
	if tally == nil {
		tally = &articleTally{}
		values.articles[key] = tally
	}
	if evaluation.Status(row.Status) != evaluation.StatusReady {
		return
	}
	tally.evaluated = true
	switch {
	case row.WinFlag != nil && *row.WinFlag:
		tally.profitable++
	case row.DirectionReturnRatio != nil && *row.DirectionReturnRatio < 0:
		tally.losing++
	}
}

// articleWinStats wins an article when its profitable recommendations
// outnumber losing ones. Ties stay in the denominator without counting as a
// win; articles without any READY recommendation are excluded from it.
func articleWinStats(values aggregate) ArticleWinStats {
	result := ArticleWinStats{ArticleCount: len(values.articles)}
	for _, tally := range values.articles {
		if !tally.evaluated {
			continue
		}
		result.EvaluatedArticleCount++
		if tally.profitable > tally.losing {
			result.ArticleWinCount++
		}
	}
	result.ArticleWinRate = ratio(result.ArticleWinCount, result.EvaluatedArticleCount)
	return result
}
