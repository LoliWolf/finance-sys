package nacoscfg

import (
	"context"
	"fmt"
	"log/slog"

	"finance-sys/internal/config"
	"finance-sys/internal/domain"
	"finance-sys/internal/repository"
)

type Reloader struct {
	loader  *Loader
	runtime *config.Runtime
	repo    *repository.Repository
	logger  *slog.Logger
}

func NewReloader(loader *Loader, runtime *config.Runtime, repo *repository.Repository, logger *slog.Logger) *Reloader {
	return &Reloader{
		loader:  loader,
		runtime: runtime,
		repo:    repo,
		logger:  logger,
	}
}

func (r *Reloader) Reload(ctx context.Context) error {
	if r.logger != nil {
		r.logger.InfoContext(ctx, "config reloader start")
	}
	current := r.runtime.Current()
	if current == nil || current.Config == nil {
		if r.logger != nil {
			r.logger.ErrorContext(ctx, "config reloader failed because runtime is empty")
		}
		return fmt.Errorf("config runtime is empty")
	}
	snapshot, err := r.loader.Load(ctx, current.Config.NacosClient.CacheLastGoodConfig, false)
	if err != nil {
		if r.logger != nil {
			r.logger.ErrorContext(ctx, "config reloader load failed", "error", err.Error())
		}
		return err
	}
	r.runtime.Update(snapshot)
	if r.repo != nil {
		_, _ = r.repo.InsertConfigSnapshot(ctx, &domain.ConfigSnapshot{
			ConfigVersion: snapshot.Config.Meta.ConfigVersion,
			Source:        snapshot.Source,
			SHA256:        snapshot.SHA256,
			RawJSON:       string(snapshot.Raw),
		})
	}
	if r.logger != nil {
		r.logger.InfoContext(ctx, "config reloader success", "config_version", snapshot.Config.Meta.ConfigVersion, "sha256", snapshot.SHA256)
	}
	return nil
}
