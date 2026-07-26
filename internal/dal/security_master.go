package dal

import (
	"context"
	"errors"

	"finance-sys/internal/domain/db_model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var SecurityMasters = &SecurityMasterDML{}

type SecurityMasterDML struct{}

func (*SecurityMasterDML) Create(ctx context.Context, db *gorm.DB, model *db_model.SecurityMaster) error {
	return db.WithContext(ctx).Create(model).Error
}

func (*SecurityMasterDML) UpsertByTSCode(ctx context.Context, db *gorm.DB, model *db_model.SecurityMaster) error {
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "ts_code"}},
		DoUpdates: clause.Assignments(map[string]any{
			"symbol":      gorm.Expr("VALUES(symbol)"),
			"name":        gorm.Expr("VALUES(name)"),
			"full_name":   gorm.Expr("VALUES(full_name)"),
			"exchange":    gorm.Expr("VALUES(exchange)"),
			"market":      gorm.Expr("VALUES(market)"),
			"asset_type":  gorm.Expr("VALUES(asset_type)"),
			"list_status": gorm.Expr("VALUES(list_status)"),
			"list_date":   gorm.Expr("VALUES(list_date)"),
			"delist_date": gorm.Expr("VALUES(delist_date)"),
			"industry":    gorm.Expr("VALUES(industry)"),
			"is_active":   gorm.Expr("VALUES(is_active)"),
			"source":      gorm.Expr("VALUES(source)"),
			"raw_json":    gorm.Expr("VALUES(raw_json)"),
			"updated_at":  gorm.Expr("CURRENT_TIMESTAMP"),
		}),
	}).Create(model).Error
}

func (*SecurityMasterDML) QueryByID(ctx context.Context, db *gorm.DB, id int64) (*db_model.SecurityMaster, error) {
	var model db_model.SecurityMaster
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

func (*SecurityMasterDML) QueryByTSCode(ctx context.Context, db *gorm.DB, tsCode string) (*db_model.SecurityMaster, error) {
	var model db_model.SecurityMaster
	err := ApplyQuery(ctx, db, QueryParam{
		Where: []Condition{Eq("ts_code", tsCode)},
	}).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &model, nil
}

func (*SecurityMasterDML) QueryBySymbol(ctx context.Context, db *gorm.DB, symbol string) ([]db_model.SecurityMaster, error) {
	return SecurityMasters.QueryByParam(ctx, db, QueryParam{
		Where:  []Condition{Eq("symbol", symbol)},
		Orders: []OrderParam{OrderBy("market", false), OrderBy("ts_code", false)},
	})
}

func (*SecurityMasterDML) QueryActiveByName(ctx context.Context, db *gorm.DB, name string) ([]db_model.SecurityMaster, error) {
	return SecurityMasters.QueryByParam(ctx, db, QueryParam{
		Where: []Condition{
			Eq("name", name),
			Eq("list_status", "L"),
			Eq("is_active", true),
		},
		Orders: []OrderParam{OrderBy("ts_code", false)},
	})
}

func (*SecurityMasterDML) QueryByParam(ctx context.Context, db *gorm.DB, param QueryParam) ([]db_model.SecurityMaster, error) {
	var models []db_model.SecurityMaster
	if err := ApplyQuery(ctx, db, param).Find(&models).Error; err != nil {
		return nil, err
	}
	return models, nil
}

func (*SecurityMasterDML) QueryActiveByAssetTypes(ctx context.Context, db *gorm.DB, assetTypes []string) ([]db_model.SecurityMaster, error) {
	var models []db_model.SecurityMaster
	tx := db.WithContext(ctx).
		Where("is_active = ?", true).
		Where("list_status = ?", "L").
		Order("ts_code ASC")
	if len(assetTypes) > 0 {
		tx = tx.Where("asset_type IN ?", assetTypes)
	}
	if err := tx.Find(&models).Error; err != nil {
		return nil, err
	}
	return models, nil
}
