package dal

import (
	"context"

	"finance-sys/internal/domain/db_model"

	"gorm.io/gorm"
)

var MarketDataSyncMissingItems = &MarketDataSyncMissingItemDML{}

type MarketDataSyncMissingItemDML struct{}

func (*MarketDataSyncMissingItemDML) DeleteByRunID(ctx context.Context, db *gorm.DB, syncRunID int64) error {
	return db.WithContext(ctx).Where("sync_run_id = ?", syncRunID).Delete(&db_model.MarketDataSyncMissingItem{}).Error
}

func (*MarketDataSyncMissingItemDML) CreateBatch(ctx context.Context, db *gorm.DB, models []db_model.MarketDataSyncMissingItem) error {
	if len(models) == 0 {
		return nil
	}
	// Keep generated IDs private to this attempt, including rolled-back batches.
	models = append([]db_model.MarketDataSyncMissingItem(nil), models...)
	const batchSize = 300
	return db.WithContext(ctx).Omit("id").CreateInBatches(models, batchSize).Error
}
