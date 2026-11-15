//go:build cgo

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/domain"
	"finance-sys/internal/domain/db_model"
	"finance-sys/internal/xueqiudesk"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type fakeXueqiuDesk struct {
	mu       sync.Mutex
	articles []xueqiudesk.Article
	acked    map[int64]bool
	ackCalls [][]int64
	ackErr   error
	pageErr  error
}

func (d *fakeXueqiuDesk) Pending(ctx context.Context, afterID int64, limit int) (xueqiudesk.Page, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.pageErr != nil {
		return xueqiudesk.Page{}, d.pageErr
	}
	var page xueqiudesk.Page
	for _, a := range d.articles {
		if a.ID <= afterID || d.acked[a.ID] {
			continue
		}
		if len(page.Items) == limit {
			page.HasMore = true
			break
		}
		page.Items = append(page.Items, a)
	}
	return page, nil
}

func (d *fakeXueqiuDesk) Ack(ctx context.Context, ids []int64, by string) (xueqiudesk.AckResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ackErr != nil {
		return xueqiudesk.AckResult{}, d.ackErr
	}
	if by != "financesys" {
		return xueqiudesk.AckResult{}, fmt.Errorf("unexpected ack actor %q", by)
	}
	d.ackCalls = append(d.ackCalls, append([]int64(nil), ids...))
	for _, id := range ids {
		d.acked[id] = true
	}
	return xueqiudesk.AckResult{Acked: ids}, nil
}

type fakePipeline struct {
	mu          sync.Mutex
	nextID      int64
	bySHA       map[string]*domain.Document
	requests    []domain.DocumentIngestRequest
	analyzed    []int64
	tradeDates  []time.Time
	ingestErr   map[string]error // by file name
	analyzeErr  map[int64]error
	invalidDocs map[int64]bool
}

func (p *fakePipeline) IngestDocument(ctx context.Context, request domain.DocumentIngestRequest) (*domain.Document, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.ingestErr[request.FileName]; err != nil {
		return nil, false, err
	}
	key := string(request.Content)
	if existing, ok := p.bySHA[key]; ok {
		return existing, true, nil
	}
	p.nextID++
	doc := &domain.Document{ID: p.nextID, Author: request.Author, Institution: request.Institution, Title: request.Title,
		FileName: request.FileName, Status: domain.DocumentStatusIngested}
	p.bySHA[key] = doc
	p.requests = append(p.requests, request)
	return doc, false, nil
}

func (p *fakePipeline) AnalyzeDocumentForTradeDate(ctx context.Context, documentID int64, tradeDate time.Time) ([]domain.CandidatePlan, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.analyzed = append(p.analyzed, documentID)
	p.tradeDates = append(p.tradeDates, tradeDate)
	if err := p.analyzeErr[documentID]; err != nil {
		return nil, err
	}
	for _, doc := range p.bySHA {
		if doc.ID == documentID {
			doc.Status = domain.DocumentStatusPlanned
			if p.invalidDocs[documentID] {
				doc.Status = domain.DocumentStatusInvalid
			}
		}
	}
	return nil, nil
}

func (p *fakePipeline) GetDocumentByID(ctx context.Context, documentID int64) (*domain.Document, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, doc := range p.bySHA {
		if doc.ID == documentID {
			copied := *doc
			return &copied, nil
		}
	}
	return nil, errors.New("not found")
}

type xueqiuFixture struct {
	service  *ExternalDocumentIngestionService
	desk     *fakeXueqiuDesk
	pipeline *fakePipeline
	cfg      *config.Config
	sleeps   []time.Duration
	db       *gorm.DB
}

