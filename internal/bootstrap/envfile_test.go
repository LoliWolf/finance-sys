package bootstrap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadNacosServerAddressFromFilesLoadsOnlyAddress(t *testing.T) {
	t.Setenv("NACOS_SERVER_ADDR", "")
	t.Setenv("NACOS_DATA_ID", "")
	path := filepath.Join(t.TempDir(), "bootstrap.env")
	require.NoError(t, os.WriteFile(path, []byte("# bootstrap\r\nNACOS_SERVER_ADDR='192.168.31.234:8848'\r\n"), 0o600))

	require.NoError(t, LoadNacosServerAddressFromFiles(path))
	require.Equal(t, "192.168.31.234:8848", os.Getenv("NACOS_SERVER_ADDR"))
	require.Empty(t, os.Getenv("NACOS_DATA_ID"))
}

func TestLoadNacosServerAddressFromFilesRejectsOtherKeys(t *testing.T) {
	t.Setenv("NACOS_SERVER_ADDR", "")
	path := filepath.Join(t.TempDir(), "bootstrap.env")
	require.NoError(t, os.WriteFile(path, []byte("NACOS_SERVER_ADDR=192.168.31.234:8848\nNACOS_DATA_ID=must-not-load\n"), 0o600))

	err := LoadNacosServerAddressFromFiles(path)
	require.ErrorContains(t, err, "only NACOS_SERVER_ADDR is allowed")
	require.Empty(t, os.Getenv("NACOS_SERVER_ADDR"))
}

func TestLoadNacosServerAddressFromFilesRejectsDuplicateAddress(t *testing.T) {
	t.Setenv("NACOS_SERVER_ADDR", "")
	path := filepath.Join(t.TempDir(), "bootstrap.env")
	require.NoError(t, os.WriteFile(path, []byte("NACOS_SERVER_ADDR=first:8848\nNACOS_SERVER_ADDR=second:8848\n"), 0o600))

	err := LoadNacosServerAddressFromFiles(path)
	require.ErrorContains(t, err, "duplicate NACOS_SERVER_ADDR")
	require.Empty(t, os.Getenv("NACOS_SERVER_ADDR"))
}

func TestLoadNacosServerAddressFromFilesPreservesExplicitEnvironment(t *testing.T) {
	t.Setenv("NACOS_SERVER_ADDR", "explicit:8848")
	path := filepath.Join(t.TempDir(), "bootstrap.env")
	require.NoError(t, os.WriteFile(path, []byte("NACOS_SERVER_ADDR=file:8848\n"), 0o600))

	require.NoError(t, LoadNacosServerAddressFromFiles(path))
	require.Equal(t, "explicit:8848", os.Getenv("NACOS_SERVER_ADDR"))
}

func TestLoadNacosServerAddressFromFilesSkipsMissingFiles(t *testing.T) {
	t.Setenv("NACOS_SERVER_ADDR", "")
	path := filepath.Join(t.TempDir(), "bootstrap.env")

	require.NoError(t, LoadNacosServerAddressFromFiles(path))
	require.Empty(t, os.Getenv("NACOS_SERVER_ADDR"))
}

func TestLoadNacosServerAddressFromFilesRejectsExistingFileWithoutAddress(t *testing.T) {
	t.Setenv("NACOS_SERVER_ADDR", "")
	path := filepath.Join(t.TempDir(), "bootstrap.env")
	require.NoError(t, os.WriteFile(path, []byte("NACOS_DATA_ID=expert_trade\n"), 0o600))

	err := LoadNacosServerAddressFromFiles(path)
	require.ErrorContains(t, err, "only NACOS_SERVER_ADDR is allowed")
}
