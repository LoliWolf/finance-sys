package dal

import (
	"context"
	"errors"
	"time"

	"finance-sys/internal/domain/db_model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	TradingAgentRuns           = &TradingAgentRunDML{}
	TradingIntents             = &TradingIntentDML{}
	TradingRiskChecks          = &TradingRiskCheckDML{}
	TradingOrders              = &TradingOrderDML{}
	TradingOrderEvents         = &TradingOrderEventDML{}
	TradingFills               = &TradingFillDML{}
	TradingAccountSnapshots    = &TradingAccountSnapshotDML{}
	TradingPositionSnapshots   = &TradingPositionSnapshotDML{}
	TradingReconciliationRuns  = &TradingReconciliationRunDML{}
	TradingReconciliationDiffs = &TradingReconciliationDiffDML{}
	TradingRuntimeControls     = &TradingRuntimeControlDML{}
	TradingDailySessions       = &TradingDailySessionDML{}
	TradingPositionCycles      = &TradingPositionCycleDML{}
	TradingSkillDecisions      = &TradingSkillDecisionDML{}
	TradingBoardCapabilities   = &TradingBoardCapabilityDML{}
)

type TradingAgentRunDML struct{}

func (*TradingAgentRunDML) Create(ctx context.Context, db *gorm.DB, model *db_model.TradingAgentRun) error {
	return db.WithContext(ctx).Create(model).Error
}

func (*TradingAgentRunDML) QueryByRunKey(ctx context.Context, db *gorm.DB, runKey string) (*db_model.TradingAgentRun, error) {
	var model db_model.TradingAgentRun
	err := db.WithContext(ctx).Where("run_key = ?", runKey).First(&model).Error
	return tradingRunResult(&model, err)
}

func (*TradingAgentRunDML) QueryByID(ctx context.Context, db *gorm.DB, id int64) (*db_model.TradingAgentRun, error) {
	var model db_model.TradingAgentRun
	err := db.WithContext(ctx).Where("id = ?", id).First(&model).Error
	return tradingRunResult(&model, err)
}

func tradingRunResult(model *db_model.TradingAgentRun, err error) (*db_model.TradingAgentRun, error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return model, nil
}

func (*TradingAgentRunDML) Update(ctx context.Context, db *gorm.DB, id int64, values map[string]any) error {
	values["updated_at"] = gorm.Expr("CURRENT_TIMESTAMP(3)")
	return db.WithContext(ctx).Model(&db_model.TradingAgentRun{}).Where("id = ?", id).Updates(values).Error
}

type TradingIntentDML struct{}

func (*TradingIntentDML) Create(ctx context.Context, db *gorm.DB, model *db_model.TradingIntent) error {
	return db.WithContext(ctx).Create(model).Error
}

func (*TradingIntentDML) QueryByKey(ctx context.Context, db *gorm.DB, key string) (*db_model.TradingIntent, error) {
	var model db_model.TradingIntent
	err := db.WithContext(ctx).Where("intent_key = ?", key).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &model, err
}

func (*TradingIntentDML) QueryByID(ctx context.Context, db *gorm.DB, id int64) (*db_model.TradingIntent, error) {
	var model db_model.TradingIntent
	err := db.WithContext(ctx).Where("id = ?", id).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &model, err
}

func (*TradingIntentDML) QueryByRunID(ctx context.Context, db *gorm.DB, runID int64) ([]db_model.TradingIntent, error) {
	var models []db_model.TradingIntent
	err := db.WithContext(ctx).Where("trading_agent_run_id = ?", runID).Order("id ASC").Find(&models).Error
	return models, err
}

func (*TradingIntentDML) List(ctx context.Context, db *gorm.DB, status, symbol string, limit int) ([]db_model.TradingIntent, error) {
	var models []db_model.TradingIntent
	tx := db.WithContext(ctx).Order("created_at DESC, id DESC")
	if status != "" {
		tx = tx.Where("status = ?", status)
	}
	if symbol != "" {
		tx = tx.Where("symbol = ?", symbol)
	}
	if limit > 0 {
		tx = tx.Limit(limit)
	}
	err := tx.Find(&models).Error
	return models, err
}

