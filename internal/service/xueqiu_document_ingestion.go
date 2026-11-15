package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain"
	"finance-sys/internal/domain/db_model"
	"finance-sys/internal/utils"
	"finance-sys/internal/xueqiudesk"
)

// XueqiuDocumentIngestionRequest is the scheduled-task input. The daily 04:00
// run uses the zero value; the admin API sets Manual.
type XueqiuDocumentIngestionRequest struct {
	Manual bool `json:"manual,omitempty"`
	// MaxArticles can only lower external_documents.xueqiu.max_articles_per_run.
	MaxArticles int `json:"max_articles,omitempty"`
	// SkipAck leaves posts pending (verification runs).
	SkipAck bool `json:"skip_ack,omitempty"`
}

type XueqiuDocumentIngestionResponse struct {
	FetchedCount int `json:"fetched_count"`
	// SkippedCount: already ingested by an earlier run, owned by another
	// analysis, or no text to analyze (image-only posts).
	SkippedCount   int `json:"skipped_count"`
	SucceededCount int `json:"succeeded_count"`
	// FailedCount: stored and ACKed, but analysis failed; retry via the
	// document retry API like any other failed document.
	FailedCount int `json:"failed_count"`
	// NotStoredCount: could not be stored; left pending for the next run.
	NotStoredCount int      `json:"not_stored_count"`
	AckedCount     int      `json:"acked_count"`
	Truncated      bool     `json:"truncated"`
	DocumentIDs    []int64  `json:"document_ids"`
	Errors         []string `json:"errors,omitempty"`
}

type xueqiuDesk interface {
	Pending(ctx context.Context, afterID int64, limit int) (xueqiudesk.Page, error)
	Ack(ctx context.Context, ids []int64, by string) (xueqiudesk.AckResult, error)
}

func newXueqiuDeskClient(cfg config.XueqiuDocumentSourceConfig) (xueqiuDesk, error) {
	return xueqiudesk.New(cfg)
}

type xueqiuArticleOutcome struct {
	documentID int64
	skipped    bool
	// stored is false only when the post could not be persisted; such posts
	// are not ACKed.
	stored      bool
	analysisErr error
	storeErr    error
}

const maxReportedErrors = 20

