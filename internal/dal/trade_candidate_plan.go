package dal

import (
	"context"

	"finance-sys/internal/domain/db_model"

	"gorm.io/gorm"
)

var TradeCandidatePlans = &TradeCandidatePlanDML{}

type TradeCandidatePlanDML struct{}

func (*TradeCandidatePlanDML) Create(ctx context.Context, db *gorm.DB, model *db_model.TradeCandidatePlan) error {
	return db.WithContext(ctx).Create(model).Error
}

func (*TradeCandidatePlanDML) DeleteByDocumentID(ctx context.Context, db *gorm.DB, documentID int64) error {
	return ApplyQuery(ctx, db, QueryParam{
		Where: []Condition{Eq("document_id", documentID)},
	}).Delete(&db_model.TradeCandidatePlan{}).Error
}

func (*TradeCandidatePlanDML) QueryLatest(ctx context.Context, db *gorm.DB, limit int) ([]db_model.TradeCandidatePlan, error) {
	var models []db_model.TradeCandidatePlan
	err := ApplyQuery(ctx, db, QueryParam{
		Orders: []OrderParam{OrderBy("created_at", true)},
		Limit:  limit,
	}).Find(&models).Error
	if err != nil {
		return nil, err
	}
	return models, nil
}

func (*TradeCandidatePlanDML) QueryByDocumentID(ctx context.Context, db *gorm.DB, documentID int64) ([]db_model.TradeCandidatePlan, error) {
	var models []db_model.TradeCandidatePlan
	err := ApplyQuery(ctx, db, QueryParam{
		Where:  []Condition{Eq("document_id", documentID)},
		Orders: []OrderParam{OrderBy("created_at", false)},
	}).Find(&models).Error
	if err != nil {
		return nil, err
	}
	return models, nil
}
