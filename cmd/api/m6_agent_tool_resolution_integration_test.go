package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"finance-sys/internal/agentclient"
	"finance-sys/internal/config"
	"finance-sys/internal/domain"

	"github.com/stretchr/testify/require"
)

const (
	m6XinYiSheng = "\u65b0\u6613\u76db"
	m6XuChuang   = "\u65ed\u521b"
	m6CPOBoard   = "CPO\u677f\u5757"
)

func TestHTTPM6AnalyzeDocumentAgentResolvesStandardCodeWithNacosBootstrap(t *testing.T) {
	if os.Getenv("FINANCE_SYS_M6_NACOS_INTEGRATION") != "1" {
		t.Skip("set FINANCE_SYS_M6_NACOS_INTEGRATION=1 to run; this test writes to the Nacos-configured MySQL database")
	}
	if os.Getenv("FINANCE_SYS_M6_NACOS_DML_ACK") != "write-real-db" {
		t.Skip("set FINANCE_SYS_M6_NACOS_DML_ACK=write-real-db after manually acknowledging real database writes")
	}

	loadBootstrapEnvFile(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	app := buildIntegrationApp(t, ctx)
	defer app.Close()

	seedM1SecurityLookupFixtures(t, ctx, app.DB)

	var baseURL string
	var sawValid bool
	var sawInvalid bool
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/resolve-document", r.URL.Path)
		require.Equal(t, "m6-test-token", r.Header.Get("X-Agent-Token"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var request agentclient.ResolveDocumentRequest
		require.NoError(t, json.Unmarshal(body, &request))
		require.Equal(t, agentclient.RequestSchemaVersion, request.SchemaVersion)
		require.NotEmpty(t, request.Chunks)

		payload := string(body)
		switch {
		case bytes.Contains(body, []byte("M6_VALID_TOOL_SENTINEL")):
			sawValid = true
			xinYiSheng := m6VerifyFirstCandidate(t, baseURL, m6XinYiSheng)
			xuChuang := m6VerifyFirstCandidate(t, baseURL, m6XuChuang)
			writeM6AgentResponse(t, w, m6ValidAgentResponse(xinYiSheng, xuChuang))
		case bytes.Contains(body, []byte("M6_UNVERIFIED_TOOL_SENTINEL")):
			sawInvalid = true
			candidates := m6ResolveSecurity(t, baseURL, m6CPOBoard)
			require.Empty(t, candidates)
			m6RequireVerifyStatus(t, baseURL, "399999.SZ", http.StatusNotFound)
			writeM6AgentResponse(t, w, m6UnverifiedAgentResponse())
		default:
			http.Error(w, fmt.Sprintf("unknown M6 test sentinel in %s", payload), http.StatusBadRequest)
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
	cfg.Agent.InternalAPIBaseURL = "http://127.0.0.1:0"
	cfg.Agent.TimeoutMS = 5000
	cfg.Agent.MaxRetries = 0
	cfg.Agent.SchemaVersion = agentclient.ResponseSchemaVersion
	cfg.Agent.AllowLegacyLLMFallback = false
	cfg.Agent.Auth.Enabled = true
	cfg.Agent.Auth.HeaderName = "X-Agent-Token"
	cfg.Agent.Auth.StaticToken = "m6-test-token"
	updateM4RuntimeConfig(t, app, cfg)

	var shutdown func()
	baseURL, shutdown = startMainHTTPServerForTest(t, app)
	defer shutdown()

	unique := time.Now().UnixNano()
	validDocumentID := uploadFile(t, baseURL, fmt.Sprintf("m6-valid-tool-%d.txt", unique), []byte(fmt.Sprintf("M6_VALID_TOOL_SENTINEL %d recommend %s and %s, mention %s.", unique, m6XinYiSheng, m6XuChuang, m6CPOBoard)), map[string]string{
		"author":      "M6 Tester",
		"institution": "Integration",
		"title":       "M6 valid tool resolution",
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
	requireM3PlanSymbolsNotContains(t, validPayload.Plans, m6CPOBoard)
	require.True(t, sawValid)

	validDocument, err := app.DocumentService.GetDocumentByID(ctx, validDocumentID)
	require.NoError(t, err)
	require.Equal(t, domain.DocumentStatusPlanned, validDocument.Status)

	invalidDocumentID := uploadFile(t, baseURL, fmt.Sprintf("m6-unverified-tool-%d.txt", unique), []byte(fmt.Sprintf("M6_UNVERIFIED_TOOL_SENTINEL %d mention %s only.", unique, m6CPOBoard)), map[string]string{
		"author":      "M6 Tester",
		"institution": "Integration",
		"title":       "M6 unverified tool candidate",
	})
	invalidStatus, invalidBody := analyzeDocumentBody(t, baseURL, invalidDocumentID)
	require.Equal(t, http.StatusInternalServerError, invalidStatus, string(invalidBody))
	require.Contains(t, string(invalidBody), "no trackable securities resolved")
	require.True(t, sawInvalid)

	invalidDocument, err := app.DocumentService.GetDocumentByID(ctx, invalidDocumentID)
	require.NoError(t, err)
	require.Equal(t, domain.DocumentStatusFailed, invalidDocument.Status)
	plans, err := app.DocumentService.ListPlansByDocumentID(ctx, invalidDocumentID)
	require.NoError(t, err)
	require.Empty(t, plans)
}

func writeM6AgentResponse(t *testing.T, w http.ResponseWriter, response agentclient.ResolveDocumentResponse) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(response))
}

func m6ResolveSecurity(t *testing.T, baseURL, query string) []domain.InstrumentResolutionCandidate {
	t.Helper()
	body, err := json.Marshal(map[string]any{"query": query, "max_candidates": 5})
	require.NoError(t, err)
	resp, err := http.Post(baseURL+"/api/v1/internal/security/resolve", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(respBody))
	var payload struct {
		Candidates []domain.InstrumentResolutionCandidate `json:"candidates"`
	}
	require.NoError(t, json.Unmarshal(respBody, &payload))
	return payload.Candidates
}

func m6VerifyFirstCandidate(t *testing.T, baseURL, query string) domain.InstrumentResolutionCandidate {
	t.Helper()
	candidates := m6ResolveSecurity(t, baseURL, query)
	require.Len(t, candidates, 1)
	return m6VerifySecurity(t, baseURL, candidates[0].TSCode)
}

func m6VerifySecurity(t *testing.T, baseURL, tsCode string) domain.InstrumentResolutionCandidate {
	t.Helper()
	body, err := json.Marshal(map[string]any{"ts_code": tsCode})
	require.NoError(t, err)
	resp, err := http.Post(baseURL+"/api/v1/internal/security/verify", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(respBody))
	var payload struct {
		Verified bool                                 `json:"verified"`
		Security domain.InstrumentResolutionCandidate `json:"security"`
	}
	require.NoError(t, json.Unmarshal(respBody, &payload))
	require.True(t, payload.Verified)
	return payload.Security
}

func m6RequireVerifyStatus(t *testing.T, baseURL, tsCode string, expectedStatus int) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"ts_code": tsCode})
	require.NoError(t, err)
	resp, err := http.Post(baseURL+"/api/v1/internal/security/verify", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, expectedStatus, resp.StatusCode, string(respBody))
}

func m6ValidAgentResponse(xinYiSheng, xuChuang domain.InstrumentResolutionCandidate) agentclient.ResolveDocumentResponse {
	return agentclient.ResolveDocumentResponse{
		SchemaVersion: agentclient.ResponseSchemaVersion,
		AgentVersion:  "m6-test-agent",
		Status:        agentclient.AgentStatusPartial,
		RawIntents: []agentclient.AgentRawIntent{
			testM4AgentRawIntent(m6XinYiSheng),
			testM4AgentRawIntent(m6XuChuang),
			testM4AgentRawIntent(m6CPOBoard),
		},
		CandidatePlanInput: []agentclient.AgentCandidatePlanInput{
			m6CandidateInputFromVerified(m6XinYiSheng, xinYiSheng),
			m6CandidateInputFromVerified(m6XuChuang, xuChuang),
		},
		UntrackableTargets: []agentclient.AgentUntrackableTarget{
			{
				RawSymbol:  m6CPOBoard,
				TargetKind: "SECTOR",
				Reason:     "sector is not a single tradable security",
				Evidence:   []domain.EvidenceSpan{{ChunkIndex: 0, Text: m6CPOBoard}},
			},
		},
		Warnings: []string{"m6 test partial resolution"},
		Debug: agentclient.AgentDebug{
			GraphRunID:   "m6-tool-resolution",
			Nodes:        []string{"load_skill", "extract_raw_intents", "resolve_with_local_security", "resolve_with_external_tools", "verify_external_candidates"},
			ToolsUsed:    []string{"local_security_lookup_tool", "tushare_stock_basic_tool", "local_security_verify_tool"},
			DurationMS:   1,
			SkillName:    "instrument_resolution",
			SkillVersion: "instrument-resolution-m5-v1",
			SkillHash:    m5ValidSkillHash,
		},
	}
}

func m6UnverifiedAgentResponse() agentclient.ResolveDocumentResponse {
	return agentclient.ResolveDocumentResponse{
		SchemaVersion: agentclient.ResponseSchemaVersion,
		AgentVersion:  "m6-test-agent",
		Status:        agentclient.AgentStatusPartial,
		RawIntents:    []agentclient.AgentRawIntent{testM4AgentRawIntent(m6CPOBoard)},
		Warnings:      []string{"external candidate 399999.SZ was not verified locally"},
		Debug: agentclient.AgentDebug{
			GraphRunID:   "m6-unverified-tool",
			Nodes:        []string{"load_skill", "extract_raw_intents", "resolve_with_local_security", "resolve_with_external_tools", "verify_external_candidates"},
			ToolsUsed:    []string{"local_security_lookup_tool", "tushare_stock_basic_tool", "local_security_verify_tool"},
			DurationMS:   1,
			SkillName:    "instrument_resolution",
			SkillVersion: "instrument-resolution-m5-v1",
			SkillHash:    m5ValidSkillHash,
		},
	}
}

func m6CandidateInputFromVerified(rawSymbol string, candidate domain.InstrumentResolutionCandidate) agentclient.AgentCandidatePlanInput {
	assetType := string(candidate.AssetType)
	if assetType == string(domain.AssetTypeAShare) {
		assetType = "STOCK"
	}
	return agentclient.AgentCandidatePlanInput{
		IntentID:           "intent-" + rawSymbol,
		RawSymbol:          rawSymbol,
		Security:           agentclient.AgentSecurity{TSCode: candidate.TSCode, Symbol: candidate.Symbol, Name: candidate.Name, AssetType: assetType, Market: string(candidate.Market)},
		Direction:          domain.TradeDirectionLong,
		ReferencePrice:     88.8,
		ReferencePriceNote: domain.ReferencePriceNoteExplicitPriceMention,
		Thesis:             "source text supports recommendation",
		Evidence:           []domain.EvidenceSpan{{ChunkIndex: 0, Text: "source evidence"}},
		Risks:              []string{"volatility"},
		Confidence:         0.81,
	}
}
