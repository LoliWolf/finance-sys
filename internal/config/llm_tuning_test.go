package config_test

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"

	"finance-sys/internal/config"
	"github.com/stretchr/testify/require"
)

func TestLLMTuningValidation(t *testing.T) {
	raw, err := os.ReadFile("../../configs/example_nacos_config.json")
	require.NoError(t, err)
	for _, temperature := range []float64{-0.1, 0, 0.1, 0.5, 1, 1.1, math.NaN(), math.Inf(1)} {
		var cfg config.Config
		require.NoError(t, json.Unmarshal(raw, &cfg))
		cfg.LLM.Temperature = temperature
		if temperature >= 0 && temperature <= 1 {
			require.NoError(t, config.Validate(&cfg))
		} else {
			require.ErrorContains(t, config.Validate(&cfg), "llm.temperature")
		}
	}
	var cfg config.Config
	require.NoError(t, json.Unmarshal(raw, &cfg))
	cfg.LLM.ExtractionPrompt = strings.Repeat("中", 16000)
	require.NoError(t, config.Validate(&cfg))
	cfg.LLM.ExtractionPrompt += "文"
	require.ErrorContains(t, config.Validate(&cfg), "llm.extraction_prompt")
}
