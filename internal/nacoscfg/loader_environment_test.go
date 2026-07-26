package nacoscfg

import (
	"os"
	"testing"

	"finance-sys/internal/config"

	"github.com/stretchr/testify/require"
)

func TestLoaderDecodeDefaultsNacosDatabaseToTest(t *testing.T) {
	t.Setenv(config.FinanceSysEnvironmentVariable, "")
	raw := readExampleConfig(t)

	snapshot, err := (&Loader{}).decode(raw, "test")

	require.NoError(t, err)
	require.Equal(t, config.DatabaseProfileTest, snapshot.Config.SelectedDatabaseProfile)
	require.Contains(t, snapshot.Config.Database.DSN, "/expert_trade_test?")
}

func TestLoaderDecodeUsesProductionDatabaseOnlyForExactPROD(t *testing.T) {
	t.Setenv(config.FinanceSysEnvironmentVariable, config.ProductionEnvironmentValue)
	raw := readExampleConfig(t)

	snapshot, err := (&Loader{}).decode(raw, "test")

	require.NoError(t, err)
	require.Equal(t, config.DatabaseProfileProduction, snapshot.Config.SelectedDatabaseProfile)
	require.Contains(t, snapshot.Config.Database.DSN, "/expert_trade?")
	require.NotContains(t, snapshot.Config.Database.DSN, "/expert_trade_test?")
}

func readExampleConfig(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../configs/example_nacos_config.json")
	require.NoError(t, err)
	return raw
}
