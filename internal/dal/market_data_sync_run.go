package dal

import (
	"context"
	"errors"
	"time"

	"finance-sys/internal/domain/db_model"

	"gorm.io/gorm"
)

var MarketDataSyncRuns = &MarketDataSyncRunDML{}

type MarketDataSyncRunDML struct{}

func (*MarketDataSyncRunDML) Create(ctx context.Context, db *gorm.DB, model *db_model.MarketDataSyncRun) error {
	return db.WithContext(ctx).Create(model).Error
}

func (*MarketDataSyncRunDML) QueryByID(ctx context.Context, db *gorm.DB, id int64) (*db_model.MarketDataSyncRun, error) {
	var model db_model.MarketDataSyncRun
	err := ApplyQuery(ctx, db, QueryParam{
		Where: []Condition{Eq("id", id)},
	}).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &model, nil
}

func (*MarketDataSyncRunDML) QueryActiveByTypeAndTradeDate(ctx context.Context, db *gorm.DB, syncType string, tradeDate time.Time) (*db_model.MarketDataSyncRun, error) {
	var model db_model.MarketDataSyncRun
	err := db.WithContext(ctx).
		Where("sync_type = ?", syncType).
		Where("trade_date = ?", tradeDate).
		Where("status IN ?", []string{"QUEUED", "RUNNING"}).
		Order("id DESC").
		First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &model, nil
}

func (*MarketDataSyncRunDML) QueryByParam(ctx context.Context, db *gorm.DB, param QueryParam) ([]db_model.MarketDataSyncRun, error) {
	var models []db_model.MarketDataSyncRun
	if err := ApplyQuery(ctx, db, param).Find(&models).Error; err != nil {
		return nil, err
	}
	return models, nil
}

func (*MarketDataSyncRunDML) ClaimNextQueued(ctx context.Context, db *gorm.DB, syncType string, workerID string, now time.Time) (*db_model.MarketDataSyncRun, error) {
	var model db_model.MarketDataSyncRun
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.
			Where("sync_type = ?", syncType).
			Where("status = ?", "QUEUED").
			Order("queued_at ASC, id ASC").
			First(&model).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		result := tx.Model(&db_model.MarketDataSyncRun{}).
			Where("id = ?", model.ID).
			Where("status = ?", "QUEUED").
			Updates(map[string]any{
				"status":     "RUNNING",
				"started_at": now,
				"claimed_by": workerID,
				"claimed_at": now,
				"updated_at": gorm.Expr("CURRENT_TIMESTAMP"),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		return tx.First(&model, model.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return &model, nil
}

func (*MarketDataSyncRunDML) UpdateProgressByID(ctx context.Context, db *gorm.DB, id int64, values map[string]any) error {
	return ApplyUpdate(ctx, db.Model(&db_model.MarketDataSyncRun{}), UpdateParam{
		Where:          []Condition{Eq("id", id)},
		Values:         values,
		TouchUpdatedAt: true,
	}).Error
}
