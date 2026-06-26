package agentclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"finance-sys/internal/agentclient"
	"finance-sys/internal/config"
	"finance-sys/internal/domain"

	"github.com/stretchr/testify/require"
)

func TestAnalyzerConvertsCandidatePlanInputs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "test-token", r.Header.Get("X-Agent-Token"))
		writeAgentResponse(t, w, agentclient.ResolveDocumentResponse{
			SchemaVersion: agentclient.ResponseSchemaVersion,
			AgentVersion:  "test-agent",
			Status:        agentclient.AgentStatusResolved,
			CandidatePlanInput: []agentclient.AgentCandidatePlanInput{
				testCandidatePlanInput("新易盛", "300502.SZ", "300502", "新易盛"),
			},
		})
	}))
	defer server.Close()

	analyzer := agentclient.NewAnalyzer(testRuntimeWithAgent(server.URL), nil)
	intents, err := analyzer.Analyze(context.Background(), domain.Document{
		ID:          10,
		Title:       "M4 analyzer",
		Author:      "M4 Tester",
		Institution: "Integration",
	}, domain.ParseRun{
		ID:          20,
		CleanedText: "推荐新易盛",
		Chunks:      []domain.Chunk{{Index: 0, Text: "推荐新易盛"}},
	})
	require.NoError(t, err)
	require.Len(t, intents, 1)
	require.Equal(t, "300502.SZ", intents[0].Symbol)
	require.Equal(t, domain.AssetTypeAShare, intents[0].AssetType)
	require.Equal(t, domain.MarketSZ, intents[0].Market)
	require.Equal(t, "M4 Tester", intents[0].Analyst)
}

func TestAnalyzerConvertsRawIntentsWhenNoCandidateInputs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeAgentResponse(t, w, agentclient.ResolveDocumentResponse{
			SchemaVersion: agentclient.ResponseSchemaVersion,
			AgentVersion:  "test-agent",
			Status:        agentclient.AgentStatusPartial,
			RawIntents:    []agentclient.AgentRawIntent{testRawIntent("CPO板块")},
		})
	}))
	defer server.Close()

	analyzer := agentclient.NewAnalyzer(testRuntimeWithAgent(server.URL), nil)
	intents, err := analyzer.Analyze(context.Background(), domain.Document{
		ID:          10,
		Author:      "M4 Tester",
		Institution: "Integration",
	}, domain.ParseRun{
		ID:          20,
		CleanedText: "推荐CPO板块",
		Chunks:      []domain.Chunk{{Index: 0, Text: "推荐CPO板块"}},
	})
	require.NoError(t, err)
	require.Len(t, intents, 1)
	require.Equal(t, "CPO板块", intents[0].Symbol)
}

func TestAnalyzerRejectsSchemaMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeAgentResponse(t, w, agentclient.ResolveDocumentResponse{
			SchemaVersion: "agent.resolve_document.response.v0",
			AgentVersion:  "test-agent",
			Status:        agentclient.AgentStatusResolved,
			RawIntents:    []agentclient.AgentRawIntent{testRawIntent("新易盛")},
		})
	}))
	defer server.Close()

	analyzer := agentclient.NewAnalyzer(testRuntimeWithAgent(server.URL), nil)
	_, err := analyzer.Analyze(context.Background(), domain.Document{ID: 10}, domain.ParseRun{
		ID:          20,
		CleanedText: "推荐新易盛",
		Chunks:      []domain.Chunk{{Index: 0, Text: "推荐新易盛"}},
	})
	require.ErrorContains(t, err, "agent schema_version mismatch")
}

func TestAnalyzerRejectsFailedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeAgentResponse(t, w, agentclient.ResolveDocumentResponse{
			SchemaVersion: agentclient.ResponseSchemaVersion,
			AgentVersion:  "test-agent",
			Status:        agentclient.AgentStatusFailed,
		})
	}))
	defer server.Close()

	analyzer := agentclient.NewAnalyzer(testRuntimeWithAgent(server.URL), nil)
	_, err := analyzer.Analyze(context.Background(), domain.Document{ID: 10}, domain.ParseRun{
		ID:          20,
		CleanedText: "推荐新易盛",
		Chunks:      []domain.Chunk{{Index: 0, Text: "推荐新易盛"}},
	})
	require.ErrorContains(t, err, "agent returned FAILED status")
}

func TestAnalyzerUsesTradeDateFromContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request agentclient.ResolveDocumentRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		require.Equal(t, "2026-04-13", request.TradeDate)
		writeAgentResponse(t, w, agentclient.ResolveDocumentResponse{
			SchemaVersion: agentclient.ResponseSchemaVersion,
			AgentVersion:  "test-agent",
			Status:        agentclient.AgentStatusPartial,
			RawIntents:    []agentclient.AgentRawIntent{testRawIntent("CPO鏉垮潡")},
		})
	}))
	defer server.Close()

	analyzer := agentclient.NewAnalyzer(testRuntimeWithAgent(server.URL), nil)
	ctx := domain.ContextWithTradeDate(context.Background(), time.Date(2026, 4, 13, 15, 30, 0, 0, time.FixedZone("CST", 8*60*60)))
	_, err := analyzer.Analyze(ctx, domain.Document{ID: 10}, domain.ParseRun{
		ID:          20,
		CleanedText: "鎺ㄨ崘CPO鏉垮潡",
		Chunks:      []domain.Chunk{{Index: 0, Text: "鎺ㄨ崘CPO鏉垮潡"}},
	})
	require.NoError(t, err)
}

func testRuntimeWithAgent(endpoint string) *config.Runtime {
	return config.NewRuntime(&config.Snapshot{
		Config: &config.Config{
			Meta: config.MetaConfig{
				Timezone:      "Asia/Shanghai",
				ConfigVersion: 1,
			},
			Rules: config.RulesConfig{
				TradeDateOffsetDays: 1,
			},
			Agent: testAgentConfig(endpoint, 0),
		},
	})
}

func testRawIntent(symbol string) agentclient.AgentRawIntent {
	return agentclient.AgentRawIntent{
		IntentID:           "intent-1",
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

func testCandidatePlanInput(rawSymbol, tsCode, symbol, name string) agentclient.AgentCandidatePlanInput {
	return agentclient.AgentCandidatePlanInput{
		IntentID:           "intent-1",
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
