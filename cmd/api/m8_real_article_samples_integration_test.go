package main

import (
	"context"
	"encoding/json"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"finance-sys/internal/agentclient"
	"finance-sys/internal/bootstrap"
	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain"
	"finance-sys/internal/domain/db_model"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	m8IntegrationEnv                  = "FINANCE_SYS_M8_REAL_ARTICLE_INTEGRATION"
	m8DMLAckEnv                       = "FINANCE_SYS_M8_NACOS_DML_ACK"
	m8SampleCountEnv                  = "FINANCE_SYS_M8_SAMPLE_COUNT"
	m8UntrackableSampleCountEnv       = "FINANCE_SYS_M8_UNTRACKABLE_SAMPLE_COUNT"
	m8SampleSeedEnv                   = "FINANCE_SYS_M8_SAMPLE_SEED"
	m8DefaultSampleCount              = 12
	m8DefaultUntrackableCount         = 4
	m8DefaultSampleSeed         int64 = 20260529
	m8AgentStaticToken                = "m8-test-token"
	m8SkillHash                       = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

var m8TSCodePattern = regexp.MustCompile(`^\d{6}\.(SH|SZ|BJ)$`)

type m8SecurityFixture struct {
	Keyword  string
	TSCode   string
	Symbol   string
	Name     string
	Exchange string
	Market   string
}

var m8SecurityFixtures = []m8SecurityFixture{
	{Keyword: "\u65b0\u6613\u76db", TSCode: "989001.SZ", Symbol: "989001", Name: "M8 Xinyisheng", Exchange: "SZSE", Market: "SZ"},
	{Keyword: "\u65ed\u521b", TSCode: "989002.SZ", Symbol: "989002", Name: "M8 Zhongji Xuchuang", Exchange: "SZSE", Market: "SZ"},
	{Keyword: "\u4e2d\u9645\u65ed\u521b", TSCode: "989002.SZ", Symbol: "989002", Name: "M8 Zhongji Xuchuang", Exchange: "SZSE", Market: "SZ"},
	{Keyword: "\u957f\u6c5f\u901a\u4fe1", TSCode: "989003.SH", Symbol: "989003", Name: "M8 Changjiang Telecom", Exchange: "SSE", Market: "SH"},
	{Keyword: "\u70fd\u706b\u901a\u4fe1", TSCode: "989004.SH", Symbol: "989004", Name: "M8 Fiberhome", Exchange: "SSE", Market: "SH"},
	{Keyword: "\u4fe1\u79d1\u79fb\u52a8", TSCode: "989005.SH", Symbol: "989005", Name: "M8 CICT Mobile", Exchange: "SSE", Market: "SH"},
	{Keyword: "\u676d\u7535\u80a1\u4efd", TSCode: "989006.SH", Symbol: "989006", Name: "M8 Hangdian", Exchange: "SSE", Market: "SH"},
	{Keyword: "\u822a\u5929\u53d1\u5c55", TSCode: "989007.SZ", Symbol: "989007", Name: "M8 Aerospace Development", Exchange: "SZSE", Market: "SZ"},
	{Keyword: "\u5929\u94f6\u673a\u7535", TSCode: "989008.SZ", Symbol: "989008", Name: "M8 Tianyin", Exchange: "SZSE", Market: "SZ"},
	{Keyword: "\u4e0a\u6d77\u65b0\u9633", TSCode: "989009.SZ", Symbol: "989009", Name: "M8 Shanghai Xinyang", Exchange: "SZSE", Market: "SZ"},
	{Keyword: "\u4e09\u53d8\u79d1\u6280", TSCode: "989010.SZ", Symbol: "989010", Name: "M8 Sanbian", Exchange: "SZSE", Market: "SZ"},
	{Keyword: "\u82f1\u7ef4\u514b", TSCode: "989011.SZ", Symbol: "989011", Name: "M8 Envicool", Exchange: "SZSE", Market: "SZ"},
	{Keyword: "\u897f\u90e8\u6750\u6599", TSCode: "989012.SZ", Symbol: "989012", Name: "M8 Western Materials", Exchange: "SZSE", Market: "SZ"},
	{Keyword: "\u5965\u7279\u7ef4", TSCode: "989013.SH", Symbol: "989013", Name: "M8 Autowell", Exchange: "SSE", Market: "SH"},
	{Keyword: "\u7ea2\u5b9d\u4e3d", TSCode: "989014.SZ", Symbol: "989014", Name: "M8 Hongbaoli", Exchange: "SZSE", Market: "SZ"},
	{Keyword: "\u4fe1\u7ef4\u901a\u4fe1", TSCode: "989015.SZ", Symbol: "989015", Name: "M8 Sunway", Exchange: "SZSE", Market: "SZ"},
	{Keyword: "\u84dd\u8272\u5149\u6807", TSCode: "989016.SZ", Symbol: "989016", Name: "M8 BlueFocus", Exchange: "SZSE", Market: "SZ"},
}

type m8UntrackableFixture struct {
	Keyword   string
	RawSymbol string
	Kind      string
	Reason    string
}

var m8UntrackableFixtures = []m8UntrackableFixture{
	{Keyword: "CPO", RawSymbol: "CPO\u677f\u5757", Kind: "SECTOR", Reason: "sector is not a single tradable security"},
	{Keyword: "\u677f\u5757", RawSymbol: "\u70ed\u70b9\u677f\u5757", Kind: "SECTOR", Reason: "sector is not a single tradable security"},
	{Keyword: "\u65b9\u5411", RawSymbol: "AI\u4e3b\u9898", Kind: "THEME", Reason: "theme is not a single tradable security"},
	{Keyword: "\u4e3b\u9898", RawSymbol: "AI\u4e3b\u9898", Kind: "THEME", Reason: "theme is not a single tradable security"},
	{Keyword: "\u4e2a\u80a1", RawSymbol: "A\u80a1\u76f8\u5173\u6807\u7684", Kind: "BROAD_PHRASE", Reason: "broad phrase is not a single tradable security"},
	{Keyword: "DEEPSEEK", RawSymbol: "DeepSeek\u4e3b\u9898", Kind: "THEME", Reason: "theme is not a single tradable security"},
	{Keyword: "spaceX", RawSymbol: "spaceX\u4e3b\u9898", Kind: "THEME", Reason: "theme is not a single tradable security"},
}

func TestHTTPM8RealArticlePDFSamplesUseAgentAndPersistPlansWithNacosBootstrap(t *testing.T) {
	m8RequireEnabled(t)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()

	loadBootstrapEnvFile(t)
	app := buildIntegrationApp(t, ctx)
	defer app.Close()
	seedM8SecurityFixtures(t, ctx, app.DB)

	recorder := newM8AgentRecorder()
	agentServer := httptest.NewServer(http.HandlerFunc(recorder.handleArticleAwareRequest(t)))
	defer agentServer.Close()

	m8ConfigureRuntime(t, app, agentServer.URL)
	baseURL, shutdown := startMainHTTPServerForTest(t, app)
	defer shutdown()

	root := m8ArticleRoot()
	samples := m8SelectSamples(t, root, m8SampleCount(), m8SampleSeed(), m8PathHasSecurityFixture)
	require.Len(t, samples, m8SampleCount())

	for _, path := range samples {
		documentID := m8UploadPDFSample(t, baseURL, path, "M8 article sample: ")
		status, body := analyzeDocumentBody(t, baseURL, documentID)
		require.Equal(t, http.StatusOK, status, "analyze failed for %s: %s", path, string(body))

		var payload struct {
			Status string                 `json:"status"`
			Plans  []domain.CandidatePlan `json:"plans"`
		}
		require.NoError(t, json.Unmarshal(body, &payload))
		require.Equal(t, "planned", payload.Status)
		require.NotEmpty(t, payload.Plans, path)
		for _, plan := range payload.Plans {
			require.True(t, m8TSCodePattern.MatchString(plan.Symbol), "plan symbol must be verified ts_code: %+v", plan)
			require.NotContains(t, plan.Symbol, "\u677f\u5757")
			require.NotContains(t, plan.Symbol, "\u4e3b\u9898")
			require.NotContains(t, plan.Symbol, "\u4e2a\u80a1")
		}

		parseRun, err := app.DocumentService.GetLatestParseRunByDocumentID(ctx, documentID)
		require.NoError(t, err)
		require.Equal(t, domain.ParseRunStatusParsed, parseRun.Status)
		require.NotEmpty(t, parseRun.Chunks)
		require.NotEmpty(t, strings.TrimSpace(parseRun.CleanedText), path)

		runs := m7ListResolutionRuns(t, baseURL, documentID)
		require.NotEmpty(t, runs.Items)
		run := runs.Items[0]
		require.Equal(t, domain.ResolutionRunStatusSucceeded, run.Status)
		require.Equal(t, string(domain.ResolutionRouteAgentPrimary), run.Route)
		require.False(t, run.FallbackUsed)
		require.Equal(t, len(payload.Plans), run.CandidatePlanCount)
		require.GreaterOrEqual(t, run.RawTargetCount, run.CandidatePlanInputCount)
		require.Equal(t, m8SkillHash, run.SkillHash)

		detail := m7GetResolutionRun(t, baseURL, run.ID)
		require.NotContains(t, string(mustJSON(t, detail.ToolTraces)), m8AgentStaticToken)
		require.NotContains(t, string(mustJSON(t, detail.RawMetadata)), m8AgentStaticToken)
		recorder.requireSawDocument(t, documentID)
	}
}

func TestHTTPM8RealArticlePDFUntrackableSamplesAreObservedWithNacosBootstrap(t *testing.T) {
	m8RequireEnabled(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	loadBootstrapEnvFile(t)
	app := buildIntegrationApp(t, ctx)
	defer app.Close()
	seedM8SecurityFixtures(t, ctx, app.DB)

	recorder := newM8AgentRecorder()
	agentServer := httptest.NewServer(http.HandlerFunc(recorder.handleArticleAwareRequest(t)))
	defer agentServer.Close()

	m8ConfigureRuntime(t, app, agentServer.URL)
	baseURL, shutdown := startMainHTTPServerForTest(t, app)
	defer shutdown()

	root := m8ArticleRoot()
	samples := m8SelectSamples(t, root, m8UntrackableSampleCount(), m8SampleSeed()+17, func(path string) bool {
		return m8PathHasUntrackableHint(path) && !m8PathHasSecurityFixture(path)
	})
	require.NotEmpty(t, samples)

	for _, path := range samples {
		documentID := m8UploadPDFSample(t, baseURL, path, "M8 untrackable sample: ")
		status, body := analyzeDocumentBody(t, baseURL, documentID)
		require.Equal(t, http.StatusInternalServerError, status, "expected untrackable-only article to fail planning: %s", path)
		require.Contains(t, string(body), "no trackable securities resolved")

		document, err := app.DocumentService.GetDocumentByID(ctx, documentID)
		require.NoError(t, err)
		require.Equal(t, domain.DocumentStatusInvalid, document.Status)
		plans, err := app.DocumentService.ListPlansByDocumentID(ctx, documentID)
		require.NoError(t, err)
		require.Empty(t, plans)

		runs := m7ListResolutionRuns(t, baseURL, documentID)
		require.NotEmpty(t, runs.Items)
		run := runs.Items[0]
		require.Equal(t, domain.ResolutionRunStatusFailed, run.Status)
		require.Equal(t, string(domain.ResolutionRouteAgentPrimary), run.Route)
		require.Zero(t, run.CandidatePlanInputCount)
		require.Zero(t, run.CandidatePlanCount)
		require.Greater(t, run.UntrackableCount, 0)

		untrackables := m7ListUntrackableTargets(t, baseURL, documentID)
		require.NotEmpty(t, untrackables.Items)
		for _, item := range untrackables.Items {
			require.NotRegexp(t, m8TSCodePattern, item.RawTarget)
			require.NotEmpty(t, item.ReasonCode)
		}
		recorder.requireSawDocument(t, documentID)
	}
}

type m8AgentRecorder struct {
	mu       sync.Mutex
	requests map[int64]agentclient.ResolveDocumentRequest
}

func newM8AgentRecorder() *m8AgentRecorder {
	return &m8AgentRecorder{requests: make(map[int64]agentclient.ResolveDocumentRequest)}
}

func (r *m8AgentRecorder) handleArticleAwareRequest(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, req *http.Request) {
		require.Equal(t, "/v1/resolve-document", req.URL.Path)
		require.Equal(t, m8AgentStaticToken, req.Header.Get("X-Agent-Token"))
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		var request agentclient.ResolveDocumentRequest
		require.NoError(t, json.Unmarshal(body, &request))
		require.Equal(t, agentclient.RequestSchemaVersion, request.SchemaVersion)
		require.NotEmpty(t, request.RequestID)
		require.NotEmpty(t, request.Chunks)
		require.Greater(t, m8TotalChunkRunes(request.Chunks), 0)

		r.mu.Lock()
		r.requests[request.Document.DocumentID] = request
		r.mu.Unlock()

		response := m8BuildAgentResponse(request)
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(response))
	}
}

