package dal

import (
	"context"

	"finance-sys/internal/domain/db_model"

	"gorm.io/gorm"
)

var UntrackableTargets = &UntrackableTargetDML{}

type UntrackableTargetDML struct{}

func (*UntrackableTargetDML) CreateBatch(ctx context.Context, db *gorm.DB, models []db_model.UntrackableTarget) error {
	if len(models) == 0 {
		return nil
	}
	return db.WithContext(ctx).CreateInBatches(models, 100).Error
}

func (*UntrackableTargetDML) DeactivateByDocumentID(ctx context.Context, db *gorm.DB, documentID int64) error {
	return ApplyUpdate(ctx, db.Model(&db_model.UntrackableTarget{}), UpdateParam{
		Where:          []Condition{Eq("document_id", documentID), Eq("is_active", true)},
		Values:         map[string]any{"is_active": false},
		TouchUpdatedAt: true,
	}).Error
}

func (*UntrackableTargetDML) QueryActiveByDocumentID(ctx context.Context, db *gorm.DB, documentID int64) ([]db_model.UntrackableTarget, error) {
	var models []db_model.UntrackableTarget
	err := ApplyQuery(ctx, db, QueryParam{
		Where:  []Condition{Eq("document_id", documentID), Eq("is_active", true)},
		Orders: []OrderParam{OrderBy("created_at", true), OrderBy("id", true)},
	}).Find(&models).Error
	if err != nil {
		return nil, err
	}
	return models, nil
}

func (*UntrackableTargetDML) QueryByResolutionRunID(ctx context.Context, db *gorm.DB, resolutionRunID int64) ([]db_model.UntrackableTarget, error) {
	var models []db_model.UntrackableTarget
	err := ApplyQuery(ctx, db, QueryParam{
		Where:  []Condition{Eq("resolution_run_id", resolutionRunID)},
		Orders: []OrderParam{OrderBy("created_at", false), OrderBy("id", false)},
	}).Find(&models).Error
	if err != nil {
		return nil, err
	}
	return models, nil
}