func newXueqiuFixture(t *testing.T) *xueqiuFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Exec(`CREATE TABLE external_document_ingestions (
		id INTEGER PRIMARY KEY AUTOINCREMENT, source_type INTEGER NOT NULL, source_path TEXT NOT NULL,
		source_path_hash TEXT NOT NULL, source_version TEXT NOT NULL, file_name TEXT NOT NULL,
		article_date DATETIME NOT NULL, remote_size INTEGER NOT NULL DEFAULT 0, remote_modified_at DATETIME,
		content_sha256 TEXT NOT NULL DEFAULT '', document_id INTEGER, status INTEGER NOT NULL,
		attempt_count INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '',
		discovered_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, downloaded_at DATETIME, analyzed_at DATETIME,
		config_version INTEGER NOT NULL, created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(source_type, source_path_hash, source_version))`).Error)

	cfg := &config.Config{}
	cfg.Meta.Timezone = "Asia/Shanghai"
	cfg.Meta.ConfigVersion = 79
	cfg.ExternalDocuments.Xueqiu = config.XueqiuDocumentSourceConfig{
		Enabled: true, BaseURL: "http://desk", Institution: "雪球", RequestTimeoutMS: 1000,
		PageSize: 2, MaxArticlesPerRun: 100, AnalysisIntervalMS: 1000, AckBy: "financesys",
	}
	f := &xueqiuFixture{
		desk:     &fakeXueqiuDesk{acked: map[int64]bool{}},
		pipeline: &fakePipeline{bySHA: map[string]*domain.Document{}, ingestErr: map[string]error{}, analyzeErr: map[int64]error{}, invalidDocs: map[int64]bool{}},
		cfg:      cfg,
		db:       db,
	}
	clock := time.Date(2026, time.November, 16, 4, 0, 0, 0, config.ChinaLocation)
	f.service = &ExternalDocumentIngestionService{
		db:            db,
		runtime:       config.NewRuntime(&config.Snapshot{Config: cfg}),
		documents:     f.pipeline,
		now:           func() time.Time { return clock },
		newXueqiuDesk: func(config.XueqiuDocumentSourceConfig) (xueqiuDesk, error) { return f.desk, nil },
		sleep: func(ctx context.Context, d time.Duration) error {
			f.sleeps = append(f.sleeps, d)
			return nil
		},
	}
	return f
}

func xueqiuArticle(id, statusID int64, author string, text string, published time.Time) xueqiudesk.Article {
	at := published
	return xueqiudesk.Article{
		ID: id, StatusID: statusID, Author: xueqiudesk.Author{ID: 900 + id, Name: author}, Text: text,
		URL: fmt.Sprintf("https://xueqiu.com/%d/%d", 900+id, statusID), Symbols: []string{"SH600519"},
		PublishedAt: &at, SyncedAt: published.Add(time.Hour), Origin: "new",
	}
}

func (f *xueqiuFixture) ingestionRows(t *testing.T) []db_model.ExternalDocumentIngestion {
	t.Helper()
	var rows []db_model.ExternalDocumentIngestion
	require.NoError(t, f.db.Order("id").Find(&rows).Error)
	return rows
}

