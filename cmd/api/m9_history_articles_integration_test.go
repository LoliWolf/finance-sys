package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"finance-sys/internal/agentclient"
	"finance-sys/internal/bootstrap"
	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain"

	"github.com/stretchr/testify/require"
)

const (
	m9RealHistoryIntegrationEnv     = "FINANCE_SYS_M9_REAL_HISTORY_ARTICLE_INTEGRATION"
	m9RealHistoryDMLAckEnv          = "FINANCE_SYS_M9_REAL_HISTORY_DML_ACK"
	m9RealHistoryConcurrencyEnv     = "FINANCE_SYS_M9_REAL_HISTORY_MAX_CONCURRENCY"
	m9RealHistoryAnalyzeAttemptsEnv = "FINANCE_SYS_M9_REAL_HISTORY_ANALYZE_MAX_ATTEMPTS"
	m9RealHistoryInstitution        = "\u4e2a\u4eba"
	m9RealHistoryTitlePrefix        = "M9_REAL_HISTORY"
	m9FakeOCRSentinel               = "M9_HISTORY_OCR_TEXT"
	m9RealHistoryDefaultConcurrency = 1
	m9RealHistoryMaxConcurrency     = 200
	m9RealHistoryAgentTimeoutMS     = 600000
)

var m9HistoryDatePattern = regexp.MustCompile(`(\d{8})$`)

type m9HistoryArticle struct {
	Path     string
	Date     time.Time
	DateText string
	SHA256   string
}

type m9HistoryArticleResult struct {
	DocumentID        int64
	Duplicate         bool
	Analyzed          bool
	Invalid           bool
	PlanCount         int
	EventCount        int
	ArticleDate       string
	ExpectedTradeDate string
	Path              string
	SHA256            string
}

type m9DeferredAnalyzeError struct {
	DocumentID int64
	Status     int
	Body       string
	Err        error
}

func (err *m9DeferredAnalyzeError) Error() string {
	detail := strings.TrimSpace(err.Body)
	if detail == "" && err.Err != nil {
		detail = err.Err.Error()
	}
	if detail == "" {
		detail = "transient analyze failure"
	}
	if err.Status != 0 {
		return fmt.Sprintf("deferred analyze retry for document %d after http %d: %s", err.DocumentID, err.Status, detail)
	}
	return fmt.Sprintf("deferred analyze retry for document %d: %s", err.DocumentID, detail)
}

func (err *m9DeferredAnalyzeError) Unwrap() error {
	return err.Err
}

type m9UploadDocumentResponse struct {
	Duplicate bool                   `json:"duplicate"`
	Document  domain.Document        `json:"document"`
	Plans     []domain.CandidatePlan `json:"plans"`
}

type m9AnalyzeOnceTracker struct {
	mu        sync.Mutex
	attempted map[int64]struct{}
	inFlight  map[int64]chan struct{}
}

func newM9AnalyzeOnceTracker() *m9AnalyzeOnceTracker {
	return &m9AnalyzeOnceTracker{
		attempted: make(map[int64]struct{}),
		inFlight:  make(map[int64]chan struct{}),
	}
}

func (tracker *m9AnalyzeOnceTracker) run(ctx context.Context, documentID int64, analyze func() error) (bool, error) {
	for {
		tracker.mu.Lock()
		if _, ok := tracker.attempted[documentID]; ok {
			if inFlight, running := tracker.inFlight[documentID]; running {
				tracker.mu.Unlock()
				select {
				case <-inFlight:
					continue
				case <-ctx.Done():
					return false, ctx.Err()
				}
			}
			tracker.mu.Unlock()
			return false, nil
		}
		inFlight := make(chan struct{})
		tracker.attempted[documentID] = struct{}{}
		tracker.inFlight[documentID] = inFlight
		tracker.mu.Unlock()

		err := analyze()

		tracker.mu.Lock()
		if err != nil {
			delete(tracker.attempted, documentID)
		}
		delete(tracker.inFlight, documentID)
		close(inFlight)
		tracker.mu.Unlock()
		return true, err
	}
}

func TestHTTPM9RealHistoryArticlesIncrementallyUploadAndAnalyzeWithNacosAgent(t *testing.T) {
	m9RequireRealHistoryEnabled(t)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Hour)
	defer cancel()

	loadBootstrapEnvFile(t)
	app := buildIntegrationApp(t, ctx)
	defer app.Close()

	baseURL, shutdown := startMainHTTPServerForTest(t, app)
	defer shutdown()

	allArticles := m9HistoryArticles(t, m8ArticleRoot())
	uniqueArticles, duplicateFileCount := m9UniqueHistoryArticlesBySHA(t, allArticles)
	require.Greater(t, len(uniqueArticles), 1000, "expected the historical article corpus, not a small fixture")
	concurrency := m9RealHistoryConcurrency(t)
	t.Logf("M9 real history corpus: files=%d unique_sha=%d duplicate_files=%d concurrency=%d", len(allArticles), len(uniqueArticles), duplicateFileCount, concurrency)

	resultsBySHA := make(map[string]m9HistoryArticleResult, len(uniqueArticles))
	var failures []string
	analyzeTracker := newM9AnalyzeOnceTracker()
	m9ConfigureRealHistoryRuntime(t, app, time.Time{})

	pending := uniqueArticles
	for pass := 1; len(pending) > 0; pass++ {
		groupResults, nextPending, groupFailures := m9RunHistoryArticleGroup(ctx, app, baseURL, analyzeTracker, pending, concurrency)
		for _, result := range groupResults {
			resultsBySHA[result.SHA256] = result
		}

		t.Logf("M9 real history pass=%d completed=%d deferred_for_retry=%d total_completed=%d", pass, len(groupResults), len(nextPending), len(resultsBySHA))
		if len(groupFailures) > 0 {
			failures = append(failures, groupFailures...)
			break
		}
		if len(nextPending) == 0 {
			break
		}
		pending = nextPending
		select {
		case <-time.After(m9RetrySweepBackoff(pass)):
		case <-ctx.Done():
			failures = append(failures, ctx.Err().Error())
			pending = nil
		}
	}

	if len(failures) > 0 {
		t.Fatalf("real history article run failed for %d unique article(s); first failures:\n%s", len(failures), strings.Join(m9FirstStrings(failures, 20), "\n"))
	}

	results := make([]m9HistoryArticleResult, 0, len(uniqueArticles))
	var missing []string
	for _, article := range uniqueArticles {
		result, ok := resultsBySHA[article.SHA256]
		if !ok {
			missing = append(missing, article.Path)
			continue
		}
		results = append(results, result)
	}
	if len(missing) > 0 {
		t.Fatalf("real history article run ended with %d pending article(s); first pending:\n%s", len(missing), strings.Join(m9FirstStrings(missing, 20), "\n"))
	}
	require.Len(t, results, len(uniqueArticles))

	createdOrAnalyzed := 0
	for _, result := range results {
		if result.Analyzed {
			createdOrAnalyzed++
		}
	}
	t.Logf("M9 real history completed: unique_sha=%d analyzed_or_repaired=%d duplicate_planned_skipped=%d", len(results), createdOrAnalyzed, len(results)-createdOrAnalyzed)

	m9VerifyRealHistoryResultSet(t, app, results)
}

