package service

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain"
	"finance-sys/internal/domain/db_model"
	"finance-sys/internal/openlist"
	"finance-sys/internal/utils"

	"gorm.io/gorm"
)

type OpenListDocumentIngestionRequest struct {
	FullScan bool `json:"full_scan"`
}

type OpenListDocumentIngestionResponse struct {
	DiscoveredCount int      `json:"discovered_count"`
	ProcessedCount  int      `json:"processed_count"`
	SkippedCount    int      `json:"skipped_count"`
	SucceededCount  int      `json:"succeeded_count"`
	FailedCount     int      `json:"failed_count"`
	DocumentIDs     []int64  `json:"document_ids"`
	Errors          []string `json:"errors,omitempty"`
}

type ExternalDocumentIngestionService struct {
	db        *gorm.DB
	runtime   *config.Runtime
	documents *DocumentService
	logger    *slog.Logger
	now       func() time.Time
}

type openListArticleResult struct {
	documentID int64
	skipped    bool
	err        error
}

func NewExternalDocumentIngestionService(db *gorm.DB, runtime *config.Runtime, documents *DocumentService, logger *slog.Logger) *ExternalDocumentIngestionService {
	return &ExternalDocumentIngestionService{
		db:        db,
		runtime:   runtime,
		documents: documents,
		logger:    logger,
		now:       time.Now,
	}
}

