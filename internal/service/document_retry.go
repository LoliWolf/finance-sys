package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"finance-sys/internal/dal"
	"finance-sys/internal/domain"
	"finance-sys/internal/domain/db_model"
)

var (
	ErrDocumentRetryNotAllowed   = errors.New("仅允许没有成功结果的 FAILED 文档重新分析")
	ErrDocumentAnalysisBusy      = errors.New("文档正在分析，请勿重复提交")
	ErrDocumentRetryDateRequired = errors.New("无法确定原分析日期，请确认后填写原推荐日期")
	ErrDocumentRetryDateConflict = errors.New("日期与原分析或来源日期不一致，请核实后重试")
)

type DocumentRetryResult struct {
	DocumentID int64  `json:"document_id"`
	Status     string `json:"status"`
	PlanCount  int    `json:"plan_count"`
	TradeDate  string `json:"trade_date"`
	Warning    string `json:"warning,omitempty"`
}

type documentRetryContextKey struct{}

func (s *DocumentService) updateAnalysisStatus(ctx context.Context, id int64, status string) error {
	// Keep the database claim until the retry's final outcome is persisted.
	// A transient PARSED/FAILED write must not allow another process to claim it.
	if retry, _ := ctx.Value(documentRetryContextKey{}).(bool); retry && (status == "PARSED" || status == "FAILED") {
		return nil
	}
	return dal.Documents.UpdateStatusByID(ctx, s.db, id, status)
}

func (s *DocumentService) acquireDocumentAnalysis(id int64) (func(), error) {
	s.analysisMu.Lock()
	defer s.analysisMu.Unlock()
	if _, busy := s.activeAnalyses[id]; busy {
		return nil, ErrDocumentAnalysisBusy
	}
	if s.activeAnalyses == nil {
		s.activeAnalyses = make(map[int64]struct{})
	}
	s.activeAnalyses[id] = struct{}{}
	return func() { s.analysisMu.Lock(); delete(s.activeAnalyses, id); s.analysisMu.Unlock() }, nil
}

func (s *DocumentService) RetryFailedDocument(ctx context.Context, id int64, requestedDate *time.Time) (*DocumentRetryResult, error) {
	return s.retryFailedDocument(ctx, id, requestedDate, func(ctx context.Context, id int64, date time.Time) ([]domain.CandidatePlan, error) {
		ctx = context.WithValue(ctx, documentRetryContextKey{}, true)
		return s.analyzeDocumentUnlocked(domain.ContextWithTradeDate(ctx, date), id)
	})
}

func (s *DocumentService) retryFailedDocument(ctx context.Context, id int64, requestedDate *time.Time, analyze func(context.Context, int64, time.Time) ([]domain.CandidatePlan, error)) (*DocumentRetryResult, error) {
	release, err := s.acquireDocumentAnalysis(id)
	if err != nil {
		return nil, err
	}
	defer release()
	document, err := dal.Documents.QueryMetadataByID(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	if document.Status != string(domain.DocumentStatusFailed) {
		return nil, ErrDocumentRetryNotAllowed
	}
	runs, err := dal.InstrumentResolutionRuns.QueryByDocumentID(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	for _, run := range runs {
		if run.Status == string(domain.ResolutionRunStatusSucceeded) {
			return nil, ErrDocumentRetryNotAllowed
		}
	}
	sources, err := dal.ExternalDocumentIngestions.QueryByDocumentID(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	date, err := retryDocumentDate(runs, sources, requestedDate)
	if err != nil {
		return nil, err
	}
	claimed, err := dal.Documents.ClaimFailedAnalysis(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, ErrDocumentRetryNotAllowed
	}
	// Always recover a retry interrupted by cancellation/error without overwriting
	// a successfully committed PLANNED/INVALID result.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if resetErr := dal.Documents.RestoreFailedAnalysis(cleanup, s.db, id); resetErr != nil && s.logger != nil {
			s.logger.Error("restore failed document retry state", "document_id", id, "error", resetErr.Error())
		}
	}()
	if err := dal.ExternalDocumentIngestions.BeginDocumentRetry(ctx, s.db, id); err != nil {
		return nil, err
	}
	plans, analyzeErr := analyze(ctx, id, date)
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	current, err := dal.Documents.QueryMetadataByID(cleanup, s.db, id)
	if err != nil {
		return nil, err
	}
	completed := current.Status == string(domain.DocumentStatusPlanned) || current.Status == string(domain.DocumentStatusInvalid)
	if !completed {
		if analyzeErr == nil {
			analyzeErr = fmt.Errorf("analysis did not reach a terminal document state")
		}
		_ = dal.ExternalDocumentIngestions.FinishDocumentRetry(cleanup, s.db, id, false, truncateString(analyzeErr.Error(), 1000))
		return nil, analyzeErr
	}
	result := &DocumentRetryResult{DocumentID: id, Status: current.Status, PlanCount: len(plans), TradeDate: date.Format(time.DateOnly)}
	if syncErr := dal.ExternalDocumentIngestions.FinishDocumentRetry(cleanup, s.db, id, true, ""); syncErr != nil {
		result.Warning = "文章已分析完成，但来源状态同步失败，请检查摄取记录"
		if s.logger != nil {
			s.logger.Error("sync document retry source state", "document_id", id, "error", syncErr.Error())
		}
	}
	return result, nil
}

// Prefer the previous attempt's recorded date, then an unambiguous source date.
// Never substitute today's date for a historical article.
func retryDocumentDate(runs []db_model.InstrumentResolutionRun, sources []db_model.ExternalDocumentIngestion, requested *time.Time) (time.Time, error) {
	var original time.Time
	for _, run := range runs {
		var metadata struct {
			TradeDate string `json:"trade_date"`
		}
		if json.Unmarshal(run.RawMetadataJSON, &metadata) == nil && metadata.TradeDate != "" {
			value, err := time.Parse(time.DateOnly, metadata.TradeDate)
			if err != nil {
				return time.Time{}, ErrDocumentRetryDateConflict
			}
			original = value
			break
		}
	}
	if original.IsZero() {
		for _, source := range sources {
			if source.ArticleDate.IsZero() {
				continue
			}
			value, _ := time.Parse(time.DateOnly, source.ArticleDate.Format(time.DateOnly))
			if !original.IsZero() && !original.Equal(value) {
				return time.Time{}, ErrDocumentRetryDateConflict
			}
			original = value
		}
	}
	if requested != nil && !requested.IsZero() {
		value, _ := time.Parse(time.DateOnly, requested.Format(time.DateOnly))
		if !original.IsZero() && !original.Equal(value) {
			return time.Time{}, ErrDocumentRetryDateConflict
		}
		original = value
	}
	if original.IsZero() {
		return time.Time{}, ErrDocumentRetryDateRequired
	}
	return original, nil
}