func (r *m8AgentRecorder) requireSawDocument(t *testing.T, documentID int64) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.requests[documentID]
	require.True(t, ok, "agent did not receive document %d", documentID)
}

func m8BuildAgentResponse(request agentclient.ResolveDocumentRequest) agentclient.ResolveDocumentResponse {
	if strings.HasPrefix(request.Document.Title, "M8 untrackable sample:") {
		return m8UntrackableOnlyAgentResponse()
	}

	corpus := request.Document.Title + "\n" + m8JoinChunks(request.Chunks)
	candidates := m8MatchSecurityFixtures(corpus)
	untrackables := m8MatchUntrackableFixtures(corpus)
	rawIntents := make([]agentclient.AgentRawIntent, 0, len(candidates)+len(untrackables))
	inputs := make([]agentclient.AgentCandidatePlanInput, 0, len(candidates))
	for _, fixture := range candidates {
		rawIntents = append(rawIntents, m8RawIntent(fixture.Keyword))
		inputs = append(inputs, m8CandidateInput(fixture))
	}
	for _, item := range untrackables {
		rawIntents = append(rawIntents, m8RawIntent(item.RawSymbol))
	}
	status := agentclient.AgentStatusResolved
	if len(untrackables) > 0 {
		status = agentclient.AgentStatusPartial
	}
	return agentclient.ResolveDocumentResponse{
		SchemaVersion:      agentclient.ResponseSchemaVersion,
		AgentVersion:       "m8-article-aware-agent",
		Status:             status,
		RawIntents:         rawIntents,
		CandidatePlanInput: inputs,
		UntrackableTargets: m8AgentUntrackables(untrackables),
		Warnings:           []string{"m8 article sample deterministic agent fixture"},
		Debug:              m8AgentDebug("m8-real-article-sample"),
	}
}

