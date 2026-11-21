package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"finance-sys/internal/domain/db_model"
	"finance-sys/internal/service"

	"github.com/stretchr/testify/require"
)

type fakeWireDeskRuns struct {
	calls []string
	got   []service.WireDeskIngestionRequest
	err   error
}

func (f *fakeWireDeskRuns) record(source string, request service.WireDeskIngestionRequest) (*db_model.ScheduledTaskRun, error) {
	f.calls = append(f.calls, source)
	f.got = append(f.got, request)
	if f.err != nil {
		return nil, f.err
	}
	return &db_model.ScheduledTaskRun{ID: int64(len(f.calls)), TaskKey: "manual-" + source, Status: "PENDING"}, nil
}

func (f *fakeWireDeskRuns) EnqueueManualXueqiuRun(_ context.Context, request service.WireDeskIngestionRequest) (*db_model.ScheduledTaskRun, error) {
	return f.record("xueqiu", request)
}

func (f *fakeWireDeskRuns) EnqueueManualTHSRun(_ context.Context, request service.WireDeskIngestionRequest) (*db_model.ScheduledTaskRun, error) {
	return f.record("ths", request)
}

func (f *fakeWireDeskRuns) EnqueueManualEastmoneyRun(_ context.Context, request service.WireDeskIngestionRequest) (*db_model.ScheduledTaskRun, error) {
	return f.record("eastmoney", request)
}

func TestEnqueueWireDeskRunsRouteToTheirSource(t *testing.T) {
	fake := &fakeWireDeskRuns{}
	s := &Server{wireDeskRuns: fake}

	recorder := httptest.NewRecorder()
	s.handleEnqueueTHSRun(recorder, httptest.NewRequest(http.MethodPost, "/admin/external-documents/ths/runs", strings.NewReader(`{"max_articles":5,"skip_ack":true}`)))
	require.Equal(t, http.StatusAccepted, recorder.Code, recorder.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, "manual-ths", body["task_key"])

	recorder = httptest.NewRecorder()
	s.handleEnqueueXueqiuRun(recorder, httptest.NewRequest(http.MethodPost, "/admin/external-documents/xueqiu/runs", http.NoBody))
	require.Equal(t, http.StatusAccepted, recorder.Code, recorder.Body.String())

	require.Equal(t, []string{"ths", "xueqiu"}, fake.calls)
	require.Equal(t, service.WireDeskIngestionRequest{MaxArticles: 5, SkipAck: true}, fake.got[0])
	require.Equal(t, service.WireDeskIngestionRequest{}, fake.got[1], "an empty body is a full run")
}

func TestEnqueueTHSRunRejectsBadInput(t *testing.T) {
	fake := &fakeWireDeskRuns{}
	s := &Server{wireDeskRuns: fake}
	recorder := httptest.NewRecorder()
	s.handleEnqueueTHSRun(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"maxArticles":5}`)))
	require.Equal(t, http.StatusBadRequest, recorder.Code, "unknown fields are rejected")
	require.Empty(t, fake.calls)

	fake.err = errors.New("THS document ingestion is disabled")
	recorder = httptest.NewRecorder()
	s.handleEnqueueTHSRun(recorder, httptest.NewRequest(http.MethodPost, "/", http.NoBody))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "disabled")

	recorder = httptest.NewRecorder()
	(&Server{}).handleEnqueueTHSRun(recorder, httptest.NewRequest(http.MethodPost, "/", http.NoBody))
	require.Equal(t, http.StatusNotImplemented, recorder.Code)
}

func TestEnqueueEastmoneyRun(t *testing.T) {
	fake := &fakeWireDeskRuns{}
	s := &Server{wireDeskRuns: fake}
	w := httptest.NewRecorder()
	s.handleEnqueueEastmoneyRun(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"max_articles":3,"skip_ack":true}`)))
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	require.Equal(t, []string{"eastmoney"}, fake.calls)
	require.Equal(t, service.WireDeskIngestionRequest{MaxArticles: 3, SkipAck: true}, fake.got[0])
	w = httptest.NewRecorder()
	s.handleEnqueueEastmoneyRun(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"typo":1}`)))
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Len(t, fake.calls, 1)
	w = httptest.NewRecorder()
	(&Server{}).handleEnqueueEastmoneyRun(w, httptest.NewRequest(http.MethodPost, "/", http.NoBody))
	require.Equal(t, http.StatusNotImplemented, w.Code)
}
