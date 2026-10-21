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

func TestModelAnalyzerSendsConfiguredExtraHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "finance-sys-agent", r.Header.Get("X-Client-Name"))
		require.Equal(t, "m9-real-history", r.Header.Get("X-Request-Source"))
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": `{"plans":[{"analyst":"Alice","institution":"Research","symbol":"600519","asset_type":"","market":"","direction":"LONG","reference_price":1688,"reference_price_note":"explicit_price_mention","thesis":"channel checks improved","evidence":[{"chunk_index":0,"text":"source evidence"}],"risks":["volatility"],"confidence":0.82}]}`,
					},
				},
			},
		})
	}))
	defer server.Close()

	runtime := testRuntime(server.URL, 0)
	cfg := runtime.Config()
	cfg.LLM.ExtraHeaders = map[string]string{
		"Authorization":    "Bearer wrong",
		"Content-Type":     "text/plain",
		"X-Client-Name":    "finance-sys-agent",
		"X-Request-Source": "m9-real-history",
	}
	analyzer := llm.NewModelAnalyzer(runtime, telemetry.NewLogger(string(config.LogLevelError)))
	intents, err := analyzer.Analyze(context.Background(), domain.Document{
		Title:       "daily",
		Author:      "Alice",
		Institution: "Research",
	}, domain.ParseRun{
		CleanedText: "recommend 600519.SH",
		Chunks: []domain.Chunk{
			{Index: 0, Text: "recommend 600519.SH"},
		},
	})

	require.NoError(t, err)
	require.Len(t, intents, 1)
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

func TestModelAnalyzerAcceptsRawInstrumentTextAfterM3(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": `{"plans":[{"analyst":"Alice","institution":"Research","symbol":"CPO板块","direction":"LONG","reference_price":88.8,"thesis":"explicit recommendation from source text","evidence":[{"chunk_index":0,"text":"source evidence"}],"risks":["volatility"],"confidence":0.81}]}`,
					},
				},
			},
		})
	}))
	defer server.Close()

	analyzer := llm.NewModelAnalyzer(testRuntime(server.URL, 1), telemetry.NewLogger(string(config.LogLevelError)))
	intents, err := analyzer.Analyze(context.Background(), domain.Document{
		Title:       "daily",
		Author:      "Alice",
		Institution: "Research",
	}, domain.ParseRun{
		CleanedText: "recommend 300502.SZ",
		Chunks: []domain.Chunk{
			{Index: 0, Text: "recommend 300502.SZ"},
		},
	})
	require.NoError(t, err)
	require.Len(t, intents, 1)
	require.Equal(t, int32(1), attempts.Load())
	require.Equal(t, "CPO板块", intents[0].Symbol)
}

func TestModelAnalyzerDoesNotAppendExchangeToTextSymbol(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": `{"plans":[{"analyst":"Alice","institution":"Research","symbol":"CPO板块","direction":"LONG","reference_price":88.8,"thesis":"explicit recommendation from source text","evidence":[{"chunk_index":0,"text":"source evidence"}],"risks":["volatility"],"confidence":0.81}]}`,
					},
				},
			},
		})
	}))
	defer server.Close()

	analyzer := llm.NewModelAnalyzer(testRuntime(server.URL, 0), telemetry.NewLogger(string(config.LogLevelError)))
	intents, err := analyzer.Analyze(context.Background(), domain.Document{
		Title:       "daily",
		Author:      "Alice",
		Institution: "Research",
	}, domain.ParseRun{
		CleanedText: "recommend CPO sector",
		Chunks: []domain.Chunk{
			{Index: 0, Text: "recommend CPO sector"},
		},
	})
	require.NoError(t, err)
	require.Len(t, intents, 1)
	require.Equal(t, "CPO板块", intents[0].Symbol)
	require.Equal(t, int32(1), attempts.Load())
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