func (*TradingIntentDML) Update(ctx context.Context, db *gorm.DB, id int64, values map[string]any) error {
	values["updated_at"] = gorm.Expr("CURRENT_TIMESTAMP(3)")
	return db.WithContext(ctx).Model(&db_model.TradingIntent{}).Where("id = ?", id).Updates(values).Error
}

func (*TradingIntentDML) ClaimNextReady(ctx context.Context, db *gorm.DB, workerID, claimToken string, now, deadline time.Time) (*db_model.TradingIntent, error) {
	var model db_model.TradingIntent
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ?", "READY_FOR_EXECUTION").
			Where("execution_status IN ?", []string{"READY", "RETRY"}).
			Where("valid_from <= ? AND valid_until >= ?", now, now).
			Where("next_execution_at IS NULL OR next_execution_at <= ?", now).
			Where("execution_claim_deadline IS NULL OR execution_claim_deadline < ?", now).
			Order("CASE WHEN trade_action = 'SELL' THEN 0 ELSE 1 END, valid_until ASC, id ASC").
			First(&model)
		if errors.Is(query.Error, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if query.Error != nil {
			return query.Error
		}
		return tx.Model(&db_model.TradingIntent{}).Where("id = ?", model.ID).Updates(map[string]any{
			"execution_status": "CLAIMED", "execution_claim_token": claimToken, "execution_claimed_by": workerID,
			"execution_claimed_at": now, "execution_claim_deadline": deadline,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	model.ExecutionStatus = "CLAIMED"
	model.ExecutionClaimToken = claimToken
	model.ExecutionClaimedBy = workerID
	model.ExecutionClaimedAt = &now
	model.ExecutionClaimDeadline = &deadline
	return &model, nil
}

type TradingRiskCheckDML struct{}

func (*TradingRiskCheckDML) CreateBatch(ctx context.Context, db *gorm.DB, models []db_model.TradingRiskCheck) error {
	if len(models) == 0 {
		return nil
	}
	return db.WithContext(ctx).Create(&models).Error
}

type TradingOrderDML struct{}

func (*TradingOrderDML) Create(ctx context.Context, db *gorm.DB, model *db_model.TradingOrder) error {
	return db.WithContext(ctx).Create(model).Error
}

func (*TradingOrderDML) QueryByClientOrderID(ctx context.Context, db *gorm.DB, clientOrderID string) (*db_model.TradingOrder, error) {
	var model db_model.TradingOrder
	err := db.WithContext(ctx).Where("client_order_id = ?", clientOrderID).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &model, err
}

func (*TradingOrderDML) List(ctx context.Context, db *gorm.DB, status, symbol string, limit int) ([]db_model.TradingOrder, error) {
	var models []db_model.TradingOrder
	tx := db.WithContext(ctx).Order("created_at DESC, id DESC")
	if status != "" {
		tx = tx.Where("status = ?", status)
	}
	if symbol != "" {
		tx = tx.Where("symbol = ?", symbol)
	}
	if limit > 0 {
		tx = tx.Limit(limit)
	}
	err := tx.Find(&models).Error
	return models, err
}

func (*TradingOrderDML) QueryOpen(ctx context.Context, db *gorm.DB) ([]db_model.TradingOrder, error) {
	var models []db_model.TradingOrder
	err := db.WithContext(ctx).Where("status IN ?", []string{"DISPATCH_PENDING", "BRIDGE_QUEUED", "SUBMITTED", "PARTIALLY_FILLED", "UNKNOWN"}).Order("id ASC").Find(&models).Error
	return models, err
}

func (*TradingOrderDML) Update(ctx context.Context, db *gorm.DB, id int64, values map[string]any) error {
	values["updated_at"] = gorm.Expr("CURRENT_TIMESTAMP(3)")
	return db.WithContext(ctx).Model(&db_model.TradingOrder{}).Where("id = ?", id).Updates(values).Error
}

func (*TradingOrderDML) CountCreatedSince(ctx context.Context, db *gorm.DB, since time.Time) (int64, error) {
	var count int64
	err := db.WithContext(ctx).Model(&db_model.TradingOrder{}).Where("created_at >= ?", since).Count(&count).Error
	return count, err
}

func (*TradingOrderDML) SumFilledAmountSince(ctx context.Context, db *gorm.DB, since time.Time) (string, error) {
	var amount string
	err := db.WithContext(ctx).Model(&db_model.TradingOrder{}).
		Select("COALESCE(SUM(filled_amount), 0)").
		Where("created_at >= ?", since).
		Scan(&amount).Error
	if amount == "" {
		amount = "0"
	}
	return amount, err
}

func (*TradingOrderDML) ExistsRecentForIntent(ctx context.Context, db *gorm.DB, recommendationEventID *int64, symbol string, since time.Time) (bool, error) {
	var count int64
	tx := db.WithContext(ctx).Table("trading_orders AS o").
		Joins("JOIN trading_intents AS i ON i.id = o.trading_intent_id").
		Where("o.created_at >= ?", since).
		Where("o.status NOT IN ?", []string{"REJECTED", "DRY_RUN"})
	if recommendationEventID != nil {
		tx = tx.Where("i.recommendation_event_id = ?", *recommendationEventID)
	} else {
		tx = tx.Where("i.symbol = ?", symbol)
	}
	err := tx.Count(&count).Error
	return count > 0, err
}

type TradingOrderEventDML struct{}

func (*TradingOrderEventDML) CreateIdempotent(ctx context.Context, db *gorm.DB, model *db_model.TradingOrderEvent) (bool, error) {
	result := db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(model)
	return result.RowsAffected > 0, result.Error
}

type TradingFillDML struct{}

func (*TradingFillDML) CreateIdempotent(ctx context.Context, db *gorm.DB, model *db_model.TradingFill) (bool, error) {
	result := db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(model)
	return result.RowsAffected > 0, result.Error
}

func (*TradingFillDML) ListByAccount(ctx context.Context, db *gorm.DB, accountID string) ([]db_model.TradingFill, error) {
	var models []db_model.TradingFill
	err := db.WithContext(ctx).Where("account_id = ?", accountID).Order("traded_at ASC, id ASC").Find(&models).Error
	return models, err
}

func (*TradingFillDML) UpdateCommission(ctx context.Context, db *gorm.DB, id int64, values map[string]any) error {
	return db.WithContext(ctx).Model(&db_model.TradingFill{}).Where("id = ?", id).Updates(values).Error
}

func (*TradingFillDML) UpdateCommissionByExec(ctx context.Context, db *gorm.DB, accountID, execID string, values map[string]any) error {
	return db.WithContext(ctx).Model(&db_model.TradingFill{}).
		Where("account_id = ? AND exec_id = ?", accountID, execID).
		Updates(values).Error
}

func (*TradingFillDML) SumCommissionByOrder(ctx context.Context, db *gorm.DB, orderID int64) (string, error) {
	var total string
	err := db.WithContext(ctx).Model(&db_model.TradingFill{}).
		Select("COALESCE(SUM(commission), 0)").
		Where("trading_order_id = ?", orderID).
		Scan(&total).Error
	if total == "" {
		total = "0"
	}
	return total, err
}

type TradingAccountSnapshotDML struct{}

func (*TradingAccountSnapshotDML) Create(ctx context.Context, db *gorm.DB, model *db_model.TradingAccountSnapshot) error {
	return db.WithContext(ctx).Create(model).Error
}

func (*TradingAccountSnapshotDML) Latest(ctx context.Context, db *gorm.DB, accountID string) (*db_model.TradingAccountSnapshot, error) {
	var model db_model.TradingAccountSnapshot
	tx := db.WithContext(ctx)
	if accountID != "" {
		tx = tx.Where("account_id = ?", accountID)
	}
	err := tx.Order("snapshot_at DESC, id DESC").First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &model, err
}

type TradingPositionSnapshotDML struct{}

func (*TradingPositionSnapshotDML) CreateBatch(ctx context.Context, db *gorm.DB, models []db_model.TradingPositionSnapshot) error {
	if len(models) == 0 {
		return nil
	}
	return db.WithContext(ctx).Create(&models).Error
}

func (*TradingPositionSnapshotDML) ByAccountSnapshot(ctx context.Context, db *gorm.DB, snapshotID int64) ([]db_model.TradingPositionSnapshot, error) {
	var models []db_model.TradingPositionSnapshot
	err := db.WithContext(ctx).Where("account_snapshot_id = ?", snapshotID).Order("eastmoney_symbol ASC").Find(&models).Error
	return models, err
}

type TradingReconciliationRunDML struct{}

func (*TradingReconciliationRunDML) Create(ctx context.Context, db *gorm.DB, model *db_model.TradingReconciliationRun) error {
	return db.WithContext(ctx).Create(model).Error
}

func (*TradingReconciliationRunDML) Update(ctx context.Context, db *gorm.DB, id int64, values map[string]any) error {
	values["updated_at"] = gorm.Expr("CURRENT_TIMESTAMP(3)")
	return db.WithContext(ctx).Model(&db_model.TradingReconciliationRun{}).Where("id = ?", id).Updates(values).Error
}

type TradingReconciliationDiffDML struct{}

func (*TradingReconciliationDiffDML) CreateBatch(ctx context.Context, db *gorm.DB, models []db_model.TradingReconciliationDiff) error {
	if len(models) == 0 {
		return nil
	}
	return db.WithContext(ctx).Create(&models).Error
}

type TradingRuntimeControlDML struct{}

func (*TradingRuntimeControlDML) KillSwitch(ctx context.Context, db *gorm.DB) (*db_model.TradingRuntimeControl, error) {
	var model db_model.TradingRuntimeControl
	err := db.WithContext(ctx).Where("control_key = ?", "KILL_SWITCH").First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &model, err
}

func (*TradingRuntimeControlDML) SetKillSwitch(ctx context.Context, db *gorm.DB, enabled bool, reason, changedBy string, configVersion int64) error {
	now := time.Now()
	model := db_model.TradingRuntimeControl{ControlKey: "KILL_SWITCH", Enabled: enabled, Reason: reason, ChangedBy: changedBy, ConfigVersion: configVersion, ChangedAt: now}
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "control_key"}},
		DoUpdates: clause.Assignments(map[string]any{
			"enabled": enabled, "reason": reason, "changed_by": changedBy, "config_version": configVersion,
			"changed_at": now, "updated_at": gorm.Expr("CURRENT_TIMESTAMP(3)"),
		}),
	}).Create(&model).Error
}

