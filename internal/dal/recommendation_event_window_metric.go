package dal

import (
	"context"

	"finance-sys/internal/domain/db_model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var RecommendationEventWindowMetrics = &RecommendationEventWindowMetricDML{}

type RecommendationEventWindowMetricDML struct{}

func (*RecommendationEventWindowMetricDML) UpsertBatch(ctx context.Context, db *gorm.DB, models []db_model.RecommendationEventWindowMetric) error {
	if len(models) == 0 {
		return nil
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "recommendation_event_id"}, {Name: "window_days"}, {Name: "quote_source"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"blogger_id", "security_master_id", "ts_code", "symbol", "security_name", "asset_type", "market", "industry", "sector_type",
			"direction", "recommend_date", "status", "reason_code", "reason_message", "base_date", "base_close_price",
			"entry_date", "entry_price", "exit_date", "exit_close_price", "expected_quote_count", "actual_quote_count",
			"missing_quote_count", "raw_return_ratio", "direction_return_ratio", "max_favorable_return_ratio",
			"max_adverse_return_ratio", "max_drawdown_ratio", "win_flag", "best_trade_date", "worst_trade_date",
			"evaluation_run_id", "calc_version", "config_version", "calculated_at", "updated_at",
		}),
	}).CreateInBatches(models, 300).Error
}

func (*RecommendationEventWindowMetricDML) QueryByEventID(ctx context.Context, db *gorm.DB, eventID int64) ([]db_model.RecommendationEventWindowMetric, error) {
	var models []db_model.RecommendationEventWindowMetric
	err := db.WithContext(ctx).
		Where("recommendation_event_id = ?", eventID).
		Order("window_days ASC").
		Find(&models).Error
	return models, err
}

func (*RecommendationEventWindowMetricDML) QueryByEventIDAndSource(ctx context.Context, db *gorm.DB, eventID int64, source string) ([]db_model.RecommendationEventWindowMetric, error) {
	var models []db_model.RecommendationEventWindowMetric
	err := db.WithContext(ctx).
		Where("recommendation_event_id = ?", eventID).
		Where("quote_source = ?", source).
		Find(&models).Error
	return models, err
}

func (*RecommendationEventWindowMetricDML) QueryByEventIDsAndSource(ctx context.Context, db *gorm.DB, eventIDs []int64, source string) ([]db_model.RecommendationEventWindowMetric, error) {
	if len(eventIDs) == 0 {
		return []db_model.RecommendationEventWindowMetric{}, nil
	}
	var models []db_model.RecommendationEventWindowMetric
	err := db.WithContext(ctx).
		Where("recommendation_event_id IN ?", eventIDs).
		Where("quote_source = ?", source).
		Find(&models).Error
	return models, err
}

func (*RecommendationEventWindowMetricDML) QueryByParam(ctx context.Context, db *gorm.DB, param QueryParam) ([]db_model.RecommendationEventWindowMetric, error) {
	var models []db_model.RecommendationEventWindowMetric
	if err := ApplyQuery(ctx, db, param).Find(&models).Error; err != nil {
		return nil, err
	}
	return models, nil
}
