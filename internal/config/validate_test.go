package config_test

import (
	"encoding/json"
	"os"
	"testing"

	"finance-sys/internal/config"

	"github.com/stretchr/testify/require"
)

func TestValidateExampleConfig(t *testing.T) {
	raw, err := os.ReadFile("../../configs/example_nacos_config.json")
	require.NoError(t, err)

	var cfg config.Config
	require.NoError(t, json.Unmarshal(raw, &cfg))
	require.NoError(t, config.Validate(&cfg))
}

func TestValidateAllowsConfiguredLongAgentTimeout(t *testing.T) {
	raw, err := os.ReadFile("../../configs/example_nacos_config.json")
	require.NoError(t, err)

	var cfg config.Config
	require.NoError(t, json.Unmarshal(raw, &cfg))
	cfg.Agent.Enabled = true
	cfg.Agent.TimeoutMS = 12000001

	require.NoError(t, config.Validate(&cfg))
}

func TestValidateRejectsLLMExtraHeadersOverridingProtectedHeaders(t *testing.T) {
	raw, err := os.ReadFile("../../configs/example_nacos_config.json")
	require.NoError(t, err)

	var cfg config.Config
	require.NoError(t, json.Unmarshal(raw, &cfg))
	cfg.LLM.ExtraHeaders = map[string]string{
		"Authorization": "Bearer wrong",
		"Content-Type":  "text/plain",
	}

	err = config.Validate(&cfg)
	require.Error(t, err)
	require.ErrorContains(t, err, "llm.extra_headers must not override Authorization")
	require.ErrorContains(t, err, "llm.extra_headers must not override Content-Type")
}

func TestValidateMarketDataTushareRequiresEnabledToken(t *testing.T) {
	raw, err := os.ReadFile("../../configs/example_nacos_config.json")
	require.NoError(t, err)

	var cfg config.Config
	require.NoError(t, json.Unmarshal(raw, &cfg))
	cfg.MarketData.Enabled = true
	cfg.MarketData.Provider = "tushare"
	cfg.MarketData.Tushare.Enabled = true
	cfg.MarketData.Tushare.Tokens = []config.TushareTokenConfig{
		{Alias: "primary", Token: "", Enabled: true, Weight: 1},
	}

	err = config.Validate(&cfg)
	require.Error(t, err)
	require.ErrorContains(t, err, "market_data.tushare.tokens token is required when token is enabled")
}

func TestValidateMarketDataRequiresTushareWhenEnabled(t *testing.T) {
	raw, err := os.ReadFile("../../configs/example_nacos_config.json")
	require.NoError(t, err)

	var cfg config.Config
	require.NoError(t, json.Unmarshal(raw, &cfg))
	cfg.MarketData.Enabled = true
	cfg.MarketData.Provider = "tushare"
	cfg.MarketData.Tushare.Enabled = false

	err = config.Validate(&cfg)
	require.Error(t, err)
	require.ErrorContains(t, err, "market_data.tushare.enabled must be true when market_data.enabled is true")
}

func TestValidateMarketDataTushareAllowsDuplicateTokenAlias(t *testing.T) {
	raw, err := os.ReadFile("../../configs/example_nacos_config.json")
	require.NoError(t, err)

	var cfg config.Config
	require.NoError(t, json.Unmarshal(raw, &cfg))
	cfg.MarketData.Enabled = true
	cfg.MarketData.Provider = "tushare"
	cfg.MarketData.Tushare.Enabled = true
	cfg.MarketData.Tushare.Tokens = []config.TushareTokenConfig{
		{Alias: "primary", Token: "token-a", Enabled: true, Weight: 1},
		{Alias: "primary", Token: "token-b", Enabled: true, Weight: 1},
	}

	err = config.Validate(&cfg)
	require.NoError(t, err)
}

func TestValidateRecommendationPerformanceAcceptsConfiguredWindows(t *testing.T) {
	raw, err := os.ReadFile("../../configs/example_nacos_config.json")
	require.NoError(t, err)

	var cfg config.Config
	require.NoError(t, json.Unmarshal(raw, &cfg))
	cfg.Evaluation.Enabled = true
	cfg.Evaluation.RecommendationPerformance = config.RecommendationPerformanceConfig{
		Enabled:               true,
		Windows:               []int{5, 10, 30, 90},
		QuoteSource:           "TUSHARE",
		EntryPriceRule:        "NEXT_TRADE_OPEN",
		BasePriceRule:         "RECOMMEND_DATE_CLOSE_OR_NEXT_CLOSE",
		WinThresholdRatio:     0,
		MinQuoteCoverageRatio: 0.9,
		CalcVersion:           "v1",
		AsyncWorker: config.EvaluationWorkerConfig{
			Enabled:           true,
			PollIntervalMS:    500,
			ClaimTimeoutMS:    60000,
			MaxConcurrentRuns: 1,
			BatchSize:         500,
		},
		Ranking: config.EvaluationRankingConfig{
			DefaultWindowDays:     30,
			DefaultMinSampleCount: 5,
			DefaultSort:           "performance_score",
		},
	}

	require.NoError(t, config.Validate(&cfg))
}

func TestValidateRecommendationPerformanceRejectsDuplicateAndUnknownDefaultWindow(t *testing.T) {
	raw, err := os.ReadFile("../../configs/example_nacos_config.json")
	require.NoError(t, err)

	var cfg config.Config
	require.NoError(t, json.Unmarshal(raw, &cfg))
	cfg.Evaluation.Enabled = true
	cfg.Evaluation.RecommendationPerformance.Enabled = true
	cfg.Evaluation.RecommendationPerformance.Windows = []int{5, 5, 10}
	cfg.Evaluation.RecommendationPerformance.QuoteSource = "TUSHARE"
	cfg.Evaluation.RecommendationPerformance.EntryPriceRule = "NEXT_TRADE_OPEN"
	cfg.Evaluation.RecommendationPerformance.BasePriceRule = "RECOMMEND_DATE_CLOSE_OR_NEXT_CLOSE"
	cfg.Evaluation.RecommendationPerformance.MinQuoteCoverageRatio = 0.9
	cfg.Evaluation.RecommendationPerformance.CalcVersion = "v1"
	cfg.Evaluation.RecommendationPerformance.Ranking.DefaultWindowDays = 30
	cfg.Evaluation.RecommendationPerformance.Ranking.DefaultSort = "win_rate"

	err = config.Validate(&cfg)
	require.Error(t, err)
	require.ErrorContains(t, err, "windows must not contain duplicates")
	require.ErrorContains(t, err, "ranking.default_window_days must be present in windows")
}
