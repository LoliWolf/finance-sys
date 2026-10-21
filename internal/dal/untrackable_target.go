package dal

import (
	"context"
	"errors"

	"finance-sys/internal/domain/db_model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var UntrackableTargets = &UntrackableTargetDML{}

type UntrackableTargetDML struct{}

func (*UntrackableTargetDML) QueryForRecovery(ctx context.Context, db *gorm.DB, documentID, id int64) (*db_model.UntrackableTarget, error) {
	var model db_model.UntrackableTarget
	err := db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND document_id = ?", id, documentID).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &model, err
}

func (*UntrackableTargetDML) DeactivateByID(ctx context.Context, db *gorm.DB, documentID, id int64) error {
	result := db.WithContext(ctx).Model(&db_model.UntrackableTarget{}).
		Where("id = ? AND document_id = ? AND is_active = ?", id, documentID, true).
		Updates(map[string]any{"is_active": false, "updated_at": gorm.Expr("CURRENT_TIMESTAMP")})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}

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
