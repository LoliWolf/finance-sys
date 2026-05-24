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

func TestHTTPM7ResolutionObservationWithNacosBootstrap(t *testing.T) {
	if os.Getenv("FINANCE_SYS_M7_NACOS_INTEGRATION") != "1" {
		t.Skip("set FINANCE_SYS_M7_NACOS_INTEGRATION=1 to run; this test writes to the Nacos-configured MySQL database")
	}
	if os.Getenv("FINANCE_SYS_M7_NACOS_DML_ACK") != "write-real-db" {
		t.Skip("set FINANCE_SYS_M7_NACOS_DML_ACK=write-real-db after manually acknowledging real database writes")
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
		require.Equal(t, "m7-test-token", r.Header.Get("X-Agent-Token"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var request agentclient.ResolveDocumentRequest
		require.NoError(t, json.Unmarshal(body, &request))
		require.Equal(t, agentclient.RequestSchemaVersion, request.SchemaVersion)
		require.NotEmpty(t, request.Chunks)

		switch {
		case bytes.Contains(body, []byte("M7_VALID_OBSERVATION_SENTINEL")):
			sawValid = true
			xinYiSheng := m6VerifyFirstCandidate(t, baseURL, m6XinYiSheng)
			xuChuang := m6VerifyFirstCandidate(t, baseURL, m6XuChuang)
			writeM7AgentResponse(t, w, m7ValidAgentResponse(xinYiSheng, xuChuang))
		case bytes.Contains(body, []byte("M7_UNTRACKABLE_OBSERVATION_SENTINEL")):
			sawInvalid = true
			candidates := m6ResolveSecurity(t, baseURL, m6CPOBoard)
			require.Empty(t, candidates)
			m6RequireVerifyStatus(t, baseURL, "399999.SZ", http.StatusNotFound)
			writeM7AgentResponse(t, w, m7UntrackableOnlyAgentResponse())
		default:
			http.Error(w, "unknown M7 test sentinel", http.StatusBadRequest)
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
	cfg.Agent.Auth.StaticToken = "m7-test-token"
	cfg.Agent.Observation = config.ObservationConfig{
		Enabled:           true,
		PersistSuccess:    true,
		PersistFailure:    true,
		PersistToolTraces: true,
		ShadowSampleRate:  1,
		MaxTargetsPerRun:  100,
		MaxJSONBytes:      65535,
		RetentionDays:     90,
	}
	updateM4RuntimeConfig(t, app, cfg)

	var shutdown func()
	baseURL, shutdown = startMainHTTPServerForTest(t, app)
	defer shutdown()

	unique := time.Now().UnixNano()
	validDocumentID := uploadFile(t, baseURL, fmt.Sprintf("m7-valid-observation-%d.txt", unique), []byte(fmt.Sprintf("M7_VALID_OBSERVATION_SENTINEL %d recommend %s and %s, mention %s.", unique, m6XinYiSheng, m6XuChuang, m6CPOBoard)), map[string]string{
		"author":      "M7 Tester",
		"institution": "Integration",
		"title":       "M7 valid observation",
	})
	validStatus, validBody := analyzeDocumentBody(t, baseURL, validDocumentID)
	require.Equal(t, http.StatusOK, validStatus, string(validBody))
	require.True(t, sawValid)

	validRuns := m7ListResolutionRuns(t, baseURL, validDocumentID)
	require.NotEmpty(t, validRuns.Items)
	validRun := validRuns.Items[0]
	require.Equal(t, domain.ResolutionRunStatusSucceeded, validRun.Status)
	require.Equal(t, "primary", validRun.AgentMode)
	require.Equal(t, string(domain.ResolutionRouteAgentPrimary), validRun.Route)
	require.Equal(t, m5ValidSkillHash, validRun.SkillHash)
	require.Equal(t, 3, validRun.RawTargetCount)
	require.Equal(t, 2, validRun.CandidatePlanInputCount)
	require.Equal(t, 2, validRun.CandidatePlanCount)
	require.Equal(t, 1, validRun.UntrackableCount)
	require.Equal(t, 3, validRun.ToolCallCount)
	require.False(t, validRun.FallbackUsed)

	validRunDetail := m7GetResolutionRun(t, baseURL, validRun.ID)
	require.Len(t, validRunDetail.Targets, 3)
	require.Len(t, validRunDetail.ToolTraces, 3)
	require.NotContains(t, string(mustJSON(t, validRunDetail.ToolTraces)), "m7-test-token")
	require.NotContains(t, string(mustJSON(t, validRunDetail.RawMetadata)), "m7-test-token")

	validUntrackables := m7ListUntrackableTargets(t, baseURL, validDocumentID)
	require.Len(t, validUntrackables.Items, 1)
	require.Equal(t, m6CPOBoard, validUntrackables.Items[0].RawTarget)
	require.Equal(t, string(domain.UntrackableReasonSectorNotTradable), validUntrackables.Items[0].ReasonCode)
	require.Equal(t, domain.InstrumentTargetKindSector, validUntrackables.Items[0].TargetKind)
	require.Equal(t, "agent", validUntrackables.Items[0].Source)

	invalidDocumentID := uploadFile(t, baseURL, fmt.Sprintf("m7-untrackable-observation-%d.txt", unique), []byte(fmt.Sprintf("M7_UNTRACKABLE_OBSERVATION_SENTINEL %d mention %s only.", unique, m6CPOBoard)), map[string]string{
		"author":      "M7 Tester",
		"institution": "Integration",
		"title":       "M7 untrackable observation",
	})
	invalidStatus, invalidBody := analyzeDocumentBody(t, baseURL, invalidDocumentID)
	require.Equal(t, http.StatusInternalServerError, invalidStatus, string(invalidBody))
	require.Contains(t, string(invalidBody), "no trackable securities resolved")
	require.True(t, sawInvalid)

	invalidRuns := m7ListResolutionRuns(t, baseURL, invalidDocumentID)
	require.NotEmpty(t, invalidRuns.Items)
	invalidRun := invalidRuns.Items[0]
	require.Equal(t, domain.ResolutionRunStatusFailed, invalidRun.Status)
	require.Equal(t, string(domain.ResolutionRouteAgentPrimary), invalidRun.Route)
	require.Equal(t, 1, invalidRun.RawTargetCount)
	require.Equal(t, 0, invalidRun.CandidatePlanInputCount)
	require.Equal(t, 0, invalidRun.CandidatePlanCount)
	require.Equal(t, 1, invalidRun.UntrackableCount)
	require.NotEmpty(t, invalidRun.ErrorCode)
	require.Contains(t, invalidRun.ErrorMessage, "no trackable securities resolved")

	invalidUntrackables := m7ListUntrackableTargets(t, baseURL, invalidDocumentID)
	require.Len(t, invalidUntrackables.Items, 1)
	require.Equal(t, m6CPOBoard, invalidUntrackables.Items[0].RawTarget)
	require.Equal(t, string(domain.UntrackableReasonSectorNotTradable), invalidUntrackables.Items[0].ReasonCode)

	invalidDocument, err := app.DocumentService.GetDocumentByID(ctx, invalidDocumentID)
	require.NoError(t, err)
	require.Equal(t, domain.DocumentStatusFailed, invalidDocument.Status)
	plans, err := app.DocumentService.ListPlansByDocumentID(ctx, invalidDocumentID)
	require.NoError(t, err)
	require.Empty(t, plans)
}

type m7ResolutionRunsPayload struct {
	DocumentID int64                  `json:"document_id"`
	Items      []domain.ResolutionRun `json:"items"`
}

type m7UntrackableTargetsPayload struct {
	DocumentID int64                      `json:"document_id"`
	Items      []domain.UntrackableTarget `json:"items"`
}

func writeM7AgentResponse(t *testing.T, w http.ResponseWriter, response agentclient.ResolveDocumentResponse) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(response))
}

func m7ValidAgentResponse(xinYiSheng, xuChuang domain.InstrumentResolutionCandidate) agentclient.ResolveDocumentResponse {
	response := m6ValidAgentResponse(xinYiSheng, xuChuang)
	response.AgentVersion = "m7-test-agent"
	response.Debug.GraphRunID = "m7-valid-observation"
	response.Debug.DurationMS = 7
	return response
}

func m7UntrackableOnlyAgentResponse() agentclient.ResolveDocumentResponse {
	return agentclient.ResolveDocumentResponse{
		SchemaVersion: agentclient.ResponseSchemaVersion,
		AgentVersion:  "m7-test-agent",
		Status:        agentclient.AgentStatusPartial,
		RawIntents:    []agentclient.AgentRawIntent{testM4AgentRawIntent(m6CPOBoard)},
		UntrackableTargets: []agentclient.AgentUntrackableTarget{
			{
				RawSymbol:  m6CPOBoard,
				TargetKind: "SECTOR",
				Reason:     "sector is not a single tradable security",
				Evidence:   []domain.EvidenceSpan{{ChunkIndex: 0, Text: m6CPOBoard}},
			},
		},
		Warnings: []string{"m7 untrackable only"},
		Debug: agentclient.AgentDebug{
			GraphRunID:   "m7-untrackable-observation",
			Nodes:        []string{"load_skill", "extract_raw_intents", "resolve_with_local_security", "resolve_with_external_tools", "verify_external_candidates"},
			ToolsUsed:    []string{"local_security_lookup_tool", "tushare_stock_basic_tool", "local_security_verify_tool"},
			DurationMS:   5,
			SkillName:    "instrument_resolution",
			SkillVersion: "instrument-resolution-m5-v1",
			SkillHash:    m5ValidSkillHash,
		},
	}
}

func m7ListResolutionRuns(t *testing.T, baseURL string, documentID int64) m7ResolutionRunsPayload {
	t.Helper()
	var payload m7ResolutionRunsPayload
	m7GetJSON(t, fmt.Sprintf("%s/api/v1/documents/%d/resolution-runs", baseURL, documentID), &payload)
	return payload
}

func m7GetResolutionRun(t *testing.T, baseURL string, runID int64) domain.ResolutionRun {
	t.Helper()
	var payload domain.ResolutionRun
	m7GetJSON(t, fmt.Sprintf("%s/api/v1/resolution-runs/%d", baseURL, runID), &payload)
	return payload
}

func m7ListUntrackableTargets(t *testing.T, baseURL string, documentID int64) m7UntrackableTargetsPayload {
	t.Helper()
	var payload m7UntrackableTargetsPayload
	m7GetJSON(t, fmt.Sprintf("%s/api/v1/documents/%d/untrackable-targets", baseURL, documentID), &payload)
	return payload
}

func m7GetJSON(t *testing.T, url string, target any) {
	t.Helper()
	resp, err := http.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	require.NoError(t, json.Unmarshal(body, target))
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return raw
}
