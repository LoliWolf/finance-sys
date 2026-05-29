package dal

import (
	"context"

	"finance-sys/internal/domain/db_model"

	"gorm.io/gorm"
)

var InstrumentResolutionRuns = &InstrumentResolutionRunDML{}

type InstrumentResolutionRunDML struct{}

func (*InstrumentResolutionRunDML) Create(ctx context.Context, db *gorm.DB, model *db_model.InstrumentResolutionRun) error {
	return db.WithContext(ctx).Create(model).Error
}

func (*InstrumentResolutionRunDML) DeleteByID(ctx context.Context, db *gorm.DB, id int64) error {
	return db.WithContext(ctx).Where("id = ?", id).Delete(&db_model.InstrumentResolutionRun{}).Error
}

func (*InstrumentResolutionRunDML) UpdateByID(ctx context.Context, db *gorm.DB, id int64, values map[string]any) error {
	return ApplyUpdate(ctx, db.Model(&db_model.InstrumentResolutionRun{}), UpdateParam{
		Where:          []Condition{Eq("id", id)},
		Values:         values,
		TouchUpdatedAt: true,
	}).Error
}

func (*InstrumentResolutionRunDML) QueryByID(ctx context.Context, db *gorm.DB, id int64) (*db_model.InstrumentResolutionRun, error) {
	var model db_model.InstrumentResolutionRun
	err := ApplyQuery(ctx, db, QueryParam{
		Where: []Condition{Eq("id", id)},
		Limit: 1,
	}).First(&model).Error
	if err == gorm.ErrRecordNotFound {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &model, nil
}

func (*InstrumentResolutionRunDML) QueryByDocumentID(ctx context.Context, db *gorm.DB, documentID int64) ([]db_model.InstrumentResolutionRun, error) {
	var models []db_model.InstrumentResolutionRun
	err := ApplyQuery(ctx, db, QueryParam{
		Where:  []Condition{Eq("document_id", documentID)},
		Orders: []OrderParam{OrderBy("created_at", true), OrderBy("id", true)},
	}).Find(&models).Error
	if err != nil {
		return nil, err
	}
	return models, nil
}
