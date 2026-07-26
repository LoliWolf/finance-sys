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

func (*MarketDataSyncRunDML) ClaimNextQueued(ctx context.Context, db *gorm.DB, syncType string, workerID string, now time.Time, claimTimeout time.Duration) (*db_model.MarketDataSyncRun, error) {
	var model db_model.MarketDataSyncRun
	staleBefore := now.Add(-claimTimeout)
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Where("sync_type = ?", syncType).Where("status = ?", "QUEUED")
		if claimTimeout > 0 {
			query = tx.Where("sync_type = ?", syncType).
				Where("status = ? OR (status = ? AND started_at < ?)", "QUEUED", "RUNNING", staleBefore)
		}
		result := query.Order("queued_at ASC, id ASC").Limit(1).Find(&model)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}

		condition := tx.Model(&db_model.MarketDataSyncRun{}).Where("id = ?", model.ID)
		if model.Status == "QUEUED" {
			condition = condition.Where("status = ?", "QUEUED")
		} else {
			condition = condition.Where("status = ? AND started_at < ?", "RUNNING", staleBefore)
		}
		result = condition.Updates(map[string]any{
			"status":        "RUNNING",
			"started_at":    now,
			"finished_at":   nil,
			"claimed_by":    workerID,
			"claimed_at":    now,
			"error_code":    "",
			"error_message": "",
			"updated_at":    gorm.Expr("CURRENT_TIMESTAMP"),
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
