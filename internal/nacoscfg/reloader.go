package nacoscfg

import (
	"context"
	"fmt"

	"finance-sys/internal/config"
	"finance-sys/internal/domain"
	"finance-sys/internal/repository"
)

type Reloader struct {
	loader  *Loader
	runtime *config.Runtime
	repo    *repository.Repository
}

func NewReloader(loader *Loader, runtime *config.Runtime, repo *repository.Repository) *Reloader {
	return &Reloader{
		loader:  loader,
		runtime: runtime,
		repo:    repo,
	}
}

func (r *Reloader) Reload(ctx context.Context) error {
	current := r.runtime.Current()
	if current == nil || current.Config == nil {
		return fmt.Errorf("config runtime is empty")
	}
	snapshot, err := r.loader.Load(ctx, current.Config.NacosClient.CacheLastGoodConfig, false)
	if err != nil {
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
	return nil
}
