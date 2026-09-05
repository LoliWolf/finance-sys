package stats

import (
	"testing"
	"time"

	"finance-sys/internal/domain/db_model"

	"github.com/stretchr/testify/require"
)

func TestReportCurrentMetricCalculatesLongAndShortDirectionReturns(t *testing.T) {
	dataAsOf := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	entryDate := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	entryPrice := 10.0
	metrics := map[int]db_model.RecommendationEventWindowMetric{5: {
		WindowDays: 5, EntryDate: &entryDate, EntryPrice: &entryPrice, CalcVersion: "v2",
	}}
	quote := db_model.StockDailyQuote{TSCode: "600000.SH", TradeDate: dataAsOf, ClosePrice: 12}
	item := DocumentReportRecommendation{TSCode: "600000.SH", Direction: "LONG"}

	longResult := reportCurrentMetric(item, &dataAsOf, quote, metrics, []int{5}, "v2", 0)
	require.Equal(t, "READY", longResult.Status)
	require.InDelta(t, 0.2, *longResult.DirectionReturnRatio, 0.000001)
	require.True(t, *longResult.WinFlag)

	item.Direction = "SHORT"
	shortResult := reportCurrentMetric(item, &dataAsOf, quote, metrics, []int{5}, "v2", 0)
	require.Equal(t, "READY", shortResult.Status)
	require.InDelta(t, 10.0/12.0-1, *shortResult.DirectionReturnRatio, 0.000001)
	require.False(t, *shortResult.WinFlag)
}

func TestReportCurrentMetricRejectsStaleQuoteAndOutdatedEntry(t *testing.T) {
	dataAsOf := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	entryDate := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	entryPrice := 10.0
	item := DocumentReportRecommendation{TSCode: "600000.SH", Direction: "LONG", Windows: []DocumentReportWindowMetric{{Status: "OUTDATED", ReasonCode: "CALC_VERSION_OUTDATED"}}}
	metrics := map[int]db_model.RecommendationEventWindowMetric{5: {
		WindowDays: 5, EntryDate: &entryDate, EntryPrice: &entryPrice, CalcVersion: "v1",
	}}
	quote := db_model.StockDailyQuote{TSCode: "600000.SH", TradeDate: dataAsOf.AddDate(0, 0, -1), ClosePrice: 12}

	outdated := reportCurrentMetric(item, &dataAsOf, quote, metrics, []int{5}, "v2", 0)
	require.Equal(t, "OUTDATED", outdated.Status)

	metrics[5] = db_model.RecommendationEventWindowMetric{WindowDays: 5, EntryDate: &entryDate, EntryPrice: &entryPrice, CalcVersion: "v2"}
	stale := reportCurrentMetric(item, &dataAsOf, quote, metrics, []int{5}, "v2", 0)
	require.Equal(t, "INCOMPLETE", stale.Status)
	require.Equal(t, "LATEST_QUOTE_STALE", stale.ReasonCode)
}

func TestAggregateDocumentReportKeepsWindowsAndBloggersSeparate(t *testing.T) {
	positive := 0.1
	negative := -0.05
	win := true
	loss := false
	recommendations := []DocumentReportRecommendation{
		{RecommendationEventID: 1, BloggerID: 10, BloggerName: "甲", Windows: []DocumentReportWindowMetric{{WindowDays: 5, Status: "READY", DirectionReturnRatio: &positive, WinFlag: &win}, {WindowDays: 10, Status: "PENDING"}}, Current: DocumentReportCurrentMetric{Status: "READY", DirectionReturnRatio: &positive, WinFlag: &win}},
		{RecommendationEventID: 2, BloggerID: 20, BloggerName: "乙", Windows: []DocumentReportWindowMetric{{WindowDays: 5, Status: "READY", DirectionReturnRatio: &negative, WinFlag: &loss}, {WindowDays: 10, Status: "NOT_EVALUATED"}}, Current: DocumentReportCurrentMetric{Status: "INCOMPLETE"}},
	}

	summary, bloggers := aggregateDocumentReport(recommendations, []int{5, 10})

	require.Equal(t, 2, summary.RecommendationCount)
	require.Equal(t, 2, summary.BloggerCount)
	require.Equal(t, 2, summary.Windows[0].EvaluatedCount)
	require.Equal(t, 1, summary.Windows[0].WinCount)
	require.Equal(t, 1, summary.Windows[1].PendingCount)
	require.Equal(t, 1, summary.Windows[1].IncompleteCount)
	require.Equal(t, 1, summary.Current.EvaluatedCount)
	require.Len(t, bloggers, 2)
	require.Equal(t, 10, int(bloggers[0].BloggerID))
}

func TestDocumentReportStatusDistinguishesMissingPartialAndReady(t *testing.T) {
	base := DocumentReportListItem{Status: "PLANNED", RecommendationCount: 2, ExpectedMetricCount: 8}
	require.Equal(t, "NEEDS_EVALUATION", documentReportStatus(base))

	base.ReadyMetricCount = 4
	require.Equal(t, "PARTIAL", documentReportStatus(base))

	base.ReadyMetricCount = 8
	require.Equal(t, "READY", documentReportStatus(base))

	base.Status = "INVALID"
	require.Equal(t, "READY", documentReportStatus(base), "document workflow status must not hide an existing usable report")

	base.RecommendationCount = 0
	base.ExpectedMetricCount = 0
	base.ReadyMetricCount = 0
	require.Equal(t, "INVALID", documentReportStatus(base))
}
