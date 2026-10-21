package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"finance-sys/internal/stats"
	"github.com/stretchr/testify/require"
)

type exportStatsStub struct {
	performanceStatsService
	filter stats.Filter
	err    error
	calls  int
}

func (s *exportStatsStub) RecommendationExport(_ context.Context, filter stats.Filter) (*stats.RecommendationExportReport, error) {
	s.filter = filter
	s.calls++
	return &stats.RecommendationExportReport{Total: 3, Items: []stats.RecommendationLedgerItem{}}, s.err
}

func TestRecommendationExportForwardsSameFiltersAndDisablesCaching(t *testing.T) {
	stub := &exportStatsStub{}
	s := &Server{stats: stub}
	r := httptest.NewRequest(http.MethodGet, "/recommendation-performance/report?blogger_name=Alice&direction=SHORT&market=SH&asset_type=A_SHARE&symbol=600001.SH&status=READY&date_from=2026-01-01&date_to=2026-09-01", nil)
	w := httptest.NewRecorder()
	s.handleRecommendationExport(w, r)
	require.Equal(t, 200, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, "Alice", stub.filter.BloggerName)
	require.Equal(t, "SHORT", stub.filter.Direction)
	require.Equal(t, "600001.SH", stub.filter.Symbol)
	require.Equal(t, "READY", stub.filter.Status)
}

func TestRecommendationExportRejectsOversizeAndBadDate(t *testing.T) {
	stub := &exportStatsStub{err: &stats.RecommendationExportLimitError{Total: 2001, Limit: 2000}}
	s := &Server{stats: stub}
	w := httptest.NewRecorder()
	s.handleRecommendationExport(w, httptest.NewRequest(http.MethodGet, "/report", nil))
	require.Equal(t, 422, w.Code)
	require.Contains(t, w.Body.String(), "EXPORT_TOO_LARGE")
	stub.calls = 0
	w = httptest.NewRecorder()
	s.handleRecommendationExport(w, httptest.NewRequest(http.MethodGet, "/report?date_from=wrong", nil))
	require.Equal(t, 400, w.Code)
	require.Zero(t, stub.calls)
}
