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

func databaseSelectionConfig() config.Config {
	return config.Config{
		DatabaseProduction: config.DatabaseConfig{DSN: "production-dsn"},
		DatabaseTest:       config.DatabaseConfig{DSN: "test-dsn"},
	}
}
