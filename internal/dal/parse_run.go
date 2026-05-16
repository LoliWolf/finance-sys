package dal

import (
	"context"
	"errors"

	"finance-sys/internal/domain/db_model"

	"gorm.io/gorm"
)

var ParseRuns = &ParseRunDML{}

type ParseRunDML struct{}

func (*ParseRunDML) Create(ctx context.Context, db *gorm.DB, model *db_model.ParseRun) error {
	return db.WithContext(ctx).Create(model).Error
}

func (*ParseRunDML) QueryLatestByDocumentID(ctx context.Context, db *gorm.DB, documentID int64) (*db_model.ParseRun, error) {
	var model db_model.ParseRun
	err := ApplyQuery(ctx, db, QueryParam{
		Where:  []Condition{Eq("document_id", documentID)},
		Orders: []OrderParam{OrderBy("created_at", true)},
		Limit:  1,
	}).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &model, nil
}
