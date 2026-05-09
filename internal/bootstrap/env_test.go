package bootstrap

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadNacosBootstrapFromEnv(t *testing.T) {
	t.Setenv("NACOS_SERVER_ADDR", "127.0.0.1:8848")
	t.Setenv("NACOS_NAMESPACE", "public")
	t.Setenv("NACOS_GROUP", "DEFAULT_GROUP")
	t.Setenv("NACOS_DATA_ID", "expert_trade")
	t.Setenv("NACOS_USERNAME", "nacos")
	t.Setenv("NACOS_PASSWORD", "secret")

	cfg, err := LoadNacosBootstrapFromEnv()
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:8848", cfg.ServerAddr)
	require.Equal(t, "public", cfg.Namespace)
	require.Equal(t, "DEFAULT_GROUP", cfg.Group)
	require.Equal(t, "expert_trade", cfg.DataID)
	require.Equal(t, "nacos", cfg.Username)
	require.Equal(t, "secret", cfg.Password)
}

func TestLoadNacosBootstrapFromEnvRequiresCoreFields(t *testing.T) {
	t.Setenv("NACOS_SERVER_ADDR", "127.0.0.1:8848")
	t.Setenv("NACOS_NAMESPACE", "public")
	t.Setenv("NACOS_GROUP", "DEFAULT_GROUP")
	t.Setenv("NACOS_DATA_ID", "")

	_, err := LoadNacosBootstrapFromEnv()
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing nacos bootstrap environment variables")
}
