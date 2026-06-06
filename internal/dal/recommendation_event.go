package dal

import (
	"context"
	"errors"

	"finance-sys/internal/domain/db_model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var RecommendationEvents = &RecommendationEventDML{}

type RecommendationEventDML struct{}

func (*RecommendationEventDML) Create(ctx context.Context, db *gorm.DB, model *db_model.RecommendationEvent) error {
	return db.WithContext(ctx).Create(model).Error
}

func (*RecommendationEventDML) UpsertByDedupeKey(ctx context.Context, db *gorm.DB, model *db_model.RecommendationEvent) error {
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "dedupe_key"}},
		DoUpdates: clause.Assignments(map[string]any{
			"blogger_id":      gorm.Expr("VALUES(blogger_id)"),
			"plan_id":         gorm.Expr("VALUES(plan_id)"),
			"parse_run_id":    gorm.Expr("VALUES(parse_run_id)"),
			"reference_price": gorm.Expr("VALUES(reference_price)"),
			"confidence":      gorm.Expr("VALUES(confidence)"),
			"status":          gorm.Expr("VALUES(status)"),
			"thesis":          gorm.Expr("VALUES(thesis)"),
			"config_version":  gorm.Expr("VALUES(config_version)"),
			"rule_version":    gorm.Expr("VALUES(rule_version)"),
			"updated_at":      gorm.Expr("CURRENT_TIMESTAMP"),
		}),
	}).Create(model).Error
}

func (*RecommendationEventDML) QueryByID(ctx context.Context, db *gorm.DB, id int64) (*db_model.RecommendationEvent, error) {
	var model db_model.RecommendationEvent
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

func (*RecommendationEventDML) QueryByDedupeKey(ctx context.Context, db *gorm.DB, dedupeKey string) (*db_model.RecommendationEvent, error) {
	var model db_model.RecommendationEvent
	err := ApplyQuery(ctx, db, QueryParam{
		Where: []Condition{Eq("dedupe_key", dedupeKey)},
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

func (*RecommendationEventDML) QueryByDocumentID(ctx context.Context, db *gorm.DB, documentID int64) ([]db_model.RecommendationEvent, error) {
	return RecommendationEvents.QueryByParam(ctx, db, QueryParam{
		Where:  []Condition{Eq("source_document_id", documentID)},
		Orders: []OrderParam{OrderBy("created_at", true), OrderBy("id", true)},
	})
}

func (*RecommendationEventDML) QueryLatest(ctx context.Context, db *gorm.DB, limit int) ([]db_model.RecommendationEvent, error) {
	return RecommendationEvents.QueryByParam(ctx, db, QueryParam{
		Orders: []OrderParam{OrderBy("created_at", true), OrderBy("id", true)},
		Limit:  limit,
	})
}

func (*RecommendationEventDML) QueryByParam(ctx context.Context, db *gorm.DB, param QueryParam) ([]db_model.RecommendationEvent, error) {
	var models []db_model.RecommendationEvent
	if err := ApplyQuery(ctx, db, param).Find(&models).Error; err != nil {
		return nil, err
	}
	return models, nil
}
