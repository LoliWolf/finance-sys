package dal

import (
	"context"

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
		Columns: []clause.Column{{Name: "ts_code"}, {Name: "trade_date"}, {Name: "source"}},
		DoUpdates: clause.Assignments(map[string]any{
			"security_master_id": gorm.Expr("VALUES(security_master_id)"),
			"symbol":             gorm.Expr("VALUES(symbol)"),
			"security_name":      gorm.Expr("VALUES(security_name)"),
			"exchange":           gorm.Expr("VALUES(exchange)"),
			"market":             gorm.Expr("VALUES(market)"),
			"asset_type":         gorm.Expr("VALUES(asset_type)"),
			"industry":           gorm.Expr("VALUES(industry)"),
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
		}),
	}).CreateInBatches(models, batchSize).Error
}
