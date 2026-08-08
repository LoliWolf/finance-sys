package dal

import (
	"context"
	"errors"
	"sort"

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
	return securityMasterUpsert(ctx, db, model)
}

func (*SecurityMasterDML) UpsertBatch(ctx context.Context, db *gorm.DB, models []db_model.SecurityMaster, batchSize int) error {
	if len(models) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = 500
	}
	rows := make([]map[string]any, 0, len(models))
	for i := range models {
		rows = append(rows, securityMasterWriteRow(&models[i]))
	}
	return db.WithContext(ctx).
		Model(&db_model.SecurityMaster{}).
		Clauses(securityMasterUpsertClause()).
		CreateInBatches(rows, batchSize).Error
}

func securityMasterUpsert(ctx context.Context, db *gorm.DB, model *db_model.SecurityMaster) error {
	return db.WithContext(ctx).
		Model(&db_model.SecurityMaster{}).
		Clauses(securityMasterUpsertClause()).
		Create(securityMasterWriteRow(model)).Error
}

func securityMasterUpsertClause() clause.OnConflict {
	return clause.OnConflict{
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
			"sector_type": gorm.Expr("VALUES(sector_type)"),
			"is_active":   gorm.Expr("VALUES(is_active)"),
			"source":      gorm.Expr("VALUES(source)"),
			"raw_json":    gorm.Expr("VALUES(raw_json)"),
			"updated_at":  gorm.Expr("CURRENT_TIMESTAMP"),
		}),
	}
}

func securityMasterWriteRow(model *db_model.SecurityMaster) map[string]any {
	return map[string]any{
		"ts_code":     model.TSCode,
		"symbol":      model.Symbol,
		"name":        model.Name,
		"full_name":   model.FullName,
		"exchange":    model.Exchange,
		"market":      model.Market,
		"asset_type":  model.AssetType,
		"list_status": model.ListStatus,
		"list_date":   model.ListDate,
		"delist_date": model.DelistDate,
		"industry":    model.Industry,
		"sector_type": model.SectorType,
		"is_active":   model.IsActive,
		"source":      model.Source,
		"raw_json":    model.RawJSON,
	}
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

func (*SecurityMasterDML) QueryByTSCodes(ctx context.Context, db *gorm.DB, tsCodes []string) ([]db_model.SecurityMaster, error) {
	if len(tsCodes) == 0 {
		return nil, nil
	}
	const queryBatchSize = 500
	models := make([]db_model.SecurityMaster, 0, len(tsCodes))
	for start := 0; start < len(tsCodes); start += queryBatchSize {
		end := min(start+queryBatchSize, len(tsCodes))
		var batch []db_model.SecurityMaster
		if err := db.WithContext(ctx).Where("ts_code IN ?", tsCodes[start:end]).Find(&batch).Error; err != nil {
			return nil, err
		}
		models = append(models, batch...)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].TSCode < models[j].TSCode })
	return models, nil
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

func (*SecurityMasterDML) DeactivateMissingBySourceAndAssetType(ctx context.Context, db *gorm.DB, source string, assetType string, activeTSCodes []string) error {
	tx := db.WithContext(ctx).Model(&db_model.SecurityMaster{}).
		Where("source = ? AND asset_type = ?", source, assetType)
	if len(activeTSCodes) > 0 {
		tx = tx.Where("ts_code NOT IN ?", activeTSCodes)
	}
	return tx.Updates(map[string]any{
		"is_active":  false,
		"updated_at": gorm.Expr("CURRENT_TIMESTAMP"),
	}).Error
}