// SyncXueqiu reads every pending post from the wire desk (oldest first), stores
// each as a document, analyzes it one at a time, and ACKs every post that is
// stored in FinanceSys, whether or not its analysis succeeded.
func (s *ExternalDocumentIngestionService) SyncXueqiu(ctx context.Context, request XueqiuDocumentIngestionRequest) (*XueqiuDocumentIngestionResponse, error) {
	if s == nil || s.db == nil || s.runtime == nil || s.documents == nil {
		return nil, fmt.Errorf("external document ingestion service is unavailable")
	}
	cfg := s.runtime.Config()
	if cfg == nil {
		return nil, fmt.Errorf("config runtime unavailable")
	}
	source := cfg.ExternalDocuments.Xueqiu
	if !source.Enabled {
		return nil, fmt.Errorf("Xueqiu document ingestion is disabled")
	}
	location, err := time.LoadLocation(cfg.Meta.Timezone)
	if err != nil {
		return nil, fmt.Errorf("load ingestion timezone %q: %w", cfg.Meta.Timezone, err)
	}
	desk, err := s.newXueqiuDesk(source)
	if err != nil {
		return nil, err
	}
	limit := source.MaxArticlesPerRun
	if request.MaxArticles > 0 && request.MaxArticles < limit {
		limit = request.MaxArticles
	}
	gate := &analysisSpacing{interval: time.Duration(source.AnalysisIntervalMS) * time.Millisecond, now: s.now, sleep: s.sleep}

	response := &XueqiuDocumentIngestionResponse{DocumentIDs: []int64{}}
	documentIDs := make(map[int64]struct{})
	var afterID int64
	for response.FetchedCount < limit {
		pageSize := source.PageSize
		if remaining := limit - response.FetchedCount; remaining < pageSize {
			pageSize = remaining
		}
		page, err := desk.Pending(ctx, afterID, pageSize)
		if err != nil {
			return response, fmt.Errorf("read Xueqiu pending posts: %w", err)
		}
		if len(page.Items) == 0 {
			break
		}
		var ackIDs []int64
		for _, article := range page.Items {
			if ctx.Err() != nil {
				break
			}
			response.FetchedCount++
			outcome := s.processXueqiuArticle(ctx, article, cfg, location, gate)
			if outcome.documentID > 0 {
				documentIDs[outcome.documentID] = struct{}{}
			}
			switch {
			case outcome.storeErr != nil:
				response.NotStoredCount++
				response.addError(fmt.Sprintf("post %d: %v", article.StatusID, outcome.storeErr))
			case outcome.analysisErr != nil:
				response.FailedCount++
				response.addError(fmt.Sprintf("post %d (document %d): %v", article.StatusID, outcome.documentID, outcome.analysisErr))
			case outcome.skipped:
				response.SkippedCount++
			default:
				response.SucceededCount++
			}
			if outcome.stored {
				ackIDs = append(ackIDs, article.ID)
			}
		}
		if len(ackIDs) > 0 && !request.SkipAck {
			// ACK what is already stored even if the run is being canceled.
			result, err := desk.Ack(context.WithoutCancel(ctx), ackIDs, source.AckBy)
			response.AckedCount += len(result.Acked) + len(result.AlreadyAcked)
			if err != nil {
				return response.finish(documentIDs), fmt.Errorf("ACK Xueqiu posts: %w", err)
			}
		}
		if ctx.Err() != nil {
			return response.finish(documentIDs), ctx.Err()
		}
		afterID = page.Items[len(page.Items)-1].ID
		if !page.HasMore {
			break
		}
		response.Truncated = response.FetchedCount >= limit
	}
	response.finish(documentIDs)
	if s.logger != nil {
		s.logger.InfoContext(ctx, "Xueqiu ingestion run completed", "fetched", response.FetchedCount,
			"succeeded", response.SucceededCount, "failed", response.FailedCount, "skipped", response.SkippedCount,
			"not_stored", response.NotStoredCount, "acked", response.AckedCount, "truncated", response.Truncated)
	}
	if response.NotStoredCount > 0 {
		return response, fmt.Errorf("%d Xueqiu posts could not be stored and stay pending: %s",
			response.NotStoredCount, strings.Join(response.Errors, "; "))
	}
	return response, nil
}

func (r *XueqiuDocumentIngestionResponse) addError(message string) {
	if len(r.Errors) < maxReportedErrors {
		r.Errors = append(r.Errors, message)
	}
}

func (r *XueqiuDocumentIngestionResponse) finish(ids map[int64]struct{}) *XueqiuDocumentIngestionResponse {
	r.DocumentIDs = r.DocumentIDs[:0]
	for id := range ids {
		r.DocumentIDs = append(r.DocumentIDs, id)
	}
	sort.Slice(r.DocumentIDs, func(i, j int) bool { return r.DocumentIDs[i] < r.DocumentIDs[j] })
	return r
}