func TestM9AnalyzeOnceTrackerRunsOnlyOneAnalyzeForTheSameDocumentID(t *testing.T) {
	tracker := newM9AnalyzeOnceTracker()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var calls int
	var callsMu sync.Mutex
	start := make(chan struct{})
	var wg sync.WaitGroup
	analyzed := make(chan bool, 8)
	errCh := make(chan error, 8)

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			didAnalyze, err := tracker.run(ctx, 42, func() error {
				callsMu.Lock()
				calls++
				callsMu.Unlock()
				time.Sleep(50 * time.Millisecond)
				return nil
			})
			analyzed <- didAnalyze
			errCh <- err
		}()
	}

	close(start)
	wg.Wait()
	close(analyzed)
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}
	var analyzeCount int
	for didAnalyze := range analyzed {
		if didAnalyze {
			analyzeCount++
		}
	}
	require.Equal(t, 1, analyzeCount)
	require.Equal(t, 1, calls)

	didAnalyze, err := tracker.run(ctx, 42, func() error {
		callsMu.Lock()
		calls++
		callsMu.Unlock()
		return nil
	})
	require.NoError(t, err)
	require.False(t, didAnalyze)
	require.Equal(t, 1, calls)
}

func TestM9AnalyzeOnceTrackerAllowsRetryAfterAnalyzeError(t *testing.T) {
	tracker := newM9AnalyzeOnceTracker()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var calls int
	didAnalyze, err := tracker.run(ctx, 42, func() error {
		calls++
		return errors.New("transient analyze error")
	})
	require.Error(t, err)
	require.True(t, didAnalyze)

	didAnalyze, err = tracker.run(ctx, 42, func() error {
		calls++
		return nil
	})
	require.NoError(t, err)
	require.True(t, didAnalyze)
	require.Equal(t, 2, calls)
}

func TestM9UniqueHistoryArticlesBySHAUsesOnlyFileBytes(t *testing.T) {
	root := t.TempDir()
	firstDir := filepath.Join(root, "articles-20260202")
	secondDir := filepath.Join(root, "articles-20260203")
	require.NoError(t, os.MkdirAll(firstDir, 0o755))
	require.NoError(t, os.MkdirAll(secondDir, 0o755))
	content := []byte("same pdf bytes")
	firstPath := filepath.Join(firstDir, "alpha.pdf")
	secondPath := filepath.Join(secondDir, "beta-renamed.pdf")
	require.NoError(t, os.WriteFile(firstPath, content, 0o644))
	require.NoError(t, os.WriteFile(secondPath, content, 0o644))

	articles := m9HistoryArticles(t, root)
	unique, duplicateCount := m9UniqueHistoryArticlesBySHA(t, articles)

	require.Len(t, articles, 2)
	require.Len(t, unique, 1)
	require.Equal(t, 1, duplicateCount)
	require.Equal(t, sha256.Sum256(content), m9MustDecodeSHA256(t, unique[0].SHA256))
}

func TestM9DocumentStatusNeedsAnalyzeSkipsTerminalInvalidDuplicates(t *testing.T) {
	require.False(t, m9DocumentStatusNeedsAnalyze(true, domain.DocumentStatusPlanned))
	require.False(t, m9DocumentStatusNeedsAnalyze(true, domain.DocumentStatusInvalid))
	require.True(t, m9DocumentStatusNeedsAnalyze(true, domain.DocumentStatusFailed))
	require.True(t, m9DocumentStatusNeedsAnalyze(false, domain.DocumentStatusInvalid))
}

func TestM9DocumentStatusNeedsAnalyzeReprocessesMismatchedPlanDates(t *testing.T) {
	expected := time.Date(2026, 2, 12, 0, 0, 0, 0, time.UTC)

	require.False(t, m9DocumentStatusNeedsAnalyzeForPlanDates(
		true,
		domain.DocumentStatusPlanned,
		[]time.Time{expected},
		expected,
	))
	require.True(t, m9DocumentStatusNeedsAnalyzeForPlanDates(
		true,
		domain.DocumentStatusPlanned,
		[]time.Time{expected.AddDate(0, 0, 1)},
		expected,
	))
	require.True(t, m9DocumentStatusNeedsAnalyzeForPlanDates(
		true,
		domain.DocumentStatusPlanned,
		nil,
		expected,
	))
	require.False(t, m9DocumentStatusNeedsAnalyzeForPlanDates(
		true,
		domain.DocumentStatusInvalid,
		[]time.Time{expected.AddDate(0, 0, 1)},
		expected,
	))
}

func TestM9AnalyzeNonOKAcceptsTerminalDocumentStatuses(t *testing.T) {
	require.True(t, m9AnalyzeNonOKAcceptsDocumentStatus(domain.DocumentStatusPlanned))
	require.True(t, m9AnalyzeNonOKAcceptsDocumentStatus(domain.DocumentStatusInvalid))
	require.False(t, m9AnalyzeNonOKAcceptsDocumentStatus(domain.DocumentStatusFailed))
	require.False(t, m9AnalyzeNonOKAcceptsDocumentStatus(domain.DocumentStatusParsed))
}

