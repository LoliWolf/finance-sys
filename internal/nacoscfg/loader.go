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

func (l *Loader) Load(ctx context.Context, allowCache bool, failFast bool) (*config.Snapshot, error) {
	raw, source, err := l.fetch(ctx)
	if err != nil {
		if allowCache {
			if cached, cacheErr := l.loadCache(); cacheErr == nil {
				l.logger.Warn("nacos fetch failed; using cached config", "error", err.Error())
				return cached, nil
			}
		}
		if failFast {
			return nil, err
		}
		return nil, fmt.Errorf("load config: %w", err)
	}

	snapshot, err := l.decode(raw, source)
	if err != nil {
		if allowCache {
			if cached, cacheErr := l.loadCache(); cacheErr == nil {
				l.logger.Warn("config decode failed; using cached config", "error", err.Error())
				return cached, nil
			}
		}
		return nil, err
	}

	if err := l.cache(snapshot); err != nil {
		l.logger.Warn("cache config snapshot", "error", err.Error())
	}
	return snapshot, nil
}

func (l *Loader) fetch(_ context.Context) ([]byte, string, error) {
	serverConfigs, err := buildServerConfigs(l.bootstrap.ServerAddr)
	if err != nil {
		return nil, "", err
	}

	clientConfig := constant.NewClientConfig(
		constant.WithNamespaceId(l.bootstrap.Namespace),
		constant.WithUsername(l.bootstrap.Username),
		constant.WithPassword(l.bootstrap.Password),
		constant.WithTimeoutMs(5000),
		constant.WithNotLoadCacheAtStart(true),
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

func (l *Loader) cache(snapshot *config.Snapshot) error {
	dir := snapshot.Config.NacosClient.CacheDir
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "last-good-config.json")
	return os.WriteFile(path, snapshot.Raw, 0o644)
}

func (l *Loader) loadCache() (*config.Snapshot, error) {
	dir := os.Getenv("NACOS_CACHE_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "expert-trade-cache")
	}
	if cacheDir := l.loadCacheDirFromLocalFile(); cacheDir != "" {
		dir = cacheDir
	}
	raw, err := os.ReadFile(filepath.Join(dir, "last-good-config.json"))
	if err != nil {
		return nil, err
	}
	return l.decode(raw, "cache")
}

func (l *Loader) loadCacheDirFromLocalFile() string {
	path := filepath.Join("configs", "example_nacos_config.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var cfg config.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return ""
	}
	return cfg.NacosClient.CacheDir
}

func buildServerConfigs(serverAddr string) ([]constant.ServerConfig, error) {
	parts := strings.Split(serverAddr, ",")
	configs := make([]constant.ServerConfig, 0, len(parts))
	for _, part := range parts {
		host, port, err := net.SplitHostPort(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("parse server addr %q: %w", part, err)
		}
		portValue, err := strconv.ParseUint(port, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse port %q: %w", port, err)
		}
		configs = append(configs, *constant.NewServerConfig(host, portValue))
	}
	return configs, nil
}
