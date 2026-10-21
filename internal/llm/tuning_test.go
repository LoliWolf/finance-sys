package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"finance-sys/internal/domain"
	"finance-sys/internal/llm"
	"github.com/stretchr/testify/require"
)

func TestModelAnalyzerUsesNacosPromptAndTemperature(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Temperature float64 `json:"temperature"`
			Messages    []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		require.Equal(t, 0.6, request.Temperature)
		require.Contains(t, request.Messages[0].Content, "NACOS SEMANTIC PROMPT")
		require.Contains(t, request.Messages[0].Content, `"plans"`)
		require.Contains(t, request.Messages[0].Content, "Do not generate entry price")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"plans":[{"symbol":"600519.SH","direction":"LONG","reference_price":0,"reference_price_note":"price_missing_in_text","thesis":"明确关注","evidence":[{"chunk_index":0,"text":"关注茅台"}],"risks":[],"confidence":0.8}]}`}}}})
	}))
	defer server.Close()
	runtime := testRuntime(server.URL, 0)
	runtime.Config().LLM.Temperature = 0.6
	runtime.Config().LLM.ExtractionPrompt = "NACOS SEMANTIC PROMPT"
	_, err := llm.NewModelAnalyzer(runtime, nil).Analyze(context.Background(), domain.Document{}, domain.ParseRun{
		CleanedText: "关注茅台", Chunks: []domain.Chunk{{Index: 0, Text: "关注茅台"}},
	})
	require.NoError(t, err)
}
