//go:build cgo

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"finance-sys/internal/dal"
	"finance-sys/internal/domain"
	"finance-sys/internal/domain/db_model"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func retryTestService(t *testing.T) *DocumentService {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&db_model.Document{}))
	for _, ddl := range []string{
		`CREATE TABLE recommendation_events (id INTEGER PRIMARY KEY, source_document_id INTEGER)`,
		`CREATE TABLE trade_candidate_plans (id INTEGER PRIMARY KEY, document_id INTEGER)`,
		`CREATE TABLE instrument_resolution_runs (id INTEGER PRIMARY KEY, document_id INTEGER, status TEXT, raw_metadata_json TEXT, created_at DATETIME)`,
		`CREATE TABLE external_document_ingestions (id INTEGER PRIMARY KEY, document_id INTEGER, article_date DATE, status INTEGER, attempt_count INTEGER DEFAULT 0, last_error TEXT, analyzed_at DATETIME, updated_at DATETIME)`,
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	require.NoError(t, db.Create(&db_model.Document{ID: 1, Status: "FAILED", FileName: "report.pdf", RawContent: []byte("source")}).Error)
	require.NoError(t, db.Exec(`INSERT INTO external_document_ingestions(id,document_id,article_date,status,last_error) VALUES(1,1,'2024-01-03',6,'old failure')`).Error)
	return &DocumentService{db: db}
}

func TestDocumentRetryRejectsEveryNonFailedStateBeforeAnalysis(t *testing.T) {
	for _, status := range []string{"PLANNED", "INVALID", "PARSED", "INGESTED", "ANALYZING"} {
		t.Run(status, func(t *testing.T) {
			s := retryTestService(t)
			require.NoError(t, dal.Documents.UpdateStatusByID(context.Background(), s.db, 1, status))
			_, err := s.RetryFailedDocument(context.Background(), 1, nil)
			require.ErrorIs(t, err, ErrDocumentRetryNotAllowed)
		})
	}
}

func TestDocumentRetryRejectsFailedDocumentWithExistingSuccess(t *testing.T) {
	for _, statement := range []string{
		`INSERT INTO recommendation_events(id,source_document_id) VALUES(1,1)`,
		`INSERT INTO trade_candidate_plans(id,document_id) VALUES(1,1)`,
		`UPDATE external_document_ingestions SET status=5 WHERE id=1`,
		`INSERT INTO instrument_resolution_runs(id,document_id,status,raw_metadata_json) VALUES(1,1,'SUCCEEDED','{}')`,
	} {
		s := retryTestService(t)
		require.NoError(t, s.db.Exec(statement).Error)
		_, err := s.RetryFailedDocument(context.Background(), 1, nil)
		require.ErrorIs(t, err, ErrDocumentRetryNotAllowed)
	}
}

func TestDocumentRetrySuccessPreservesDateAndCannotBeRepeated(t *testing.T) {
	s := retryTestService(t)
	called := 0
	result, err := s.retryFailedDocument(context.Background(), 1, nil, func(ctx context.Context, id int64, date time.Time) ([]domain.CandidatePlan, error) {
		called++
		require.Equal(t, "2024-01-03", date.Format(time.DateOnly))
		// Even another service instance cannot claim this document a second time.
		other := &DocumentService{db: s.db}
		_, second := other.RetryFailedDocument(ctx, id, nil)
		require.ErrorIs(t, second, ErrDocumentRetryNotAllowed)
		return nil, dal.Documents.UpdateStatusByID(ctx, s.db, id, "PLANNED")
	})
	require.NoError(t, err)
	require.Equal(t, 1, called)
	require.Equal(t, "PLANNED", result.Status)
	require.Equal(t, "2024-01-03", result.TradeDate)
	sources, err := dal.ExternalDocumentIngestions.QueryByDocumentID(context.Background(), s.db, 1)
	require.NoError(t, err)
	require.EqualValues(t, 5, sources[0].Status)
	require.EqualValues(t, 1, sources[0].AttemptCount)
	_, err = s.RetryFailedDocument(context.Background(), 1, nil)
	require.ErrorIs(t, err, ErrDocumentRetryNotAllowed)
}

func TestDocumentRetryFailureRestoresFailedEvenAfterCancellation(t *testing.T) {
	s := retryTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	_, err := s.retryFailedDocument(ctx, 1, nil, func(ctx context.Context, id int64, date time.Time) ([]domain.CandidatePlan, error) {
		require.NoError(t, dal.Documents.UpdateStatusByID(ctx, s.db, id, "PARSED"))
		cancel()
		return nil, context.Canceled
	})
	require.ErrorIs(t, err, context.Canceled)
	document, err := dal.Documents.QueryMetadataByID(context.Background(), s.db, 1)
	require.NoError(t, err)
	require.Equal(t, "FAILED", document.Status)
	_, err = s.acquireDocumentAnalysis(1)
	require.NoError(t, err)
}

func TestDocumentRetryTreatsNoValidRecommendationAsCompleted(t *testing.T) {
	s := retryTestService(t)
	result, err := s.retryFailedDocument(context.Background(), 1, nil, func(ctx context.Context, id int64, date time.Time) ([]domain.CandidatePlan, error) {
		require.NoError(t, dal.Documents.UpdateStatusByID(ctx, s.db, id, "INVALID"))
		return nil, errors.New("no valid intent")
	})
	require.NoError(t, err)
	require.Equal(t, "INVALID", result.Status)
	_, err = s.RetryFailedDocument(context.Background(), 1, nil)
	require.ErrorIs(t, err, ErrDocumentRetryNotAllowed)
}

func TestRetryKeepsClaimUntilFinalOutcome(t *testing.T) {
	s := retryTestService(t)
	claimed, err := dal.Documents.ClaimFailedAnalysis(context.Background(), s.db, 1)
	require.NoError(t, err)
	require.True(t, claimed)
	ctx := context.WithValue(context.Background(), documentRetryContextKey{}, true)
	for _, status := range []string{"PARSED", "FAILED"} {
		require.NoError(t, s.updateAnalysisStatus(ctx, 1, status))
		document, err := dal.Documents.QueryMetadataByID(ctx, s.db, 1)
		require.NoError(t, err)
		require.Equal(t, "ANALYZING", document.Status)
		claimed, err = dal.Documents.ClaimFailedAnalysis(ctx, s.db, 1)
		require.NoError(t, err)
		require.False(t, claimed)
	}
	require.NoError(t, s.updateAnalysisStatus(ctx, 1, "PLANNED"))
	document, err := dal.Documents.QueryMetadataByID(ctx, s.db, 1)
	require.NoError(t, err)
	require.Equal(t, "PLANNED", document.Status)
}
