package bootstrap

import (
	"fmt"
	"os"
	"strings"

	"finance-sys/internal/nacoscfg"
)

const (
	defaultNacosNamespace = "public"
	defaultNacosGroup     = "DEFAULT_GROUP"
	defaultNacosDataID    = "expert_trade"
)

func LoadNacosBootstrapFromEnv() (nacoscfg.BootstrapConfig, error) {
	cfg := nacoscfg.BootstrapConfig{
		ServerAddr: strings.TrimSpace(os.Getenv("NACOS_SERVER_ADDR")),
		Namespace:  defaultNacosNamespace,
		Group:      defaultNacosGroup,
		DataID:     defaultNacosDataID,
	}
	if cfg.ServerAddr == "" {
		return nacoscfg.BootstrapConfig{}, fmt.Errorf("NACOS_SERVER_ADDR is required")
	}
	return cfg, nil
}
