package nacoscfg

import (
	"context"
	"log/slog"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/domain"
	"finance-sys/internal/repository"
)

type Watcher struct {
	loader  *Loader
	runtime *config.Runtime
	repo    *repository.Repository
	logger  *slog.Logger
}

func NewWatcher(loader *Loader, runtime *config.Runtime, repo *repository.Repository, logger *slog.Logger) *Watcher {
	return &Watcher{
		loader:  loader,
		runtime: runtime,
		repo:    repo,
		logger:  logger,
	}
}

func (w *Watcher) Run(ctx context.Context) {
	current := w.runtime.Current()
	if current == nil || current.Config == nil {
		w.logger.Warn("config watcher skipped because runtime is empty")
		return
	}
	interval := time.Duration(current.Config.NacosClient.PollIntervalSeconds) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.poll(ctx)
		}
	}
}

func (w *Watcher) poll(ctx context.Context) {
	current := w.runtime.Current()
	if current == nil || current.Config == nil {
		return
	}

	next, err := w.loader.Load(ctx, current.Config.NacosClient.CacheLastGoodConfig, false)
	if err != nil {
		w.logger.Warn("poll nacos config", "error", err.Error())
		return
	}
	if current.SHA256 == next.SHA256 {
		return
	}

	w.runtime.Update(next)
	if w.repo != nil {
		_, _ = w.repo.InsertConfigSnapshot(ctx, &domain.ConfigSnapshot{
			ConfigVersion: next.Config.Meta.ConfigVersion,
			Source:        next.Source,
			SHA256:        next.SHA256,
			RawJSON:       string(next.Raw),
		})
	}
	w.logger.Info("config runtime updated", "config_version", next.Config.Meta.ConfigVersion, "sha256", next.SHA256)
}
