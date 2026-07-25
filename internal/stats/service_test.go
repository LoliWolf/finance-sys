package stats

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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
