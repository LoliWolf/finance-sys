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

func TestValidateAllowsTwoMinuteAgentTimeout(t *testing.T) {
	raw, err := os.ReadFile("../../configs/example_nacos_config.json")
	require.NoError(t, err)

	var cfg config.Config
	require.NoError(t, json.Unmarshal(raw, &cfg))
	cfg.Agent.Enabled = true
	cfg.Agent.TimeoutMS = 120000

	require.NoError(t, config.Validate(&cfg))
}

func TestValidateRejectsAgentTimeoutAboveTwoMinutes(t *testing.T) {
	raw, err := os.ReadFile("../../configs/example_nacos_config.json")
	require.NoError(t, err)

	var cfg config.Config
	require.NoError(t, json.Unmarshal(raw, &cfg))
	cfg.Agent.Enabled = true
	cfg.Agent.TimeoutMS = 120001

	err = config.Validate(&cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "agent.timeout_ms must be in (0,120000] when agent.enabled is true")
}
