package stats

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm/schema"
)

func TestRecommendationLedgerItemCanBeScannedByGorm(t *testing.T) {
	_, err := schema.Parse(&RecommendationLedgerItem{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)
}

func TestMergeRecommendationLedgerMetricsKeepsEveryConfiguredWindow(t *testing.T) {
	return5 := 0.125
	items := []RecommendationLedgerItem{{
		RecommendationEventID: 42,
		Symbol:                "600519",
	}}
	metrics := []recommendationLedgerMetricRow{{
		RecommendationEventID: 42,
		TSCode:                "600519.SH",
		SecurityName:          "贵州茅台",
		WindowDays:            5,
		Status:                "READY",
		RawReturnRatio:        &return5,
	}, {
		RecommendationEventID: 42,
		WindowDays:            10,
		Status:                "PENDING",
		ReasonCode:            "WINDOW_NOT_MATURED",
	}}

	mergeRecommendationLedgerMetrics(items, metrics, []int{5, 10, 30, 90})

	require.Equal(t, "600519.SH", items[0].TSCode)
	require.Equal(t, "贵州茅台", items[0].SecurityName)
	require.Len(t, items[0].Windows, 4)
	require.Equal(t, 5, items[0].Windows[0].WindowDays)
	require.Equal(t, "READY", items[0].Windows[0].Status)
	require.Equal(t, &return5, items[0].Windows[0].ReturnRatio)
	require.Equal(t, "PENDING", items[0].Windows[1].Status)
	require.Equal(t, 30, items[0].Windows[2].WindowDays)
	require.Empty(t, items[0].Windows[2].Status)
	require.Nil(t, items[0].Windows[2].ReturnRatio)
	require.Equal(t, 90, items[0].Windows[3].WindowDays)
}

func TestPaginationMetadataIncludesTotalPages(t *testing.T) {
	page, totalPages := paginationMetadata(8689, 50, 50)
	require.Equal(t, 2, page)
	require.Equal(t, 174, totalPages)

	page, totalPages = paginationMetadata(0, 0, 20)
	require.Equal(t, 1, page)
	require.Equal(t, 0, totalPages)
}

func TestPerformanceScoreIsDeterministicAndBounded(t *testing.T) {
	values := aggregate{
		evaluatedCount: 10,
		winCount:       6,
		returns:        []float64{0.10, 0.08, 0.05, 0.02, 0.01, 0.005, -0.01, -0.03, -0.04, -0.05},
	}

	first := performanceScore(values)
	second := performanceScore(values)
	require.Equal(t, first, second)
	require.InDelta(t, 58.025, first, 0.0001)
	require.GreaterOrEqual(t, first, 0.0)
	require.LessOrEqual(t, first, 100.0)
}

func TestMedianDoesNotMutateInput(t *testing.T) {
	values := []float64{0.3, -0.1, 0.2, 0.0}
	original := append([]float64(nil), values...)

	require.InDelta(t, 0.1, median(values), 0.000001)
	require.Equal(t, original, values)
}

func TestSortBloggerRankingsUsesStableTieBreakers(t *testing.T) {
	items := []BloggerRankingItem{
		{BloggerID: 3, PerformanceScore: 70, EvaluatedCount: 20},
		{BloggerID: 2, PerformanceScore: 70, EvaluatedCount: 25},
		{BloggerID: 1, PerformanceScore: 70, EvaluatedCount: 25},
	}

	sortBloggerRankings(items, "performance_score")
	require.Equal(t, []int64{1, 2, 3}, []int64{items[0].BloggerID, items[1].BloggerID, items[2].BloggerID})
}
