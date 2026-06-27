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

func TestValidateMarketDataTushareRejectsDuplicateTokenAlias(t *testing.T) {
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
	require.Error(t, err)
	require.ErrorContains(t, err, "market_data.tushare.tokens alias must be unique")
}
