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
	"finance-sys/internal/wiredesk"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type fakeWireDesk struct {
	mu       sync.Mutex
	articles []wiredesk.Article
	acked    map[int64]bool
	ackCalls [][]int64
	ackErr   error
	pageErr  error
}

func (d *fakeWireDesk) Pending(ctx context.Context, afterID int64, limit int) (wiredesk.Page, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.pageErr != nil {
		return wiredesk.Page{}, d.pageErr
	}
	var page wiredesk.Page
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

func (d *fakeWireDesk) Ack(ctx context.Context, ids []int64, by string) (wiredesk.AckResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ackErr != nil {
		return wiredesk.AckResult{}, d.ackErr
	}
	if by != "financesys" {
		return wiredesk.AckResult{}, fmt.Errorf("unexpected ack actor %q", by)
	}
	d.ackCalls = append(d.ackCalls, append([]int64(nil), ids...))
	for _, id := range ids {
		d.acked[id] = true
	}
	return wiredesk.AckResult{Acked: ids}, nil
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
	desk     *fakeWireDesk
	pipeline *fakePipeline
	cfg      *config.Config
	db       *gorm.DB
	clockMu  sync.Mutex
	clock    time.Time
	sleeps   []time.Duration
	// deskSources records which source each desk client was built for.
	deskSources []string
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
	cfg.ExternalDocuments.Xueqiu = config.WireDeskDocumentSourceConfig{
		Enabled: true, BaseURL: "http://desk", Institution: "雪球", RequestTimeoutMS: 1000,
		PageSize: 2, MaxArticlesPerRun: 100, AnalysisIntervalMS: 1000, AckBy: "financesys",
	}
	cfg.ExternalDocuments.THS = config.WireDeskDocumentSourceConfig{
		Enabled: true, BaseURL: "http://ths-desk", Institution: "同花顺", RequestTimeoutMS: 1000,
		PageSize: 2, MaxArticlesPerRun: 100, AnalysisConcurrency: 2, AnalysisIntervalMS: 3000, AckBy: "financesys",
	}
	f := &xueqiuFixture{
		desk:     &fakeWireDesk{acked: map[int64]bool{}},
		pipeline: &fakePipeline{bySHA: map[string]*domain.Document{}, ingestErr: map[string]error{}, analyzeErr: map[int64]error{}, invalidDocs: map[int64]bool{}},
		cfg:      cfg,
		db:       db,
	}
	f.clock = time.Date(2026, time.November, 16, 4, 0, 0, 0, config.ChinaLocation)
	f.service = &ExternalDocumentIngestionService{
		db:        db,
		runtime:   config.NewRuntime(&config.Snapshot{Config: cfg}),
		documents: f.pipeline,
		now: func() time.Time {
			f.clockMu.Lock()
			defer f.clockMu.Unlock()
			return f.clock
		},
		newWireDesk: func(source wireDeskSource, _ config.WireDeskDocumentSourceConfig) (wireDesk, error) {
			f.deskSources = append(f.deskSources, source.label)
			return f.desk, nil
		},
		// Sleeping advances the fake clock, like real time would.
		sleep: func(ctx context.Context, d time.Duration) error {
			f.clockMu.Lock()
			defer f.clockMu.Unlock()
			f.sleeps = append(f.sleeps, d)
			if until := f.clock.Add(d); until.After(f.clock) {
				f.clock = until
			}
			return nil
		},
	}
	return f
}

func xueqiuArticle(id, statusID int64, author string, text string, published time.Time) wiredesk.Article {
	at := published
	return wiredesk.Article{
		ID: id, StatusID: statusID, Author: wiredesk.Author{ID: 900 + id, Name: author}, Text: text,
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
	f.desk.articles = []wiredesk.Article{
		xueqiuArticle(1, 101, "博主甲", "看好 $贵州茅台(SH600519)$，逢低加仓", late),
		xueqiuArticle(2, 102, "博主乙", "", late),            // image-only: consumed without a document
		xueqiuArticle(3, 103, "博主丙", "减仓宁德时代，等回调", late),  // analysis fails: stored, ACKed, journaled
		xueqiuArticle(4, 104, "博主丁", "今天只是闲聊，没有标的", late), // no tradable intent: INVALID is success
		xueqiuArticle(5, 105, "博主戊", "存储失败的帖子", late),     // cannot be stored: stays pending
	}
	f.pipeline.analyzeErr[2] = errors.New("model timeout") // document 2 = post 103
	f.pipeline.invalidDocs[3] = true                       // document 3 = post 104
	f.pipeline.ingestErr["xueqiu-905-105.txt"] = errors.New("db down")

	resp, err := f.service.SyncXueqiu(context.Background(), WireDeskIngestionRequest{})
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
	resp, err = f.service.SyncXueqiu(context.Background(), WireDeskIngestionRequest{})
	require.NoError(t, err)
	require.Equal(t, 1, resp.FetchedCount)
	require.Equal(t, 1, resp.SucceededCount)
	require.True(t, f.desk.acked[5])
}

// blockingPipeline holds analyses until `want` are in flight together.
type blockingPipeline struct {
	*fakePipeline
	mu       sync.Mutex
	inFlight int
	peak     int
	want     int
	release  chan struct{}
}

func (p *blockingPipeline) AnalyzeDocumentForTradeDate(ctx context.Context, documentID int64, tradeDate time.Time) ([]domain.CandidatePlan, error) {
	p.mu.Lock()
	p.inFlight++
	if p.inFlight > p.peak {
		p.peak = p.inFlight
	}
	if p.inFlight == p.want {
		close(p.release)
	}
	p.mu.Unlock()
	select {
	case <-p.release:
	case <-time.After(5 * time.Second):
		return nil, errors.New("analyses never ran in parallel")
	}
	defer func() {
		p.mu.Lock()
		p.inFlight--
		p.mu.Unlock()
	}()
	return p.fakePipeline.AnalyzeDocumentForTradeDate(ctx, documentID, tradeDate)
}

func TestSyncXueqiuAnalyzesUpToConcurrencyInParallel(t *testing.T) {
	f := newXueqiuFixture(t)
	f.cfg.ExternalDocuments.Xueqiu.PageSize = 10
	f.cfg.ExternalDocuments.Xueqiu.AnalysisConcurrency = 3
	f.cfg.ExternalDocuments.Xueqiu.AnalysisIntervalMS = 0
	blocking := &blockingPipeline{fakePipeline: f.pipeline, want: 3, release: make(chan struct{})}
	f.service.documents = blocking
	at := time.Date(2026, time.November, 15, 2, 0, 0, 0, time.UTC)
	for i := int64(1); i <= 7; i++ {
		f.desk.articles = append(f.desk.articles, xueqiuArticle(i, 100+i, "博主", fmt.Sprintf("帖子 %d 看好 $贵州茅台(SH600519)$", i), at))
	}
	resp, err := f.service.SyncXueqiu(context.Background(), WireDeskIngestionRequest{})
	require.NoError(t, err)
	require.Equal(t, 7, resp.SucceededCount)
	require.Equal(t, 7, resp.AckedCount)
	require.Equal(t, 3, blocking.peak, "never more than analysis_concurrency posts in flight")
	require.Len(t, f.ingestionRows(t), 7)
}

func TestAnalysisSpacingSpacesConcurrentStarts(t *testing.T) {
	var mu sync.Mutex
	clock := time.Date(2026, time.November, 16, 4, 0, 0, 0, time.UTC)
	var delays []time.Duration
	gate := &analysisSpacing{
		interval: time.Second,
		now:      func() time.Time { mu.Lock(); defer mu.Unlock(); return clock },
		sleep: func(ctx context.Context, d time.Duration) error {
			mu.Lock()
			delays = append(delays, d)
			mu.Unlock()
			return nil
		},
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); require.NoError(t, gate.wait(context.Background())) }()
	}
	wg.Wait()
	// Four simultaneous callers start at +0s, +1s, +2s, +3s.
	require.ElementsMatch(t, []time.Duration{time.Second, 2 * time.Second, 3 * time.Second}, delays)
}

