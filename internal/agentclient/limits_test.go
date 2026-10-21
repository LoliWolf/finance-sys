package agentclient_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"finance-sys/internal/agentclient"
	"finance-sys/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestAnalyzerRequestsUnlimitedDefaultAndPreservesLongResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request agentclient.ResolveDocumentRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		require.Zero(t, request.Limits.MaxIntents)
		require.Zero(t, request.Limits.MaxUntrackableTargets)
		items := make([]agentclient.AgentRawIntent, 72)
		for i := range items {
			items[i] = testRawIntent(fmt.Sprintf("%06d.SH", 600000+i))
		}
		writeAgentResponse(t, w, agentclient.ResolveDocumentResponse{
			SchemaVersion: agentclient.ResponseSchemaVersion, AgentVersion: "test-agent",
			Status: agentclient.AgentStatusResolved, RawIntents: items,
		})
	}))
	defer server.Close()
	result, err := agentclient.NewAnalyzer(testRuntimeWithAgent(server.URL), nil).Analyze(context.Background(), domain.Document{}, domain.ParseRun{
		CleanedText: "推荐新易盛", Chunks: []domain.Chunk{{Index: 0, Text: "推荐新易盛"}},
	})
	require.NoError(t, err)
	require.Len(t, result, 72)
}
