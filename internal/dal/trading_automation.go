package dal

import (
	"context"
	"errors"
	"time"

	"finance-sys/internal/domain/db_model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// All locks are held by a short service-owned transaction. One simulation
// account is supported; the durable control row serializes ledger mutations.
func (*TradingRuntimeControlDML) Lock(ctx context.Context, tx *gorm.DB) error {
	var control db_model.TradingRuntimeControl
	return tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("control_key = ?", "KILL_SWITCH").First(&control).Error
}

func (*TradingOrderDML) QueryByID(ctx context.Context, db *gorm.DB, id int64) (*db_model.TradingOrder, error) {
	var v db_model.TradingOrder
	err := db.WithContext(ctx).Where("id = ?", id).First(&v).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &v, err
}

func (*TradingOrderDML) ByIntent(ctx context.Context, db *gorm.DB, id int64) (*db_model.TradingOrder, error) {
	var v db_model.TradingOrder
	err := db.WithContext(ctx).Where("trading_intent_id = ?", id).First(&v).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &v, err
}

func (*TradingOrderDML) ByCycle(ctx context.Context, db *gorm.DB, cycleID int64) ([]db_model.TradingOrder, error) {
	var v []db_model.TradingOrder
	err := db.WithContext(ctx).Model(&db_model.TradingOrder{}).Select("trading_orders.*").Joins("JOIN trading_intents i ON i.id=trading_orders.trading_intent_id").Where("i.position_cycle_id = ?", cycleID).Order("trading_orders.id ASC").Find(&v).Error
	return v, err
}

func (*TradingOrderDML) ByAccount(ctx context.Context, db *gorm.DB, account string) ([]db_model.TradingOrder, error) {
	var v []db_model.TradingOrder
	err := db.WithContext(ctx).Where("account_id = ?", account).Order("id ASC").Find(&v).Error
	return v, err
}

func (*TradingFillDML) ByOrder(ctx context.Context, db *gorm.DB, id int64) ([]db_model.TradingFill, error) {
	var v []db_model.TradingFill
	err := db.WithContext(ctx).Where("trading_order_id = ?", id).Order("id ASC").Find(&v).Error
	return v, err
}

func (*TradingPositionCycleDML) ByKey(ctx context.Context, db *gorm.DB, key string) (*db_model.TradingPositionCycle, error) {
	var v db_model.TradingPositionCycle
	err := db.WithContext(ctx).Where("cycle_key = ?", key).First(&v).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &v, err
}

func (*TradingIntentDML) HasActiveExit(ctx context.Context, db *gorm.DB, cycleID int64) (bool, error) {
	var n int64
	err := db.WithContext(ctx).Model(&db_model.TradingIntent{}).Where("position_cycle_id=? AND trade_action='SELL'", cycleID).
		Where("status IN ?", []string{"READY_FOR_EXECUTION", "ORDER_CREATED"}).
		Where("execution_status NOT IN ?", []string{"SHADOW_READY", "FAILED", "EXPIRED"}).
		Where("NOT EXISTS (SELECT 1 FROM trading_orders o WHERE o.trading_intent_id=trading_intents.id AND o.status IN ('FILLED','CANCELED','REJECTED','DRY_RUN'))").Count(&n).Error
	return n > 0, err
}

func (*TradingIntentDML) FinishClaim(ctx context.Context, db *gorm.DB, id int64, token string, now time.Time, values map[string]any) (bool, error) {
	values["updated_at"] = now
	r := db.WithContext(ctx).Model(&db_model.TradingIntent{}).Where("id=? AND execution_claim_token=? AND execution_claim_deadline>=?", id, token, now).Updates(values)
	return r.RowsAffected == 1, r.Error
}

func (*TradingIntentDML) Expire(ctx context.Context, db *gorm.DB, now time.Time) error {
	return db.WithContext(ctx).Model(&db_model.TradingIntent{}).Where("status='READY_FOR_EXECUTION' AND valid_until<=?", now).
		Where("execution_status IN ?", []string{"READY", "RETRY", "SHADOW_READY", "CLAIMED"}).
		Where("execution_claim_deadline IS NULL OR execution_claim_deadline<=?", now).
		Updates(map[string]any{"status": "EXPIRED", "execution_status": "EXPIRED", "updated_at": now}).Error
}

func (*TradingDailySessionDML) ByDate(ctx context.Context, db *gorm.DB, account string, date time.Time) (*db_model.TradingDailySession, error) {
	var v db_model.TradingDailySession
	err := db.WithContext(ctx).Where("account_id=? AND trade_date>=? AND trade_date<?", account, date, date.AddDate(0, 0, 1)).First(&v).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &v, err
}

func (*TradingDailySessionDML) Update(ctx context.Context, db *gorm.DB, id int64, values map[string]any) error {
	return db.WithContext(ctx).Model(&db_model.TradingDailySession{}).Where("id=?", id).Updates(values).Error
}

func (*TradingAccountSnapshotDML) Earliest(ctx context.Context, db *gorm.DB, account string) (*db_model.TradingAccountSnapshot, error) {
	var v db_model.TradingAccountSnapshot
	err := db.WithContext(ctx).Where("account_id=?", account).Order("snapshot_at ASC,id ASC").First(&v).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &v, err
}

func (*TradingAgentRunDML) Claim(ctx context.Context, tx *gorm.DB, worker, token string, now, deadline time.Time) (*db_model.TradingAgentRun, error) {
	var v db_model.TradingAgentRun
	err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("status='QUEUED' OR (status='RUNNING' AND claim_deadline IS NOT NULL AND claim_deadline<?)", now).Order("id ASC").First(&v).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	err = tx.WithContext(ctx).Model(&v).Updates(map[string]any{"status": "RUNNING", "worker_id": worker, "claim_token": token, "claimed_at": now, "claim_deadline": deadline, "started_at": now}).Error
	v.ClaimToken = token
	v.ClaimDeadline = &deadline
	return &v, err
}

func (*TradingAgentRunDML) FinishClaim(ctx context.Context, db *gorm.DB, id int64, token string, now time.Time, values map[string]any) (bool, error) {
	values["updated_at"] = now
	r := db.WithContext(ctx).Model(&db_model.TradingAgentRun{}).Where("id=? AND status='RUNNING' AND claim_token=? AND claim_deadline>=?", id, token, now).Updates(values)
	return r.RowsAffected == 1, r.Error
}

func (*TradingReconciliationRunDML) ByID(ctx context.Context, db *gorm.DB, id int64) (*db_model.TradingReconciliationRun, error) {
	var v db_model.TradingReconciliationRun
	err := db.WithContext(ctx).Where("id=?", id).First(&v).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &v, err
}

func (*TradingReconciliationRunDML) ByKey(ctx context.Context, db *gorm.DB, key string) (*db_model.TradingReconciliationRun, error) {
	var v db_model.TradingReconciliationRun
	err := db.WithContext(ctx).Where("run_key=?", key).First(&v).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &v, err
}

func (*TradingReconciliationRunDML) Claim(ctx context.Context, tx *gorm.DB, worker, token string, now, deadline time.Time) (*db_model.TradingReconciliationRun, error) {
	var n int64
	if err := tx.WithContext(ctx).Model(&db_model.TradingReconciliationRun{}).Where("status='RUNNING' AND claim_deadline>=?", now).Count(&n).Error; err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, ErrNotFound
	}
	var v db_model.TradingReconciliationRun
	err := tx.WithContext(ctx).Where("status='QUEUED' OR (status='RUNNING' AND claim_deadline IS NOT NULL AND claim_deadline<?)", now).Order("id ASC").First(&v).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	err = tx.WithContext(ctx).Model(&v).Updates(map[string]any{"status": "RUNNING", "worker_id": worker, "claim_token": token, "claim_deadline": deadline}).Error
	v.ClaimToken = token
	v.ClaimDeadline = &deadline
	v.Status = "RUNNING"
	return &v, err
}

