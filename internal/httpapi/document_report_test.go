package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"finance-sys/internal/service"

	"github.com/stretchr/testify/require"
)

func TestParseDocumentReportListFilterUsesShanghaiDayBoundaries(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/document-reports?query=%E7%A0%94%E6%8A%A5&status=planned&date_from=2026-09-05&date_to=2026-09-05&limit=20&offset=40", nil)

	filter, err := parseDocumentReportListFilter(request)

	require.NoError(t, err)
	require.Equal(t, "研报", filter.Query)
	require.Equal(t, "PLANNED", filter.Status)
	require.Equal(t, 20, filter.Limit)
	require.Equal(t, 40, filter.Offset)
	require.Equal(t, time.Date(2026, 9, 4, 16, 0, 0, 0, time.UTC), *filter.CreatedFrom)
	require.Equal(t, time.Date(2026, 9, 5, 16, 0, 0, 0, time.UTC), *filter.CreatedBefore)
}

func TestNormalizeDocumentEvaluationIDsDeduplicatesSortsAndCaps(t *testing.T) {
	ids, err := normalizeDocumentEvaluationIDs([]int64{9, 3, 9, 5})
	require.NoError(t, err)
	require.Equal(t, []int64{3, 5, 9}, ids)

	_, err = normalizeDocumentEvaluationIDs(nil)
	require.ErrorContains(t, err, "must not be empty")

	_, err = normalizeDocumentEvaluationIDs([]int64{1, 0})
	require.ErrorContains(t, err, "invalid document_id")

	tooMany := make([]int64, maxDocumentEvaluationBatchSize+1)
	for index := range tooMany {
		tooMany[index] = int64(index + 1)
	}
	_, err = normalizeDocumentEvaluationIDs(tooMany)
	require.ErrorContains(t, err, "at most 100")
}

func TestHandleCreateDocumentEvaluationRunQueuesDocumentFilter(t *testing.T) {
	fake := &fakeDocumentEvaluationService{response: &service.RecommendationEvaluationRunResponse{
		RunID: 81, Status: service.RecommendationEvaluationRunStatusQueued, RunType: service.RecommendationEvaluationRunTypeManual,
	}}
	server := &Server{evaluation: fake}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/documents/evaluation-runs", strings.NewReader(`{"document_ids":[7,2,7],"force_rebuild":true}`))
	recorder := httptest.NewRecorder()

	server.handleCreateDocumentEvaluationRun(recorder, request)

	require.Equal(t, http.StatusAccepted, recorder.Code)
	require.Equal(t, []int64{2, 7}, fake.request.DocumentIDs)
	require.True(t, fake.request.ForceRebuild)
	require.NotNil(t, fake.request.OnlyActive)
	require.False(t, *fake.request.OnlyActive)
	require.True(t, fake.request.ExcludeSuperseded)
	require.JSONEq(t, `{"run_id":81,"status":"QUEUED","run_type":"MANUAL","message":"document recommendation evaluation task queued","document_count":2}`, recorder.Body.String())
}

type fakeDocumentEvaluationService struct {
	request  service.RecommendationEvaluationRequest
	response *service.RecommendationEvaluationRunResponse
}

func (f *fakeDocumentEvaluationService) CreateRun(_ context.Context, request service.RecommendationEvaluationRequest) (*service.RecommendationEvaluationRunResponse, error) {
	f.request = request
	return f.response, nil
}

func (*fakeDocumentEvaluationService) GetRun(context.Context, int64) (*service.RecommendationEvaluationRunView, error) {
	return &service.RecommendationEvaluationRunView{}, nil
}

func (*fakeDocumentEvaluationService) ListRuns(context.Context, service.RecommendationEvaluationRunQuery) ([]service.RecommendationEvaluationRunView, error) {
	return nil, nil
}
