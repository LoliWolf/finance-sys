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

	"finance-sys/internal/agentclient"
	"finance-sys/internal/config"
	"finance-sys/internal/domain"

	"github.com/stretchr/testify/require"
)

const m5ValidSkillHash = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestHTTPM5AnalyzeDocumentCarriesSkillHashWithNacosBootstrap(t *testing.T) {
	if os.Getenv("FINANCE_SYS_M5_NACOS_INTEGRATION") != "1" {
		t.Skip("set FINANCE_SYS_M5_NACOS_INTEGRATION=1 to run; this test writes to the Nacos-configured MySQL database")
	}
	if os.Getenv("FINANCE_SYS_M5_NACOS_DML_ACK") != "write-real-db" {
		t.Skip("set FINANCE_SYS_M5_NACOS_DML_ACK=write-real-db after manually acknowledging real database writes")
	}

	loadBootstrapEnvFile(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	app := buildIntegrationApp(t, ctx)
	defer app.Close()

	seedM1SecurityLookupFixtures(t, ctx, app.DB)

	var sawSkillHash bool
	var sawInvalidSkillHash bool
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/resolve-document", r.URL.Path)
		require.Equal(t, "m5-test-token", r.Header.Get("X-Agent-Token"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var request agentclient.ResolveDocumentRequest
		require.NoError(t, json.Unmarshal(body, &request))
		require.Equal(t, agentclient.RequestSchemaVersion, request.SchemaVersion)
		require.NotEmpty(t, request.Chunks)

		payload := string(body)
		switch {
		case strings.Contains(payload, "M5_VALID_SKILL_HASH_SENTINEL"):
			sawSkillHash = true
			writeM5AgentResponse(t, w, m5AgentResponse(m5ValidSkillHash))
		case strings.Contains(payload, "M5_INVALID_SKILL_HASH_SENTINEL"):
			sawInvalidSkillHash = true
			writeM5AgentResponse(t, w, m5AgentResponse("sha256:invalid"))
		default:
			http.Error(w, "unknown M5 test sentinel", http.StatusBadRequest)
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
	cfg.Agent.Auth.StaticToken = "m5-test-token"
	updateM4RuntimeConfig(t, app, cfg)

	baseURL, shutdown := startMainHTTPServerForTest(t, app)
	defer shutdown()

	unique := time.Now().UnixNano()
	validDocumentID := uploadFile(t, baseURL, fmt.Sprintf("m5-valid-skill-hash-%d.txt", unique), []byte(fmt.Sprintf("M5_VALID_SKILL_HASH_SENTINEL %d 推荐新易盛和旭创，同时提到CPO板块。", unique)), map[string]string{
		"author":      "M5 Tester",
		"institution": "Integration",
		"title":       "M5 valid skill hash",
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
	require.True(t, sawSkillHash)

	invalidDocumentID := uploadFile(t, baseURL, fmt.Sprintf("m5-invalid-skill-hash-%d.txt", unique), []byte(fmt.Sprintf("M5_INVALID_SKILL_HASH_SENTINEL %d 推荐新易盛。", unique)), map[string]string{
		"author":      "M5 Tester",
		"institution": "Integration",
		"title":       "M5 invalid skill hash",
	})
	invalidStatus, invalidBody := analyzeDocumentBody(t, baseURL, invalidDocumentID)
	require.Equal(t, http.StatusInternalServerError, invalidStatus, string(invalidBody))
	require.Contains(t, string(invalidBody), "agent debug.skill_hash")
	require.True(t, sawInvalidSkillHash)

	invalidDocument, err := app.DocumentService.GetDocumentByID(ctx, invalidDocumentID)
	require.NoError(t, err)
	require.Equal(t, domain.DocumentStatusFailed, invalidDocument.Status)
	plans, err := app.DocumentService.ListPlansByDocumentID(ctx, invalidDocumentID)
	require.NoError(t, err)
	require.Empty(t, plans)
}

func writeM5AgentResponse(t *testing.T, w http.ResponseWriter, response agentclient.ResolveDocumentResponse) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(response))
}

func m5AgentResponse(skillHash string) agentclient.ResolveDocumentResponse {
	return agentclient.ResolveDocumentResponse{
		SchemaVersion: agentclient.ResponseSchemaVersion,
		AgentVersion:  "m5-test-agent",
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
		Warnings: []string{"m5 test partial resolution"},
		Debug: agentclient.AgentDebug{
			GraphRunID:   "m5-skill-loader",
			Nodes:        []string{"load_skill", "extract_raw_intents", "resolve_candidates"},
			DurationMS:   1,
			SkillName:    "instrument_resolution",
			SkillVersion: "instrument-resolution-m5-v1",
			SkillHash:    skillHash,
		},
	}
}
