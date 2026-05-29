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
	"sync/atomic"
	"testing"
	"time"

	"finance-sys/internal/agentclient"
	"finance-sys/internal/bootstrap"
	"finance-sys/internal/config"
	"finance-sys/internal/domain"

	"github.com/stretchr/testify/require"
)

func TestHTTPM4AnalyzeDocumentUsesAgentWithNacosBootstrap(t *testing.T) {
	if os.Getenv("FINANCE_SYS_M4_NACOS_INTEGRATION") != "1" {
		t.Skip("set FINANCE_SYS_M4_NACOS_INTEGRATION=1 to run; this test writes to the Nacos-configured MySQL database")
	}
	if os.Getenv("FINANCE_SYS_M4_NACOS_DML_ACK") != "write-real-db" {
		t.Skip("set FINANCE_SYS_M4_NACOS_DML_ACK=write-real-db after manually acknowledging real database writes")
	}

	loadBootstrapEnvFile(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	app := buildIntegrationApp(t, ctx)
	defer app.Close()

	seedM1SecurityLookupFixtures(t, ctx, app.DB)

	var timeoutAttempts atomic.Int32
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/resolve-document", r.URL.Path)
		require.Equal(t, "m4-test-token", r.Header.Get("X-Agent-Token"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		payload := string(body)
		var request agentclient.ResolveDocumentRequest
		require.NoError(t, json.Unmarshal(body, &request))
		require.Equal(t, agentclient.RequestSchemaVersion, request.SchemaVersion)
		require.NotEmpty(t, request.RequestID)
		require.NotEmpty(t, request.Chunks)

		switch {
		case strings.Contains(payload, "M4_VALID_CANDIDATE_SENTINEL"):
			writeM4AgentResponse(t, w, agentclient.ResolveDocumentResponse{
				SchemaVersion: agentclient.ResponseSchemaVersion,
				AgentVersion:  "m4-test-agent",
				Status:        agentclient.AgentStatusPartial,
				RawIntents: []agentclient.AgentRawIntent{
					testM4AgentRawIntent("新易盛"),
					testM4AgentRawIntent("旭创"),
					testM4AgentRawIntent("CPO板块"),
				},
				CandidatePlanInput: []agentclient.AgentCandidatePlanInput{
					testM4AgentCandidateInput("新易盛", "300502.SZ", "300502", "新易盛"),
					testM4AgentCandidateInput("旭创", "300308.SZ", "300308", "中际旭创"),
				},
				UntrackableTargets: []agentclient.AgentUntrackableTarget{
					{
						RawSymbol:  "CPO板块",
						TargetKind: "SECTOR",
						Reason:     "sector is not a single tradable security",
						Evidence:   []domain.EvidenceSpan{{ChunkIndex: 0, Text: "CPO板块"}},
					},
				},
				Warnings: []string{"m4 test partial resolution"},
				Debug:    agentclient.AgentDebug{GraphRunID: "m4-valid-candidate", Nodes: []string{"extract_raw_intents", "resolve_candidates"}, DurationMS: 1},
			})
		case strings.Contains(payload, "M4_RAW_FALLBACK_SENTINEL"):
			writeM4AgentResponse(t, w, agentclient.ResolveDocumentResponse{
				SchemaVersion: agentclient.ResponseSchemaVersion,
				AgentVersion:  "m4-test-agent",
				Status:        agentclient.AgentStatusResolved,
				RawIntents: []agentclient.AgentRawIntent{
					testM4AgentRawIntent("新易盛"),
					testM4AgentRawIntent("旭创"),
					testM4AgentRawIntent("CPO板块"),
				},
				Debug: agentclient.AgentDebug{GraphRunID: "m4-raw-fallback", Nodes: []string{"extract_raw_intents"}, DurationMS: 1},
			})
		case strings.Contains(payload, "M4_INVALID_SCHEMA_SENTINEL"):
			writeM4AgentResponse(t, w, agentclient.ResolveDocumentResponse{
				SchemaVersion: "agent.resolve_document.response.v0",
				AgentVersion:  "m4-test-agent",
				Status:        agentclient.AgentStatusResolved,
				RawIntents:    []agentclient.AgentRawIntent{testM4AgentRawIntent("新易盛")},
			})
		case strings.Contains(payload, "M4_FAILED_STATUS_SENTINEL"):
			writeM4AgentResponse(t, w, agentclient.ResolveDocumentResponse{
				SchemaVersion: agentclient.ResponseSchemaVersion,
				AgentVersion:  "m4-test-agent",
				Status:        agentclient.AgentStatusFailed,
				Warnings:      []string{"forced failure"},
			})
		case strings.Contains(payload, "M4_TIMEOUT_SENTINEL"):
			timeoutAttempts.Add(1)
			time.Sleep(80 * time.Millisecond)
			writeM4AgentResponse(t, w, agentclient.ResolveDocumentResponse{
				SchemaVersion: agentclient.ResponseSchemaVersion,
				AgentVersion:  "m4-test-agent",
				Status:        agentclient.AgentStatusResolved,
				RawIntents:    []agentclient.AgentRawIntent{testM4AgentRawIntent("新易盛")},
			})
		default:
			http.Error(w, "unknown M4 test sentinel", http.StatusBadRequest)
		}
	}))
	defer agentServer.Close()

	cfg := cloneConfig(t, app.Runtime.Config())
	cfg.Security.Auth.Enabled = false
	cfg.Document.APIUploadEnabled = true
	cfg.Document.AutoAnalyzeUpload = false
	cfg.Document.SHA256Dedup = false
	cfg.Agent.Enabled = true
	cfg.Agent.Mode = config.AgentModePrimary
	cfg.Agent.Endpoint = agentServer.URL + "/v1/resolve-document"
	cfg.Agent.HealthEndpoint = agentServer.URL + "/healthz"
	cfg.Agent.TimeoutMS = 5000
	cfg.Agent.MaxRetries = 0
	cfg.Agent.SchemaVersion = agentclient.ResponseSchemaVersion
	cfg.Agent.AllowLegacyLLMFallback = false
	cfg.Agent.Auth.Enabled = true
	cfg.Agent.Auth.HeaderName = "X-Agent-Token"
	cfg.Agent.Auth.StaticToken = "m4-test-token"
	updateM4RuntimeConfig(t, app, cfg)

	baseURL, shutdown := startMainHTTPServerForTest(t, app)
	defer shutdown()

	unique := time.Now().UnixNano()
	validDocumentID := uploadFile(t, baseURL, fmt.Sprintf("m4-valid-candidate-%d.txt", unique), []byte(fmt.Sprintf("M4_VALID_CANDIDATE_SENTINEL %d 推荐新易盛和旭创，同时提到CPO板块。", unique)), map[string]string{
		"author":      "M4 Tester",
		"institution": "Integration",
		"title":       "M4 valid candidate resolution",
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

	rawDocumentID := uploadFile(t, baseURL, fmt.Sprintf("m4-raw-fallback-%d.txt", unique), []byte(fmt.Sprintf("M4_RAW_FALLBACK_SENTINEL %d 推荐新易盛和旭创，同时提到CPO板块。", unique)), map[string]string{
		"author":      "M4 Tester",
		"institution": "Integration",
		"title":       "M4 raw fallback resolution",
	})
	rawStatus, rawBody := analyzeDocumentBody(t, baseURL, rawDocumentID)
	require.Equal(t, http.StatusOK, rawStatus, string(rawBody))
	var rawPayload struct {
		Status string                 `json:"status"`
		Plans  []domain.CandidatePlan `json:"plans"`
	}
	require.NoError(t, json.Unmarshal(rawBody, &rawPayload))
	require.Equal(t, "planned", rawPayload.Status)
	require.Len(t, rawPayload.Plans, 2)
	requireM3PlanSymbols(t, rawPayload.Plans, "300502.SZ", "300308.SZ")
	requireM3PlanSymbolsNotContains(t, rawPayload.Plans, "CPO板块")

	assertM4AnalyzeFails(t, ctx, app, baseURL, fmt.Sprintf("m4-invalid-schema-%d.txt", unique), fmt.Sprintf("M4_INVALID_SCHEMA_SENTINEL %d 推荐新易盛。", unique), "agent schema_version mismatch")
	assertM4AnalyzeFails(t, ctx, app, baseURL, fmt.Sprintf("m4-failed-status-%d.txt", unique), fmt.Sprintf("M4_FAILED_STATUS_SENTINEL %d 推荐新易盛。", unique), "agent returned FAILED status")

	timeoutCfg := cloneConfig(t, app.Runtime.Config())
	timeoutCfg.Agent.TimeoutMS = 10
	timeoutCfg.Agent.MaxRetries = 1
	updateM4RuntimeConfig(t, app, timeoutCfg)
	assertM4AnalyzeFails(t, ctx, app, baseURL, fmt.Sprintf("m4-timeout-%d.txt", unique), fmt.Sprintf("M4_TIMEOUT_SENTINEL %d 推荐新易盛。", unique), "agent resolve document failed after 2 attempts")
	require.Equal(t, int32(2), timeoutAttempts.Load())
}

func updateM4RuntimeConfig(t *testing.T, app *bootstrap.App, cfg *config.Config) {
	t.Helper()
	app.Runtime.Update(&config.Snapshot{
		Config:   cfg,
		Source:   app.Runtime.Current().Source,
		SHA256:   app.Runtime.Current().SHA256,
		LoadedAt: app.Runtime.Current().LoadedAt,
		Raw:      app.Runtime.Current().Raw,
	})
}

func writeM4AgentResponse(t *testing.T, w http.ResponseWriter, response agentclient.ResolveDocumentResponse) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(response))
}