func TestSyncXueqiuSkipsPostsAlreadyIngestedAndHonorsLimitAndSkipAck(t *testing.T) {
	f := newXueqiuFixture(t)
	at := time.Date(2026, time.November, 15, 2, 0, 0, 0, time.UTC)
	for i := int64(1); i <= 5; i++ {
		f.desk.articles = append(f.desk.articles, xueqiuArticle(i, 100+i, "博主", fmt.Sprintf("帖子 %d 看好 $贵州茅台(SH600519)$", i), at))
	}
	resp, err := f.service.SyncXueqiu(context.Background(), WireDeskIngestionRequest{MaxArticles: 3, SkipAck: true})
	require.NoError(t, err)
	require.Equal(t, 3, resp.FetchedCount)
	require.True(t, resp.Truncated)
	require.Zero(t, resp.AckedCount)
	require.Empty(t, f.desk.acked, "skip_ack leaves the queue untouched")

	// A full run sees the first three again but does not re-analyze them.
	analyzedBefore := len(f.pipeline.analyzed)
	resp, err = f.service.SyncXueqiu(context.Background(), WireDeskIngestionRequest{})
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
	_, err := f.service.SyncXueqiu(context.Background(), WireDeskIngestionRequest{})
	require.ErrorContains(t, err, "read Xueqiu pending posts")

	f = newXueqiuFixture(t)
	f.desk.articles = []wiredesk.Article{xueqiuArticle(1, 101, "博主", "看好 $贵州茅台(SH600519)$", time.Now())}
	f.desk.ackErr = errors.New("desk down")
	resp, err := f.service.SyncXueqiu(context.Background(), WireDeskIngestionRequest{})
	require.ErrorContains(t, err, "ACK Xueqiu posts")
	require.Equal(t, 1, resp.SucceededCount, "the post is stored even though the ACK failed; the journal skips it next time")

	f.cfg.ExternalDocuments.Xueqiu.Enabled = false
	_, err = f.service.SyncXueqiu(context.Background(), WireDeskIngestionRequest{})
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

	manual, err := json.Marshal(WireDeskIngestionRequest{Manual: true, MaxArticles: 3})
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

func thsArticle(id, statusID int64, author string, text string, published time.Time) wiredesk.Article {
	at := published
	return wiredesk.Article{
		ID: id, StatusID: statusID, Title: strings.Split(text, "\n")[0],
		Author: wiredesk.Author{ID: 429085934, Name: author}, Text: text,
		URL:         fmt.Sprintf("https://t.10jqka.com.cn/lgt/short_view/index.html?contentId=c%d", statusID),
		PublishedAt: &at, SyncedAt: published.Add(10 * time.Minute), Origin: "new",
	}
}

func TestSyncTHSStoresWithTonghuashunProvenanceAndAcksAll(t *testing.T) {
	f := newXueqiuFixture(t)
	morning := time.Date(2026, time.November, 20, 0, 45, 1, 0, time.UTC) // 08:45 Beijing time
	f.desk.articles = []wiredesk.Article{
		thsArticle(1, 728850093, "首创证券伍彩云", "【盘前提示1120】\n关注半导体设备，北方华创(002371)逢低布局", morning),
		thsArticle(2, 728850094, "首创证券伍彩云", "", morning), // image-only text-live message
		thsArticle(3, 728850095, "首创证券伍彩云", "大盘震荡，控制仓位", morning),
	}
	f.desk.articles[1].Images = []wiredesk.Image{{URL: "https://u.thsi.cn/a.png"}}
	f.pipeline.invalidDocs[2] = true // no tradable intent: still a successful analysis

	resp, err := f.service.SyncTHS(context.Background(), WireDeskIngestionRequest{})
	require.NoError(t, err)
	require.Equal(t, []string{"THS"}, f.deskSources, "the THS run builds the THS desk client")
	require.Equal(t, 3, resp.FetchedCount)
	require.Equal(t, 2, resp.SucceededCount)
	require.Equal(t, 1, resp.SkippedCount)
	require.Equal(t, 3, resp.AckedCount, "every stored post, including the skipped image-only one, is ACKed")
	require.Equal(t, map[int64]bool{1: true, 2: true, 3: true}, f.desk.acked)
	require.Equal(t, []time.Duration{3 * time.Second}, f.sleeps, "THS analyses are spaced by its own analysis_interval_ms")

	first := f.pipeline.requests[0]
	require.Equal(t, "首创证券伍彩云", first.Author)
	require.Equal(t, "同花顺", first.Institution)
	require.Equal(t, "ths-429085934-728850093.txt", first.FileName)
	require.Equal(t, "【盘前提示1120】", first.Title)
	body := string(first.Content)
	require.True(t, strings.HasPrefix(body, "作者：首创证券伍彩云（同花顺用户 429085934）\n"), "a title that only repeats the first line is not duplicated:\n%s", body)
	require.Contains(t, body, "发布时间：2026-11-20 08:45 CST")
	require.Contains(t, body, "原文链接：https://t.10jqka.com.cn/lgt/short_view/index.html?contentId=c728850093")
	require.NotContains(t, body, "标注的标的")
	require.True(t, strings.HasSuffix(body, "关注半导体设备，北方华创(002371)逢低布局\n"))
	for _, d := range f.pipeline.tradeDates {
		require.Equal(t, "2026-11-20", d.Format(time.DateOnly))
	}

	rows := f.ingestionRows(t)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Equal(t, int32(domain.ExternalDocumentSourceTypeTHS), row.SourceType)
		require.Equal(t, "https://t.10jqka.com.cn/pid_"+row.SourceVersion+".shtml", row.SourcePath)
		require.Equal(t, domain.ExternalDocumentIngestionStatusSucceeded, domain.ExternalDocumentIngestionStatus(row.Status))
	}
}