func m8UntrackableOnlyAgentResponse() agentclient.ResolveDocumentResponse {
	item := m8UntrackableFixture{RawSymbol: "CPO\u677f\u5757", Kind: "SECTOR", Reason: "sector is not a single tradable security"}
	return agentclient.ResolveDocumentResponse{
		SchemaVersion:      agentclient.ResponseSchemaVersion,
		AgentVersion:       "m8-article-aware-agent",
		Status:             agentclient.AgentStatusPartial,
		RawIntents:         []agentclient.AgentRawIntent{m8RawIntent(item.RawSymbol)},
		UntrackableTargets: m8AgentUntrackables([]m8UntrackableFixture{item}),
		Warnings:           []string{"m8 untrackable-only article fixture"},
		Debug:              m8AgentDebug("m8-real-article-untrackable"),
	}
}

func m8AgentDebug(graphRunID string) agentclient.AgentDebug {
	return agentclient.AgentDebug{
		GraphRunID:   graphRunID,
		Nodes:        []string{"load_skill", "extract_raw_intents", "resolve_with_local_security", "verify_external_candidates"},
		ToolsUsed:    []string{"local_security_lookup_tool", "local_security_verify_tool"},
		DurationMS:   1,
		SkillName:    "instrument_resolution",
		SkillVersion: "instrument-resolution-m8-v1",
		SkillHash:    m8SkillHash,
	}
}

