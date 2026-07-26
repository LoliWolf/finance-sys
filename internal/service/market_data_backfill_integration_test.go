package service_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"finance-sys/internal/bootstrap"
	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/service"

	"github.com/stretchr/testify/require"
)

const (
	runRealStockDailyBackfillEnv = "FINANCE_SYS_RUN_REAL_STOCK_DAILY_BACKFILL"
	backfillDateConcurrencyEnv   = "FINANCE_SYS_STOCK_DAILY_BACKFILL_CONCURRENCY"
	backfillStartDateEnv         = "FINANCE_SYS_STOCK_DAILY_BACKFILL_START_DATE"
	backfillEndDateEnv           = "FINANCE_SYS_STOCK_DAILY_BACKFILL_END_DATE"
	backfillDateConcurrency      = 10
)

func TestBackfillDateWorkerCountCanBeOverriddenByEnv(t *testing.T) {
	t.Setenv(backfillDateConcurrencyEnv, "1")
	require.Equal(t, 1, backfillDateWorkerCount(179))
}

func TestBackfillDateWorkerCountDefaultsToTen(t *testing.T) {
	require.Equal(t, backfillDateConcurrency, backfillDateWorkerCount(179))
}

func TestRealBackfillStockDailyFrom20260101ToToday(t *testing.T) {
	if os.Getenv(runRealStockDailyBackfillEnv) != "1" {
		t.Skipf("set %s=1 to run real DB/Tushare stock daily backfill", runRealStockDailyBackfillEnv)
	}
	if testing.Short() {
		t.Skip("real DB/Tushare stock daily backfill is disabled in short mode")
	}

	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	start, end, err := backfillDateRange(location, time.Now())
	require.NoError(t, err)
	dates := calendarDatesInclusive(start, end)
	require.NotEmpty(t, dates)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Hour)
	defer cancel()

	app, err := bootstrap.Build(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, app.Close())
	})
	require.NoError(t, forceEnableStockDailyBackfill(app))

	securities, err := dal.SecurityMasters.QueryActiveByAssetTypes(ctx, app.DB, []string{"STOCK", "A_SHARE", "ETF"})
	require.NoError(t, err)
	require.NotEmpty(t, securities, "expected DB to contain active A-share or ETF securities")

	var maxRunIDBefore int64
	require.NoError(t, app.DB.WithContext(ctx).Raw("SELECT COALESCE(MAX(id), 0) FROM market_data_sync_runs").Scan(&maxRunIDBefore).Error)

	workers := backfillDateWorkerCount(len(dates))
	t.Logf("backfill stock daily quotes from %s to %s, dates=%d, securities=%d, date_concurrency=%d",
		start.Format(time.DateOnly), end.Format(time.DateOnly), len(dates), len(securities), workers)

	type result struct {
		tradeDate string
		syncRunID int64
		err       error
	}

	jobs := make(chan time.Time)
	results := make(chan result, len(dates))
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for tradeDate := range jobs {
				tradeDateText := tradeDate.Format(time.DateOnly)
				response, err := app.MarketDataService.CreateStockDailySyncRun(ctx, service.StockDailySyncRequest{
					TradeDate: tradeDateText,
				})
				if err == nil {
					err = app.MarketDataService.ExecuteStockDailyRun(ctx, response.SyncRunID)
				}
				if err != nil {
					results <- result{tradeDate: tradeDateText, err: err}
					continue
				}
				results <- result{tradeDate: tradeDateText, syncRunID: response.SyncRunID}
			}
		}()
	}

feedDates:
	for _, tradeDate := range dates {
		select {
		case <-ctx.Done():
			break feedDates
		case jobs <- tradeDate:
		}
	}
	close(jobs)
	wg.Wait()
	close(results)

	var failures []string
	completed := 0
	for item := range results {
		if item.err != nil {
			failures = append(failures, fmt.Sprintf("%s run=%d err=%v", item.tradeDate, item.syncRunID, item.err))
			continue
		}
		completed++
	}
	require.NoError(t, ctx.Err())
	if len(failures) > 0 {
		limit := len(failures)
		if limit > 20 {
			limit = 20
		}
		t.Fatalf("stock daily backfill failed for %d dates, completed=%d, first failures:\n%s", len(failures), completed, strings.Join(failures[:limit], "\n"))
	}
	t.Logf("stock daily backfill completed, dates=%d", completed)
	logCreatedRunStatusCounts(t, ctx, app, maxRunIDBefore)
	requireNoDuplicateStockDailyQuotes(t, ctx, app)
}

func TestBackfillDateRangeCanBeOverriddenByEnv(t *testing.T) {
	t.Setenv(backfillStartDateEnv, "2026-06-26")
	t.Setenv(backfillEndDateEnv, "2026-07-26")
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	start, end, err := backfillDateRange(location, time.Date(2026, 8, 1, 0, 0, 0, 0, location))
	require.NoError(t, err)
	require.Equal(t, "2026-06-26", start.Format(time.DateOnly))
	require.Equal(t, "2026-07-26", end.Format(time.DateOnly))
}

