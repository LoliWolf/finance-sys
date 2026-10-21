package service

import (
	"encoding/json"
	"testing"
	"time"

	"finance-sys/internal/domain/db_model"
	"github.com/stretchr/testify/require"
)

func TestRetryDocumentDateNeverUsesToday(t *testing.T) {
	original := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)
	sources := []db_model.ExternalDocumentIngestion{{ArticleDate: original}}
	date, err := retryDocumentDate(nil, sources, nil)
	require.NoError(t, err)
	require.Equal(t, original, date)
	_, err = retryDocumentDate(nil, nil, nil)
	require.ErrorIs(t, err, ErrDocumentRetryDateRequired)
	date, err = retryDocumentDate(nil, nil, &original)
	require.NoError(t, err)
	require.Equal(t, original, date)
	other := original.AddDate(0, 0, 1)
	_, err = retryDocumentDate(nil, sources, &other)
	require.ErrorIs(t, err, ErrDocumentRetryDateConflict)
	_, err = retryDocumentDate(nil, append(sources, db_model.ExternalDocumentIngestion{ArticleDate: other}), nil)
	require.ErrorIs(t, err, ErrDocumentRetryDateConflict)
}

func TestRetryUsesPreviouslyRecordedDateAndPreservesAuditMetadata(t *testing.T) {
	run := db_model.InstrumentResolutionRun{RawMetadataJSON: []byte(`{"trade_date":"2024-01-03"}`)}
	date, err := retryDocumentDate([]db_model.InstrumentResolutionRun{run}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "2024-01-03", date.Format(time.DateOnly))
	metadata := buildRawMetadata(AnalysisObservation{}, nil)
	retainResolutionTradeDate(&run, metadata)
	raw, err := json.Marshal(metadata)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"trade_date":"2024-01-03"`)
}

func TestDocumentAnalysisGuardRejectsDuplicateAndReleases(t *testing.T) {
	s := &DocumentService{}
	release, err := s.acquireDocumentAnalysis(7)
	require.NoError(t, err)
	_, err = s.acquireDocumentAnalysis(7)
	require.ErrorIs(t, err, ErrDocumentAnalysisBusy)
	other, err := s.acquireDocumentAnalysis(8)
	require.NoError(t, err)
	other()
	release()
	release, err = s.acquireDocumentAnalysis(7)
	require.NoError(t, err)
	release()
	require.Empty(t, s.activeAnalyses)
}