func m8RawIntent(rawSymbol string) agentclient.AgentRawIntent {
	return agentclient.AgentRawIntent{
		IntentID:           "m8-intent-" + rawSymbol,
		RawSymbol:          rawSymbol,
		Direction:          domain.TradeDirectionLong,
		ReferencePrice:     10.25,
		ReferencePriceNote: domain.ReferencePriceNoteExplicitPriceMention,
		Thesis:             "real article sample reached the agent fixture",
		Evidence:           []domain.EvidenceSpan{{ChunkIndex: 0, Text: rawSymbol}},
		Risks:              []string{"sample regression risk"},
		Confidence:         0.8,
	}
}

func m8CandidateInput(fixture m8SecurityFixture) agentclient.AgentCandidatePlanInput {
	return agentclient.AgentCandidatePlanInput{
		IntentID:           "m8-intent-" + fixture.Keyword,
		RawSymbol:          fixture.Keyword,
		Security:           agentclient.AgentSecurity{TSCode: fixture.TSCode, Symbol: fixture.Symbol, Name: fixture.Name, AssetType: "STOCK", Market: fixture.Market},
		Direction:          domain.TradeDirectionLong,
		ReferencePrice:     10.25,
		ReferencePriceNote: domain.ReferencePriceNoteExplicitPriceMention,
		Thesis:             "real article sample reached the agent fixture",
		Evidence:           []domain.EvidenceSpan{{ChunkIndex: 0, Text: fixture.Keyword}},
		Risks:              []string{"sample regression risk"},
		Confidence:         0.8,
	}
}

