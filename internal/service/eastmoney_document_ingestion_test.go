//go:build cgo

package service

import (
	"context"
	"encoding/json"
	"errors"
	"finance-sys/internal/config"
	"finance-sys/internal/domain"
	"finance-sys/internal/domain/db_model"
	"finance-sys/internal/wiredesk"
	"fmt"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func enableEastmoney(f *xueqiuFixture) {
	f.cfg.ExternalDocuments.Eastmoney = config.WireDeskDocumentSourceConfig{
		Enabled: true, BaseURL: "http://eastmoney-desk", Institution: "东方财富", RequestTimeoutMS: 1000,
		PageSize: 2, MaxArticlesPerRun: 0, AnalysisConcurrency: 1, AnalysisIntervalMS: 5000, AckBy: "financesys",
	}
}
func eastmoneyArticle(id int64, text string, at time.Time) wiredesk.Article {
	return wiredesk.Article{ID: id, StatusID: 1798706640 + id, Title: "关注中际旭创", Text: text,
		Author: wiredesk.Author{ID: 6880045160009350, Name: "东方财富作者"}, URL: fmt.Sprintf("https://caifuhao.eastmoney.com/news/20261119000000000000%d", id),
		PublishedAt: &at, SyncedAt: at.Add(time.Minute), Symbols: []string{"SZ300308"}, Origin: "new"}
}
func TestSyncEastmoneyDrainsAllPagesSlowlyAndAcksStoredPosts(t *testing.T) {
	f := newXueqiuFixture(t)
	enableEastmoney(f)
	at := time.Date(2026, 11, 21, 1, 2, 0, 0, time.UTC)
	f.desk.articles = []wiredesk.Article{
		eastmoneyArticle(1, "看好中际旭创，回调布局", at), eastmoneyArticle(2, "", at),
		eastmoneyArticle(3, "关注业绩高增长的新易盛", at), eastmoneyArticle(4, "没有可交易意图", at), eastmoneyArticle(5, "北方华创逢低布局", at),
	}
	f.desk.articles[1].Images = []wiredesk.Image{{URL: "https://gbres.dfcfw.com/a.jpg"}}
	f.pipeline.analyzeErr[2] = errors.New("model temporary error")
	f.pipeline.invalidDocs[3] = true
	response, err := f.service.SyncEastmoney(context.Background(), WireDeskIngestionRequest{})
	require.NoError(t, err)
	require.Equal(t, []string{"Eastmoney"}, f.deskSources)
	require.Equal(t, 5, response.FetchedCount)
	require.Equal(t, 3, response.SucceededCount)
	require.Equal(t, 1, response.SkippedCount)
	require.Equal(t, 1, response.FailedCount)
	require.Equal(t, 5, response.AckedCount)
	require.False(t, response.Truncated, "zero limit drains all pages")
	require.Equal(t, []time.Duration{5 * time.Second, 5 * time.Second, 5 * time.Second}, f.sleeps)
	require.Equal(t, [][]int64{{1, 2}, {3, 4}, {5}}, f.desk.ackCalls)
	first := f.pipeline.requests[0]
	require.Equal(t, "东方财富", first.Institution)
	require.Equal(t, "东方财富作者", first.Author)
	require.Equal(t, "eastmoney-6880045160009350-1798706641.txt", first.FileName)
	require.Contains(t, string(first.Content), "东方财富用户 6880045160009350")
	require.Contains(t, string(first.Content), "发布时间：2026-11-21 09:02 CST")
	require.Contains(t, string(first.Content), "东方财富标注的标的：SZ300308")
	for _, row := range f.ingestionRows(t) {
		require.Equal(t, int32(domain.ExternalDocumentSourceTypeEastmoney), row.SourceType)
		require.Equal(t, "https://guba.eastmoney.com/news,cfhpl,"+row.SourceVersion+".html", row.SourcePath)
	}
}
func TestEastmoneyManualLimitCanLowerUnlimitedSourceAndSkipAck(t *testing.T) {
	f := newXueqiuFixture(t)
	enableEastmoney(f)
	for id := int64(1); id <= 5; id++ {
		f.desk.articles = append(f.desk.articles, eastmoneyArticle(id, "看好中际旭创", f.clock))
	}
	response, err := f.service.SyncEastmoney(context.Background(), WireDeskIngestionRequest{MaxArticles: 3, SkipAck: true})
	require.NoError(t, err)
	require.Equal(t, 3, response.FetchedCount)
	require.True(t, response.Truncated)
	require.Empty(t, f.desk.ackCalls)
}
func TestEastmoneyNotStoredPostsStayPending(t *testing.T) {
	f := newXueqiuFixture(t)
	enableEastmoney(f)
	f.desk.articles = []wiredesk.Article{eastmoneyArticle(1, "看好中际旭创", f.clock)}
	f.pipeline.ingestErr["eastmoney-6880045160009350-1798706641.txt"] = errors.New("storage unavailable")
	response, err := f.service.SyncEastmoney(context.Background(), WireDeskIngestionRequest{})
	require.ErrorContains(t, err, "could not be stored")
	require.Equal(t, 1, response.NotStoredCount)
	require.Zero(t, response.AckedCount)
	require.Empty(t, f.desk.ackCalls)
}
func TestEastmoneyDailyEnqueueAtSevenAndManualRun(t *testing.T) {
	s, tasks, cfg, _ := openListScheduleFixture(t)
	cfg.Scheduler.OpenListDocumentIngestion.Enabled = false
	cfg.Scheduler.EastmoneyDocumentIngestion = config.DailyTaskScheduleConfig{Enabled: true, Hour: 7}
	cfg.ExternalDocuments.Eastmoney.Enabled = true
	at := time.Date(2026, 11, 21, 7, 0, 0, 0, config.ChinaLocation)
	for _, offset := range []time.Duration{-time.Second, 0, 59 * time.Second, time.Minute} {
		require.NoError(t, s.enqueueDueTasks(context.Background(), cfg, at.Add(offset)))
	}
	var runs []db_model.ScheduledTaskRun
	require.NoError(t, tasks.db.Find(&runs).Error)
	require.Len(t, runs, 1)
	require.Equal(t, uint16(domain.ScheduledTaskTypeEastmoneyDocuments), runs[0].TaskType)
	require.True(t, runs[0].ScheduledAt.Equal(at))
	runs[0].AttemptCount = 1
	reason, err := eastmoneyScheduledRunSkipReason(&runs[0], cfg, at.Add(30*time.Second))
	require.NoError(t, err)
	require.Empty(t, reason)
	reason, err = eastmoneyScheduledRunSkipReason(&runs[0], cfg, at.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, "MISSED_EASTMONEY_START_WINDOW", reason)
	s.runtime = config.NewRuntime(&config.Snapshot{Config: cfg})
	s.now = func() time.Time { return at.Add(time.Hour) }
	run, err := s.EnqueueManualEastmoneyRun(context.Background(), WireDeskIngestionRequest{MaxArticles: 2, SkipAck: true})
	require.NoError(t, err)
	require.Equal(t, uint16(domain.ScheduledTaskTypeEastmoneyDocuments), run.TaskType)
	require.True(t, strings.HasPrefix(run.TaskKey, "manual-"))
	var request WireDeskIngestionRequest
	require.NoError(t, json.Unmarshal(run.InputJSON, &request))
	require.True(t, request.Manual)
	run.AttemptCount = 1
	reason, err = eastmoneyScheduledRunSkipReason(run, cfg, at.Add(time.Hour))
	require.NoError(t, err)
	require.Empty(t, reason)
	cfg.ExternalDocuments.Eastmoney.Enabled = false
	_, err = s.EnqueueManualEastmoneyRun(context.Background(), WireDeskIngestionRequest{})
	require.ErrorContains(t, err, "disabled")
}