func (s *ExternalDocumentIngestionService) SyncOpenList(ctx context.Context, request OpenListDocumentIngestionRequest) (*OpenListDocumentIngestionResponse, error) {
	if s == nil || s.db == nil || s.runtime == nil || s.documents == nil {
		return nil, fmt.Errorf("external document ingestion service is unavailable")
	}
	cfg := s.runtime.Config()
	if cfg == nil {
		return nil, fmt.Errorf("config runtime unavailable")
	}
	openListCfg := cfg.ExternalDocuments.OpenList
	if !openListCfg.Enabled {
		return nil, fmt.Errorf("OpenList document ingestion is disabled")
	}
	client, err := openlist.New(openListCfg)
	if err != nil {
		return nil, err
	}
	location, err := time.LoadLocation(cfg.Meta.Timezone)
	if err != nil {
		return nil, fmt.Errorf("load ingestion timezone %q: %w", cfg.Meta.Timezone, err)
	}
	now := s.now().In(location)
	articles, err := client.Discover(ctx, openListCfg.RootPath, now, openListCfg.ScanLookbackDays, request.FullScan)
	if err != nil {
		return nil, err
	}
	response := &OpenListDocumentIngestionResponse{DiscoveredCount: len(articles)}
	if len(articles) == 0 {
		return response, nil
	}

	workerCount := cfg.Processing.LLMMaxConcurrency
	if workerCount > len(articles) {
		workerCount = len(articles)
	}
	if workerCount < 1 {
		workerCount = 1
	}
	jobs := make(chan openlist.RemoteArticle)
	results := make(chan openListArticleResult, len(articles))
	var workers sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for article := range jobs {
				results <- s.processOpenListArticle(ctx, client, article, cfg)
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, article := range articles {
			select {
			case jobs <- article:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()

	documentIDSet := make(map[int64]struct{})
	for result := range results {
		if result.err != nil {
			response.ProcessedCount++
			response.FailedCount++
			if len(response.Errors) < 20 {
				response.Errors = append(response.Errors, result.err.Error())
			}
			continue
		}
		if result.skipped {
			response.SkippedCount++
			continue
		}
		response.ProcessedCount++
		response.SucceededCount++
		if result.documentID > 0 {
			documentIDSet[result.documentID] = struct{}{}
		}
	}
	response.DocumentIDs = make([]int64, 0, len(documentIDSet))
	for documentID := range documentIDSet {
		response.DocumentIDs = append(response.DocumentIDs, documentID)
	}
	sort.Slice(response.DocumentIDs, func(i, j int) bool { return response.DocumentIDs[i] < response.DocumentIDs[j] })
	if ctx.Err() != nil {
		return response, ctx.Err()
	}
	if response.FailedCount > 0 {
		return response, fmt.Errorf("OpenList ingestion completed with %d failed files: %s", response.FailedCount, strings.Join(response.Errors, "; "))
	}
	return response, nil
}

func (s *ExternalDocumentIngestionService) processOpenListArticle(ctx context.Context, client *openlist.Client, article openlist.RemoteArticle, cfg *config.Config) openListArticleResult {
	pathHash := utils.SHA256Hex([]byte(article.Path))
	var remoteModifiedAt *time.Time
	if !article.Modified.IsZero() {
		modified := article.Modified.UTC()
		remoteModifiedAt = &modified
	}
	row := &db_model.ExternalDocumentIngestion{
		SourceType:       int32(domain.ExternalDocumentSourceTypeOpenList),
		SourcePath:       article.Path,
		SourcePathHash:   pathHash,
		SourceVersion:    article.SourceVersion,
		FileName:         article.Name,
		ArticleDate:      article.ArticleDate,
		RemoteSize:       article.Size,
		RemoteModifiedAt: remoteModifiedAt,
		Status:           int32(domain.ExternalDocumentIngestionStatusDiscovered),
		LastError:        "",
		DiscoveredAt:     s.now().UTC(),
		ConfigVersion:    cfg.Meta.ConfigVersion,
	}
	ingestion, created, err := dal.ExternalDocumentIngestions.Ensure(ctx, s.db, row)
	if err != nil {
		return openListArticleResult{err: fmt.Errorf("record OpenList article %s: %w", article.Path, err)}
	}
	if !created && domain.ExternalDocumentIngestionStatus(ingestion.Status).Terminal() {
		return openListArticleResult{skipped: true}
	}
	if err := dal.ExternalDocumentIngestions.MarkDownloading(ctx, s.db, ingestion.ID); err != nil {
		return openListArticleResult{err: fmt.Errorf("mark OpenList article downloading %s: %w", article.Path, err)}
	}

	fail := func(processErr error) openListArticleResult {
		if updateErr := dal.ExternalDocumentIngestions.MarkFailed(context.WithoutCancel(ctx), s.db, ingestion.ID, processErr.Error()); updateErr != nil {
			processErr = fmt.Errorf("%w; persist ingestion failure: %v", processErr, updateErr)
		}
		return openListArticleResult{err: fmt.Errorf("OpenList article %s: %w", article.Path, processErr)}
	}
	maxBytes := int64(cfg.Document.MaxFileSizeMB) * 1024 * 1024
	content, err := client.Download(ctx, article.Path, maxBytes)
	if err != nil {
		return fail(err)
	}
	contentSHA256 := utils.SHA256Hex(content)
	document, duplicate, err := s.documents.IngestDocument(ctx, domain.DocumentIngestRequest{
		Author:      inferExternalDocumentAuthor(article.Name),
		Institution: cfg.ExternalDocuments.OpenList.Institution,
		Title:       externalDocumentTitle(article.Name),
		FileName:    article.Name,
		Content:     content,
		PDFUseOCR:   true,
	})
	if err != nil {
		return fail(err)
	}
	if err := dal.ExternalDocumentIngestions.MarkIngested(ctx, s.db, ingestion.ID, document.ID, contentSHA256, s.now().UTC()); err != nil {
		return fail(err)
	}
	if duplicate && (document.Status == domain.DocumentStatusPlanned || document.Status == domain.DocumentStatusInvalid) {
		if err := dal.ExternalDocumentIngestions.MarkSucceeded(ctx, s.db, ingestion.ID, s.now().UTC()); err != nil {
			return fail(err)
		}
		return openListArticleResult{documentID: document.ID}
	}
	if err := dal.ExternalDocumentIngestions.MarkAnalyzing(ctx, s.db, ingestion.ID); err != nil {
		return fail(err)
	}
	tradeDate := article.ArticleDate.AddDate(0, 0, 1)
	_, analyzeErr := s.documents.AnalyzeDocumentForTradeDate(ctx, document.ID, tradeDate)
	if analyzeErr != nil {
		current, loadErr := s.documents.GetDocumentByID(context.WithoutCancel(ctx), document.ID)
		if loadErr == nil && current.Status == domain.DocumentStatusInvalid {
			analyzeErr = nil
		}
	}
	if analyzeErr != nil {
		return fail(analyzeErr)
	}
	if err := dal.ExternalDocumentIngestions.MarkSucceeded(ctx, s.db, ingestion.ID, s.now().UTC()); err != nil {
		return fail(err)
	}
	if s.logger != nil {
		s.logger.InfoContext(ctx, "OpenList article ingestion completed", "path", article.Path, "document_id", document.ID, "duplicate", duplicate, "article_date", article.ArticleDate.Format(time.DateOnly), "trade_date", tradeDate.Format(time.DateOnly))
	}
	return openListArticleResult{documentID: document.ID}
}

func inferExternalDocumentAuthor(fileName string) string {
	base := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	for i, r := range base {
		if r >= '0' && r <= '9' {
			return strings.TrimSpace(base[:i])
		}
	}
	return ""
}

func externalDocumentTitle(fileName string) string {
	base := strings.TrimSpace(strings.TrimSuffix(fileName, filepath.Ext(fileName)))
	if utf8.RuneCountInString(base) <= 255 {
		return base
	}
	runes := []rune(base)
	return string(runes[:255])
}
