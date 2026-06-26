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
