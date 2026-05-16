package dal

import (
	"context"
	"errors"

	"finance-sys/internal/domain/db_model"

	"gorm.io/gorm"
)

var Documents = &DocumentDML{}

type DocumentDML struct{}

func (*DocumentDML) Create(ctx context.Context, db *gorm.DB, model *db_model.Document) error {
	return db.WithContext(ctx).Create(model).Error
}

func (*DocumentDML) QueryByID(ctx context.Context, db *gorm.DB, id int64) (*db_model.Document, error) {
	var model db_model.Document
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

func (*DocumentDML) QueryContentByID(ctx context.Context, db *gorm.DB, id int64) ([]byte, error) {
	var model db_model.Document
	err := ApplyQuery(ctx, db, QueryParam{
		Where: []Condition{Eq("id", id)},
	}).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return model.RawContent, nil
}

func (*DocumentDML) QueryBySHA(ctx context.Context, db *gorm.DB, sha string) (*db_model.Document, error) {
	var model db_model.Document
	err := ApplyQuery(ctx, db, QueryParam{
		Where: []Condition{Eq("sha256", sha)},
	}).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &model, nil
}

func (*DocumentDML) QueryLatest(ctx context.Context, db *gorm.DB, limit int) ([]db_model.Document, error) {
	var models []db_model.Document
	err := ApplyQuery(ctx, db, QueryParam{
		Orders: []OrderParam{OrderBy("created_at", true)},
		Limit:  limit,
	}).Find(&models).Error
	if err != nil {
		return nil, err
	}
	return models, nil
}

func (*DocumentDML) UpdateStatusByID(ctx context.Context, db *gorm.DB, id int64, status string) error {
	return ApplyUpdate(ctx, db.Model(&db_model.Document{}), UpdateParam{
		Where: []Condition{Eq("id", id)},
		Values: map[string]any{
			"status": status,
		},
		TouchUpdatedAt: true,
	}).Error
}