func TestSyncTHSAndXueqiuJournalsDoNotCollide(t *testing.T) {
	f := newXueqiuFixture(t)
	at := time.Date(2026, time.November, 20, 1, 0, 0, 0, time.UTC)
	f.desk.articles = []wiredesk.Article{xueqiuArticle(1, 777, "雪球博主", "看好 $贵州茅台(SH600519)$", at)}
	_, err := f.service.SyncXueqiu(context.Background(), WireDeskIngestionRequest{})
	require.NoError(t, err)

	// Same desk-local id and status id on the other platform is a new post.
	f.desk.acked = map[int64]bool{}
	f.desk.articles = []wiredesk.Article{thsArticle(1, 777, "同花顺投顾", "关注贵州茅台(600519)回调机会", at)}
	resp, err := f.service.SyncTHS(context.Background(), WireDeskIngestionRequest{})
	require.NoError(t, err)
	require.Equal(t, 1, resp.SucceededCount)
	require.Zero(t, resp.SkippedCount)
	require.Equal(t, []string{"Xueqiu", "THS"}, f.deskSources)
	rows := f.ingestionRows(t)
	require.Len(t, rows, 2)
	require.Equal(t, int32(domain.ExternalDocumentSourceTypeXueqiu), rows[0].SourceType)
	require.Equal(t, int32(domain.ExternalDocumentSourceTypeTHS), rows[1].SourceType)
}

