package nacoscfg

import (
	"context"
	"fmt"
	"log/slog"

	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"

	"gorm.io/gorm"
)

type Reloader struct {
	loader  *Loader
	runtime *config.Runtime
	db      *gorm.DB
	logger  *slog.Logger
}

func NewReloader(loader *Loader, runtime *config.Runtime, db *gorm.DB, logger *slog.Logger) *Reloader {
	return &Reloader{
		loader:  loader,
		runtime: runtime,
		db:      db,
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
	snapshot, err := r.loader.Load(ctx, false, false)
	if err != nil {
		if r.logger != nil {
			r.logger.ErrorContext(ctx, "config reloader load failed", "error", err.Error())
		}
		return err
	}
	r.runtime.Update(snapshot)
	if r.db != nil {
		_ = dal.ConfigSnapshots.Create(ctx, r.db, &db_model.ConfigSnapshot{
			ConfigVersion: snapshot.Config.Meta.ConfigVersion,
			Source:        snapshot.Source,
			Sha256:        snapshot.SHA256,
			RawJSON:       snapshot.Raw,
		})
	}
	if r.logger != nil {
		r.logger.InfoContext(ctx, "config reloader success", "config_version", snapshot.Config.Meta.ConfigVersion, "sha256", snapshot.SHA256)
	}
	return nil
}
