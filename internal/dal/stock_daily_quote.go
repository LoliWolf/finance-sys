package dal

import (
	"context"
	"errors"
	"time"

	"finance-sys/internal/domain/db_model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var StockDailyQuotes = &StockDailyQuoteDML{}

type StockDailyQuoteDML struct{}

func (*StockDailyQuoteDML) UpsertBatch(ctx context.Context, db *gorm.DB, models []db_model.StockDailyQuote) error {
	if len(models) == 0 {
		return nil
	}
	const batchSize = 300
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "ts_code"}, {Name: "trade_date"}, {Name: "source"}},
		DoUpdates: clause.Assignments(stockDailyQuoteUpsertAssignments()),
	}).CreateInBatches(models, batchSize).Error
}

func stockDailyQuoteUpsertAssignments() map[string]any {
	return map[string]any{
		"security_master_id": gorm.Expr("VALUES(security_master_id)"),
		"symbol":             gorm.Expr("VALUES(symbol)"),
		"security_name":      gorm.Expr("VALUES(security_name)"),
		"exchange":           gorm.Expr("VALUES(exchange)"),
		"market":             gorm.Expr("VALUES(market)"),
		"asset_type":         gorm.Expr("VALUES(asset_type)"),
		"industry":           gorm.Expr("VALUES(industry)"),
		"sector_type":        gorm.Expr("VALUES(sector_type)"),
		"list_status":        gorm.Expr("VALUES(list_status)"),
		"open_price":         gorm.Expr("VALUES(open_price)"),
		"high_price":         gorm.Expr("VALUES(high_price)"),
		"low_price":          gorm.Expr("VALUES(low_price)"),
		"close_price":        gorm.Expr("VALUES(close_price)"),
		"pre_close_price":    gorm.Expr("VALUES(pre_close_price)"),
		"change_amount":      gorm.Expr("VALUES(change_amount)"),
		"pct_chg":            gorm.Expr("VALUES(pct_chg)"),
		"volume":             gorm.Expr("VALUES(volume)"),
		"amount":             gorm.Expr("VALUES(amount)"),
		"tushare_content":    gorm.Expr("VALUES(tushare_content)"),
		"config_version":     gorm.Expr("VALUES(config_version)"),
		"updated_at":         gorm.Expr("CURRENT_TIMESTAMP"),
	}
}

func (*StockDailyQuoteDML) QueryByTSCodeRange(ctx context.Context, db *gorm.DB, tsCode string, source string, dateFrom time.Time, dateTo time.Time) ([]db_model.StockDailyQuote, error) {
	var models []db_model.StockDailyQuote
	tx := db.WithContext(ctx).
		Where("ts_code = ?", tsCode).
		Where("source = ?", source).
		Where("trade_date >= ?", dateFrom).
		Order("trade_date ASC")
	if !dateTo.IsZero() {
		tx = tx.Where("trade_date <= ?", dateTo)
	}
	if err := tx.Find(&models).Error; err != nil {
		return nil, err
	}
	return models, nil
}

func (*StockDailyQuoteDML) QueryTradingDates(ctx context.Context, db *gorm.DB, source string, dateFrom time.Time, dateTo time.Time) ([]time.Time, error) {
	var dates []time.Time
	tx := db.WithContext(ctx).
		Model(&db_model.StockDailyQuote{}).
		Distinct("trade_date").
		Where("source = ?", source).
		Where("trade_date >= ?", dateFrom).
		Order("trade_date ASC")
	if !dateTo.IsZero() {
		tx = tx.Where("trade_date <= ?", dateTo)
	}
	if err := tx.Pluck("trade_date", &dates).Error; err != nil {
		return nil, err
	}
	return dates, nil
}

func (*StockDailyQuoteDML) QueryLatestTradeDate(ctx context.Context, db *gorm.DB, source string) (time.Time, error) {
	var model db_model.StockDailyQuote
	err := db.WithContext(ctx).
		Where("source = ?", source).
		Order("trade_date DESC").
		First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return time.Time{}, ErrNotFound
	}
	if err != nil {
		return time.Time{}, err
	}
	return model.TradeDate, nil
}
