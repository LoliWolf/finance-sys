package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain"
	"finance-sys/internal/domain/db_model"
	"finance-sys/internal/service"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestHTTPM3AnalyzeDocumentResolvesSecurityWithNacosBootstrap(t *testing.T) {
	if os.Getenv("FINANCE_SYS_M3_NACOS_INTEGRATION") != "1" {
		t.Skip("set FINANCE_SYS_M3_NACOS_INTEGRATION=1 to run; this test writes to the Nacos-configured MySQL database")
	}
	if os.Getenv("FINANCE_SYS_M3_NACOS_DML_ACK") != "write-real-db" {
		t.Skip("set FINANCE_SYS_M3_NACOS_DML_ACK=write-real-db after manually acknowledging real database writes")
	}

	loadBootstrapEnvFile(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	app := buildIntegrationApp(t, ctx)
	defer app.Close()

	seedM1SecurityLookupFixtures(t, ctx, app.DB)
	seedM3AmbiguousAliasFixtures(t, ctx, app.DB)

	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		payload := string(body)
		switch {
		case strings.Contains(payload, "M3_VALID_SENTINEL"):
			writeM3ChatCompletion(t, w, []string{"新易盛", "旭创", "CPO板块"})
		case strings.Contains(payload, "M3_UNKNOWN_SENTINEL"):
			writeM3ChatCompletion(t, w, []string{"不存在的股票简称"})
		case strings.Contains(payload, "M3_UNTRACKABLE_SENTINEL"):
			writeM3ChatCompletion(t, w, []string{"A股贵金属个股"})
		case strings.Contains(payload, "M3_AMBIGUOUS_SENTINEL"):
			writeM3ChatCompletion(t, w, []string{"重名标的"})
		default:
			writeM3ChatCompletion(t, w, []string{"不存在的默认测试标的"})
		}
	}))
	defer llmServer.Close()

	cfg := cloneConfig(t, app.Runtime.Config())
	cfg.Security.Auth.Enabled = false
	cfg.Document.APIUploadEnabled = true
	cfg.Document.AutoAnalyzeUpload = false
	cfg.Document.SHA256Dedup = false
	cfg.LLM.Enabled = true
	cfg.LLM.Provider = config.LLMProviderOpenAICompatible
	cfg.LLM.Endpoint = llmServer.URL
	cfg.LLM.APIKey = "m3-test-key"
	cfg.LLM.Model = "m3-test-model"
	cfg.LLM.TimeoutMS = 5000
	cfg.LLM.MaxRetries = 0
	app.Runtime.Update(&config.Snapshot{
		Config:   cfg,
		Source:   app.Runtime.Current().Source,
		SHA256:   app.Runtime.Current().SHA256,
		LoadedAt: app.Runtime.Current().LoadedAt,
		Raw:      app.Runtime.Current().Raw,
	})

	baseURL, shutdown := startMainHTTPServerForTest(t, app)
	defer shutdown()

	unique := time.Now().UnixNano()
	validDocumentID := uploadFile(t, baseURL, fmt.Sprintf("m3-valid-%d.txt", unique), []byte(fmt.Sprintf("M3_VALID_SENTINEL %d 推荐新易盛和旭创，同时提到 CPO 板块。", unique)), map[string]string{
		"author": "M3 Tester",
		"title":  "M3 valid resolution",
	})
	validStatus, validBody := analyzeDocumentBody(t, baseURL, validDocumentID)
	require.Equal(t, http.StatusOK, validStatus, string(validBody))
	var validPayload struct {
		Status string                 `json:"status"`
		Plans  []domain.CandidatePlan `json:"plans"`
	}
	require.NoError(t, json.Unmarshal(validBody, &validPayload))
	require.Equal(t, "planned", validPayload.Status)
	require.Len(t, validPayload.Plans, 2)
	requireM3PlanSymbols(t, validPayload.Plans, "300502.SZ", "300308.SZ")
	requireM3PlanSymbolsNotContains(t, validPayload.Plans, "CPO板块")

	validDocument, err := app.DocumentService.GetDocumentByID(ctx, validDocumentID)
	require.NoError(t, err)
	require.Equal(t, domain.DocumentStatusPlanned, validDocument.Status)

	assertM3AnalyzeFails(t, ctx, app.DocumentService, baseURL, fmt.Sprintf("m3-unknown-%d.txt", unique), fmt.Sprintf("M3_UNKNOWN_SENTINEL %d 提到不存在的股票简称。", unique), "security not found for instrument")
	assertM3AnalyzeFails(t, ctx, app.DocumentService, baseURL, fmt.Sprintf("m3-untrackable-%d.txt", unique), fmt.Sprintf("M3_UNTRACKABLE_SENTINEL %d 只推荐 A股贵金属个股。", unique), "no trackable securities resolved")
	assertM3AnalyzeFails(t, ctx, app.DocumentService, baseURL, fmt.Sprintf("m3-ambiguous-%d.txt", unique), fmt.Sprintf("M3_AMBIGUOUS_SENTINEL %d 提到重名标的。", unique), "ambiguous instrument")
}

