package nacoscfg

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"finance-sys/internal/config"

	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

type Loader struct {
	bootstrap BootstrapConfig
	logger    *slog.Logger
}

type BootstrapConfig struct {
	ServerAddr string
	Namespace  string
	Group      string
	DataID     string
	Username   string
	Password   string
}

func NewLoader(bootstrapCfg BootstrapConfig, logger *slog.Logger) *Loader {
	return &Loader{
		bootstrap: bootstrapCfg,
		logger:    logger,
	}
}

func (l *Loader) Load(ctx context.Context, _ bool, failFast bool) (*config.Snapshot, error) {
	raw, source, err := l.fetch(ctx)
	if err != nil {
		if failFast {
			return nil, err
		}
		return nil, fmt.Errorf("load config from nacos: %w", err)
	}

	snapshot, err := l.decode(raw, source)
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

func (l *Loader) fetch(_ context.Context) ([]byte, string, error) {
	serverConfigs, err := buildServerConfigs(l.bootstrap.ServerAddr)
	if err != nil {
		return nil, "", err
	}

	cacheDir := filepath.Join(os.TempDir(), "finance-sys-nacos-sdk-cache")
	defer func() {
		if err := os.RemoveAll(cacheDir); err != nil && l.logger != nil {
			l.logger.Warn("remove nacos sdk transient cache", "cache_dir", cacheDir, "error", err.Error())
		}
	}()

	clientConfig := constant.NewClientConfig(
		constant.WithNamespaceId(l.bootstrap.Namespace),
		constant.WithUsername(l.bootstrap.Username),
		constant.WithPassword(l.bootstrap.Password),
		constant.WithTimeoutMs(5000),
		constant.WithNotLoadCacheAtStart(true),
		constant.WithDisableUseSnapShot(true),
		constant.WithCacheDir(cacheDir),
	)

	client, err := clients.NewConfigClient(vo.NacosClientParam{
		ClientConfig:  clientConfig,
		ServerConfigs: serverConfigs,
	})
	if err != nil {
		return nil, "", fmt.Errorf("new nacos client: %w", err)
	}

	content, err := client.GetConfig(vo.ConfigParam{
		DataId: l.bootstrap.DataID,
		Group:  l.bootstrap.Group,
	})
	if err != nil {
		return nil, "", fmt.Errorf("get nacos config: %w", err)
	}
	if strings.TrimSpace(content) == "" {
		return nil, "", fmt.Errorf("nacos config response is empty")
	}
	return []byte(content), "nacos", nil
}

func (l *Loader) decode(raw []byte, source string) (*config.Snapshot, error) {
	var cfg config.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("decode nacos config: %w", err)
	}
	if err := config.Validate(&cfg); err != nil {
		return nil, err
	}
	return config.NewSnapshot(&cfg, raw, source, time.Now())
}

func buildServerConfigs(serverAddr string) ([]constant.ServerConfig, error) {
	parts := strings.Split(serverAddr, ",")
	configs := make([]constant.ServerConfig, 0, len(parts))
	for _, part := range parts {
		raw := strings.TrimSpace(part)
		if raw == "" {
			continue
		}
		host, port, err := net.SplitHostPort(raw)
		if err != nil {
			return nil, fmt.Errorf("parse server addr %q: %w", part, err)
		}
		portValue, err := strconv.ParseUint(port, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse port %q: %w", port, err)
		}
		configs = append(configs, *constant.NewServerConfig(host, portValue))
	}
	if len(configs) == 0 {
		return nil, fmt.Errorf("nacos server addr is empty")
	}
	return configs, nil
}