func TestSyncXueqiuStoresAnalyzesAndAcksEveryStoredPost(t *testing.T) {
	f := newXueqiuFixture(t)
	// 22:30 Beijing time on the 15th is still the 15th for the recommendation date.
	late := time.Date(2026, time.November, 15, 14, 30, 0, 0, time.UTC)
	f.desk.articles = []xueqiudesk.Article{
		xueqiuArticle(1, 101, "博主甲", "看好 $贵州茅台(SH600519)$，逢低加仓", late),
		xueqiuArticle(2, 102, "博主乙", "", late),            // image-only: consumed without a document
		xueqiuArticle(3, 103, "博主丙", "减仓宁德时代，等回调", late),  // analysis fails: stored, ACKed, journaled
		xueqiuArticle(4, 104, "博主丁", "今天只是闲聊，没有标的", late), // no tradable intent: INVALID is success
		xueqiuArticle(5, 105, "博主戊", "存储失败的帖子", late),     // cannot be stored: stays pending
	}
	f.pipeline.analyzeErr[2] = errors.New("model timeout") // document 2 = post 103
	f.pipeline.invalidDocs[3] = true                       // document 3 = post 104
	f.pipeline.ingestErr["xueqiu-905-105.txt"] = errors.New("db down")

	resp, err := f.service.SyncXueqiu(context.Background(), XueqiuDocumentIngestionRequest{})
	require.Error(t, err, "a post that could not be stored must fail the run")
	require.Contains(t, err.Error(), "stay pending")
	require.Equal(t, 5, resp.FetchedCount)
	require.Equal(t, 2, resp.SucceededCount)
	require.Equal(t, 1, resp.SkippedCount)
	require.Equal(t, 1, resp.FailedCount)
	require.Equal(t, 1, resp.NotStoredCount)
	require.Equal(t, 4, resp.AckedCount)
	require.Equal(t, []int64{1, 2, 3}, resp.DocumentIDs)
	require.Equal(t, map[int64]bool{1: true, 2: true, 3: true, 4: true}, f.desk.acked, "everything stored is ACKed; the unstored post stays pending")
	require.Equal(t, [][]int64{{1, 2}, {3, 4}}, f.desk.ackCalls, "ACK after each page")

	first := f.pipeline.requests[0]
	require.Equal(t, "博主甲", first.Author, "the Xueqiu author is the blogger")
	require.Equal(t, "雪球", first.Institution)
	require.Equal(t, "xueqiu-901-101.txt", first.FileName)
	body := string(first.Content)
	require.Contains(t, body, "作者：博主甲（雪球用户 901）")
	require.Contains(t, body, "发布时间：2026-11-15 22:30 CST")
	require.Contains(t, body, "原文链接：https://xueqiu.com/901/101")
	require.Contains(t, body, "雪球标注的标的：SH600519")
	require.True(t, strings.HasSuffix(body, "看好 $贵州茅台(SH600519)$，逢低加仓\n"))
	for _, d := range f.pipeline.tradeDates {
		require.Equal(t, "2026-11-15", d.Format(time.DateOnly))
	}
	require.Equal(t, []time.Duration{time.Second, time.Second}, f.sleeps, "analyses are spaced by analysis_interval_ms")

	rows := f.ingestionRows(t)
	require.Len(t, rows, 4)
	statuses := map[string]domain.ExternalDocumentIngestionStatus{}
	for _, row := range rows {
		require.Equal(t, int32(domain.ExternalDocumentSourceTypeXueqiu), row.SourceType)
		statuses[row.SourceVersion] = domain.ExternalDocumentIngestionStatus(row.Status)
	}
	require.Equal(t, domain.ExternalDocumentIngestionStatusSucceeded, statuses["101"])
	require.Equal(t, domain.ExternalDocumentIngestionStatusFailed, statuses["103"])
	require.Equal(t, domain.ExternalDocumentIngestionStatusSucceeded, statuses["104"])
	require.Equal(t, domain.ExternalDocumentIngestionStatusFailed, statuses["105"])

	// Next day: the stored-failed post is re-offered only if still pending; the
	// unstored one is retried and, once storable, analyzed and ACKed.
	delete(f.pipeline.ingestErr, "xueqiu-905-105.txt")
	resp, err = f.service.SyncXueqiu(context.Background(), XueqiuDocumentIngestionRequest{})
	require.NoError(t, err)
	require.Equal(t, 1, resp.FetchedCount)
	require.Equal(t, 1, resp.SucceededCount)
	require.True(t, f.desk.acked[5])
}

func TestSyncXueqiuSkipsPostsAlreadyIngestedAndHonorsLimitAndSkipAck(t *testing.T) {
	f := newXueqiuFixture(t)
	at := time.Date(2026, time.November, 15, 2, 0, 0, 0, time.UTC)
	for i := int64(1); i <= 5; i++ {
		f.desk.articles = append(f.desk.articles, xueqiuArticle(i, 100+i, "博主", fmt.Sprintf("帖子 %d 看好 $贵州茅台(SH600519)$", i), at))
	}
	resp, err := f.service.SyncXueqiu(context.Background(), XueqiuDocumentIngestionRequest{MaxArticles: 3, SkipAck: true})
	require.NoError(t, err)
	require.Equal(t, 3, resp.FetchedCount)
	require.True(t, resp.Truncated)
	require.Zero(t, resp.AckedCount)
	require.Empty(t, f.desk.acked, "skip_ack leaves the queue untouched")

	// A full run sees the first three again but does not re-analyze them.
	analyzedBefore := len(f.pipeline.analyzed)
	resp, err = f.service.SyncXueqiu(context.Background(), XueqiuDocumentIngestionRequest{})
	require.NoError(t, err)
	require.Equal(t, 5, resp.FetchedCount)
	require.Equal(t, 3, resp.SkippedCount)
	require.Equal(t, 2, resp.SucceededCount)
	require.Equal(t, 5, resp.AckedCount)
	require.False(t, resp.Truncated)
	require.Equal(t, analyzedBefore+2, len(f.pipeline.analyzed))
}

func TestSyncXueqiuStopsOnDeskErrors(t *testing.T) {
	f := newXueqiuFixture(t)
	f.desk.pageErr = errors.New("connection refused")
	_, err := f.service.SyncXueqiu(context.Background(), XueqiuDocumentIngestionRequest{})
	require.ErrorContains(t, err, "read Xueqiu pending posts")

	f = newXueqiuFixture(t)
	f.desk.articles = []xueqiudesk.Article{xueqiuArticle(1, 101, "博主", "看好 $贵州茅台(SH600519)$", time.Now())}
	f.desk.ackErr = errors.New("desk down")
	resp, err := f.service.SyncXueqiu(context.Background(), XueqiuDocumentIngestionRequest{})
	require.ErrorContains(t, err, "ACK Xueqiu posts")
	require.Equal(t, 1, resp.SucceededCount, "the post is stored even though the ACK failed; the journal skips it next time")

	f.cfg.ExternalDocuments.Xueqiu.Enabled = false
	_, err = f.service.SyncXueqiu(context.Background(), XueqiuDocumentIngestionRequest{})
	require.ErrorContains(t, err, "disabled")
}

