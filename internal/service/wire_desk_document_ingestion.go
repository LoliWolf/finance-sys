package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain"
	"finance-sys/internal/domain/db_model"
	"finance-sys/internal/utils"
	"finance-sys/internal/wiredesk"
)

// WireDeskIngestionRequest is the scheduled-task input of a wire desk run
// (Xueqiu daily at 04:00, Tonghuashun at 06:00). Daily runs use the zero
// value; the admin API sets Manual.
type WireDeskIngestionRequest struct {
	Manual bool `json:"manual,omitempty"`
	// MaxArticles can only lower the source's max_articles_per_run.
	MaxArticles int `json:"max_articles,omitempty"`
	// SkipAck leaves posts pending (verification runs).
	SkipAck bool `json:"skip_ack,omitempty"`
}

type WireDeskIngestionResponse struct {
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

type wireDesk interface {
	Pending(ctx context.Context, afterID int64, limit int) (wiredesk.Page, error)
	Ack(ctx context.Context, ids []int64, by string) (wiredesk.AckResult, error)
}

func newWireDeskClient(source wireDeskSource, cfg config.WireDeskDocumentSourceConfig) (wireDesk, error) {
	return wiredesk.New(source.deskName(), cfg)
}

// wireDeskSource describes one desk-backed external source. Both desks share
// the API contract; only configuration, provenance and labels differ.
type wireDeskSource struct {
	// label names the source in errors and logs ("Xueqiu", "THS").
	label string
	// skipPrefix prefixes scheduled-run skip reasons ("XUEQIU", "THS").
	skipPrefix string
	// platform is the Chinese platform name used in document headers.
	platform string
	// filePrefix starts document file names ("xueqiu-901-101.txt").
	filePrefix string
	sourceType domain.ExternalDocumentSourceType
	config     func(*config.Config) config.WireDeskDocumentSourceConfig
	schedule   func(*config.Config) config.DailyTaskScheduleConfig
	// sourcePath is the canonical post URL, independent of the desk's own
	// ids; it keys the ingestion journal together with the status id.
	sourcePath func(wiredesk.Article) string
}

var (
	xueqiuWireDesk = wireDeskSource{
		label: "Xueqiu", skipPrefix: "XUEQIU", platform: "雪球", filePrefix: "xueqiu",
		sourceType: domain.ExternalDocumentSourceTypeXueqiu,
		config:     func(c *config.Config) config.WireDeskDocumentSourceConfig { return c.ExternalDocuments.Xueqiu },
		schedule:   func(c *config.Config) config.DailyTaskScheduleConfig { return c.Scheduler.XueqiuDocumentIngestion },
		sourcePath: func(a wiredesk.Article) string {
			return fmt.Sprintf("https://xueqiu.com/%d/%d", a.Author.ID, a.StatusID)
		},
	}
	thsWireDesk = wireDeskSource{
		label: "THS", skipPrefix: "THS", platform: "同花顺", filePrefix: "ths",
		sourceType: domain.ExternalDocumentSourceTypeTHS,
		config:     func(c *config.Config) config.WireDeskDocumentSourceConfig { return c.ExternalDocuments.THS },
		schedule:   func(c *config.Config) config.DailyTaskScheduleConfig { return c.Scheduler.THSDocumentIngestion },
		// Status ids (pid) are unique across short views, posts, articles and
		// text-live messages; the item-type specific link stays in the header.
		sourcePath: func(a wiredesk.Article) string {
			return fmt.Sprintf("https://t.10jqka.com.cn/pid_%d.shtml", a.StatusID)
		},
	}
)

func (s wireDeskSource) deskName() string { return strings.ToLower(s.label) + " desk" }

type wireDeskArticleOutcome struct {
	processed  bool
	documentID int64
	skipped    bool
	// stored is false only when the post could not be persisted; such posts
	// are not ACKed.
	stored      bool
	analysisErr error
	storeErr    error
}

const maxReportedErrors = 20

// SyncXueqiu runs the Xueqiu wire desk source; see syncWireDesk.
func (s *ExternalDocumentIngestionService) SyncXueqiu(ctx context.Context, request WireDeskIngestionRequest) (*WireDeskIngestionResponse, error) {
	return s.syncWireDesk(ctx, xueqiuWireDesk, request)
}

// SyncTHS runs the Tonghuashun wire desk source; see syncWireDesk.
func (s *ExternalDocumentIngestionService) SyncTHS(ctx context.Context, request WireDeskIngestionRequest) (*WireDeskIngestionResponse, error) {
	return s.syncWireDesk(ctx, thsWireDesk, request)
}

// syncWireDesk reads every pending post from a wire desk (oldest first),
// stores each as a document, analyzes up to analysis_concurrency at a time,
// and ACKs every post that is stored in FinanceSys, whether or not its
// analysis succeeded.
func (s *ExternalDocumentIngestionService) syncWireDesk(ctx context.Context, desc wireDeskSource, request WireDeskIngestionRequest) (*WireDeskIngestionResponse, error) {
	if s == nil || s.db == nil || s.runtime == nil || s.documents == nil {
		return nil, fmt.Errorf("external document ingestion service is unavailable")
	}
	cfg := s.runtime.Config()
	if cfg == nil {
		return nil, fmt.Errorf("config runtime unavailable")
	}
	source := desc.config(cfg)
	if !source.Enabled {
		return nil, fmt.Errorf("%s document ingestion is disabled", desc.label)
	}
	location, err := time.LoadLocation(cfg.Meta.Timezone)
	if err != nil {
		return nil, fmt.Errorf("load ingestion timezone %q: %w", cfg.Meta.Timezone, err)
	}
	desk, err := s.newWireDesk(desc, source)
	if err != nil {
		return nil, err
	}
	limit := source.MaxArticlesPerRun
	if request.MaxArticles > 0 && request.MaxArticles < limit {
		limit = request.MaxArticles
	}
	gate := &analysisSpacing{interval: time.Duration(source.AnalysisIntervalMS) * time.Millisecond, now: s.now, sleep: s.sleep}

	response := &WireDeskIngestionResponse{DocumentIDs: []int64{}}
	documentIDs := make(map[int64]struct{})
	var afterID int64
	for response.FetchedCount < limit {
		pageSize := source.PageSize
		if remaining := limit - response.FetchedCount; remaining < pageSize {
			pageSize = remaining
		}
		page, err := desk.Pending(ctx, afterID, pageSize)
		if err != nil {
			return response, fmt.Errorf("read %s pending posts: %w", desc.label, err)
		}
		if len(page.Items) == 0 {
			break
		}
		var ackIDs []int64
		outcomes := s.processWireDeskPage(ctx, desc, page.Items, cfg, location, gate, source.AnalysisConcurrency)
		for i, article := range page.Items {
			outcome := outcomes[i]
			if !outcome.processed {
				continue // run canceled before this post started
			}
			response.FetchedCount++
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
				return response.finish(documentIDs), fmt.Errorf("ACK %s posts: %w", desc.label, err)
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
		s.logger.InfoContext(ctx, "wire desk ingestion run completed", "source", desc.label, "fetched", response.FetchedCount,
			"succeeded", response.SucceededCount, "failed", response.FailedCount, "skipped", response.SkippedCount,
			"not_stored", response.NotStoredCount, "acked", response.AckedCount, "truncated", response.Truncated)
	}
	if response.NotStoredCount > 0 {
		return response, fmt.Errorf("%d %s posts could not be stored and stay pending: %s",
			response.NotStoredCount, desc.label, strings.Join(response.Errors, "; "))
	}
	return response, nil
}

// processWireDeskPage handles one page with up to `workers` posts in flight.
// Analyses mostly wait on the model, so a few workers keep a daily run
// short; request starts stay spaced by the gate and the
// global processing.llm_max_qps limit.
func (s *ExternalDocumentIngestionService) processWireDeskPage(ctx context.Context, desc wireDeskSource, items []wiredesk.Article, cfg *config.Config, location *time.Location, gate *analysisSpacing, workers int) []wireDeskArticleOutcome {
	outcomes := make([]wireDeskArticleOutcome, len(items))
	if workers < 1 {
		workers = 1
	}
	if workers > len(items) {
		workers = len(items)
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				outcome := s.processWireDeskArticle(ctx, desc, items[i], cfg, location, gate)
				outcome.processed = true
				outcomes[i] = outcome
			}
		}()
	}
feed:
	for i := range items {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	return outcomes
}

func (r *WireDeskIngestionResponse) addError(message string) {
	if len(r.Errors) < maxReportedErrors {
		r.Errors = append(r.Errors, message)
	}
}

func (r *WireDeskIngestionResponse) finish(ids map[int64]struct{}) *WireDeskIngestionResponse {
	r.DocumentIDs = r.DocumentIDs[:0]
	for id := range ids {
		r.DocumentIDs = append(r.DocumentIDs, id)
	}
	sort.Slice(r.DocumentIDs, func(i, j int) bool { return r.DocumentIDs[i] < r.DocumentIDs[j] })
	return r
}

func (s *ExternalDocumentIngestionService) processWireDeskArticle(ctx context.Context, desc wireDeskSource, article wiredesk.Article, cfg *config.Config, location *time.Location, gate *analysisSpacing) wireDeskArticleOutcome {
	if strings.TrimSpace(article.Text) == "" {
		// Image-only post: nothing for the text pipeline; consumed as-is.
		return wireDeskArticleOutcome{skipped: true, stored: true}
	}
	published := article.SyncedAt
	if article.PublishedAt != nil && !article.PublishedAt.IsZero() {
		published = *article.PublishedAt
	}
	published = published.In(location)
	content := []byte(wireDeskDocumentText(desc, article, published))
	sourcePath := desc.sourcePath(article)
	fileName := fmt.Sprintf("%s-%d-%d.txt", desc.filePrefix, article.Author.ID, article.StatusID)
	publishedUTC := published.UTC()
	row := &db_model.ExternalDocumentIngestion{
		SourceType:       int32(desc.sourceType),
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
		return wireDeskArticleOutcome{storeErr: fmt.Errorf("record ingestion: %w", err)}
	}
	if !created && domain.ExternalDocumentIngestionStatus(ingestion.Status).Terminal() {
		return wireDeskArticleOutcome{skipped: true, stored: true}
	}
	if err := dal.ExternalDocumentIngestions.MarkDownloading(ctx, s.db, ingestion.ID); err != nil {
		return wireDeskArticleOutcome{storeErr: fmt.Errorf("mark ingestion started: %w", err)}
	}
	markFailed := func(cause error) error {
		if updateErr := dal.ExternalDocumentIngestions.MarkFailed(context.WithoutCancel(ctx), s.db, ingestion.ID, cause.Error()); updateErr != nil {
			return fmt.Errorf("%w; persist ingestion failure: %v", cause, updateErr)
		}
		return cause
	}

	document, duplicate, err := s.documents.IngestDocument(ctx, domain.DocumentIngestRequest{
		Author:      strings.TrimSpace(article.Author.Name),
		Institution: desc.config(cfg).Institution,
		Title:       wireDeskDocumentTitle(desc, article),
		FileName:    fileName,
		Content:     content,
	})
	if err != nil {
		return wireDeskArticleOutcome{storeErr: markFailed(err)}
	}
	if err := dal.ExternalDocumentIngestions.MarkIngested(ctx, s.db, ingestion.ID, document.ID, utils.SHA256Hex(content), s.now().UTC()); err != nil {
		return wireDeskArticleOutcome{documentID: document.ID, storeErr: markFailed(err)}
	}
	stored := wireDeskArticleOutcome{documentID: document.ID, stored: true}
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
		s.logger.InfoContext(ctx, "wire desk post ingestion completed", "source", desc.label, "status_id", article.StatusID,
			"author", article.Author.Name, "document_id", document.ID, "duplicate", duplicate,
			"recommend_date", published.Format(time.DateOnly))
	}
	return stored
}