func (*TradingReconciliationRunDML) FinishClaim(ctx context.Context, db *gorm.DB, id int64, token string, now time.Time, values map[string]any) (bool, error) {
	return TradingReconciliationRuns.UpdateClaim(ctx, db, id, token, now, values)
}

// UpdateClaim also checkpoints RUNNING attempts without releasing the execution barrier.
func (*TradingReconciliationRunDML) UpdateClaim(ctx context.Context, db *gorm.DB, id int64, token string, now time.Time, values map[string]any) (bool, error) {
	values["updated_at"] = now
	r := db.WithContext(ctx).Model(&db_model.TradingReconciliationRun{}).Where("id=? AND status='RUNNING' AND claim_token=? AND claim_deadline>=?", id, token, now).Updates(values)
	return r.RowsAffected == 1, r.Error
}

func (*TradingReconciliationRunDML) HasRunning(ctx context.Context, db *gorm.DB, account string) (bool, error) {
	var n int64
	// An expired lease remains a barrier until a recovery worker finishes it.
	err := db.WithContext(ctx).Model(&db_model.TradingReconciliationRun{}).Where("account_id=? AND status='RUNNING'", account).Count(&n).Error
	return n > 0, err
}

func (*TradingBoardCapabilityDML) ByBoard(ctx context.Context, db *gorm.DB, account, board string) (*db_model.TradingBoardCapability, error) {
	var v db_model.TradingBoardCapability
	err := db.WithContext(ctx).Where("account_id=? AND board_type=? AND environment='SIMULATION'", account, board).First(&v).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &v, err
}