func m8AgentUntrackables(items []m8UntrackableFixture) []agentclient.AgentUntrackableTarget {
	result := make([]agentclient.AgentUntrackableTarget, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item.RawSymbol == "" {
			continue
		}
		if _, ok := seen[item.RawSymbol]; ok {
			continue
		}
		seen[item.RawSymbol] = struct{}{}
		result = append(result, agentclient.AgentUntrackableTarget{
			RawSymbol:  item.RawSymbol,
			TargetKind: item.Kind,
			Reason:     item.Reason,
			Evidence:   []domain.EvidenceSpan{{ChunkIndex: 0, Text: item.RawSymbol}},
		})
	}
	return result
}

func seedM8SecurityFixtures(t *testing.T, ctx context.Context, db *gorm.DB) {
	t.Helper()
	for _, fixture := range m8SecurityFixtures {
		row := db_model.SecurityMaster{
			TSCode:     fixture.TSCode,
			Symbol:     fixture.Symbol,
			Name:       fixture.Name,
			FullName:   fixture.Name + " Co Ltd",
			Exchange:   fixture.Exchange,
			Market:     fixture.Market,
			AssetType:  "STOCK",
			ListStatus: "L",
			Industry:   "M8_TEST",
			IsActive:   true,
			Source:     "TEST_M8",
			RawJSON:    []byte(`{"fixture":"m8_real_article_samples"}`),
		}
		require.NoError(t, dal.SecurityMasters.UpsertByTSCode(ctx, db, &row))
	}
}

func m8RequireEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv(m8IntegrationEnv) != "1" {
		t.Skipf("set %s=1 to run real article PDF integration tests", m8IntegrationEnv)
	}
	if os.Getenv(m8DMLAckEnv) != "write-real-db" {
		t.Skipf("set %s=write-real-db after acknowledging this test writes to the configured MySQL database", m8DMLAckEnv)
	}
	if runtime.GOOS != "windows" {
		t.Skip("real article OCR integration currently uses the Windows OCR wrapper")
	}
	require.DirExists(t, m8ArticleRoot())
}

func m8ConfigureRuntime(t *testing.T, app *bootstrap.App, agentBaseURL string) {
	t.Helper()
	cfg := cloneConfig(t, app.Runtime.Config())
	cfg.Security.Auth.Enabled = false
	cfg.Document.APIUploadEnabled = true
	cfg.Document.AutoAnalyzeUpload = false
	cfg.Document.SHA256Dedup = true
	cfg.Document.PDFOCR = config.PDFOCRConfig{
		Enabled:              true,
		Command:              filepath.Join("..", "..", "tools", "guziyuan_pdf_ocr_tool", "ocr_pdf.bat"),
		Args:                 []string{"{input}", "--stdout"},
		MinTextChars:         80,
		TimeoutMS:            120000,
		TreatExitCodeOneAsOK: true,
	}
	cfg.Agent.Enabled = true
	cfg.Agent.Mode = config.AgentModePrimary
	cfg.Agent.Endpoint = agentBaseURL + "/v1/resolve-document"
	cfg.Agent.HealthEndpoint = agentBaseURL + "/healthz"
	cfg.Agent.InternalAPIBaseURL = "http://127.0.0.1:0"
	cfg.Agent.TimeoutMS = 5000
	cfg.Agent.MaxRetries = 0
	cfg.Agent.SchemaVersion = agentclient.ResponseSchemaVersion
	cfg.Agent.AllowLegacyLLMFallback = false
	cfg.Agent.Auth.Enabled = true
	cfg.Agent.Auth.HeaderName = "X-Agent-Token"
	cfg.Agent.Auth.StaticToken = m8AgentStaticToken
	cfg.Agent.Observation = config.ObservationConfig{
		Enabled:           true,
		PersistSuccess:    true,
		PersistFailure:    true,
		PersistToolTraces: true,
		ShadowSampleRate:  1,
		MaxTargetsPerRun:  200,
		MaxJSONBytes:      262144,
		RetentionDays:     90,
	}
	app.Runtime.Update(&config.Snapshot{
		Config:   cfg,
		Source:   app.Runtime.Current().Source,
		SHA256:   app.Runtime.Current().SHA256,
		LoadedAt: app.Runtime.Current().LoadedAt,
		Raw:      app.Runtime.Current().Raw,
	})
}

