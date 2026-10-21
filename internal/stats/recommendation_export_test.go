package stats

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExportSummaryUsesRawReturnsAndExcludesUnavailable(t *testing.T) {
	positive, negative := 0.1, -0.2
	items := []RecommendationLedgerItem{
		{BloggerID: 2, BloggerName: "乙", TSCode: "600001.SH", Direction: "LONG", Windows: []RecommendationWindowReturn{{WindowDays: 5, Status: "READY", ReturnRatio: &positive}, {WindowDays: 10, Status: "PENDING"}}},
		{BloggerID: 1, BloggerName: "甲", TSCode: "600001.SH", Direction: "SHORT", Windows: []RecommendationWindowReturn{{WindowDays: 5, Status: "READY", ReturnRatio: &negative}}},
		{BloggerID: 2, BloggerName: "乙", TSCode: "600002.SH", Windows: []RecommendationWindowReturn{{WindowDays: 5, Status: "INCOMPLETE"}}},
	}
	summary, bloggers, securities := summarizeRecommendationExport(items, []int{5, 10})
	require.Equal(t, 2, securities)
	require.Equal(t, 3, summary[0].SampleCount)
	require.Equal(t, 2, summary[0].EvaluatedCount)
	require.Equal(t, 1, summary[0].UnavailableCount)
	require.InDelta(t, 0.5, *summary[0].RiseRate, 1e-9)
	require.InDelta(t, -0.05, *summary[0].AvgReturnRatio, 1e-9, "SHORT is not inverted into a synthetic portfolio return")
	require.Equal(t, 1, summary[1].PendingCount)
	require.Nil(t, summary[1].RiseRate)
	require.Nil(t, summary[1].AvgReturnRatio)
	require.EqualValues(t, 2, bloggers[0].BloggerID)
	require.Equal(t, 2, bloggers[0].RecommendationCount)
}

func TestExportFiltersExcludePagination(t *testing.T) {
	date := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	filters := recommendationExportFilters(Filter{BloggerName: "研究员", Symbol: "600001.SH", DateFrom: &date, Limit: 1, Offset: 200})
	require.Equal(t, "研究员", filters["blogger_name"])
	require.Equal(t, "2026-09-01", filters["date_from"])
	require.NotContains(t, filters, "limit")
	require.NotContains(t, filters, "offset")
}