func wireDeskDocumentTitle(desc wireDeskSource, article wiredesk.Article) string {
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
	return fmt.Sprintf("%s帖子 %d", desc.platform, article.StatusID)
}

// wireDeskDocumentText renders a post as the plain-text document the parser
// and model read: a short provenance header, then the body.
func wireDeskDocumentText(desc wireDeskSource, article wiredesk.Article, published time.Time) string {
	var b strings.Builder
	if title := strings.TrimSpace(article.Title); title != "" && !strings.HasPrefix(strings.TrimSpace(article.Text), title) {
		b.WriteString(title + "\n")
	}
	fmt.Fprintf(&b, "作者：%s（%s用户 %d）\n", strings.TrimSpace(article.Author.Name), desc.platform, article.Author.ID)
	fmt.Fprintf(&b, "发布时间：%s\n", published.Format("2006-01-02 15:04 MST"))
	link := strings.TrimSpace(article.URL)
	if link == "" {
		link = desc.sourcePath(article)
	}
	fmt.Fprintf(&b, "原文链接：%s\n", link)
	if len(article.Symbols) > 0 {
		fmt.Fprintf(&b, "%s标注的标的：%s\n", desc.platform, strings.Join(article.Symbols, "、"))
	}
	if len(article.Images) > 0 {
		fmt.Fprintf(&b, "附图：%d 张（未包含在本文中）\n", len(article.Images))
	}
	if article.Partial {
		fmt.Fprintf(&b, "说明：完整正文获取失败，以下为%s列表页摘要。\n", desc.platform)
	}
	b.WriteString("\n")
	b.WriteString(strings.TrimSpace(article.Text))
	b.WriteString("\n")
	return b.String()
}

