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

var ExternalDocumentIngestions = &ExternalDocumentIngestionDML{}

type ExternalDocumentIngestionDML struct{}

func (*ExternalDocumentIngestionDML) Ensure(ctx context.Context, db *gorm.DB, model *db_model.ExternalDocumentIngestion) (*db_model.ExternalDocumentIngestion, bool, error) {
	result := db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "source_type"},
			{Name: "source_path_hash"},
			{Name: "source_version"},
		},
		DoNothing: true,
	}).Create(model)
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected > 0 {
		return model, true, nil
	}
	existing, err := ExternalDocumentIngestions.QueryBySourceVersion(ctx, db, domain.ExternalDocumentSourceType(model.SourceType), model.SourcePathHash, model.SourceVersion)
	return existing, false, err
}

func (*ExternalDocumentIngestionDML) QueryBySourceVersion(ctx context.Context, db *gorm.DB, sourceType domain.ExternalDocumentSourceType, pathHash string, sourceVersion string) (*db_model.ExternalDocumentIngestion, error) {
	var model db_model.ExternalDocumentIngestion
	err := db.WithContext(ctx).
		Where("source_type = ? AND source_path_hash = ? AND source_version = ?", uint16(sourceType), pathHash, sourceVersion).
		First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &model, nil
}

func (*ExternalDocumentIngestionDML) MarkDownloading(ctx context.Context, db *gorm.DB, id int64) error {
	return ApplyUpdate(ctx, db.Model(&db_model.ExternalDocumentIngestion{}), UpdateParam{
		Where: []Condition{Eq("id", id)},
		Values: map[string]any{
			"status":        uint8(domain.ExternalDocumentIngestionStatusDownloading),
			"attempt_count": gorm.Expr("attempt_count + 1"),
			"last_error":    "",
		},
		TouchUpdatedAt: true,
	}).Error
}

func (*ExternalDocumentIngestionDML) MarkIngested(ctx context.Context, db *gorm.DB, id int64, documentID int64, contentSHA256 string, downloadedAt time.Time) error {
	return ApplyUpdate(ctx, db.Model(&db_model.ExternalDocumentIngestion{}), UpdateParam{
		Where: []Condition{Eq("id", id)},
		Values: map[string]any{
			"status":         uint8(domain.ExternalDocumentIngestionStatusIngested),
			"document_id":    documentID,
			"content_sha256": contentSHA256,
			"downloaded_at":  downloadedAt,
			"last_error":     "",
		},
		TouchUpdatedAt: true,
	}).Error
}

func (*ExternalDocumentIngestionDML) MarkAnalyzing(ctx context.Context, db *gorm.DB, id int64) error {
	return ApplyUpdate(ctx, db.Model(&db_model.ExternalDocumentIngestion{}), UpdateParam{
		Where:          []Condition{Eq("id", id)},
		Values:         map[string]any{"status": uint8(domain.ExternalDocumentIngestionStatusAnalyzing), "last_error": ""},
		TouchUpdatedAt: true,
	}).Error
}

func (*ExternalDocumentIngestionDML) MarkSucceeded(ctx context.Context, db *gorm.DB, id int64, analyzedAt time.Time) error {
	return ApplyUpdate(ctx, db.Model(&db_model.ExternalDocumentIngestion{}), UpdateParam{
		Where: []Condition{Eq("id", id)},
		Values: map[string]any{
			"status":      uint8(domain.ExternalDocumentIngestionStatusSucceeded),
			"analyzed_at": analyzedAt,
			"last_error":  "",
		},
		TouchUpdatedAt: true,
	}).Error
}

func (*ExternalDocumentIngestionDML) MarkFailed(ctx context.Context, db *gorm.DB, id int64, message string) error {
	return ApplyUpdate(ctx, db.Model(&db_model.ExternalDocumentIngestion{}), UpdateParam{
		Where: []Condition{Eq("id", id)},
		Values: map[string]any{
			"status":     uint8(domain.ExternalDocumentIngestionStatusFailed),
			"last_error": message,
		},
		TouchUpdatedAt: true,
	}).Error
}