func (s *ExternalDocumentIngestionService) processXueqiuArticle(ctx context.Context, article xueqiudesk.Article, cfg *config.Config, location *time.Location, gate *analysisSpacing) xueqiuArticleOutcome {
	if strings.TrimSpace(article.Text) == "" {
		// Image-only post: nothing for the text pipeline; consumed as-is.
		return xueqiuArticleOutcome{skipped: true, stored: true}
	}
	published := article.SyncedAt
	if article.PublishedAt != nil && !article.PublishedAt.IsZero() {
		published = *article.PublishedAt
	}
	published = published.In(location)
	content := []byte(xueqiuDocumentText(article, published))
	sourcePath := xueqiuSourcePath(article)
	fileName := fmt.Sprintf("xueqiu-%d-%d.txt", article.Author.ID, article.StatusID)
	publishedUTC := published.UTC()
	row := &db_model.ExternalDocumentIngestion{
		SourceType:       int32(domain.ExternalDocumentSourceTypeXueqiu),
		SourcePath:       sourcePath,
		SourcePathHash:   utils.SHA256Hex([]byte(sourcePath)),
		SourceVersion:    strconv.FormatInt(article.StatusID, 10),
		FileName:         fileName,
		ArticleDate:      domain.RecommendationDateForArticle(published),
		RemoteSize:       int64(len(content)),
		RemoteModifiedAt: &publishedUTC,
		Status:           int32(domain.ExternalDocumentIngestionStatusDiscovered),
		LastError:        "",
		DiscoveredAt:     s.now().UTC(),
		ConfigVersion:    cfg.Meta.ConfigVersion,
	}
	ingestion, created, err := dal.ExternalDocumentIngestions.Ensure(ctx, s.db, row)
	if err != nil {
		return xueqiuArticleOutcome{storeErr: fmt.Errorf("record ingestion: %w", err)}
	}
	if !created && domain.ExternalDocumentIngestionStatus(ingestion.Status).Terminal() {
		return xueqiuArticleOutcome{skipped: true, stored: true}
	}
	if err := dal.ExternalDocumentIngestions.MarkDownloading(ctx, s.db, ingestion.ID); err != nil {
		return xueqiuArticleOutcome{storeErr: fmt.Errorf("mark ingestion started: %w", err)}
	}
	markFailed := func(cause error) error {
		if updateErr := dal.ExternalDocumentIngestions.MarkFailed(context.WithoutCancel(ctx), s.db, ingestion.ID, cause.Error()); updateErr != nil {
			return fmt.Errorf("%w; persist ingestion failure: %v", cause, updateErr)
		}
		return cause
	}

	document, duplicate, err := s.documents.IngestDocument(ctx, domain.DocumentIngestRequest{
		Author:      strings.TrimSpace(article.Author.Name),
		Institution: cfg.ExternalDocuments.Xueqiu.Institution,
		Title:       xueqiuDocumentTitle(article),
		FileName:    fileName,
		Content:     content,
	})
	if err != nil {
		return xueqiuArticleOutcome{storeErr: markFailed(err)}
	}
	if err := dal.ExternalDocumentIngestions.MarkIngested(ctx, s.db, ingestion.ID, document.ID, utils.SHA256Hex(content), s.now().UTC()); err != nil {
		return xueqiuArticleOutcome{documentID: document.ID, storeErr: markFailed(err)}
	}
	stored := xueqiuArticleOutcome{documentID: document.ID, stored: true}
	if duplicate && (document.Status == domain.DocumentStatusPlanned || document.Status == domain.DocumentStatusInvalid) {
		if err := dal.ExternalDocumentIngestions.MarkSucceeded(ctx, s.db, ingestion.ID, s.now().UTC()); err != nil {
			stored.analysisErr = markFailed(err)
		}
		return stored
	}
	if err := dal.ExternalDocumentIngestions.MarkAnalyzing(ctx, s.db, ingestion.ID); err != nil {
		stored.analysisErr = markFailed(err)
		return stored
	}
	if err := gate.wait(ctx); err != nil {
		stored.analysisErr = markFailed(err)
		return stored
	}
	_, analyzeErr := s.documents.AnalyzeDocumentForTradeDate(ctx, document.ID, domain.RecommendationDateForArticle(published))
	if errors.Is(analyzeErr, ErrDocumentAnalysisBusy) {
		// A manual retry/upload owns this document; leave its outcome alone.
		stored.skipped = true
		return stored
	}
	if analyzeErr != nil {
		current, loadErr := s.documents.GetDocumentByID(context.WithoutCancel(ctx), document.ID)
		if loadErr == nil && current.Status == domain.DocumentStatusInvalid {
			analyzeErr = nil // no tradable intent in the post: a valid outcome
		}
	}
	if analyzeErr != nil {
		stored.analysisErr = markFailed(analyzeErr)
		return stored
	}
	if err := dal.ExternalDocumentIngestions.MarkSucceeded(ctx, s.db, ingestion.ID, s.now().UTC()); err != nil {
		stored.analysisErr = markFailed(err)
		return stored
	}
	if s.logger != nil {
		s.logger.InfoContext(ctx, "Xueqiu post ingestion completed", "status_id", article.StatusID,
			"author", article.Author.Name, "document_id", document.ID, "duplicate", duplicate,
			"recommend_date", published.Format(time.DateOnly))
	}
	return stored
}

// xueqiuSourcePath is the canonical post URL, independent of the desk's own ids.
func xueqiuSourcePath(article xueqiudesk.Article) string {
	return fmt.Sprintf("https://xueqiu.com/%d/%d", article.Author.ID, article.StatusID)
}

