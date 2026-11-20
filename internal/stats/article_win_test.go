package stats

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func articleMetric(eventID, bloggerID, documentID int64, status string, directionReturn float64) metricRow {
	row := metricRow{RecommendationEventID: eventID, BloggerID: bloggerID, SourceDocumentID: documentID, Status: status}
	if status == "READY" {
		win := directionReturn > 0
		row.WinFlag = &win
		row.DirectionReturnRatio = &directionReturn
	}
	return row
}

func TestArticleWinStatsCountsProfitableAgainstLosingRecommendations(t *testing.T) {
	rows := []metricRow{
		// Article 10: 2 profitable vs 1 losing -> win.
		articleMetric(1, 1, 10, "READY", 0.05),
		articleMetric(2, 1, 10, "READY", 0.01),
		articleMetric(3, 1, 10, "READY", -0.02),
		// Article 11: 1 vs 1 plus a zero return -> tie, stays in denominator.
		articleMetric(4, 1, 11, "READY", 0.03),
		articleMetric(5, 1, 11, "READY", -0.03),
		articleMetric(6, 1, 11, "READY", 0),
		// Article 12: one profitable plus many pending/incomplete -> win.
		articleMetric(7, 1, 12, "READY", 0.02),
		articleMetric(8, 1, 12, "PENDING", 0),
		articleMetric(9, 1, 12, "INCOMPLETE", 0),
		// Article 13: only a zero return -> evaluated tie.
		articleMetric(10, 1, 13, "READY", 0),
		// Article 14: nothing evaluable -> outside the denominator.
		articleMetric(11, 1, 14, "PENDING", 0),
		// Article 15: losing only.
		articleMetric(12, 1, 15, "READY", -0.04),
	}
	values := aggregate{}
	for _, row := range rows {
		addMetric(&values, row)
	}

	stats := articleWinStats(values)
	require.Equal(t, ArticleWinStats{ArticleCount: 6, EvaluatedArticleCount: 5, ArticleWinCount: 2, ArticleWinRate: 0.4}, stats)
	// Recommendation-level counts are unchanged by the article dimension.
	require.Equal(t, 9, values.evaluatedCount)
	require.Equal(t, 4, values.winCount)
}

func TestArticleWinStatsUsesWinThresholdForProfitAndNegativeReturnForLoss(t *testing.T) {
	belowThreshold, negative := 0.005, -0.01
	notWin := false
	values := aggregate{}
	// With a positive threshold a small gain is neither profit nor loss.
	addMetric(&values, metricRow{RecommendationEventID: 1, BloggerID: 1, SourceDocumentID: 7, Status: "READY", WinFlag: &notWin, DirectionReturnRatio: &belowThreshold})
	addMetric(&values, metricRow{RecommendationEventID: 2, BloggerID: 1, SourceDocumentID: 8, Status: "READY", WinFlag: &notWin, DirectionReturnRatio: &belowThreshold})
	addMetric(&values, metricRow{RecommendationEventID: 3, BloggerID: 1, SourceDocumentID: 8, Status: "READY", WinFlag: &notWin, DirectionReturnRatio: &negative})

	require.Equal(t, ArticleWinStats{ArticleCount: 2, EvaluatedArticleCount: 2}, articleWinStats(values))
	require.Equal(t, 1, values.articles[articleKey{bloggerID: 1, sourceDocumentID: 8}].losing)
}

func TestArticleWinStatsSeparatesBloggersAndMissingDocuments(t *testing.T) {
	values := aggregate{}
	// One document with two bloggers yields one article per blogger.
	addMetric(&values, articleMetric(1, 1, 20, "READY", 0.02))
	addMetric(&values, articleMetric(2, 2, 20, "READY", -0.02))
	// Rows without a source document never merge into one shared article.
	addMetric(&values, articleMetric(3, 3, 0, "READY", 0.02))
	addMetric(&values, articleMetric(4, 3, 0, "READY", -0.02))

	require.Equal(t, ArticleWinStats{ArticleCount: 4, EvaluatedArticleCount: 4, ArticleWinCount: 2, ArticleWinRate: 0.5}, articleWinStats(values))
	require.Equal(t, ArticleWinStats{}, articleWinStats(aggregate{}))
}

func TestSortBloggerRankingsByArticleWinRate(t *testing.T) {
	items := []BloggerRankingItem{
		{BloggerID: 1, WinRate: 0.9, ArticleWinStats: ArticleWinStats{ArticleWinRate: 0.5, EvaluatedArticleCount: 10}},
		{BloggerID: 2, WinRate: 0.4, ArticleWinStats: ArticleWinStats{ArticleWinRate: 0.75, EvaluatedArticleCount: 4}},
		{BloggerID: 3, WinRate: 0.5, ArticleWinStats: ArticleWinStats{ArticleWinRate: 0.75, EvaluatedArticleCount: 8}},
	}

	sortBloggerRankings(items, "article_win_rate")
	require.Equal(t, []int64{3, 2, 1}, []int64{items[0].BloggerID, items[1].BloggerID, items[2].BloggerID})
}