func TestValidateIntentAcceptsRawInstrumentTextAfterM3(t *testing.T) {
	for _, symbol := range []string{"CPO板块", "A股贵金属个股", "CPO板块.SZ", "新易盛"} {
		t.Run(symbol, func(t *testing.T) {
			require.NoError(t, llm.ValidateIntent(domain.PlanIntent{
				Symbol:         symbol,
				Direction:      domain.TradeDirectionLong,
				ReferencePrice: 1,
				Thesis:         "supported by text",
				Confidence:     0.8,
			}))
		})
	}
}

func TestValidateTSCode(t *testing.T) {
	require.True(t, llm.ValidateTSCode("300502.SZ"))
	require.True(t, llm.ValidateTSCode("600519.SH"))
	require.True(t, llm.ValidateTSCode("430047.BJ"))
	require.True(t, llm.ValidateTSCode("000001.sz"))
	require.False(t, llm.ValidateTSCode("CPO板块.SZ"))
	require.False(t, llm.ValidateTSCode("新易盛"))
	require.False(t, llm.ValidateTSCode("600519"))
}

func TestModelAnalyzerInfersBJMarket(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": `{"plans":[{"analyst":"Alice","institution":"Research","symbol":"430047","direction":"LONG","reference_price":10.2,"thesis":"supported by source text","evidence":[{"chunk_index":0,"text":"source evidence"}],"risks":["volatility"],"confidence":0.72}]}`,
					},
				},
			},
		})
	}))
	defer server.Close()

	analyzer := llm.NewModelAnalyzer(testRuntime(server.URL, 0), telemetry.NewLogger(string(config.LogLevelError)))
	intents, err := analyzer.Analyze(context.Background(), domain.Document{
		Title:       "daily",
		Author:      "Alice",
		Institution: "Research",
	}, domain.ParseRun{
		CleanedText: "recommend 430047.BJ",
		Chunks: []domain.Chunk{
			{Index: 0, Text: "recommend 430047.BJ"},
		},
	})
	require.NoError(t, err)
	require.Len(t, intents, 1)
	require.Equal(t, "430047.BJ", intents[0].Symbol)
	require.Equal(t, domain.MarketBJ, intents[0].Market)
}

func TestModelAnalyzerPrefersExplicitAuthorOverModelAuthor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": `{"plans":[{"analyst":"模型作者、联合作者","institution":"Research","symbol":"300502","direction":"LONG","reference_price":0,"thesis":"supported by source text","evidence":[{"chunk_index":0,"text":"source evidence"}],"risks":[],"confidence":0.8}]}`,
					},
				},
			},
		})
	}))
	defer server.Close()

	analyzer := llm.NewModelAnalyzer(testRuntime(server.URL, 0), telemetry.NewLogger(string(config.LogLevelError)))
	intents, err := analyzer.Analyze(context.Background(), domain.Document{
		Author: "手工作者",
	}, domain.ParseRun{
		CleanedText: "模型作者（分析师） 联合作者（分析师） 推荐新易盛",
		Chunks:      []domain.Chunk{{Index: 0, Text: "模型作者（分析师） 联合作者（分析师） 推荐新易盛"}},
	})
	require.NoError(t, err)
	require.Equal(t, "手工作者", intents[0].Analyst)
}

func TestModelAnalyzerUsesFirstModelAuthorWhenExplicitAuthorIsEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": `{"plans":[{"analyst":"张豪杰、韩笑","institution":"开源证券","symbol":"002353","direction":"LONG","reference_price":0,"thesis":"supported by source text","evidence":[{"chunk_index":0,"text":"source evidence"}],"risks":[],"confidence":0.8}]}`,
					},
				},
			},
		})
	}))
	defer server.Close()

	analyzer := llm.NewModelAnalyzer(testRuntime(server.URL, 0), telemetry.NewLogger(string(config.LogLevelError)))
	intents, err := analyzer.Analyze(context.Background(), domain.Document{}, domain.ParseRun{
		CleanedText: "张豪杰（分析师） 韩笑（分析师） 推荐杰瑞股份",
		Chunks:      []domain.Chunk{{Index: 0, Text: "张豪杰（分析师） 韩笑（分析师） 推荐杰瑞股份"}},
	})
	require.NoError(t, err)
	require.Equal(t, "张豪杰", intents[0].Analyst)
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
