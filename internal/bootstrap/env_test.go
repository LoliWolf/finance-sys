package bootstrap

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadNacosBootstrapFromEnv(t *testing.T) {
	t.Setenv("NACOS_SERVER_ADDR", "127.0.0.1:8848")

	cfg, err := LoadNacosBootstrapFromEnv()
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:8848", cfg.ServerAddr)
	require.Equal(t, "public", cfg.Namespace)
	require.Equal(t, "DEFAULT_GROUP", cfg.Group)
	require.Equal(t, "expert_trade", cfg.DataID)
	require.Empty(t, cfg.Username)
	require.Empty(t, cfg.Password)
}

func TestLoadNacosBootstrapFromEnvIgnoresNonAddressOverrides(t *testing.T) {
	t.Setenv("NACOS_SERVER_ADDR", "127.0.0.1:8848")
	t.Setenv("NACOS_NAMESPACE", "other-namespace")
	t.Setenv("NACOS_GROUP", "OTHER_GROUP")
	t.Setenv("NACOS_DATA_ID", "other-data-id")
	t.Setenv("NACOS_USERNAME", "local-user")
	t.Setenv("NACOS_PASSWORD", "local-password")

	cfg, err := LoadNacosBootstrapFromEnv()
	require.NoError(t, err)
	require.Equal(t, "public", cfg.Namespace)
	require.Equal(t, "DEFAULT_GROUP", cfg.Group)
	require.Equal(t, "expert_trade", cfg.DataID)
	require.Empty(t, cfg.Username)
	require.Empty(t, cfg.Password)
}

func TestLoadNacosBootstrapFromEnvRequiresServerAddress(t *testing.T) {
	t.Setenv("NACOS_SERVER_ADDR", "  ")

	_, err := LoadNacosBootstrapFromEnv()
	require.Error(t, err)
	require.Contains(t, err.Error(), "NACOS_SERVER_ADDR is required")
}

func TestLoadInitialSnapshotUsesLocalExampleWithoutNacosAddress(t *testing.T) {
	t.Setenv("NACOS_SERVER_ADDR", "")
	t.Setenv("FINANCE_SYS_ENV", "")

	snapshot, loader, err := LoadInitialSnapshot(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	require.Nil(t, loader)
	require.Equal(t, "local_example", snapshot.Source)
	require.NotNil(t, snapshot.Config)
	require.Equal(t, "finance-sys", snapshot.Config.Meta.AppName)
	require.Equal(t, "test", string(snapshot.Config.SelectedDatabaseProfile))
	require.Contains(t, snapshot.Config.Database.DSN, "/expert_trade_test?")
}

func TestLoadInitialSnapshotUsesLocalProductionDatabaseOnlyForExactPROD(t *testing.T) {
	t.Setenv("NACOS_SERVER_ADDR", "")
	t.Setenv("FINANCE_SYS_ENV", "PROD")

	snapshot, loader, err := LoadInitialSnapshot(context.Background(), nil)
	require.NoError(t, err)
	require.Nil(t, loader)
	require.Equal(t, "production", string(snapshot.Config.SelectedDatabaseProfile))
	require.Contains(t, snapshot.Config.Database.DSN, "/expert_trade?")
	require.NotContains(t, snapshot.Config.Database.DSN, "/expert_trade_test?")
}

func TestLoadInitialSnapshotFallsBackWhenNacosCannotBeRead(t *testing.T) {
	t.Setenv("NACOS_SERVER_ADDR", "invalid-address-without-port")
	t.Setenv("FINANCE_SYS_ENV", "")

	snapshot, loader, err := LoadInitialSnapshot(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	require.Nil(t, loader)
	require.Equal(t, "local_example", snapshot.Source)
}
