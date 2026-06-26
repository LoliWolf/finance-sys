package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/domain"

	"github.com/stretchr/testify/require"
)

func TestHTTPM0AnalyzeRejectsInvalidSymbolsWithNacosBootstrap(t *testing.T) {
	if os.Getenv("FINANCE_SYS_M0_NACOS_INTEGRATION") != "1" {
		t.Skip("set FINANCE_SYS_M0_NACOS_INTEGRATION=1 to run; this test writes to the Nacos-configured MySQL database")
	}
	if os.Getenv("FINANCE_SYS_M0_NACOS_DML_ACK") != "write-real-db" {
		t.Skip("set FINANCE_SYS_M0_NACOS_DML_ACK=write-real-db after manually acknowledging real database writes")
	}

	loadBootstrapEnvFile(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	app := buildIntegrationApp(t, ctx)
	defer app.Close()
	seedM1SecurityLookupFixtures(t, ctx, app.DB)

	var invalidAttempts atomic.Int32
	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		switch {
		case strings.Contains(string(body), "M0_VALID_SENTINEL"):
			writeChatCompletion(t, w, "300502.SZ")
		case strings.Contains(string(body), "M0_INVALID_SENTINEL"):
			invalidAttempts.Add(1)
			writeChatCompletion(t, w, "CPO板块")
		default:
			writeChatCompletion(t, w, "A股贵金属个股")
		}
	}))
	defer llmServer.Close()

	cfg := cloneConfig(t, app.Runtime.Config())
	cfg.Security.Auth.Enabled = false
	cfg.Document.APIUploadEnabled = true
	cfg.Document.AutoAnalyzeUpload = false
	cfg.Document.SHA256Dedup = true
	cfg.LLM.Enabled = true
	cfg.LLM.Provider = config.LLMProviderOpenAICompatible
	cfg.LLM.Endpoint = llmServer.URL
	cfg.LLM.APIKey = "m0-test-key"
	cfg.LLM.Model = "m0-test-model"
	cfg.LLM.TimeoutMS = 5000
	cfg.LLM.MaxRetries = 1
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
	validDocumentID := uploadFile(t, baseURL, fmt.Sprintf("m0-valid-%d.txt", unique), []byte(fmt.Sprintf("M0_VALID_SENTINEL %d recommend 300502.SZ reference price 88.8", unique)), map[string]string{
		"author": "M0 Tester",
		"title":  "M0 valid symbol",
	})
	validStatus, validBody := analyzeDocumentBody(t, baseURL, validDocumentID)
	require.Equal(t, http.StatusOK, validStatus, string(validBody))
	var validPayload struct {
		Status string                 `json:"status"`
		Plans  []domain.CandidatePlan `json:"plans"`
	}
	require.NoError(t, json.Unmarshal(validBody, &validPayload))
	require.Equal(t, "planned", validPayload.Status)
	require.Len(t, validPayload.Plans, 1)
	require.Equal(t, "300502.SZ", validPayload.Plans[0].Symbol)

	invalidDocumentID := uploadFile(t, baseURL, fmt.Sprintf("m0-invalid-%d.txt", unique), []byte(fmt.Sprintf("M0_INVALID_SENTINEL %d mentions CPO sector only", unique)), map[string]string{
		"author": "M0 Tester",
		"title":  "M0 invalid symbol",
	})
	invalidStatus, invalidBody := analyzeDocumentBody(t, baseURL, invalidDocumentID)
	require.Equal(t, http.StatusInternalServerError, invalidStatus, string(invalidBody))
	require.Contains(t, string(invalidBody), "no trackable securities resolved")
	require.Equal(t, int32(1), invalidAttempts.Load())

	invalidDocument, err := app.DocumentService.GetDocumentByID(ctx, invalidDocumentID)
	require.NoError(t, err)
	require.Equal(t, domain.DocumentStatusInvalid, invalidDocument.Status)
	invalidPlans, err := app.DocumentService.ListPlansByDocumentID(ctx, invalidDocumentID)
	require.NoError(t, err)
	require.Empty(t, invalidPlans)
}

func loadBootstrapEnvFile(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "bootstrap_go122.env.example"))
	require.NoError(t, err)
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		t.Setenv(strings.TrimSpace(key), strings.TrimSpace(value))
	}
}

func writeChatCompletion(t *testing.T, w http.ResponseWriter, symbol string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	content := fmt.Sprintf(`{"plans":[{"analyst":"M0 Tester","institution":"Integration","symbol":%q,"direction":"LONG","reference_price":88.8,"reference_price_note":"explicit_price_mention","thesis":"explicit recommendation from source text","evidence":[{"chunk_index":0,"text":"source evidence"}],"risks":["volatility"],"confidence":0.81}]}`, symbol)
	require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
		"choices": []any{
			map[string]any{
				"message": map[string]any{
					"content": content,
				},
			},
		},
	}))
}

func analyzeDocumentBody(t *testing.T, baseURL string, documentID int64) (int, []byte) {
	t.Helper()
	resp, err := http.Post(fmt.Sprintf("%s/api/v1/documents/%d/analyze", baseURL, documentID), "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, body
}
