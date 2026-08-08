package dal

import (
	"context"

	"finance-sys/internal/domain/db_model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var SecurityAliases = &SecurityAliasDML{}

type SecurityAliasDML struct{}

func (*SecurityAliasDML) Create(ctx context.Context, db *gorm.DB, model *db_model.SecurityAlias) error {
	return db.WithContext(ctx).Create(model).Error
}

func (*SecurityAliasDML) UpsertByAliasAndSecurityID(ctx context.Context, db *gorm.DB, model *db_model.SecurityAlias) error {
	return db.WithContext(ctx).Clauses(securityAliasUpsertClause()).Create(model).Error
}

func (*SecurityAliasDML) UpsertBatch(ctx context.Context, db *gorm.DB, models []db_model.SecurityAlias, batchSize int) error {
	if len(models) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = 500
	}
	return db.WithContext(ctx).Clauses(securityAliasUpsertClause()).CreateInBatches(&models, batchSize).Error
}

func securityAliasUpsertClause() clause.OnConflict {
	return clause.OnConflict{
		Columns: []clause.Column{
			{Name: "normalized_alias"},
			{Name: "security_master_id"},
			{Name: "alias_type"},
		},
		DoUpdates: clause.Assignments(map[string]any{
			"alias":      gorm.Expr("VALUES(alias)"),
			"source":     gorm.Expr("VALUES(source)"),
			"confidence": gorm.Expr("VALUES(confidence)"),
			"is_active":  gorm.Expr("VALUES(is_active)"),
			"updated_at": gorm.Expr("CURRENT_TIMESTAMP"),
		}),
	}
}

func (*SecurityAliasDML) QueryByNormalizedAlias(ctx context.Context, db *gorm.DB, normalizedAlias string) ([]db_model.SecurityAlias, error) {
	return SecurityAliases.QueryByParam(ctx, db, QueryParam{
		Where:  []Condition{Eq("normalized_alias", normalizedAlias)},
		Orders: []OrderParam{OrderBy("confidence", true), OrderBy("id", false)},
	})
}

func (*SecurityAliasDML) QueryActiveByNormalizedAlias(ctx context.Context, db *gorm.DB, normalizedAlias string) ([]db_model.SecurityAlias, error) {
	return SecurityAliases.QueryByParam(ctx, db, QueryParam{
		Where: []Condition{
			Eq("normalized_alias", normalizedAlias),
			Eq("is_active", true),
		},
		Orders: []OrderParam{OrderBy("confidence", true), OrderBy("id", false)},
	})
}

func (*SecurityAliasDML) QueryBySecurityMasterID(ctx context.Context, db *gorm.DB, securityMasterID int64) ([]db_model.SecurityAlias, error) {
	return SecurityAliases.QueryByParam(ctx, db, QueryParam{
		Where:  []Condition{Eq("security_master_id", securityMasterID)},
		Orders: []OrderParam{OrderBy("id", false)},
	})
}

func (*SecurityAliasDML) QueryByParam(ctx context.Context, db *gorm.DB, param QueryParam) ([]db_model.SecurityAlias, error) {
	var models []db_model.SecurityAlias
	if err := ApplyQuery(ctx, db, param).Find(&models).Error; err != nil {
		return nil, err
	}
	return models, nil
}