func TestXueqiuScheduledRunSkipReasons(t *testing.T) {
	cfg := &config.Config{}
	cfg.Meta.Timezone = "Asia/Shanghai"
	cfg.Scheduler.Enabled = true
	cfg.Scheduler.XueqiuDocumentIngestion = config.DailyTaskScheduleConfig{Enabled: true, Hour: 4}
	cfg.ExternalDocuments.Xueqiu.Enabled = true
	at := time.Date(2026, time.November, 16, 4, 0, 0, 0, config.ChinaLocation)
	scheduled := func(attempt int32) *db_model.ScheduledTaskRun {
		return &db_model.ScheduledTaskRun{TaskKey: "2026-11-16", ScheduledAt: at.UTC(), AttemptCount: attempt, InputJSON: []byte(`{}`)}
	}
	reason, err := xueqiuScheduledRunSkipReason(scheduled(1), cfg, at.Add(20*time.Second))
	require.NoError(t, err)
	require.Empty(t, reason)
	reason, _ = xueqiuScheduledRunSkipReason(scheduled(1), cfg, at.Add(2*time.Hour))
	require.Equal(t, "MISSED_XUEQIU_START_WINDOW", reason)
	reason, _ = xueqiuScheduledRunSkipReason(scheduled(2), cfg, at.Add(10*time.Second))
	require.Equal(t, "XUEQIU_ATTEMPT_ALREADY_STARTED", reason)

	manual, err := json.Marshal(XueqiuDocumentIngestionRequest{Manual: true, MaxArticles: 3})
	require.NoError(t, err)
	reason, _ = xueqiuScheduledRunSkipReason(&db_model.ScheduledTaskRun{TaskKey: "manual-x", ScheduledAt: at, AttemptCount: 1, InputJSON: manual}, cfg, at.Add(9*time.Hour))
	require.Empty(t, reason, "manual runs ignore the daily window")

	cfg.Scheduler.XueqiuDocumentIngestion.Hour = 5
	reason, _ = xueqiuScheduledRunSkipReason(scheduled(1), cfg, at.Add(10*time.Second))
	require.Equal(t, "OBSOLETE_XUEQIU_SCHEDULE", reason)
	cfg.ExternalDocuments.Xueqiu.Enabled = false
	reason, _ = xueqiuScheduledRunSkipReason(scheduled(1), cfg, at)
	require.Equal(t, "XUEQIU_SOURCE_DISABLED", reason)
}

func TestXueqiuDailyEnqueueOnlyInStartMinute(t *testing.T) {
	scheduler, tasks, cfg, _ := openListScheduleFixture(t)
	cfg.Scheduler.OpenListDocumentIngestion.Enabled = false
	cfg.Scheduler.XueqiuDocumentIngestion = config.DailyTaskScheduleConfig{Enabled: true, Hour: 4}
	cfg.ExternalDocuments.Xueqiu.Enabled = true
	at := time.Date(2026, time.November, 16, 4, 0, 0, 0, config.ChinaLocation)
	ctx := context.Background()
	for _, offset := range []time.Duration{-time.Second, time.Minute, 3 * time.Hour} {
		require.NoError(t, scheduler.enqueueDueTasks(ctx, cfg, at.Add(offset)))
	}
	var count int64
	require.NoError(t, tasks.db.Model(&db_model.ScheduledTaskRun{}).Count(&count).Error)
	require.Zero(t, count)
	for _, offset := range []time.Duration{0, 30 * time.Second, 59 * time.Second} {
		require.NoError(t, scheduler.enqueueDueTasks(ctx, cfg, at.Add(offset)))
	}
	var runs []db_model.ScheduledTaskRun
	require.NoError(t, tasks.db.Find(&runs).Error)
	require.Len(t, runs, 1)
	require.Equal(t, uint16(domain.ScheduledTaskTypeXueqiuDocuments), runs[0].TaskType)
	require.Equal(t, "2026-11-16", runs[0].TaskKey)
}