type TradingDailySessionDML struct{}

func (*TradingDailySessionDML) Upsert(ctx context.Context, db *gorm.DB, model *db_model.TradingDailySession) error {
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "session_key"}},
		DoUpdates: clause.AssignmentColumns([]string{"status", "preflight_status", "auth_state", "bridge_state", "decision_run_id", "opened_at", "closed_at", "preflight_json", "summary_json", "error_code", "error_message", "config_version", "updated_at"}),
	}).Create(model).Error
}

func (*TradingDailySessionDML) List(ctx context.Context, db *gorm.DB, accountID string, limit int) ([]db_model.TradingDailySession, error) {
	var models []db_model.TradingDailySession
	tx := db.WithContext(ctx).Order("trade_date DESC, id DESC")
	if accountID != "" {
		tx = tx.Where("account_id = ?", accountID)
	}
	if limit > 0 {
		tx = tx.Limit(limit)
	}
	err := tx.Find(&models).Error
	return models, err
}

type TradingPositionCycleDML struct{}

func (*TradingPositionCycleDML) Create(ctx context.Context, db *gorm.DB, model *db_model.TradingPositionCycle) error {
	return db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "cycle_key"}}, DoNothing: true}).Create(model).Error
}

func (*TradingPositionCycleDML) QueryByID(ctx context.Context, db *gorm.DB, id int64) (*db_model.TradingPositionCycle, error) {
	var model db_model.TradingPositionCycle
	err := db.WithContext(ctx).Where("id = ?", id).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &model, err
}