func TestM9TransientAnalyzeFailureIncludesOCRResourceFailures(t *testing.T) {
	body := []byte(`{"error":"ocr failed for article.pdf: Windows OCR failed: OutOfMemoryException"}`)

	require.True(t, m9IsTransientAnalyzeFailure(http.StatusInternalServerError, body))
}

func TestM9TransientAnalyzeFailureExcludesTerminalInvalidPDFParseFailures(t *testing.T) {
	body := []byte(`{"error":"pdftotext failed for article.pdf: exit status 99; ocr failed: RuntimeError: pdftoppm failed\nSyntax Error: Invalid page count 0\nWrong page range given: the first page (1) can not be after the last page (0)."}`)

	require.False(t, m9IsTransientAnalyzeFailure(http.StatusInternalServerError, body))
}

func TestM9RealHistoryAgentConfigAvoidsTimeoutRetryOverlap(t *testing.T) {
	cfg := config.AgentConfig{
		TimeoutMS:  120000,
		MaxRetries: 2,
	}

	tuned := m9RealHistoryAgentConfig(cfg)

	require.Equal(t, m9RealHistoryAgentTimeoutMS, tuned.TimeoutMS)
	require.Zero(t, tuned.MaxRetries)
}

func TestM9RealHistoryInstitutionIsPersonal(t *testing.T) {
	require.Equal(t, "\u4e2a\u4eba", m9RealHistoryInstitution)
}

func TestM9AnalyzeDocumentBodyWithRetryStopsAfterDocumentBecomesTerminal(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		http.Error(w, "deadlock found", http.StatusInternalServerError)
	}))
	defer server.Close()

	var statusChecks int
	status, body, err := m9AnalyzeDocumentBodyWithRetryE(context.Background(), server.URL, 42, time.Time{}, func() (bool, error) {
		statusChecks++
		return true, nil
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusInternalServerError, status)
	require.Contains(t, string(body), "deadlock found")
	require.Equal(t, int32(1), attempts.Load())
	require.Equal(t, 1, statusChecks)
}

