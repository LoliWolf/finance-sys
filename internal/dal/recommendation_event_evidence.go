package dal

import (
	"context"

	"finance-sys/internal/domain/db_model"

	"gorm.io/gorm"
)

var RecommendationEventEvidences = &RecommendationEventEvidenceDML{}

type RecommendationEventEvidenceDML struct{}

func (*RecommendationEventEvidenceDML) CreateBatch(ctx context.Context, db *gorm.DB, models []db_model.RecommendationEventEvidence) error {
	if len(models) == 0 {
		return nil
	}
	return db.WithContext(ctx).Create(&models).Error
}

func (*RecommendationEventEvidenceDML) DeleteByEventID(ctx context.Context, db *gorm.DB, eventID int64) error {
	return ApplyQuery(ctx, db, QueryParam{
		Where: []Condition{Eq("recommendation_event_id", eventID)},
	}).Delete(&db_model.RecommendationEventEvidence{}).Error
}

func (*RecommendationEventEvidenceDML) QueryByEventID(ctx context.Context, db *gorm.DB, eventID int64) ([]db_model.RecommendationEventEvidence, error) {
	var models []db_model.RecommendationEventEvidence
	if err := ApplyQuery(ctx, db, QueryParam{
		Where:  []Condition{Eq("recommendation_event_id", eventID)},
		Orders: []OrderParam{OrderBy("id", false)},
	}).Find(&models).Error; err != nil {
		return nil, err
	}
	return models, nil
}

func (*RecommendationEventEvidenceDML) QueryByEventIDs(ctx context.Context, db *gorm.DB, eventIDs []int64) ([]db_model.RecommendationEventEvidence, error) {
	if len(eventIDs) == 0 {
		return []db_model.RecommendationEventEvidence{}, nil
	}
	var models []db_model.RecommendationEventEvidence
	if err := db.WithContext(ctx).
		Where("recommendation_event_id IN ?", eventIDs).
		Order("recommendation_event_id ASC, id ASC").
		Find(&models).Error; err != nil {
		return nil, err
	}
	return models, nil
}