func (*TradingPositionCycleDML) QueryOpenBySymbol(ctx context.Context, db *gorm.DB, accountID, eastmoneySymbol string) (*db_model.TradingPositionCycle, error) {
	var model db_model.TradingPositionCycle
	err := db.WithContext(ctx).Where("account_id = ? AND eastmoney_symbol = ? AND status IN ?", accountID, eastmoneySymbol, []string{"OPEN", "EXIT_PENDING"}).Order("id DESC").First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &model, err
}

func (*TradingPositionCycleDML) ListOpen(ctx context.Context, db *gorm.DB, accountID string) ([]db_model.TradingPositionCycle, error) {
	var models []db_model.TradingPositionCycle
	err := db.WithContext(ctx).Where("account_id = ? AND status IN ?", accountID, []string{"OPEN", "EXIT_PENDING"}).Order("id ASC").Find(&models).Error
	return models, err
}

func (*TradingPositionCycleDML) List(ctx context.Context, db *gorm.DB, accountID, status string, limit int) ([]db_model.TradingPositionCycle, error) {
	var models []db_model.TradingPositionCycle
	tx := db.WithContext(ctx).Order("id DESC")
	if accountID != "" {
		tx = tx.Where("account_id = ?", accountID)
	}
	if status != "" {
		tx = tx.Where("status = ?", status)
	}
	if limit > 0 {
		tx = tx.Limit(limit)
	}
	err := tx.Find(&models).Error
	return models, err
}