func seedM3AmbiguousAliasFixtures(t *testing.T, ctx context.Context, db *gorm.DB) {
	t.Helper()

	records := []db_model.SecurityMaster{
		{
			TSCode:     "999981.SH",
			Symbol:     "999981",
			Name:       "M3重名标的一号",
			FullName:   "M3重名标的一号股份有限公司",
			Exchange:   "SSE",
			Market:     "SH",
			AssetType:  "STOCK",
			ListStatus: "L",
			Industry:   "测试",
			IsActive:   true,
			Source:     "TEST",
			RawJSON:    []byte(`{"fixture":"m3_candidate_assembler","case":"ambiguous"}`),
		},
		{
			TSCode:     "999982.SH",
			Symbol:     "999982",
			Name:       "M3重名标的二号",
			FullName:   "M3重名标的二号股份有限公司",
			Exchange:   "SSE",
			Market:     "SH",
			AssetType:  "STOCK",
			ListStatus: "L",
			Industry:   "测试",
			IsActive:   true,
			Source:     "TEST",
			RawJSON:    []byte(`{"fixture":"m3_candidate_assembler","case":"ambiguous"}`),
		},
	}
	for i := range records {
		require.NoError(t, dal.SecurityMasters.UpsertByTSCode(ctx, db, &records[i]))
	}

	for _, tsCode := range []string{"999981.SH", "999982.SH"} {
		securityMaster, err := dal.SecurityMasters.QueryByTSCode(ctx, db, tsCode)
		require.NoError(t, err)
		require.NoError(t, dal.SecurityAliases.UpsertByAliasAndSecurityID(ctx, db, &db_model.SecurityAlias{
			SecurityMasterID: securityMaster.ID,
			AliasName:        "重名标的",
			NormalizedAlias:  service.NormalizeSecurityAlias("重名标的"),
			AliasType:        "COMMON_NAME",
			Source:           "TEST",
			Confidence:       1,
			IsActive:         true,
		}))
	}
}

func writeM3ChatCompletion(t *testing.T, w http.ResponseWriter, symbols []string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	plans := make([]map[string]any, 0, len(symbols))
	for _, symbol := range symbols {
		plans = append(plans, map[string]any{
			"analyst":              "M3 Tester",
			"institution":          "Integration",
			"symbol":               symbol,
			"direction":            "LONG",
			"reference_price":      88.8,
			"reference_price_note": "explicit_price_mention",
			"thesis":               "explicit recommendation from source text",
			"evidence":             []map[string]any{{"chunk_index": 0, "text": "source evidence"}},
			"risks":                []string{"volatility"},
			"confidence":           0.81,
		})
	}
	content, err := json.Marshal(map[string]any{"plans": plans})
	require.NoError(t, err)
	require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
		"choices": []any{
			map[string]any{
				"message": map[string]any{
					"content": string(content),
				},
			},
		},
	}))
}

func assertM3AnalyzeFails(t *testing.T, ctx context.Context, documentService *service.DocumentService, baseURL, fileName, content, expectedError string) {
	t.Helper()
	documentID := uploadFile(t, baseURL, fileName, []byte(content), map[string]string{
		"author": "M3 Tester",
		"title":  strings.TrimSuffix(fileName, ".txt"),
	})
	status, body := analyzeDocumentBody(t, baseURL, documentID)
	require.Equal(t, http.StatusInternalServerError, status, string(body))
	require.Contains(t, string(body), expectedError)

	document, err := documentService.GetDocumentByID(ctx, documentID)
	require.NoError(t, err)
	require.Equal(t, domain.DocumentStatusFailed, document.Status)
	plans, err := documentService.ListPlansByDocumentID(ctx, documentID)
	require.NoError(t, err)
	require.Empty(t, plans)
}

func requireM3PlanSymbols(t *testing.T, plans []domain.CandidatePlan, expected ...string) {
	t.Helper()
	found := make(map[string]struct{}, len(plans))
	for _, plan := range plans {
		found[plan.Symbol] = struct{}{}
	}
	for _, symbol := range expected {
		require.Contains(t, found, symbol)
	}
}

func requireM3PlanSymbolsNotContains(t *testing.T, plans []domain.CandidatePlan, unexpected string) {
	t.Helper()
	for _, plan := range plans {
		require.NotEqual(t, unexpected, plan.Symbol)
	}
}