func TestSyncTHSErrorsNameTheSource(t *testing.T) {
	f := newXueqiuFixture(t)
	f.desk.pageErr = errors.New("connection refused")
	_, err := f.service.SyncTHS(context.Background(), WireDeskIngestionRequest{})
	require.ErrorContains(t, err, "read THS pending posts")

	f = newXueqiuFixture(t)
	f.cfg.ExternalDocuments.THS.Enabled = false
	_, err = f.service.SyncTHS(context.Background(), WireDeskIngestionRequest{})
	require.ErrorContains(t, err, "THS document ingestion is disabled")
	_, err = f.service.SyncXueqiu(context.Background(), WireDeskIngestionRequest{})
	require.NoError(t, err, "disabling THS leaves Xueqiu alone")
}

func TestTHSScheduledRunSkipReasons(t *testing.T) {
	cfg := &config.Config{}
	cfg.Meta.Timezone = "Asia/Shanghai"
	cfg.Scheduler.Enabled = true
	cfg.Scheduler.THSDocumentIngestion = config.DailyTaskScheduleConfig{Enabled: true, Hour: 6}
	cfg.ExternalDocuments.THS.Enabled = true
	at := time.Date(2026, time.November, 21, 6, 0, 0, 0, config.ChinaLocation)
	scheduled := func(attempt int32) *db_model.ScheduledTaskRun {
		return &db_model.ScheduledTaskRun{TaskKey: "2026-11-21", ScheduledAt: at.UTC(), AttemptCount: attempt, InputJSON: []byte(`{}`)}
	}
	reason, err := thsScheduledRunSkipReason(scheduled(1), cfg, at.Add(30*time.Second))
	require.NoError(t, err)
	require.Empty(t, reason)
	reason, _ = thsScheduledRunSkipReason(scheduled(1), cfg, at.Add(90*time.Minute))
	require.Equal(t, "MISSED_THS_START_WINDOW", reason)
	reason, _ = thsScheduledRunSkipReason(scheduled(2), cfg, at)
	require.Equal(t, "THS_ATTEMPT_ALREADY_STARTED", reason)
	manual, err := json.Marshal(WireDeskIngestionRequest{Manual: true})
	require.NoError(t, err)
	reason, _ = thsScheduledRunSkipReason(&db_model.ScheduledTaskRun{TaskKey: "manual-x", ScheduledAt: at, AttemptCount: 1, InputJSON: manual}, cfg, at.Add(5*time.Hour))
	require.Empty(t, reason)
	cfg.Scheduler.THSDocumentIngestion.Enabled = false
	reason, _ = thsScheduledRunSkipReason(scheduled(1), cfg, at)
	require.Equal(t, "THS_SCHEDULE_DISABLED", reason)
	cfg.ExternalDocuments.THS.Enabled = false
	reason, _ = thsScheduledRunSkipReason(scheduled(1), cfg, at)
	require.Equal(t, "THS_SOURCE_DISABLED", reason)
	// The Xueqiu gate reads only its own source.
	cfg.ExternalDocuments.Xueqiu.Enabled = true
	cfg.Scheduler.XueqiuDocumentIngestion = config.DailyTaskScheduleConfig{Enabled: true, Hour: 4}
	reason, _ = xueqiuScheduledRunSkipReason(&db_model.ScheduledTaskRun{TaskKey: "2026-11-21", ScheduledAt: at.Add(-2 * time.Hour).UTC(), AttemptCount: 1}, cfg, at.Add(-2*time.Hour))
	require.Empty(t, reason)
}

