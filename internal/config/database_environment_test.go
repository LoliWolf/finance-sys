package config_test

import (
	"encoding/json"
	"testing"

	"finance-sys/internal/config"

	"github.com/stretchr/testify/require"
)

func TestSelectDatabaseForEnvironmentDefaultsToTest(t *testing.T) {
	for _, environment := range []string{"", "DEV", "prod", " PROD ", "TEST"} {
		t.Run(environment, func(t *testing.T) {
			cfg := databaseSelectionConfig()

			profile := config.SelectDatabaseForEnvironment(&cfg, environment)

			require.Equal(t, config.DatabaseProfileTest, profile)
			require.Equal(t, config.DatabaseProfileTest, cfg.SelectedDatabaseProfile)
			require.Equal(t, "test-dsn", cfg.Database.DSN)
		})
	}
}

func TestSelectDatabaseForEnvironmentRequiresExactPROD(t *testing.T) {
	cfg := databaseSelectionConfig()

	profile := config.SelectDatabaseForEnvironment(&cfg, "PROD")

	require.Equal(t, config.DatabaseProfileProduction, profile)
	require.Equal(t, config.DatabaseProfileProduction, cfg.SelectedDatabaseProfile)
	require.Equal(t, "production-dsn", cfg.Database.DSN)
}

func TestSelectDatabaseForEnvironmentNilConfigIsSafe(t *testing.T) {
	require.Equal(t, config.DatabaseProfileTest, config.SelectDatabaseForEnvironment(nil, "PROD"))
}

func TestSelectDatabaseForEnvironmentKeepsBothSerializedProfiles(t *testing.T) {
	cfg := databaseSelectionConfig()
	config.SelectDatabaseForEnvironment(&cfg, "")

	raw, err := json.Marshal(&cfg)
	require.NoError(t, err)
	var decoded map[string]config.DatabaseConfig
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Equal(t, "production-dsn", decoded["database"].DSN)
	require.Equal(t, "test-dsn", decoded["database_test"].DSN)
}

func TestApplyRuntimeEnvironmentKeepsTestPort(t *testing.T) {
	cfg := databaseSelectionConfig()
	cfg.Agent.InternalAPIBaseURL = "http://127.0.0.1:30006"

	profile := config.ApplyRuntimeEnvironment(&cfg, "")

	require.Equal(t, config.DatabaseProfileTest, profile)
	require.Equal(t, 30005, cfg.Service.HTTP.Port)
	require.Equal(t, "http://127.0.0.1:30005", cfg.Agent.InternalAPIBaseURL)
}

func TestApplyRuntimeEnvironmentSelectsProductionPortAndInternalAPI(t *testing.T) {
	cfg := databaseSelectionConfig()
	cfg.Agent.InternalAPIBaseURL = "http://127.0.0.1:30006"

	profile := config.ApplyRuntimeEnvironment(&cfg, "PROD")

	require.Equal(t, config.DatabaseProfileProduction, profile)
	require.Equal(t, 30006, cfg.Service.HTTP.Port)
	require.Equal(t, "http://127.0.0.1:30006", cfg.Agent.InternalAPIBaseURL)
}

func TestApplyRuntimeEnvironmentDoesNotRewriteUnrelatedInternalAPI(t *testing.T) {
	cfg := databaseSelectionConfig()
	cfg.Agent.InternalAPIBaseURL = "https://internal.example:8443/base"

	config.ApplyRuntimeEnvironment(&cfg, "PROD")

	require.Equal(t, 30006, cfg.Service.HTTP.Port)
	require.Equal(t, "https://internal.example:8443/base", cfg.Agent.InternalAPIBaseURL)
}

func TestApplyRuntimeEnvironmentKeepsBothHTTPPortsSerialized(t *testing.T) {
	cfg := databaseSelectionConfig()
	config.ApplyRuntimeEnvironment(&cfg, "")

	raw, err := json.Marshal(&cfg)
	require.NoError(t, err)
	var decoded struct {
		Service struct {
			HTTP struct {
				Port     int `json:"port"`
				PortTest int `json:"port_test"`
			} `json:"http"`
		} `json:"service"`
	}
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Equal(t, 30006, decoded.Service.HTTP.Port)
	require.Equal(t, 30005, decoded.Service.HTTP.PortTest)
}

func databaseSelectionConfig() config.Config {
	return config.Config{
		DatabaseProduction: config.DatabaseConfig{DSN: "production-dsn"},
		DatabaseTest:       config.DatabaseConfig{DSN: "test-dsn"},
		Service: config.ServiceConfig{HTTP: config.HTTPServerConfig{
			PortProduction: 30006,
			PortTest:       30005,
		}},
	}
}