// analysisSpacing keeps at least interval between two analysis starts of one
// run, across its workers. The global processing.llm_max_qps gate still
// applies on top.
type analysisSpacing struct {
	interval time.Duration
	now      func() time.Time
	sleep    func(context.Context, time.Duration) error
	mu       sync.Mutex
	next     time.Time
}

func (g *analysisSpacing) wait(ctx context.Context) error {
	if g.interval <= 0 {
		return nil
	}
	g.mu.Lock()
	now := g.now()
	start := g.next
	if start.Before(now) {
		start = now
	}
	g.next = start.Add(g.interval)
	g.mu.Unlock()
	if delay := start.Sub(now); delay > 0 {
		return g.sleep(ctx, delay)
	}
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

// xueqiuScheduledRunSkipReason and thsScheduledRunSkipReason gate the daily
// wire desk runs; see wireDeskScheduledRunSkipReason.
func xueqiuScheduledRunSkipReason(run *db_model.ScheduledTaskRun, cfg *config.Config, now time.Time) (string, error) {
	return wireDeskScheduledRunSkipReason(xueqiuWireDesk, run, cfg, now)
}

func thsScheduledRunSkipReason(run *db_model.ScheduledTaskRun, cfg *config.Config, now time.Time) (string, error) {
	return wireDeskScheduledRunSkipReason(thsWireDesk, run, cfg, now)
}

// wireDeskScheduledRunSkipReason mirrors the OpenList rules for the daily
// run: no catch-up outside the start minute and no second attempt of a
// crashed batch. Manual runs only require the source to be enabled.
func wireDeskScheduledRunSkipReason(desc wireDeskSource, run *db_model.ScheduledTaskRun, cfg *config.Config, now time.Time) (string, error) {
	if !desc.config(cfg).Enabled {
		return desc.skipPrefix + "_SOURCE_DISABLED", nil
	}
	var request WireDeskIngestionRequest
	if len(run.InputJSON) > 0 {
		if err := json.Unmarshal(run.InputJSON, &request); err != nil {
			return "", fmt.Errorf("decode %s scheduled task input: %w", desc.label, err)
		}
	}
	if run.AttemptCount > 1 {
		return desc.skipPrefix + "_ATTEMPT_ALREADY_STARTED", nil
	}
	if request.Manual {
		return "", nil
	}
	schedule := desc.schedule(cfg)
	if !cfg.Scheduler.Enabled || !schedule.Enabled {
		return desc.skipPrefix + "_SCHEDULE_DISABLED", nil
	}
	location, err := time.LoadLocation(cfg.Meta.Timezone)
	if err != nil {
		return "", fmt.Errorf("load %s scheduler timezone %q: %w", desc.label, cfg.Meta.Timezone, err)
	}
	now = now.In(location)
	scheduledAt := dailyScheduledAt(now, schedule.Hour, schedule.Minute)
	if run.TaskKey != scheduledAt.Format(time.DateOnly) || !run.ScheduledAt.Equal(scheduledAt) {
		return "OBSOLETE_" + desc.skipPrefix + "_SCHEDULE", nil
	}
	if !withinScheduledMinute(now, scheduledAt) {
		return "MISSED_" + desc.skipPrefix + "_START_WINDOW", nil
	}
	return "", nil
}
