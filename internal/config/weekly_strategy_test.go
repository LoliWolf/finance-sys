package config_test

import (
	"encoding/json"
	"finance-sys/internal/config"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestWeeklyStrategyConfiguration(t *testing.T) {
	raw, err := os.ReadFile("../../configs/example_nacos_config.json")
	require.NoError(t, err)
	for _, tc := range []struct {
		name    string
		mutate  func(*config.TradingDecisionConfig)
		invalid bool
	}{
		{"week", func(c *config.TradingDecisionConfig) { c.RecommendationDateMode = "WEEK_NATURAL_DAYS" }, false},
		{"scan-too-small", func(c *config.TradingDecisionConfig) { c.MaxCandidateScan = 1 }, true},
		{"scan-too-large", func(c *config.TradingDecisionConfig) { c.MaxCandidateScan = 10001 }, true},
		{"unknown-strategy", func(c *config.TradingDecisionConfig) { c.SelectionStrategy = "LLM" }, true},
		{"bad-benchmark", func(c *config.TradingDecisionConfig) { c.BenchmarkSymbol = "510300" }, true},
		{"bad-extension", func(c *config.TradingDecisionConfig) { c.MaxEntryExtensionRatio = .3 }, true},
		{"bad-atr", func(c *config.TradingDecisionConfig) { c.MaxATRRatio = -.1 }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg config.Config
			require.NoError(t, json.Unmarshal(raw, &cfg))
			tc.mutate(&cfg.Trading.Decision)
			err := config.Validate(&cfg)
			if tc.invalid {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
