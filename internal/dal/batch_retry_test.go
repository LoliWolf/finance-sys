package dal

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"finance-sys/internal/domain/db_model"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// Exercise GORM's real create callbacks with a database-free connection that
// fails the second batch after the first batch has already backfilled IDs.
type batchRetryPool struct {
	statements []string
	commits    int
	rollbacks  int
}

func (p *batchRetryPool) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	p.statements = append(p.statements, query)
	if len(p.statements) == 2 {
		return nil, &mysqldriver.MySQLError{Number: 1213, Message: "injected deadlock"}
	}
	return batchRetryResult{}, nil
}

func (*batchRetryPool) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}

func (*batchRetryPool) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, errors.New("unexpected query")
}

func (*batchRetryPool) QueryRowContext(context.Context, string, ...any) *sql.Row {
	panic("unexpected query row")
}

func (p *batchRetryPool) BeginTx(context.Context, *sql.TxOptions) (gorm.ConnPool, error) {
	return &batchRetryTx{batchRetryPool: p}, nil
}

type batchRetryTx struct {
	*batchRetryPool
}

func (*batchRetryTx) BeginTx(context.Context, *sql.TxOptions) (gorm.ConnPool, error) {
	return nil, gorm.ErrInvalidTransaction
}

func (p *batchRetryTx) Commit() error {
	p.commits++
	return nil
}

func (p *batchRetryTx) Rollback() error {
	p.rollbacks++
	return nil
}

type batchRetryResult struct{}

func (batchRetryResult) LastInsertId() (int64, error) { return 50000, nil }
func (batchRetryResult) RowsAffected() (int64, error) { return 1, nil }

func batchRetryDB(t *testing.T) (*gorm.DB, *batchRetryPool) {
	t.Helper()
	pool := &batchRetryPool{}
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: pool, SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing: true,
		Logger:               logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	return db, pool
}

func requireRetrySafeBatch[T any](t *testing.T, models []T, insert func(context.Context, *gorm.DB, []T) error) {
	t.Helper()
	db, pool := batchRetryDB(t)
	original := append([]T(nil), models...)
	require.ErrorContains(t, insert(context.Background(), db, models), "injected deadlock")
	require.Equal(t, original, models, "rollback must not leave generated IDs in caller models")
	require.Equal(t, 1, pool.rollbacks)
	require.NoError(t, insert(context.Background(), db, models))
	require.Equal(t, original, models, "successful upserts must not backfill guessed IDs either")
	require.Equal(t, 1, pool.commits)
	require.Len(t, pool.statements, 4)
	for _, statement := range pool.statements {
		columns, _, found := strings.Cut(statement, " VALUES ")
		require.True(t, found)
		require.NotContains(t, columns, "`id`", "INSERT must use only the natural key on every attempt")
	}
}

func TestStockDailyQuoteUpsertBatchRetryDoesNotReuseGeneratedIDs(t *testing.T) {
	models := make([]db_model.StockDailyQuote, 301)
	// Caller-supplied stale IDs must also be excluded from SQL.
	models[300].ID = 999
	requireRetrySafeBatch(t, models, StockDailyQuotes.UpsertBatch)
}

func TestMarketDataSyncMissingItemRetryDoesNotReuseGeneratedIDs(t *testing.T) {
	models := make([]db_model.MarketDataSyncMissingItem, 301)
	models[300].ID = 999
	requireRetrySafeBatch(t, models, MarketDataSyncMissingItems.CreateBatch)
}

func TestRecommendationWindowMetricRetryDoesNotReuseGeneratedIDs(t *testing.T) {
	models := make([]db_model.RecommendationEventWindowMetric, 301)
	models[300].ID = 999
	requireRetrySafeBatch(t, models, RecommendationEventWindowMetrics.UpsertBatch)
}

func TestGORMBatchRollbackLeavesIDsThatWouldBeReusedWithoutIsolation(t *testing.T) {
	db, pool := batchRetryDB(t)
	models := make([]db_model.StockDailyQuote, 301)
	unsafeUpsert := func() error {
		return db.Clauses(clause.OnConflict{DoUpdates: clause.Assignments(stockDailyQuoteUpsertAssignments())}).CreateInBatches(models, 300).Error
	}
	require.ErrorContains(t, unsafeUpsert(), "injected deadlock")
	require.NotZero(t, models[0].ID, "GORM mutates the caller even though the transaction rolled back")
	require.NoError(t, unsafeUpsert())
	columns, _, _ := strings.Cut(pool.statements[2], " VALUES ")
	require.Contains(t, columns, "`id`", "the old path retries using stale primary keys")
}

func TestSecurityMastersForQuoteDateUsesListingDatesNotCurrentActiveFlag(t *testing.T) {
	db, _ := batchRetryDB(t)
	db = db.Session(&gorm.Session{DryRun: true})
	var statement string
	var values []any
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:quote-date", func(tx *gorm.DB) {
		statement = tx.Statement.SQL.String()
		values = append([]any(nil), tx.Statement.Vars...)
	}))
	tradeDate := time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)
	_, err := SecurityMasters.QueryForQuoteDate(context.Background(), db, []string{"STOCK", "ETF"}, tradeDate)
	require.NoError(t, err)
	_, where, found := strings.Cut(statement, " WHERE ")
	require.True(t, found)
	require.Contains(t, where, "list_date IS NULL OR list_date <= ?")
	require.Contains(t, where, "delist_date IS NULL OR delist_date >= ?")
	require.Contains(t, where, "asset_type IN")
	require.NotContains(t, where, "is_active")
	require.NotContains(t, where, "list_status")
	require.Equal(t, []any{tradeDate, tradeDate, "STOCK", "ETF"}, values)
}