func backfillDateRange(location *time.Location, now time.Time) (time.Time, time.Time, error) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, location)
	end := dateOnlyInLocation(now, location)
	var err error
	if value := strings.TrimSpace(os.Getenv(backfillStartDateEnv)); value != "" {
		start, err = time.ParseInLocation(time.DateOnly, value, location)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("parse %s: %w", backfillStartDateEnv, err)
		}
	}
	if value := strings.TrimSpace(os.Getenv(backfillEndDateEnv)); value != "" {
		end, err = time.ParseInLocation(time.DateOnly, value, location)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("parse %s: %w", backfillEndDateEnv, err)
		}
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("backfill end date %s is before start date %s", end.Format(time.DateOnly), start.Format(time.DateOnly))
	}
	return start, end, nil
}

func calendarDatesInclusive(start time.Time, end time.Time) []time.Time {
	start = dateOnlyInLocation(start, start.Location())
	end = dateOnlyInLocation(end, start.Location())
	var dates []time.Time
	for current := start; !current.After(end); current = current.AddDate(0, 0, 1) {
		dates = append(dates, current)
	}
	return dates
}

func dateOnlyInLocation(value time.Time, location *time.Location) time.Time {
	year, month, day := value.In(location).Date()
	return time.Date(year, month, day, 0, 0, 0, 0, location)
}

func backfillDateWorkerCount(dateCount int) int {
	workers := backfillDateConcurrency
	if raw := strings.TrimSpace(os.Getenv(backfillDateConcurrencyEnv)); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err == nil && parsed > 0 {
			workers = parsed
		}
	}
	if dateCount <= 0 {
		return 0
	}
	if workers > dateCount {
		return dateCount
	}
	return workers
}

func logCreatedRunStatusCounts(t *testing.T, ctx context.Context, app *bootstrap.App, minRunID int64) {
	t.Helper()
	var rows []struct {
		Status string
		Count  int64
	}
	err := app.DB.WithContext(ctx).
		Raw("SELECT status, COUNT(*) AS count FROM market_data_sync_runs WHERE id > ? AND sync_type = ? GROUP BY status ORDER BY status", minRunID, service.MarketDataSyncTypeStockDaily).
		Scan(&rows).Error
	require.NoError(t, err)
	t.Logf("created stock daily run status counts: %+v", rows)
}

func requireNoDuplicateStockDailyQuotes(t *testing.T, ctx context.Context, app *bootstrap.App) {
	t.Helper()
	var duplicateGroups int64
	err := app.DB.WithContext(ctx).Raw(`
SELECT COUNT(*)
FROM (
  SELECT ts_code, trade_date, source
  FROM stock_daily_quotes
  GROUP BY ts_code, trade_date, source
  HAVING COUNT(*) > 1
) duplicated_quotes`).Scan(&duplicateGroups).Error
	require.NoError(t, err)
	require.Zero(t, duplicateGroups, "stock_daily_quotes must be unique by ts_code + trade_date + source")
}

func forceEnableStockDailyBackfill(app *bootstrap.App) error {
	current := app.Runtime.Current()
	if current == nil || current.Config == nil {
		return fmt.Errorf("runtime config is unavailable")
	}
	raw, err := json.Marshal(current.Config)
	if err != nil {
		return err
	}
	var cfg config.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return err
	}

	cfg.MarketData.Enabled = true
	cfg.MarketData.Provider = service.MarketDataProviderTushare
	cfg.MarketData.Tushare.Enabled = true
	if cfg.MarketData.Tushare.TimeoutMS <= 0 {
		cfg.MarketData.Tushare.TimeoutMS = 10000
	}
	for i := range cfg.MarketData.Tushare.Tokens {
		if strings.TrimSpace(cfg.MarketData.Tushare.Tokens[i].Token) != "" {
			cfg.MarketData.Tushare.Tokens[i].Enabled = true
		}
		if cfg.MarketData.Tushare.Tokens[i].Weight <= 0 {
			cfg.MarketData.Tushare.Tokens[i].Weight = 1
		}
	}
	cfg.MarketData.StockDaily.Enabled = true
	if len(cfg.MarketData.StockDaily.SyncAssetTypes) == 0 {
		cfg.MarketData.StockDaily.SyncAssetTypes = []string{"STOCK", "ETF"}
	}
	if len(cfg.MarketData.StockDaily.Fields) == 0 {
		cfg.MarketData.StockDaily.Fields = []string{"ts_code", "trade_date", "open", "high", "low", "close", "pre_close", "change", "pct_chg", "vol", "amount"}
	}
	snapshot, err := config.NewSnapshot(&cfg, nil, current.Source+"_real_backfill_override", time.Now())
	if err != nil {
		return err
	}
	app.Runtime.Update(snapshot)
	return nil
}