func m8ArticleRoot() string {
	return filepath.Join("..", "..", "testdata", "\u6e38\u8d44\u5927V\u590d\u76d8\u6587\u7ae0\u6c47\u603b2026")
}

func m8UploadPDFSample(t *testing.T, baseURL, path, titlePrefix string) int64 {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	fileName := filepath.Base(path)
	return uploadFile(t, baseURL, fileName, content, map[string]string{
		"author":      inferAuthorFromFileName(fileName),
		"institution": "M8 Real Article Samples",
		"title":       titlePrefix + strings.TrimSuffix(fileName, filepath.Ext(fileName)),
		"pdf_use_ocr": "true",
	})
}

func m8SelectSamples(t *testing.T, root string, count int, seed int64, include func(string) bool) []string {
	t.Helper()
	require.Positive(t, count)
	var candidates []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".pdf") {
			return nil
		}
		if include(path) {
			candidates = append(candidates, path)
		}
		return nil
	})
	require.NoError(t, err)
	sort.Strings(candidates)
	require.GreaterOrEqual(t, len(candidates), count, "not enough matching PDFs under %s", root)

	rng := rand.New(rand.NewSource(seed))
	rng.Shuffle(len(candidates), func(i, j int) {
		candidates[i], candidates[j] = candidates[j], candidates[i]
	})
	samples := append([]string(nil), candidates[:count]...)
	sort.Strings(samples)
	return samples
}

func m8PathHasSecurityFixture(path string) bool {
	for _, fixture := range m8SecurityFixtures {
		if m8Contains(path, fixture.Keyword) {
			return true
		}
	}
	return false
}

func m8PathHasUntrackableHint(path string) bool {
	for _, fixture := range m8UntrackableFixtures {
		if m8Contains(path, fixture.Keyword) {
			return true
		}
	}
	return false
}

func m8MatchSecurityFixtures(corpus string) []m8SecurityFixture {
	var result []m8SecurityFixture
	seen := make(map[string]struct{}, len(m8SecurityFixtures))
	for _, fixture := range m8SecurityFixtures {
		if !m8Contains(corpus, fixture.Keyword) {
			continue
		}
		if _, ok := seen[fixture.TSCode]; ok {
			continue
		}
		seen[fixture.TSCode] = struct{}{}
		result = append(result, fixture)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].TSCode < result[j].TSCode
	})
	return result
}

func m8MatchUntrackableFixtures(corpus string) []m8UntrackableFixture {
	var result []m8UntrackableFixture
	seen := make(map[string]struct{}, len(m8UntrackableFixtures))
	for _, fixture := range m8UntrackableFixtures {
		if !m8Contains(corpus, fixture.Keyword) {
			continue
		}
		if _, ok := seen[fixture.RawSymbol]; ok {
			continue
		}
		seen[fixture.RawSymbol] = struct{}{}
		result = append(result, fixture)
	}
	return result
}

func m8Contains(value, needle string) bool {
	return strings.Contains(strings.ToUpper(value), strings.ToUpper(needle))
}

func m8JoinChunks(chunks []agentclient.AgentDocumentChunk) string {
	var builder strings.Builder
	for _, chunk := range chunks {
		builder.WriteString(chunk.Text)
		builder.WriteByte('\n')
	}
	return builder.String()
}

func m8TotalChunkRunes(chunks []agentclient.AgentDocumentChunk) int {
	total := 0
	for _, chunk := range chunks {
		total += utf8.RuneCountInString(strings.TrimSpace(chunk.Text))
	}
	return total
}

func m8SampleCount() int {
	return m8EnvInt(m8SampleCountEnv, m8DefaultSampleCount)
}

func m8UntrackableSampleCount() int {
	return m8EnvInt(m8UntrackableSampleCountEnv, m8DefaultUntrackableCount)
}

func m8SampleSeed() int64 {
	raw := strings.TrimSpace(os.Getenv(m8SampleSeedEnv))
	if raw == "" {
		return m8DefaultSampleSeed
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return m8DefaultSampleSeed
	}
	return value
}

func m8EnvInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
