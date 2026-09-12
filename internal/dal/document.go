package dal

import (
	"context"
	"errors"

	"finance-sys/internal/domain/db_model"

	"gorm.io/gorm"
)

var Documents = &DocumentDML{}

type DocumentDML struct{}

func (*DocumentDML) QueryMetadataByID(ctx context.Context, db *gorm.DB, id int64) (*db_model.Document, error) {
	var model db_model.Document
	err := db.WithContext(ctx).Omit("raw_content").Where("id = ?", id).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &model, err
}

// A short atomic claim, not a transaction held open during OCR/LLM calls.
func (*DocumentDML) ClaimFailedAnalysis(ctx context.Context, db *gorm.DB, id int64) (bool, error) {
	result := db.WithContext(ctx).Model(&db_model.Document{}).
		Where("id = ? AND status = ?", id, "FAILED").
		Where("NOT EXISTS (SELECT 1 FROM recommendation_events WHERE source_document_id = ?)", id).
		Where("NOT EXISTS (SELECT 1 FROM trade_candidate_plans WHERE document_id = ?)", id).
		Where("NOT EXISTS (SELECT 1 FROM external_document_ingestions WHERE document_id = ? AND status = 5)", id).
		Updates(map[string]any{"status": "ANALYZING", "updated_at": gorm.Expr("CURRENT_TIMESTAMP")})
	return result.RowsAffected == 1, result.Error
}

func (*DocumentDML) RestoreFailedAnalysis(ctx context.Context, db *gorm.DB, id int64) error {
	return db.WithContext(ctx).Model(&db_model.Document{}).Where("id = ? AND status IN ?", id, []string{"ANALYZING", "PARSED"}).
		Updates(map[string]any{"status": "FAILED", "updated_at": gorm.Expr("CURRENT_TIMESTAMP")}).Error
}

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

func (*DocumentDML) UpdateMetadataByID(ctx context.Context, db *gorm.DB, id int64, author string, institution string, title string) error {
	return ApplyUpdate(ctx, db.Model(&db_model.Document{}), UpdateParam{
		Where: []Condition{Eq("id", id)},
		Values: map[string]any{
			"author":      author,
			"institution": institution,
			"title":       title,
		},
		TouchUpdatedAt: true,
	}).Error
}
