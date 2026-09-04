package dal

import (
	"context"
	"errors"
	"time"

	"finance-sys/internal/domain/db_model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var RecommendationEvents = &RecommendationEventDML{}

type RecommendationEventDML struct{}

type RecommendationEventEvaluationFilter struct {
	DateFrom   *time.Time
	DateTo     *time.Time
	BloggerIDs []int64
	Symbols    []string
	EventIDs   []int64
	OnlyActive bool
	AfterID    int64
	Limit      int
}

func (*RecommendationEventDML) Create(ctx context.Context, db *gorm.DB, model *db_model.RecommendationEvent) error {
	return db.WithContext(ctx).Create(model).Error
}

func (*RecommendationEventDML) UpsertByDedupeKey(ctx context.Context, db *gorm.DB, model *db_model.RecommendationEvent) error {
	sameSourceDocument := "source_document_id = VALUES(source_document_id)"
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "dedupe_key"}},
		DoUpdates: clause.Assignments(map[string]any{
			"plan_id":         gorm.Expr("IF(" + sameSourceDocument + ", VALUES(plan_id), plan_id)"),
			"parse_run_id":    gorm.Expr("IF(" + sameSourceDocument + ", VALUES(parse_run_id), parse_run_id)"),
			"reference_price": gorm.Expr("IF(" + sameSourceDocument + ", VALUES(reference_price), reference_price)"),
			"confidence":      gorm.Expr("IF(" + sameSourceDocument + ", VALUES(confidence), confidence)"),
			"status":          gorm.Expr("IF(" + sameSourceDocument + ", VALUES(status), status)"),
			"thesis":          gorm.Expr("IF(" + sameSourceDocument + ", VALUES(thesis), thesis)"),
			"config_version":  gorm.Expr("IF(" + sameSourceDocument + ", VALUES(config_version), config_version)"),
			"rule_version":    gorm.Expr("IF(" + sameSourceDocument + ", VALUES(rule_version), rule_version)"),
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

func (*RecommendationEventDML) DeleteDirectSectorEventsByDocumentID(ctx context.Context, db *gorm.DB, documentID int64) error {
	return db.WithContext(ctx).
		Where("source_document_id = ? AND asset_type = ? AND plan_id IS NULL", documentID, "SECTOR").
		Delete(&db_model.RecommendationEvent{}).Error
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

func (*RecommendationEventDML) QueryForEvaluation(ctx context.Context, db *gorm.DB, filter RecommendationEventEvaluationFilter) ([]db_model.RecommendationEvent, error) {
	var models []db_model.RecommendationEvent
	tx := db.WithContext(ctx).Order("id ASC")
	if filter.DateFrom != nil {
		tx = tx.Where("recommend_date >= ?", *filter.DateFrom)
	}
	if filter.DateTo != nil {
		tx = tx.Where("recommend_date <= ?", *filter.DateTo)
	}
	if len(filter.BloggerIDs) > 0 {
		tx = tx.Where("blogger_id IN ?", filter.BloggerIDs)
	}
	if len(filter.Symbols) > 0 {
		tx = tx.Where("symbol IN ?", filter.Symbols)
	}
	if len(filter.EventIDs) > 0 {
		tx = tx.Where("id IN ?", filter.EventIDs)
	}
	if filter.OnlyActive {
		tx = tx.Where("status = ?", "ACTIVE")
	}
	if filter.AfterID > 0 {
		tx = tx.Where("id > ?", filter.AfterID)
	}
	if filter.Limit > 0 {
		tx = tx.Limit(filter.Limit)
	}
	if err := tx.Find(&models).Error; err != nil {
		return nil, err
	}
	return models, nil
}

func (*RecommendationEventDML) QueryForTrading(ctx context.Context, db *gorm.DB, asOf time.Time, minConfidence float64, afterID int64, limit int) ([]db_model.RecommendationEvent, error) {
	var models []db_model.RecommendationEvent
	tx := db.WithContext(ctx).
		Where("asset_type IN ?", []string{"A_SHARE", "STOCK", "ETF"}).
		Where("market IN ?", []string{"SH", "SZ"}).
		Where("direction IN ?", []string{"LONG", "BUY"}).
		Where("status = ?", "ACTIVE").
		Where("confidence >= ?", minConfidence).
		Where("recommend_date <= ?", asOf).
		Where("created_at <= ?", asOf).
		Order("recommend_date DESC, id DESC")
	if afterID > 0 {
		tx = tx.Where("id < ?", afterID)
	}
	if limit > 0 {
		tx = tx.Limit(limit)
	}
	if err := tx.Find(&models).Error; err != nil {
		return nil, err
	}
	return models, nil
}
