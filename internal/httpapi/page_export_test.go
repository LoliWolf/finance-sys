package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"finance-sys/internal/stats"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

type pageExportStub struct {
	performanceStatsService
	request stats.PageExportRequest
	err     error
	calls   int
}

func (s *pageExportStub) PageExport(_ context.Context, r stats.PageExportRequest) (*stats.PageExportReport, error) {
	s.request = r
	s.calls++
	return &stats.PageExportReport{Kind: r.Kind}, s.err
}
func pageExportRequest(s *Server, kind, query string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/page-reports/"+kind+query, nil)
	route := chi.NewRouteContext()
	route.URLParams.Add("kind", kind)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, route))
	w := httptest.NewRecorder()
	s.handlePageExport(w, r)
	return w
}
func TestPageReportRoutesValidateAndForwardFilters(t *testing.T) {
	stub := &pageExportStub{}
	s := &Server{stats: stub}
	for _, kind := range []string{"dashboard", "blogger", "securities", "recommendation", "documents", "workbench"} {
		query := "?window_days=10&blogger_id=2&event_id=3&search=alpha&selected_ts_code=600000.SH"
		w := pageExportRequest(s, kind, query)
		require.Equal(t, 200, w.Code)
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		require.Equal(t, kind, stub.request.Kind)
	}
	require.Equal(t, 400, pageExportRequest(s, "blogger", "").Code)
	require.Equal(t, 400, pageExportRequest(s, "recommendation", "?event_id=-1").Code)
	require.Equal(t, 400, pageExportRequest(s, "documents", "?date_from=wrong").Code)
	require.Equal(t, 404, pageExportRequest(s, "unknown", "").Code)
	w := pageExportRequest(s, "documents", "?status=FAILED&date_from=2026-09-01&date_to=2026-09-02")
	require.Equal(t, 200, w.Code)
	require.Equal(t, "FAILED", stub.request.Documents.Status)
	require.Equal(t, "2026-08-31T16:00:00Z", stub.request.Documents.CreatedFrom.Format("2006-01-02T15:04:05Z07:00"))
}
func TestPageReportRejectsOversize(t *testing.T) {
	s := &Server{stats: &pageExportStub{err: &stats.PageExportLimitError{Total: 2001, Limit: 2000}}}
	w := pageExportRequest(s, "dashboard", "")
	require.Equal(t, 422, w.Code)
	require.Contains(t, w.Body.String(), "EXPORT_TOO_LARGE")
}