func TestTHSDailyEnqueueAtSixAndManualRuns(t *testing.T) {
	scheduler, tasks, cfg, _ := openListScheduleFixture(t)
	cfg.Scheduler.OpenListDocumentIngestion.Enabled = false
	cfg.Scheduler.THSDocumentIngestion = config.DailyTaskScheduleConfig{Enabled: true, Hour: 6}
	cfg.ExternalDocuments.THS.Enabled = true
	at := time.Date(2026, time.November, 21, 6, 0, 0, 0, config.ChinaLocation)
	ctx := context.Background()
	for _, offset := range []time.Duration{-time.Second, time.Minute, 2 * time.Hour} {
		require.NoError(t, scheduler.enqueueDueTasks(ctx, cfg, at.Add(offset)))
	}
	var count int64
	require.NoError(t, tasks.db.Model(&db_model.ScheduledTaskRun{}).Count(&count).Error)
	require.Zero(t, count)
	for _, offset := range []time.Duration{0, 59 * time.Second} {
		require.NoError(t, scheduler.enqueueDueTasks(ctx, cfg, at.Add(offset)))
	}
	var runs []db_model.ScheduledTaskRun
	require.NoError(t, tasks.db.Find(&runs).Error)
	require.Len(t, runs, 1)
	require.Equal(t, uint16(domain.ScheduledTaskTypeTHSDocuments), runs[0].TaskType)
	require.Equal(t, "2026-11-21", runs[0].TaskKey)
	require.True(t, runs[0].ScheduledAt.Equal(at))

	scheduler.runtime = config.NewRuntime(&config.Snapshot{Config: cfg})
	scheduler.now = func() time.Time { return at.Add(3 * time.Hour) }
	run, err := scheduler.EnqueueManualTHSRun(ctx, WireDeskIngestionRequest{MaxArticles: 5, SkipAck: true})
	require.NoError(t, err)
	require.Equal(t, uint16(domain.ScheduledTaskTypeTHSDocuments), run.TaskType)
	require.True(t, strings.HasPrefix(run.TaskKey, "manual-"))
	var input WireDeskIngestionRequest
	require.NoError(t, json.Unmarshal(run.InputJSON, &input))
	require.Equal(t, WireDeskIngestionRequest{Manual: true, MaxArticles: 5, SkipAck: true}, input)
	_, err = scheduler.EnqueueManualTHSRun(ctx, WireDeskIngestionRequest{MaxArticles: -1})
	require.ErrorContains(t, err, "max_articles")
	cfg.ExternalDocuments.THS.Enabled = false
	_, err = scheduler.EnqueueManualTHSRun(ctx, WireDeskIngestionRequest{})
	require.ErrorContains(t, err, "THS document ingestion is disabled")
}