func xueqiuDocumentTitle(article xueqiudesk.Article) string {
	for _, candidate := range []string{article.Title, article.DisplayTitle, article.Text} {
		title := strings.Join(strings.Fields(candidate), " ")
		if title == "" {
			continue
		}
		if utf8.RuneCountInString(title) > 60 && candidate == article.Text {
			title = string([]rune(title)[:60]) + "…"
		}
		return externalDocumentTitle(title)
	}
	return fmt.Sprintf("雪球帖子 %d", article.StatusID)
}

// xueqiuDocumentText renders a post as the plain-text document the parser and
// model read: a short provenance header, then the body.
func xueqiuDocumentText(article xueqiudesk.Article, published time.Time) string {
	var b strings.Builder
	if title := strings.TrimSpace(article.Title); title != "" {
		b.WriteString(title + "\n")
	}
	fmt.Fprintf(&b, "作者：%s（雪球用户 %d）\n", strings.TrimSpace(article.Author.Name), article.Author.ID)
	fmt.Fprintf(&b, "发布时间：%s\n", published.Format("2006-01-02 15:04 MST"))
	link := strings.TrimSpace(article.URL)
	if link == "" {
		link = xueqiuSourcePath(article)
	}
	fmt.Fprintf(&b, "原文链接：%s\n", link)
	if len(article.Symbols) > 0 {
		fmt.Fprintf(&b, "雪球标注的标的：%s\n", strings.Join(article.Symbols, "、"))
	}
	if len(article.Images) > 0 {
		fmt.Fprintf(&b, "附图：%d 张（未包含在本文中）\n", len(article.Images))
	}
	if article.Partial {
		b.WriteString("说明：完整正文获取失败，以下为雪球列表页摘要。\n")
	}
	b.WriteString("\n")
	b.WriteString(strings.TrimSpace(article.Text))
	b.WriteString("\n")
	return b.String()
}

// analysisSpacing keeps at least interval between two analysis starts of one
// run. The global processing.llm_max_qps gate still applies on top.
type analysisSpacing struct {
	interval time.Duration
	last     time.Time
	now      func() time.Time
	sleep    func(context.Context, time.Duration) error
}

func (g *analysisSpacing) wait(ctx context.Context) error {
	if g.interval > 0 && !g.last.IsZero() {
		if remaining := g.interval - g.now().Sub(g.last); remaining > 0 {
			if err := g.sleep(ctx, remaining); err != nil {
				return err
			}
		}
	}
	g.last = g.now()
	return nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// xueqiuScheduledRunSkipReason mirrors the OpenList rules for the daily run:
// no catch-up outside the start minute and no second attempt of a crashed
// batch. Manual runs only require the source to be enabled.
func xueqiuScheduledRunSkipReason(run *db_model.ScheduledTaskRun, cfg *config.Config, now time.Time) (string, error) {
	if !cfg.ExternalDocuments.Xueqiu.Enabled {
		return "XUEQIU_SOURCE_DISABLED", nil
	}
	var request XueqiuDocumentIngestionRequest
	if len(run.InputJSON) > 0 {
		if err := json.Unmarshal(run.InputJSON, &request); err != nil {
			return "", fmt.Errorf("decode Xueqiu scheduled task input: %w", err)
		}
	}
	if run.AttemptCount > 1 {
		return "XUEQIU_ATTEMPT_ALREADY_STARTED", nil
	}
	if request.Manual {
		return "", nil
	}
	schedule := cfg.Scheduler.XueqiuDocumentIngestion
	if !cfg.Scheduler.Enabled || !schedule.Enabled {
		return "XUEQIU_SCHEDULE_DISABLED", nil
	}
	location, err := time.LoadLocation(cfg.Meta.Timezone)
	if err != nil {
		return "", fmt.Errorf("load Xueqiu scheduler timezone %q: %w", cfg.Meta.Timezone, err)
	}
	now = now.In(location)
	scheduledAt := dailyScheduledAt(now, schedule.Hour, schedule.Minute)
	if run.TaskKey != scheduledAt.Format(time.DateOnly) || !run.ScheduledAt.Equal(scheduledAt) {
		return "OBSOLETE_XUEQIU_SCHEDULE", nil
	}
	if !withinScheduledMinute(now, scheduledAt) {
		return "MISSED_XUEQIU_START_WINDOW", nil
	}
	return "", nil
}
