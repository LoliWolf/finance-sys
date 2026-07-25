package dal

import (
	"context"
	"errors"
	"time"

	"finance-sys/internal/domain/db_model"

	"gorm.io/gorm"
)

var RecommendationEvaluationRuns = &RecommendationEvaluationRunDML{}

type RecommendationEvaluationRunDML struct{}

func (*RecommendationEvaluationRunDML) Create(ctx context.Context, db *gorm.DB, model *db_model.RecommendationEvaluationRun) error {
	return db.WithContext(ctx).Create(model).Error
}

func (*RecommendationEvaluationRunDML) QueryByID(ctx context.Context, db *gorm.DB, id int64) (*db_model.RecommendationEvaluationRun, error) {
	var model db_model.RecommendationEvaluationRun
	err := db.WithContext(ctx).First(&model, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &model, nil
}

func (*RecommendationEvaluationRunDML) QueryByParam(ctx context.Context, db *gorm.DB, param QueryParam) ([]db_model.RecommendationEvaluationRun, error) {
	var models []db_model.RecommendationEvaluationRun
	if err := ApplyQuery(ctx, db, param).Find(&models).Error; err != nil {
		return nil, err
	}
	return models, nil
}

func (*RecommendationEvaluationRunDML) ClaimNextQueued(ctx context.Context, db *gorm.DB, workerID string, now time.Time, claimTimeout time.Duration) (*db_model.RecommendationEvaluationRun, error) {
	var model db_model.RecommendationEvaluationRun
	staleBefore := now.Add(-claimTimeout)
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Where("status = ?", "QUEUED")
		if claimTimeout > 0 {
			query = tx.Where("status = ? OR (status = ? AND started_at < ?)", "QUEUED", "RUNNING", staleBefore)
		}
		result := query.Order("queued_at ASC, id ASC").Limit(1).Find(&model)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}

		condition := tx.Model(&db_model.RecommendationEvaluationRun{}).Where("id = ?", model.ID)
		if model.Status == "QUEUED" {
			condition = condition.Where("status = ?", "QUEUED")
		} else {
			condition = condition.Where("status = ? AND started_at < ?", "RUNNING", staleBefore)
		}
		result = condition.Updates(map[string]any{
			"status":        "RUNNING",
			"started_at":    now,
			"finished_at":   nil,
			"worker_id":     workerID,
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

func (*RecommendationEvaluationRunDML) UpdateByID(ctx context.Context, db *gorm.DB, id int64, values map[string]any) error {
	return ApplyUpdate(ctx, db.Model(&db_model.RecommendationEvaluationRun{}), UpdateParam{
		Where:          []Condition{Eq("id", id)},
		Values:         values,
		TouchUpdatedAt: true,
	}).Error
}
