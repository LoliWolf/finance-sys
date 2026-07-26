package dal

import (
	"context"
	"errors"
	"time"

	"finance-sys/internal/domain"
	"finance-sys/internal/domain/db_model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ScheduledTaskRuns = &ScheduledTaskRunDML{}

type ScheduledTaskRunDML struct{}

func (*ScheduledTaskRunDML) CreateIfAbsent(ctx context.Context, db *gorm.DB, model *db_model.ScheduledTaskRun) (bool, error) {
	result := db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "task_type"}, {Name: "task_key"}},
		DoNothing: true,
	}).Create(model)
	return result.RowsAffected > 0, result.Error
}

func (*ScheduledTaskRunDML) QueryByID(ctx context.Context, db *gorm.DB, id int64) (*db_model.ScheduledTaskRun, error) {
	var model db_model.ScheduledTaskRun
	err := db.WithContext(ctx).First(&model, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &model, nil
}

func (*ScheduledTaskRunDML) QueryByTypeAndKey(ctx context.Context, db *gorm.DB, taskType domain.ScheduledTaskType, taskKey string) (*db_model.ScheduledTaskRun, error) {
	var model db_model.ScheduledTaskRun
	err := db.WithContext(ctx).
		Where("task_type = ? AND task_key = ?", uint16(taskType), taskKey).
		First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &model, nil
}

func (*ScheduledTaskRunDML) ClaimNextDue(ctx context.Context, db *gorm.DB, workerID string, now time.Time, claimTimeout time.Duration) (*db_model.ScheduledTaskRun, error) {
	var model db_model.ScheduledTaskRun
	staleBefore := now.Add(-claimTimeout)
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Where("scheduled_at <= ?", now).
			Where("status = ? OR (status = ? AND started_at < ?)", "QUEUED", "RUNNING", staleBefore)
		result := query.Order("scheduled_at ASC, id ASC").Limit(1).Find(&model)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}

		condition := tx.Model(&db_model.ScheduledTaskRun{}).Where("id = ?", model.ID)
		if model.Status == "QUEUED" {
			condition = condition.Where("status = ?", "QUEUED")
		} else {
			condition = condition.Where("status = ? AND started_at < ?", "RUNNING", staleBefore)
		}
		result = condition.Updates(map[string]any{
			"status":        "RUNNING",
			"worker_id":     workerID,
			"attempt_count": gorm.Expr("attempt_count + 1"),
			"started_at":    now,
			"finished_at":   nil,
			"output_json":   nil,
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

func (*ScheduledTaskRunDML) UpdateByID(ctx context.Context, db *gorm.DB, id int64, values map[string]any) error {
	return ApplyUpdate(ctx, db.Model(&db_model.ScheduledTaskRun{}), UpdateParam{
		Where:          []Condition{Eq("id", id)},
		Values:         values,
		TouchUpdatedAt: true,
	}).Error
}
