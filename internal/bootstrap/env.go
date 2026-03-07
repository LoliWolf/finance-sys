package bootstrap

import (
	"fmt"
	"os"

	"finance-sys/internal/nacoscfg"
)

func LoadNacosBootstrapFromEnv() (nacoscfg.BootstrapConfig, error) {
	cfg := nacoscfg.BootstrapConfig{
		ServerAddr: os.Getenv("NACOS_SERVER_ADDR"),
		Namespace:  os.Getenv("NACOS_NAMESPACE"),
		Group:      os.Getenv("NACOS_GROUP"),
		DataID:     os.Getenv("NACOS_DATA_ID"),
		Username:   os.Getenv("NACOS_USERNAME"),
		Password:   os.Getenv("NACOS_PASSWORD"),
	}
	if cfg.ServerAddr == "" || cfg.Namespace == "" || cfg.Group == "" || cfg.DataID == "" {
		return nacoscfg.BootstrapConfig{}, fmt.Errorf("missing nacos bootstrap environment variables")
	}
	return cfg, nil
}
