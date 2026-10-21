package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"finance-sys/internal/dal"
	"finance-sys/internal/service"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

type fakeDocumentRetrier struct {
	err   error
	calls int
	date  *time.Time
}

func (f *fakeDocumentRetrier) RetryFailedDocument(_ context.Context, id int64, date *time.Time) (*service.DocumentRetryResult, error) {
	f.calls++
	f.date = date
	return &service.DocumentRetryResult{DocumentID: id, Status: "PLANNED", PlanCount: 2, TradeDate: "2024-01-03"}, f.err
}

func retryHTTPServer(fake *fakeDocumentRetrier) http.Handler {
	s := &Server{documentRetry: fake}
	router := chi.NewRouter()
	router.Post("/documents/{id}/retry-analysis", s.handleRetryDocumentAnalysis)
	return router
}

func TestRetryAnalysisHTTPRejectsNonRetryableAndMissingDates(t *testing.T) {
	for _, item := range []struct {
		err    error
		status int
	}{
		{service.ErrDocumentRetryNotAllowed, 409}, {service.ErrDocumentAnalysisBusy, 409},
		{service.ErrDocumentRetryDateConflict, 409}, {service.ErrDocumentRetryDateRequired, 422}, {dal.ErrNotFound, 404},
	} {
		fake := &fakeDocumentRetrier{err: item.err}
		recorder := httptest.NewRecorder()
		retryHTTPServer(fake).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/documents/7/retry-analysis", strings.NewReader(`{}`)))
		require.Equal(t, item.status, recorder.Code)
		if item.status == 422 {
			require.Contains(t, recorder.Body.String(), "RETRY_DATE_REQUIRED")
		}
	}
}

func TestRetryAnalysisHTTPRejectsForceAndMalformedInput(t *testing.T) {
	for _, body := range []string{`{"force":true}`, `{} {}`, `{"trade_date":"bad-date"}`} {
		fake := &fakeDocumentRetrier{}
		recorder := httptest.NewRecorder()
		retryHTTPServer(fake).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/documents/7/retry-analysis", strings.NewReader(body)))
		require.Equal(t, http.StatusBadRequest, recorder.Code)
		require.Zero(t, fake.calls)
	}
}

func TestRetryAnalysisHTTPPassesExplicitHistoricalDate(t *testing.T) {
	fake := &fakeDocumentRetrier{}
	recorder := httptest.NewRecorder()
	retryHTTPServer(fake).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/documents/7/retry-analysis", strings.NewReader(`{"trade_date":"2024-01-03"}`)))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "2024-01-03", fake.date.Format(time.DateOnly))
	require.Contains(t, recorder.Body.String(), `"plan_count":2`)
}
