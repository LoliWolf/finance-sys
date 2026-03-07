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
