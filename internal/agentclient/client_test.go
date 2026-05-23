package agentclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"finance-sys/internal/agentclient"
	"finance-sys/internal/config"

	"github.com/stretchr/testify/require"
)

func TestClientRetriesOnHTTP5xx(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attempts.Add(1)
		if count == 1 {
			http.Error(w, "temporary", http.StatusInternalServerError)
			return
		}
		writeAgentResponse(t, w, agentclient.ResolveDocumentResponse{
			SchemaVersion: agentclient.ResponseSchemaVersion,
			AgentVersion:  "test-agent",
			Status:        agentclient.AgentStatusResolved,
			RawIntents:    []agentclient.AgentRawIntent{testRawIntent("新易盛")},
		})
	}))
	defer server.Close()

	client := agentclient.NewClient(nil, nil)
	response, err := client.ResolveDocument(context.Background(), testAgentConfig(server.URL, 1), agentclient.ResolveDocumentRequest{
		RequestID: "client-retry-test",
		Chunks: []agentclient.AgentDocumentChunk{
			{ChunkIndex: 0, Text: "推荐新易盛"},
		},
	})
	require.NoError(t, err)
	require.Equal(t, agentclient.AgentStatusResolved, response.Status)
	require.Equal(t, int32(2), attempts.Load())
}

func TestClientDoesNotRetryOnHTTP4xx(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer server.Close()

	client := agentclient.NewClient(nil, nil)
	_, err := client.ResolveDocument(context.Background(), testAgentConfig(server.URL, 2), agentclient.ResolveDocumentRequest{
		RequestID: "client-4xx-test",
	})
	require.ErrorContains(t, err, "agent http 400")
	require.Equal(t, int32(1), attempts.Load())
}

func TestClientRetriesOnTimeout(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		time.Sleep(50 * time.Millisecond)
		writeAgentResponse(t, w, agentclient.ResolveDocumentResponse{
			SchemaVersion: agentclient.ResponseSchemaVersion,
			AgentVersion:  "test-agent",
			Status:        agentclient.AgentStatusResolved,
			RawIntents:    []agentclient.AgentRawIntent{testRawIntent("新易盛")},
		})
	}))
	defer server.Close()

	cfg := testAgentConfig(server.URL, 1)
	cfg.TimeoutMS = 10
	client := agentclient.NewClient(nil, nil)
	_, err := client.ResolveDocument(context.Background(), cfg, agentclient.ResolveDocumentRequest{
		RequestID: "client-timeout-test",
	})
	require.ErrorContains(t, err, "agent resolve document failed after 2 attempts")
	require.Equal(t, int32(2), attempts.Load())
}

func testAgentConfig(endpoint string, maxRetries int) config.AgentConfig {
	return config.AgentConfig{
		Enabled:       true,
		Mode:          config.AgentModePrimary,
		Endpoint:      endpoint,
		TimeoutMS:     1000,
		MaxRetries:    maxRetries,
		SchemaVersion: agentclient.ResponseSchemaVersion,
		Auth: config.AgentAuthConfig{
			Enabled:     true,
			HeaderName:  "X-Agent-Token",
			StaticToken: "test-token",
		},
	}
}

func writeAgentResponse(t *testing.T, w http.ResponseWriter, response agentclient.ResolveDocumentResponse) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(response))
}
