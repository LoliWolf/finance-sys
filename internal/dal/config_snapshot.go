package dal

import (
	"context"

	"finance-sys/internal/domain/db_model"

	"gorm.io/gorm"
)

var ConfigSnapshots = &ConfigSnapshotDML{}

type ConfigSnapshotDML struct{}

func (*ConfigSnapshotDML) Create(ctx context.Context, db *gorm.DB, model *db_model.ConfigSnapshot) error {
	return db.WithContext(ctx).Create(model).Error
}
