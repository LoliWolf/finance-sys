package dal

import (
	"context"
	"errors"

	"finance-sys/internal/domain/db_model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var Bloggers = &BloggerDML{}

type BloggerDML struct{}

func (*BloggerDML) Create(ctx context.Context, db *gorm.DB, model *db_model.Blogger) error {
	return db.WithContext(ctx).Create(model).Error
}

func (*BloggerDML) UpsertByNormalizedNameAndInstitution(ctx context.Context, db *gorm.DB, model *db_model.Blogger) error {
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "normalized_name"}, {Name: "institution"}},
		DoUpdates: clause.Assignments(map[string]any{
			"name":        gorm.Expr("VALUES(name)"),
			"source_type": gorm.Expr("VALUES(source_type)"),
			"updated_at":  gorm.Expr("CURRENT_TIMESTAMP"),
		}),
	}).Create(model).Error
}

func (*BloggerDML) QueryByID(ctx context.Context, db *gorm.DB, id int64) (*db_model.Blogger, error) {
	var model db_model.Blogger
	err := ApplyQuery(ctx, db, QueryParam{
		Where: []Condition{Eq("id", id)},
		Limit: 1,
	}).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &model, nil
}

func (*BloggerDML) QueryByNormalizedNameAndInstitution(ctx context.Context, db *gorm.DB, normalizedName string, institution string) (*db_model.Blogger, error) {
	var model db_model.Blogger
	err := ApplyQuery(ctx, db, QueryParam{
		Where: []Condition{
			Eq("normalized_name", normalizedName),
			Eq("institution", institution),
		},
		Limit: 1,
	}).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &model, nil
}