func (*TradingPositionCycleDML) Update(ctx context.Context, db *gorm.DB, id int64, values map[string]any) error {
	values["updated_at"] = gorm.Expr("CURRENT_TIMESTAMP(3)")
	return db.WithContext(ctx).Model(&db_model.TradingPositionCycle{}).Where("id = ?", id).Updates(values).Error
}

type TradingSkillDecisionDML struct{}

func (*TradingSkillDecisionDML) CreateBatch(ctx context.Context, db *gorm.DB, models []db_model.TradingSkillDecision) error {
	if len(models) == 0 {
		return nil
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "decision_key"}}, DoNothing: true}).CreateInBatches(models, 100).Error
}

func (*TradingSkillDecisionDML) ListByRunID(ctx context.Context, db *gorm.DB, runID int64) ([]db_model.TradingSkillDecision, error) {
	var models []db_model.TradingSkillDecision
	err := db.WithContext(ctx).Where("trading_agent_run_id = ?", runID).Order("id ASC").Find(&models).Error
	return models, err
}

type TradingBoardCapabilityDML struct{}

func (*TradingBoardCapabilityDML) Upsert(ctx context.Context, db *gorm.DB, model *db_model.TradingBoardCapability) error {
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "capability_key"}},
		DoUpdates: clause.AssignmentColumns([]string{"buy_enabled", "sell_enabled", "minimum_buy_volume", "buy_step", "minimum_sell_volume", "sell_step", "residual_sell_supported", "verification_status", "verified_at", "evidence_json", "trading_rule_version", "config_version", "updated_at"}),
	}).Create(model).Error
}
