package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"finance-sys/internal/config"
	"finance-sys/internal/domain"
	"finance-sys/internal/llm"
	"finance-sys/internal/telemetry"

	"github.com/stretchr/testify/require"
)

func TestModelAnalyzerExtractsIntent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": `{"plans":[{"analyst":"Alice","institution":"Research","symbol":"600519","asset_type":"","market":"","direction":"LONG","reference_price":1688,"reference_price_note":"explicit_price_mention","thesis":"渠道改善明显","evidence":[{"chunk_index":0,"text":"推荐 600519.SH，参考价 1688 元"}],"risks":["消费恢复不及预期"],"confidence":0.82}]}`,
					},
				},
			},
		})
	}))
	defer server.Close()

	analyzer := llm.NewModelAnalyzer(testRuntime(server.URL, 0), telemetry.NewLogger(string(config.LogLevelError)))
	intents, err := analyzer.Analyze(context.Background(), domain.Document{
		Title:       "日报",
		Author:      "Alice",
		Institution: "Research",
	}, domain.ParseRun{
		CleanedText: "推荐 600519.SH，参考价 1688 元，渠道改善明显。",
		Chunks: []domain.Chunk{
			{Index: 0, Text: "推荐 600519.SH，参考价 1688 元，渠道改善明显。"},
		},
	})
	require.NoError(t, err)
	require.Len(t, intents, 1)
	require.Equal(t, "600519.SH", intents[0].Symbol)
	require.Equal(t, domain.MarketSH, intents[0].Market)
	require.Equal(t, domain.AssetTypeAShare, intents[0].AssetType)
	require.Equal(t, domain.TradeDirectionLong, intents[0].Direction)
	require.Equal(t, 1688.0, intents[0].ReferencePrice)
}

func TestModelAnalyzerRetriesOnInvalidStructuredResponse(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attempts.Add(1)
		if count == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{
					map[string]any{
						"message": map[string]any{
							"content": `{"plans":[{"symbol":"","direction":"LONG"}]}`,
						},
					},
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": `{"plans":[{"analyst":"Alice","institution":"Research","symbol":"000001","direction":"LONG","reference_price":12.3,"thesis":"估值修复","evidence":[{"chunk_index":0,"text":"推荐 000001.SZ，现价 12.3 元"}],"risks":["波动加剧"],"confidence":0.74}]}`,
					},
				},
			},
		})
	}))
	defer server.Close()

	analyzer := llm.NewModelAnalyzer(testRuntime(server.URL, 1), telemetry.NewLogger(string(config.LogLevelError)))
	intents, err := analyzer.Analyze(context.Background(), domain.Document{
		Title:       "日报",
		Author:      "Alice",
		Institution: "Research",
	}, domain.ParseRun{
		CleanedText: "推荐 000001.SZ，现价 12.3 元。",
		Chunks: []domain.Chunk{
			{Index: 0, Text: "推荐 000001.SZ，现价 12.3 元。"},
		},
	})
	require.NoError(t, err)
	require.Len(t, intents, 1)
	require.Equal(t, int32(2), attempts.Load())
	require.Equal(t, "000001.SZ", intents[0].Symbol)
}

func TestModelAnalyzerFailsAfterRetries(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": `not-json`,
					},
				},
			},
		})
	}))
	defer server.Close()

	analyzer := llm.NewModelAnalyzer(testRuntime(server.URL, 2), telemetry.NewLogger(string(config.LogLevelError)))
	_, err := analyzer.Analyze(context.Background(), domain.Document{
		Title:       "日报",
		Author:      "Alice",
		Institution: "Research",
	}, domain.ParseRun{
		CleanedText: "推荐 000001.SZ。",
		Chunks: []domain.Chunk{
			{Index: 0, Text: "推荐 000001.SZ。"},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed after 3 attempts")
	require.Equal(t, int32(3), attempts.Load())
}

func TestValidateIntentRejectsInvalidStructuredOutput(t *testing.T) {
	require.ErrorContains(t, llm.ValidateIntent(domain.PlanIntent{
		Symbol:         "",
		Direction:      domain.TradeDirectionLong,
		ReferencePrice: 1,
		Thesis:         "supported by text",
		Confidence:     0.8,
	}), "symbol is required")

	require.ErrorContains(t, llm.ValidateIntent(domain.PlanIntent{
		Symbol:         "600519.SH",
		Direction:      domain.TradeDirection("BUY"),
		ReferencePrice: 1,
		Thesis:         "supported by text",
		Confidence:     0.8,
	}), "direction must be LONG or SHORT")

	require.ErrorContains(t, llm.ValidateIntent(domain.PlanIntent{
		Symbol:         "600519.SH",
		Direction:      domain.TradeDirectionLong,
		ReferencePrice: 1,
		Thesis:         "supported by text",
		Confidence:     0,
	}), "confidence must be in (0,1]")
}

func TestValidateIntentAcceptsMVPTradeIntent(t *testing.T) {
	err := llm.ValidateIntent(domain.PlanIntent{
		Analyst:        "blogger-a",
		Symbol:         "600519.SH",
		Direction:      domain.TradeDirectionLong,
		ReferencePrice: 1688,
		Thesis:         "explicit recommendation from source text",
		Confidence:     0.82,
	})
	require.NoError(t, err)
}

func testRuntime(endpoint string, maxRetries int) *config.Runtime {
	return config.NewRuntime(&config.Snapshot{
		Config: &config.Config{
			LLM: config.LLMConfig{
				Enabled:    true,
				Provider:   config.LLMProviderOpenAICompatible,
				Endpoint:   endpoint,
				APIKey:     "test-key",
				Model:      "test-model",
				TimeoutMS:  5000,
				MaxRetries: maxRetries,
			},
		},
	})
}