func testM4AgentRawIntent(symbol string) agentclient.AgentRawIntent {
	return agentclient.AgentRawIntent{
		IntentID:           "intent-" + symbol,
		RawSymbol:          symbol,
		Direction:          domain.TradeDirectionLong,
		ReferencePrice:     88.8,
		ReferencePriceNote: domain.ReferencePriceNoteExplicitPriceMention,
		Thesis:             "source text supports recommendation",
		Evidence:           []domain.EvidenceSpan{{ChunkIndex: 0, Text: "source evidence"}},
		Risks:              []string{"volatility"},
		Confidence:         0.81,
	}
}

func testM4AgentCandidateInput(rawSymbol, tsCode, symbol, name string) agentclient.AgentCandidatePlanInput {
	return agentclient.AgentCandidatePlanInput{
		IntentID:           "intent-" + rawSymbol,
		RawSymbol:          rawSymbol,
		Security:           agentclient.AgentSecurity{TSCode: tsCode, Symbol: symbol, Name: name, AssetType: "STOCK", Market: "SZ"},
		Direction:          domain.TradeDirectionLong,
		ReferencePrice:     88.8,
		ReferencePriceNote: domain.ReferencePriceNoteExplicitPriceMention,
		Thesis:             "source text supports recommendation",
		Evidence:           []domain.EvidenceSpan{{ChunkIndex: 0, Text: "source evidence"}},
		Risks:              []string{"volatility"},
		Confidence:         0.81,
	}
}

func assertM4AnalyzeFails(t *testing.T, ctx context.Context, app *bootstrap.App, baseURL, fileName, content, expectedError string) {
	t.Helper()
	documentID := uploadFile(t, baseURL, fileName, []byte(content), map[string]string{
		"author":      "M4 Tester",
		"institution": "Integration",
		"title":       strings.TrimSuffix(fileName, ".txt"),
	})
	status, body := analyzeDocumentBody(t, baseURL, documentID)
	require.Equal(t, http.StatusInternalServerError, status, string(body))
	require.Contains(t, string(body), expectedError)

	document, err := app.DocumentService.GetDocumentByID(ctx, documentID)
	require.NoError(t, err)
	require.Equal(t, domain.DocumentStatusFailed, document.Status)
	plans, err := app.DocumentService.ListPlansByDocumentID(ctx, documentID)
	require.NoError(t, err)
	require.Empty(t, plans)
}