func TestM9AnalyzeDocumentBodyWithRetryRetriesAgentFailedStatusForSameDocument(t *testing.T) {
	t.Setenv(m9RealHistoryAnalyzeAttemptsEnv, "3")

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attempts.Add(1)
		if attempt < 3 {
			http.Error(w, `{"error":"agent returned FAILED status"}`, http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"plans":[]}`))
	}))
	defer server.Close()

	var statusChecks int
	status, body, err := m9AnalyzeDocumentBodyWithRetryE(context.Background(), server.URL, 42, time.Time{}, func() (bool, error) {
		statusChecks++
		return false, nil
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.JSONEq(t, `{"plans":[]}`, string(body))
	require.Equal(t, int32(3), attempts.Load())
	require.Equal(t, 2, statusChecks)
}

func TestM9AnalyzeDocumentBodyWithRetryRetriesClientEOFForSameDocument(t *testing.T) {
	t.Setenv(m9RealHistoryAnalyzeAttemptsEnv, "2")

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attempts.Add(1)
		if attempt == 1 {
			hijacker, ok := w.(http.Hijacker)
			require.True(t, ok)
			conn, _, err := hijacker.Hijack()
			require.NoError(t, err)
			require.NoError(t, conn.Close())
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"plans":[]}`))
	}))
	defer server.Close()

	var statusChecks int
	status, body, err := m9AnalyzeDocumentBodyWithRetryE(context.Background(), server.URL, 42, time.Time{}, func() (bool, error) {
		statusChecks++
		return false, nil
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.JSONEq(t, `{"plans":[]}`, string(body))
	require.Equal(t, int32(2), attempts.Load())
	require.Equal(t, 1, statusChecks)
}

func TestM9AnalyzeDocumentBodyWithRetryDoesNotRetryContextDeadline(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		select {
		case <-time.After(200 * time.Millisecond):
		case <-r.Context().Done():
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	status, body, err := m9AnalyzeDocumentBodyWithRetryE(ctx, server.URL, 42, time.Time{}, nil)

	require.Error(t, err)
	require.Zero(t, status)
	require.Empty(t, body)
	require.Equal(t, int32(1), attempts.Load())
}

func TestM9AnalyzeDocumentBodyWithRetryAcceptsTerminalDocumentAfterClientError(t *testing.T) {
	var statusChecks int

	status, body, err := m9AnalyzeDocumentBodyWithRetryE(context.Background(), "http://127.0.0.1:1", 42, time.Time{}, func() (bool, error) {
		statusChecks++
		return true, nil
	})

	require.NoError(t, err)
	require.Zero(t, status)
	require.Empty(t, body)
	require.Equal(t, 1, statusChecks)
}

func TestM9AnalyzeDocumentBodySendsTradeDateQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/documents/42/analyze", r.URL.Path)
		require.Equal(t, "2026-04-13", r.URL.Query().Get("trade_date"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"plans":[]}`))
	}))
	defer server.Close()

	status, body, err := m9AnalyzeDocumentBodyE(context.Background(), server.URL, 42, time.Date(2026, 4, 13, 18, 30, 0, 0, time.Local))

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.JSONEq(t, `{"plans":[]}`, string(body))
}

func m9RequireRealHistoryEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv(m9RealHistoryIntegrationEnv) != "1" {
		t.Skipf("set %s=1 to run the real OCR + real Agent historical article integration test", m9RealHistoryIntegrationEnv)
	}
	if os.Getenv(m9RealHistoryDMLAckEnv) != "write-real-db" && os.Getenv("FINANCE_SYS_M9_NACOS_DML_ACK") != "write-real-db" {
		t.Skipf("set %s=write-real-db after acknowledging this test writes to the configured MySQL database", m9RealHistoryDMLAckEnv)
	}
	if runtime.GOOS != "windows" {
		t.Skip("the configured historical OCR wrapper is a Windows .bat tool")
	}
	require.DirExists(t, m8ArticleRoot())
}

func m9RealHistoryConcurrency(t *testing.T) int {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv(m9RealHistoryConcurrencyEnv))
	if raw == "" {
		return m9RealHistoryDefaultConcurrency
	}
	value, err := strconv.Atoi(raw)
	require.NoError(t, err, "%s must be an integer", m9RealHistoryConcurrencyEnv)
	require.GreaterOrEqual(t, value, 1, "%s must be >= 1", m9RealHistoryConcurrencyEnv)
	require.LessOrEqual(t, value, m9RealHistoryMaxConcurrency, "%s must be <= %d", m9RealHistoryConcurrencyEnv, m9RealHistoryMaxConcurrency)
	return value
}

func m9MustDecodeSHA256(t *testing.T, value string) [32]byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	require.NoError(t, err)
	require.Len(t, decoded, sha256.Size)
	var sum [32]byte
	copy(sum[:], decoded)
	return sum
}

func m9HistoryArticles(t *testing.T, root string) []m9HistoryArticle {
	t.Helper()
	var articles []m9HistoryArticle
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".pdf") {
			return nil
		}
		dateText, err := m9HistoryDateTextFromPath(path)
		if err != nil {
			return err
		}
		articleDate, err := time.Parse("20060102", dateText)
		if err != nil {
			return fmt.Errorf("article %s has invalid YYYYMMDD directory suffix %q: %w", path, dateText, err)
		}
		articles = append(articles, m9HistoryArticle{
			Path:     path,
			Date:     articleDate,
			DateText: dateText,
		})
		return nil
	})
	require.NoError(t, err)
	sort.Slice(articles, func(i, j int) bool {
		if articles[i].DateText == articles[j].DateText {
			return articles[i].Path < articles[j].Path
		}
		return articles[i].DateText < articles[j].DateText
	})
	return articles
}

func m9UniqueHistoryArticlesBySHA(t *testing.T, articles []m9HistoryArticle) ([]m9HistoryArticle, int) {
	t.Helper()
	seen := make(map[string]struct{}, len(articles))
	unique := make([]m9HistoryArticle, 0, len(articles))
	duplicateCount := 0
	for _, article := range articles {
		content, err := os.ReadFile(article.Path)
		require.NoError(t, err, article.Path)
		sum := sha256.Sum256(content)
		article.SHA256 = hex.EncodeToString(sum[:])
		if _, ok := seen[article.SHA256]; ok {
			duplicateCount++
			continue
		}
		seen[article.SHA256] = struct{}{}
		unique = append(unique, article)
	}
	return unique, duplicateCount
}

func m9HistoryDateTextFromPath(path string) (string, error) {
	for dir := filepath.Dir(path); dir != "." && dir != string(filepath.Separator); dir = filepath.Dir(dir) {
		match := m9HistoryDatePattern.FindStringSubmatch(filepath.Base(dir))
		if len(match) == 2 {
			return match[1], nil
		}
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
	}
	return "", fmt.Errorf("article %s is not under a directory ending with YYYYMMDD", path)
}

func m9GroupHistoryArticlesByDate(articles []m9HistoryArticle) map[string][]m9HistoryArticle {
	grouped := make(map[string][]m9HistoryArticle)
	for _, article := range articles {
		grouped[article.DateText] = append(grouped[article.DateText], article)
	}
	return grouped
}

func m9SortedHistoryDateKeys(grouped map[string][]m9HistoryArticle) []string {
	keys := make([]string, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func m9RunHistoryArticleGroup(ctx context.Context, app *bootstrap.App, baseURL string, analyzeTracker *m9AnalyzeOnceTracker, articles []m9HistoryArticle, concurrency int) ([]m9HistoryArticleResult, []m9HistoryArticle, []string) {
	dispatchCtx, stopDispatch := context.WithCancel(ctx)
	defer stopDispatch()

	sem := make(chan struct{}, concurrency)
	resultCh := make(chan m9HistoryArticleResult, len(articles))
	deferredCh := make(chan m9HistoryArticle, len(articles))
	errCh := make(chan string, len(articles))
	var wg sync.WaitGroup

dispatch:
	for _, article := range articles {
		article := article
		select {
		case sem <- struct{}{}:
		case <-dispatchCtx.Done():
			break dispatch
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			expectedTradeDate := m9ExpectedTradeDate(article.Date)
			result, err := m9ProcessRealHistoryArticle(ctx, app, baseURL, analyzeTracker, article, expectedTradeDate)
			if err != nil {
				if m9IsDeferredAnalyzeError(err) {
					deferredCh <- article
					return
				}
				errCh <- fmt.Sprintf("%s: %v", article.Path, err)
				stopDispatch()
				return
			}
			resultCh <- result
		}()
	}
	wg.Wait()
	close(resultCh)
	close(deferredCh)
	close(errCh)

	results := make([]m9HistoryArticleResult, 0, len(articles))
	for result := range resultCh {
		results = append(results, result)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Path < results[j].Path
	})

	deferred := make([]m9HistoryArticle, 0)
	for article := range deferredCh {
		deferred = append(deferred, article)
	}
	sort.Slice(deferred, func(i, j int) bool {
		return deferred[i].Path < deferred[j].Path
	})

	errors := make([]string, 0)
	for err := range errCh {
		errors = append(errors, err)
	}
	sort.Strings(errors)
	if len(errors) > 0 {
		return nil, deferred, errors
	}
	return results, deferred, nil
}

func m9ProcessRealHistoryArticle(ctx context.Context, app *bootstrap.App, baseURL string, analyzeTracker *m9AnalyzeOnceTracker, article m9HistoryArticle, expectedTradeDate time.Time) (m9HistoryArticleResult, error) {
	fileName := filepath.Base(article.Path)
	content, err := os.ReadFile(article.Path)
	if err != nil {
		return m9HistoryArticleResult{}, err
	}
	author := inferAuthorFromFileName(fileName)
	title := m9HistoryTitle(article, fileName)
	upload, err := m9UploadHistoryDocumentE(baseURL, fileName, content, map[string]string{
		"author":      author,
		"institution": m9RealHistoryInstitution,
		"title":       title,
		"pdf_use_ocr": "true",
	})
	if err != nil {
		return m9HistoryArticleResult{}, err
	}
	if upload.Document.SHA256 != article.SHA256 {
		return m9HistoryArticleResult{}, fmt.Errorf("document sha256=%s, expected file sha256=%s", upload.Document.SHA256, article.SHA256)
	}
	metadataChanged, err := m9EnsureHistoryDocumentMetadata(ctx, app, upload.Document.ID, author, m9RealHistoryInstitution, title)
	if err != nil {
		return m9HistoryArticleResult{}, err
	}

	analyzed := false
	needsAnalyze, err := m9DocumentNeedsAnalyze(ctx, app, upload, expectedTradeDate)
	if err != nil {
		return m9HistoryArticleResult{}, err
	}
	forceAnalyze := false
	if metadataChanged && upload.Duplicate {
		document, err := app.DocumentService.GetDocumentByID(ctx, upload.Document.ID)
		if err != nil {
			return m9HistoryArticleResult{}, err
		}
		if document.Status == domain.DocumentStatusPlanned {
			needsAnalyze = true
			forceAnalyze = true
		}
	}
	if needsAnalyze {
		requestedAnalyze := false
		analyzed, err = analyzeTracker.run(ctx, upload.Document.ID, func() error {
			var err error
			requestedAnalyze, err = m9AnalyzeDocumentIfStillNeeded(ctx, app, baseURL, upload.Document.ID, expectedTradeDate, forceAnalyze)
			return err
		})
		if err != nil {
			return m9HistoryArticleResult{}, err
		}
		if !requestedAnalyze {
			analyzed = false
		}
	}

	result, err := m9VerifyRealHistoryDocument(ctx, app, upload.Document.ID, article, expectedTradeDate)
	if err != nil {
		return m9HistoryArticleResult{}, err
	}
	result.Duplicate = upload.Duplicate
	result.Analyzed = analyzed
	return result, nil
}

func m9DocumentNeedsAnalyze(ctx context.Context, app *bootstrap.App, upload *m9UploadDocumentResponse, expectedTradeDate time.Time) (bool, error) {
	document, err := app.DocumentService.GetDocumentByID(ctx, upload.Document.ID)
	if err != nil {
		return false, err
	}
	return m9DocumentNeedsAnalyzeForDocument(ctx, app, upload.Duplicate, document, expectedTradeDate)
}

func m9EnsureHistoryDocumentMetadata(ctx context.Context, app *bootstrap.App, documentID int64, author string, institution string, title string) (bool, error) {
	document, err := app.DocumentService.GetDocumentByID(ctx, documentID)
	if err != nil {
		return false, err
	}
	if document.Author == author && document.Institution == institution && document.Title == title {
		return false, nil
	}
	if err := dal.Documents.UpdateMetadataByID(ctx, app.DB, documentID, author, institution, title); err != nil {
		return false, err
	}
	return true, nil
}

func m9AnalyzeDocumentIfStillNeeded(ctx context.Context, app *bootstrap.App, baseURL string, documentID int64, expectedTradeDate time.Time, force bool) (bool, error) {
	document, err := app.DocumentService.GetDocumentByID(ctx, documentID)
	if err != nil {
		return false, err
	}
	if !force {
		needsAnalyze, err := m9DocumentNeedsAnalyzeForDocument(ctx, app, true, document, expectedTradeDate)
		if err != nil {
			return false, err
		}
		if !needsAnalyze {
			return false, nil
		}
	}
	status, body, err := m9AnalyzeDocumentBodyWithRetryE(ctx, baseURL, documentID, expectedTradeDate, func() (bool, error) {
		document, err := app.DocumentService.GetDocumentByID(ctx, documentID)
		if err != nil {
			return false, err
		}
		needsAnalyze, err := m9DocumentNeedsAnalyzeForDocument(ctx, app, true, document, expectedTradeDate)
		if err != nil {
			return false, err
		}
		return !needsAnalyze, nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return true, err
		}
		return true, &m9DeferredAnalyzeError{
			DocumentID: documentID,
			Err:        err,
		}
	}
	if status != http.StatusOK {
		document, loadErr := app.DocumentService.GetDocumentByID(ctx, documentID)
		if loadErr == nil && m9AnalyzeNonOKAcceptsDocumentStatus(document.Status) {
			return false, nil
		}
		if m9IsTransientAnalyzeFailure(status, body) {
			return true, &m9DeferredAnalyzeError{
				DocumentID: documentID,
				Status:     status,
				Body:       strings.TrimSpace(string(body)),
			}
		}
		return true, fmt.Errorf("analyze http %d: %s", status, strings.TrimSpace(string(body)))
	}
	return true, nil
}

func m9IsDeferredAnalyzeError(err error) bool {
	var deferred *m9DeferredAnalyzeError
	return errors.As(err, &deferred)
}

func m9DocumentNeedsAnalyzeForDocument(ctx context.Context, app *bootstrap.App, duplicate bool, document *domain.Document, expectedTradeDate time.Time) (bool, error) {
	if document.Status != domain.DocumentStatusPlanned || expectedTradeDate.IsZero() {
		return m9DocumentStatusNeedsAnalyze(duplicate, document.Status), nil
	}
	plans, err := app.DocumentService.ListPlansByDocumentID(ctx, document.ID)
	if err != nil {
		return false, err
	}
	planDates := make([]time.Time, 0, len(plans))
	for _, plan := range plans {
		planDates = append(planDates, plan.TradeDate)
	}
	return m9DocumentStatusNeedsAnalyzeForPlanDates(duplicate, document.Status, planDates, expectedTradeDate), nil
}

func m9DocumentStatusNeedsAnalyze(duplicate bool, status domain.DocumentStatus) bool {
	return !duplicate || !m9IsTerminalHistoryDocumentStatus(status)
}

func m9DocumentStatusNeedsAnalyzeForPlanDates(duplicate bool, status domain.DocumentStatus, planDates []time.Time, expectedTradeDate time.Time) bool {
	if !duplicate {
		return true
	}
	if status == domain.DocumentStatusPlanned && !expectedTradeDate.IsZero() {
		expected := expectedTradeDate.Format(time.DateOnly)
		if len(planDates) == 0 {
			return true
		}
		for _, planDate := range planDates {
			if planDate.Format(time.DateOnly) != expected {
				return true
			}
		}
		return false
	}
	return !m9IsTerminalHistoryDocumentStatus(status)
}

func m9IsTerminalHistoryDocumentStatus(status domain.DocumentStatus) bool {
	return status == domain.DocumentStatusPlanned || status == domain.DocumentStatusInvalid
}

func m9AnalyzeNonOKAcceptsDocumentStatus(status domain.DocumentStatus) bool {
	return m9IsTerminalHistoryDocumentStatus(status)
}

func m9UploadHistoryDocumentE(baseURL, name string, content []byte, fields map[string]string) (*m9UploadDocumentResponse, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return nil, err
		}
	}
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(content); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	resp, err := http.Post(baseURL+"/api/v1/documents/upload", writer.FormDataContentType(), &body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upload http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var payload m9UploadDocumentResponse
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return nil, err
	}
	if payload.Document.ID == 0 {
		return nil, fmt.Errorf("upload response missing document.id")
	}
	return &payload, nil
}

func m9AnalyzeDocumentBodyE(ctx context.Context, baseURL string, documentID int64, tradeDate time.Time) (int, []byte, error) {
	endpoint := fmt.Sprintf("%s/api/v1/documents/%d/analyze", baseURL, documentID)
	if !tradeDate.IsZero() {
		endpoint += "?trade_date=" + url.QueryEscape(tradeDate.Format(time.DateOnly))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return 0, nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

func m9AnalyzeDocumentBodyWithRetryE(ctx context.Context, baseURL string, documentID int64, tradeDate time.Time, shouldStop func() (bool, error)) (int, []byte, error) {
	maxAttempts := m9RealHistoryAnalyzeMaxAttempts()
	var lastStatus int
	var lastBody []byte
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		status, body, err := m9AnalyzeDocumentBodyE(ctx, baseURL, documentID, tradeDate)
		lastStatus, lastBody, lastErr = status, body, err
		if !m9ShouldRetryAnalyzeAttempt(ctx, status, body, err) {
			if err != nil && shouldStop != nil {
				stop, stopErr := shouldStop()
				if stopErr != nil {
					return status, body, stopErr
				}
				if stop {
					return status, body, nil
				}
			}
			return status, body, err
		}
		if shouldStop != nil {
			stop, stopErr := shouldStop()
			if stopErr != nil {
				return status, body, stopErr
			}
			if stop {
				return status, body, nil
			}
		}
		if attempt == maxAttempts {
			break
		}
		select {
		case <-time.After(m9AnalyzeRetryBackoff(attempt)):
		case <-ctx.Done():
			return status, body, ctx.Err()
		}
	}
	return lastStatus, lastBody, lastErr
}

func m9RealHistoryAnalyzeMaxAttempts() int {
	const defaultAttempts = 1
	raw := strings.TrimSpace(os.Getenv(m9RealHistoryAnalyzeAttemptsEnv))
	if raw == "" {
		return defaultAttempts
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return defaultAttempts
	}
	return value
}

func m9AnalyzeRetryBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	backoff := time.Duration(attempt*attempt) * 100 * time.Millisecond
	if backoff > 5*time.Second {
		return 5 * time.Second
	}
	return backoff
}

func m9ShouldRetryAnalyzeAttempt(ctx context.Context, status int, body []byte, err error) bool {
	if err != nil {
		return ctx.Err() == nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
	}
	return m9IsTransientAnalyzeFailure(status, body)
}

func m9RetrySweepBackoff(pass int) time.Duration {
	if pass < 1 {
		pass = 1
	}
	backoff := time.Duration(pass) * time.Second
	if backoff > 30*time.Second {
		return 30 * time.Second
	}
	return backoff
}

func m9IsTransientAnalyzeFailure(status int, body []byte) bool {
	if status != http.StatusInternalServerError {
		return false
	}
	message := strings.ToLower(string(body))
	if m9IsTerminalInvalidParseFailure(message) {
		return false
	}
	return strings.Contains(message, "deadlock found") ||
		strings.Contains(message, "lock wait timeout") ||
		strings.Contains(message, "agent returned failed status") ||
		strings.Contains(message, "ocr failed") ||
		strings.Contains(message, "windows ocr failed") ||
		strings.Contains(message, "outofmemoryexception") ||
		strings.Contains(message, "exit status 0xc000012d")
}

func m9IsTerminalInvalidParseFailure(message string) bool {
	message = strings.ToLower(message)
	return strings.Contains(message, "invalid page count 0") ||
		strings.Contains(message, "wrong page range given") ||
		strings.Contains(message, "first page (1) can not be after the last page (0)")
}

func m9ConfigureRealHistoryRuntime(t *testing.T, app *bootstrap.App, expectedTradeDate time.Time) {
	t.Helper()
	cfg := cloneConfig(t, app.Runtime.Config())
	cfg.Security.Auth.Enabled = false
	cfg.Document.APIUploadEnabled = true
	cfg.Document.AutoAnalyzeUpload = false
	cfg.Document.SHA256Dedup = true
	cfg.Document.PDFOCR = m9RealOCRConfig(t, cfg.Document.PDFOCR)
	cfg.Agent.Enabled = true
	cfg.Agent.Mode = config.AgentModePrimary
	cfg.Agent = m9RealHistoryAgentConfig(cfg.Agent)
	cfg.Agent.AllowLegacyLLMFallback = false
	if strings.TrimSpace(cfg.Agent.SchemaVersion) == "" {
		cfg.Agent.SchemaVersion = agentclient.ResponseSchemaVersion
	}
	require.NotEmpty(t, strings.TrimSpace(cfg.Agent.Endpoint), "real Agent endpoint must come from Nacos config")
	if !expectedTradeDate.IsZero() {
		cfg.Rules.TradeDateOffsetDays = m9TradeDateOffsetFor(t, cfg, expectedTradeDate)
	}
	updateM4RuntimeConfig(t, app, cfg)
}

func m9RealHistoryAgentConfig(cfg config.AgentConfig) config.AgentConfig {
	if cfg.TimeoutMS < m9RealHistoryAgentTimeoutMS {
		cfg.TimeoutMS = m9RealHistoryAgentTimeoutMS
	}
	cfg.MaxRetries = 0
	return cfg
}

func m9RealOCRConfig(t *testing.T, current config.PDFOCRConfig) config.PDFOCRConfig {
	t.Helper()
	ocr := current
	ocr.Enabled = true
	if strings.TrimSpace(ocr.Command) == "" || strings.Contains(strings.ToLower(ocr.Command), "fake") {
		ocr.Command = filepath.Join("..", "..", "tools", "guziyuan_pdf_ocr_tool", "ocr_pdf.bat")
	}
	ocr.Command = m9ResolveOCRCommand(t, ocr.Command)
	if len(ocr.Args) == 0 {
		ocr.Args = []string{"{input}", "--stdout"}
	}
	argsText := strings.ToLower(strings.Join(ocr.Args, " "))
	require.NotContains(t, strings.ToLower(ocr.Command), "fake")
	require.NotContains(t, argsText, "echo ")
	require.NotContains(t, argsText, strings.ToLower(m9FakeOCRSentinel))
	if ocr.MinTextChars <= 0 {
		ocr.MinTextChars = 80
	}
	if ocr.TimeoutMS <= 0 {
		ocr.TimeoutMS = 120000
	}
	ocr.TreatExitCodeOneAsOK = true
	return ocr
}

func m9ResolveOCRCommand(t *testing.T, command string) string {
	t.Helper()
	candidates := []string{command}
	if !filepath.IsAbs(command) {
		candidates = append(candidates, filepath.Join("..", "..", command))
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	defaultCommand := filepath.Join("..", "..", "tools", "guziyuan_pdf_ocr_tool", "ocr_pdf.bat")
	require.FileExists(t, defaultCommand, "real OCR command not found; Nacos command=%q", command)
	return defaultCommand
}

func m9TradeDateOffsetFor(t *testing.T, cfg *config.Config, expectedTradeDate time.Time) int {
	t.Helper()
	loc, err := time.LoadLocation(cfg.Meta.Timezone)
	require.NoError(t, err)
	now := time.Now().In(loc)
	base := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	expected := time.Date(expectedTradeDate.Year(), expectedTradeDate.Month(), expectedTradeDate.Day(), 0, 0, 0, 0, loc)
	return int(expected.Sub(base).Hours() / 24)
}

func m9ExpectedTradeDate(articleDate time.Time) time.Time {
	return articleDate.AddDate(0, 0, 1)
}

func m9VerifyRealHistoryDocument(ctx context.Context, app *bootstrap.App, documentID int64, article m9HistoryArticle, expectedTradeDate time.Time) (m9HistoryArticleResult, error) {
	document, err := app.DocumentService.GetDocumentByID(ctx, documentID)
	if err != nil {
		return m9HistoryArticleResult{}, err
	}
	if document.Status != domain.DocumentStatusPlanned {
		if document.Status != domain.DocumentStatusInvalid {
			return m9HistoryArticleResult{}, fmt.Errorf("document status=%s, expected PLANNED or INVALID", document.Status)
		}
	}
	if document.SHA256 != article.SHA256 {
		return m9HistoryArticleResult{}, fmt.Errorf("document sha256=%s, expected %s", document.SHA256, article.SHA256)
	}
	if document.Institution != m9RealHistoryInstitution {
		return m9HistoryArticleResult{}, fmt.Errorf("document institution=%s, expected %s", document.Institution, m9RealHistoryInstitution)
	}

	parseRun, err := app.DocumentService.GetLatestParseRunByDocumentID(ctx, documentID)
	if err != nil {
		return m9HistoryArticleResult{}, err
	}
	if strings.Contains(parseRun.CleanedText, m9FakeOCRSentinel) {
		return m9HistoryArticleResult{}, fmt.Errorf("parse run still contains fake OCR sentinel %q", m9FakeOCRSentinel)
	}
	if document.Status == domain.DocumentStatusInvalid {
		return m9HistoryArticleResult{
			DocumentID:        documentID,
			Invalid:           true,
			ArticleDate:       article.DateText,
			ExpectedTradeDate: expectedTradeDate.Format(time.DateOnly),
			Path:              article.Path,
			SHA256:            article.SHA256,
		}, nil
	}

	expected := expectedTradeDate.Format(time.DateOnly)
	plans, err := app.DocumentService.ListPlansByDocumentID(ctx, documentID)
	if err != nil {
		return m9HistoryArticleResult{}, err
	}
	if len(plans) == 0 {
		return m9HistoryArticleResult{}, fmt.Errorf("database has no candidate plans")
	}
	for _, plan := range plans {
		if got := plan.TradeDate.Format(time.DateOnly); got != expected {
			return m9HistoryArticleResult{}, fmt.Errorf("plan %d trade_date=%s, expected %s", plan.ID, got, expected)
		}
		if plan.Institution != m9RealHistoryInstitution {
			return m9HistoryArticleResult{}, fmt.Errorf("plan %d institution=%s, expected %s", plan.ID, plan.Institution, m9RealHistoryInstitution)
		}
	}

	seenEventKeys := make(map[string]struct{}, len(plans))
	for _, plan := range plans {
		eventKey, err := m9VerifyRecommendationEventForPlan(ctx, app, *document, plan, expected)
		if err != nil {
			return m9HistoryArticleResult{}, err
		}
		seenEventKeys[eventKey] = struct{}{}
	}

	return m9HistoryArticleResult{
		DocumentID:        documentID,
		PlanCount:         len(plans),
		EventCount:        len(seenEventKeys),
		ArticleDate:       article.DateText,
		ExpectedTradeDate: expected,
		Path:              article.Path,
		SHA256:            article.SHA256,
	}, nil
}

func m9VerifyRecommendationEventForPlan(ctx context.Context, app *bootstrap.App, document domain.Document, plan domain.CandidatePlan, expectedDate string) (string, error) {
	bloggerName := strings.TrimSpace(plan.Analyst)
	if bloggerName == "" {
		bloggerName = strings.TrimSpace(document.Author)
	}
	if bloggerName == "" {
		bloggerName = strings.TrimSpace(app.Runtime.Config().Document.SourceDefaults.Author)
	}
	if bloggerName == "" {
		bloggerName = "UNKNOWN"
	}
	institution := strings.TrimSpace(plan.Institution)
	if institution == "" {
		institution = strings.TrimSpace(document.Institution)
	}
	normalizedName := m9NormalizeBloggerName(bloggerName)
	eventKey := strings.Join([]string{
		normalizedName,
		institution,
		plan.Symbol,
		string(plan.Direction),
		expectedDate,
	}, "|")

	var eventCount int64
	err := app.DB.WithContext(ctx).Raw(`
SELECT COUNT(*)
FROM recommendation_events e
JOIN bloggers b ON b.id = e.blogger_id
WHERE b.normalized_name = ?
  AND b.institution = ?
  AND e.symbol = ?
  AND e.direction = ?
  AND e.recommend_date = ?`, normalizedName, institution, plan.Symbol, string(plan.Direction), expectedDate).Scan(&eventCount).Error
	if err != nil {
		return "", err
	}
	if eventCount != 1 {
		return "", fmt.Errorf("recommendation event count for business key %q = %d, expected 1", eventKey, eventCount)
	}
	return eventKey, nil
}

func m9NormalizeBloggerName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(name)), " "))
}

func m9VerifyRealHistoryResultSet(t *testing.T, app *bootstrap.App, results []m9HistoryArticleResult) {
	t.Helper()
	documentIDs := make([]int64, 0, len(results))
	seen := make(map[int64]struct{}, len(results))
	for _, result := range results {
		if _, ok := seen[result.DocumentID]; ok {
			continue
		}
		seen[result.DocumentID] = struct{}{}
		documentIDs = append(documentIDs, result.DocumentID)
	}
	require.NotEmpty(t, documentIDs)

	var fakeParseRuns int64
	require.NoError(t, app.DB.Raw("SELECT COUNT(*) FROM parse_runs WHERE document_id IN ? AND cleaned_text LIKE ?", documentIDs, "%"+m9FakeOCRSentinel+"%").Scan(&fakeParseRuns).Error)
	require.Zero(t, fakeParseRuns, "real history run must not contain fake OCR text")

	var fakeEvidence int64
	require.NoError(t, app.DB.Raw("SELECT COUNT(*) FROM recommendation_event_evidences WHERE source_document_id IN ? AND evidence_text LIKE ?", documentIDs, "%"+m9FakeOCRSentinel+"%").Scan(&fakeEvidence).Error)
	require.Zero(t, fakeEvidence, "recommendation evidence must come from real parsed/agent output")

	var distinctSymbols int64
	require.NoError(t, app.DB.Raw("SELECT COUNT(DISTINCT symbol) FROM trade_candidate_plans WHERE document_id IN ?", documentIDs).Scan(&distinctSymbols).Error)
	require.Greater(t, distinctSymbols, int64(2), "real Agent output should not collapse the whole corpus to the old fixed two symbols")

	var wrongDocumentInstitution int64
	require.NoError(t, app.DB.Raw("SELECT COUNT(*) FROM documents WHERE id IN ? AND institution <> ?", documentIDs, m9RealHistoryInstitution).Scan(&wrongDocumentInstitution).Error)
	require.Zero(t, wrongDocumentInstitution, "M9 historical documents must use personal institution")

	var wrongPlanInstitution int64
	require.NoError(t, app.DB.Raw("SELECT COUNT(*) FROM trade_candidate_plans WHERE document_id IN ? AND institution <> ?", documentIDs, m9RealHistoryInstitution).Scan(&wrongPlanInstitution).Error)
	require.Zero(t, wrongPlanInstitution, "M9 historical candidate plans must use personal institution")

	var wrongBloggerInstitution int64
	require.NoError(t, app.DB.Raw(`
SELECT COUNT(*)
FROM bloggers b
WHERE b.id IN (
	SELECT DISTINCT blogger_id
	FROM recommendation_events
	WHERE source_document_id IN ?
)
AND b.institution <> ?`, documentIDs, m9RealHistoryInstitution).Scan(&wrongBloggerInstitution).Error)
	require.Zero(t, wrongBloggerInstitution, "M9 historical bloggers must use personal institution")

	var duplicateEvents int64
	require.NoError(t, app.DB.Raw(`
SELECT COUNT(*) FROM (
	SELECT blogger_id, symbol, direction, recommend_date
	FROM recommendation_events
	WHERE source_document_id IN ?
	GROUP BY blogger_id, symbol, direction, recommend_date
	HAVING COUNT(*) > 1
) AS duplicated_recommendation_events`, documentIDs).Scan(&duplicateEvents).Error)
	require.Zero(t, duplicateEvents, "recommendation events must be unique by blogger/symbol/direction/recommend_date")
}

func m9HistoryTitle(article m9HistoryArticle, fileName string) string {
	base := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	return m9TruncateRunes(m9RealHistoryTitlePrefix+"|"+article.DateText+"|"+base, 255)
}

func m9TruncateRunes(value string, max int) string {
	if utf8.RuneCountInString(value) <= max {
		return value
	}
	out := make([]rune, 0, max)
	for _, r := range value {
		if len(out) == max {
			break
		}
		out = append(out, r)
	}
	return string(out)
}

func m9FirstStrings(values []string, max int) []string {
	if len(values) <= max {
		return values
	}
	return values[:max]
}
