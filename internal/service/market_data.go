package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	"finance-sys/internal/marketdata"

	"gorm.io/gorm"
)

const (
	MarketDataSyncTypeStockDaily = "stock_daily"
	MarketDataProviderTushare    = "tushare"
	MarketDataSourceTushare      = "TUSHARE"

	MarketDataSyncStatusQueued        = "QUEUED"
	MarketDataSyncStatusRunning       = "RUNNING"
	MarketDataSyncStatusSucceeded     = "SUCCEEDED"
	MarketDataSyncStatusPartialFailed = "PARTIAL_FAILED"
	MarketDataSyncStatusFailed        = "FAILED"

	MissingReasonNotReturned     = "NOT_RETURNED"
	MissingReasonProviderEmpty   = "PROVIDER_EMPTY"
	MissingReasonProviderError   = "PROVIDER_ERROR"
	MissingReasonUnknownSymbol   = "UNKNOWN_PROVIDER_SYMBOL"
	MissingReasonInvalidIdentity = "INVALID_PROVIDER_IDENTITY"

	defaultStockDailyTaskConcurrency = 10
)

type MarketDataService struct {
	db             *gorm.DB
	runtime        *config.Runtime
	provider       marketdata.StockDailyProvider
	masterProvider marketdata.SecurityMasterProvider
	logger         *slog.Logger
}

type stockDailySyncRequest struct {
	TradeDate time.Time
}

type StockDailySyncRequest struct {
	TradeDate string `json:"trade_date"`
}

type StockDailySyncResponse struct {
	SyncRunID int64  `json:"sync_run_id"`
	SyncType  string `json:"sync_type"`
	TradeDate string `json:"trade_date"`
	Status    string `json:"status"`
	Deduped   bool   `json:"deduped"`
	Message   string `json:"message"`
}

type MarketDataSyncRunQuery struct {
	SyncType  string
	TradeDate *time.Time
	Limit     int
}

type stockDailyProviderRow struct {
	assetType string
	row       marketdata.ProviderRow
}

func NewMarketDataService(db *gorm.DB, runtime *config.Runtime, provider marketdata.StockDailyProvider, logger *slog.Logger) *MarketDataService {
	masterProvider, _ := provider.(marketdata.SecurityMasterProvider)
	return &MarketDataService{db: db, runtime: runtime, provider: provider, masterProvider: masterProvider, logger: logger}
}

func (s *MarketDataService) CreateStockDailySyncRun(ctx context.Context, request StockDailySyncRequest) (*StockDailySyncResponse, error) {
	normalized, err := normalizeStockDailySyncRequestFromAPI(request)
	if err != nil {
		return nil, err
	}
	cfg, err := s.currentMarketDataConfig()
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled || !cfg.StockDaily.Enabled {
		return nil, fmt.Errorf("market_data.stock_daily is disabled")
	}

	existing, err := dal.MarketDataSyncRuns.QueryActiveByTypeAndTradeDate(ctx, s.db, MarketDataSyncTypeStockDaily, normalized.TradeDate)
	if err == nil {
		return stockDailySyncResponse(existing, true), nil
	}
	if err != nil && !errors.Is(err, dal.ErrNotFound) {
		return nil, err
	}

	rawParams, err := json.Marshal(map[string]any{
		"trade_date": normalized.TradeDate.Format(time.DateOnly),
	})
	if err != nil {
		return nil, err
	}
	model := &db_model.MarketDataSyncRun{
		SyncType:          MarketDataSyncTypeStockDaily,
		Provider:          MarketDataProviderTushare,
		TradeDate:         normalized.TradeDate,
		Status:            MarketDataSyncStatusQueued,
		RequestParamsJSON: rawParams,
		QueuedAt:          time.Now().UTC(),
		ConfigVersion:     s.currentConfigVersion(),
	}
	if err := dal.MarketDataSyncRuns.Create(ctx, s.db, model); err != nil {
		return nil, err
	}
	return stockDailySyncResponse(model, false), nil
}

func (s *MarketDataService) ListSyncRuns(ctx context.Context, query MarketDataSyncRunQuery) ([]db_model.MarketDataSyncRun, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	param := dal.QueryParam{
		Orders: []dal.OrderParam{dal.OrderBy("created_at", true), dal.OrderBy("id", true)},
		Limit:  limit,
	}
	if strings.TrimSpace(query.SyncType) != "" {
		param.Where = append(param.Where, dal.Eq("sync_type", strings.TrimSpace(query.SyncType)))
	}
	if query.TradeDate != nil {
		param.Where = append(param.Where, dal.Eq("trade_date", *query.TradeDate))
	}
	return dal.MarketDataSyncRuns.QueryByParam(ctx, s.db, param)
}

func (s *MarketDataService) ClaimAndExecuteNextStockDailyRun(ctx context.Context, workerID string) (bool, error) {
	cfg, err := s.currentMarketDataConfig()
	if err != nil {
		return false, err
	}
	claimTimeout := time.Duration(cfg.AsyncWorker.ClaimTimeoutMS) * time.Millisecond
	run, err := dal.MarketDataSyncRuns.ClaimNextQueued(ctx, s.db, MarketDataSyncTypeStockDaily, workerID, time.Now().UTC(), claimTimeout)
	if errors.Is(err, dal.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := s.ExecuteStockDailyRun(ctx, run.ID); err != nil {
		return true, err
	}
	return true, nil
}

func (s *MarketDataService) ExecuteStockDailyRun(ctx context.Context, syncRunID int64) error {
	run, err := dal.MarketDataSyncRuns.QueryByID(ctx, s.db, syncRunID)
	if err != nil {
		return err
	}
	cfg, err := s.currentMarketDataConfig()
	if err != nil {
		return s.failRun(ctx, syncRunID, "CONFIG_ERROR", err)
	}
	if s.provider == nil {
		return s.failRun(ctx, syncRunID, "PROVIDER_UNAVAILABLE", fmt.Errorf("stock daily provider is not configured"))
	}
	tokens := enabledTushareTokens(cfg.Tushare.Tokens)
	if len(tokens) == 0 {
		return s.failRun(ctx, syncRunID, "NO_TOKEN", fmt.Errorf("no enabled market_data.tushare token"))
	}

	securities, err := dal.SecurityMasters.QueryForQuoteDate(ctx, s.db, marketDataSecurityAssetTypes(cfg.StockDaily.SyncAssetTypes), run.TradeDate)
	if err != nil {
		return s.failRun(ctx, syncRunID, "SECURITY_QUERY_FAILED", err)
	}

	type fetchPlan struct {
		assetType string
		fetch     func(context.Context, string, time.Time, []string) ([]marketdata.ProviderRow, error)
	}
	plans := []fetchPlan{
		{assetType: "A_SHARE", fetch: s.provider.FetchStockDaily},
		{assetType: "ETF", fetch: s.provider.FetchETFDaily},
		{assetType: "SECTOR", fetch: s.provider.FetchSectorDaily},
	}

	var quotes []db_model.StockDailyQuote
	var missing []db_model.MarketDataSyncMissingItem
	var providerRows []stockDailyProviderRow
	tokenAlias := ""
	providerErrors := 0

	for _, plan := range plans {
		if !containsString(marketDataSecurityAssetTypes(cfg.StockDaily.SyncAssetTypes), plan.assetType) {
			continue
		}
		fields := cfg.StockDaily.Fields
		if plan.assetType == "SECTOR" {
			fields = cfg.StockDaily.SectorFields
		}
		rows, alias, fetchErr := fetchRowsWithTokens(ctx, tokens, run.TradeDate, fields, plan.fetch)
		if alias != "" {
			tokenAlias = alias
		}
		if fetchErr != nil {
			providerErrors++
			missing = append(missing, missingItemsForAssetType(syncRunID, securities, run.TradeDate, plan.assetType, MissingReasonProviderError, fetchErr.Error())...)
			continue
		}
		if len(rows) == 0 {
			missing = append(missing, missingItemsForAssetType(syncRunID, securities, run.TradeDate, plan.assetType, MissingReasonProviderEmpty, "provider returned empty result")...)
			continue
		}
		for _, row := range rows {
			providerRows = append(providerRows, stockDailyProviderRow{assetType: plan.assetType, row: row})
		}
	}

	rowQuotes, associatedMissing, rowProviderErrors, err := associateStockDailyProviderRows(
		ctx,
		syncRunID,
		securities,
		run.TradeDate,
		providerRows,
		missing,
		s.currentConfigVersion(),
		defaultStockDailyTaskConcurrency,
	)
	if err != nil {
		return s.failRun(ctx, syncRunID, "PROCESS_ROWS_FAILED", err)
	}
	quotes = rowQuotes
	missing = associatedMissing
	providerErrors += rowProviderErrors

	if err := retryRetryableDBError(ctx, 20, s.logger, func() error {
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := dal.StockDailyQuotes.UpsertBatch(ctx, tx, quotes); err != nil {
				return err
			}
			if err := dal.MarketDataSyncMissingItems.DeleteByRunID(ctx, tx, syncRunID); err != nil {
				return err
			}
			if err := dal.MarketDataSyncMissingItems.CreateBatch(ctx, tx, missing); err != nil {
				return err
			}
			status := MarketDataSyncStatusSucceeded
			failedCount := providerErrors
			if len(quotes) == 0 && (len(missing) > 0 || providerErrors > 0) {
				status = MarketDataSyncStatusFailed
			} else if len(missing) > 0 || providerErrors > 0 {
				status = MarketDataSyncStatusPartialFailed
			}
			now := time.Now().UTC()
			return dal.MarketDataSyncRuns.UpdateProgressByID(ctx, tx, syncRunID, map[string]any{
				"status":         status,
				"expected_count": len(securities),
				"fetched_count":  len(quotes) + len(missing),
				"matched_count":  len(quotes),
				"upserted_count": len(quotes),
				"missing_count":  len(missing),
				"failed_count":   failedCount,
				"token_alias":    tokenAlias,
				"finished_at":    now,
				"error_code":     "",
				"error_message":  "",
			})
		})
	}); err != nil {
		return s.failRun(ctx, syncRunID, "PERSIST_FAILED", err)
	}
	return nil
}

func (s *MarketDataService) failRun(ctx context.Context, syncRunID int64, code string, err error) error {
	now := time.Now().UTC()
	_ = dal.MarketDataSyncRuns.UpdateProgressByID(ctx, s.db, syncRunID, map[string]any{
		"status":        MarketDataSyncStatusFailed,
		"error_code":    code,
		"error_message": err.Error(),
		"finished_at":   now,
	})
	return err
}

func (s *MarketDataService) currentMarketDataConfig() (config.MarketDataConfig, error) {
	cfg := s.runtime.Config()
	if cfg == nil {
		return config.MarketDataConfig{}, fmt.Errorf("config runtime unavailable")
	}
	return cfg.MarketData, nil
}

func (s *MarketDataService) currentConfigVersion() int64 {
	cfg := s.runtime.Config()
	if cfg == nil {
		return 0
	}
	return cfg.Meta.ConfigVersion
}

func normalizeStockDailySyncRequest(request stockDailySyncRequest) (stockDailySyncRequest, error) {
	if request.TradeDate.IsZero() {
		return request, fmt.Errorf("trade_date is required")
	}
	return stockDailySyncRequest{TradeDate: dateOnlyUTC(request.TradeDate)}, nil
}

func marketDataSecurityAssetTypes(values []string) []string {
	if len(values) == 0 {
		values = []string{"STOCK", "ETF", "SECTOR"}
	}
	seen := map[string]struct{}{}
	var result []string
	for _, value := range values {
		switch strings.ToUpper(strings.TrimSpace(value)) {
		case "STOCK", "A_SHARE":
			if _, ok := seen["STOCK"]; !ok {
				result = append(result, "STOCK")
				seen["STOCK"] = struct{}{}
			}
			if _, ok := seen["A_SHARE"]; !ok {
				result = append(result, "A_SHARE")
				seen["A_SHARE"] = struct{}{}
			}
		case "ETF":
			if _, ok := seen["ETF"]; !ok {
				result = append(result, "ETF")
				seen["ETF"] = struct{}{}
			}
		case "SECTOR":
			if _, ok := seen["SECTOR"]; !ok {
				result = append(result, "SECTOR")
				seen["SECTOR"] = struct{}{}
			}
		}
	}
	return result
}

func stockDailyQuoteFromProviderRow(security db_model.SecurityMaster, tradeDate time.Time, row marketdata.ProviderRow, configVersion int64) (*db_model.StockDailyQuote, error) {
	if err := marketdata.ValidateDailyIdentity(security.TSCode, tradeDate, row.Values); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(row.Values)
	if err != nil {
		return nil, err
	}
	return &db_model.StockDailyQuote{
		SecurityMasterID: security.ID,
		TSCode:           security.TSCode,
		Symbol:           security.Symbol,
		SecurityName:     security.Name,
		Exchange:         security.Exchange,
		Market:           security.Market,
		AssetType:        security.AssetType,
		Industry:         security.Industry,
		SectorType:       security.SectorType,
		ListStatus:       security.ListStatus,
		TradeDate:        dateOnlyUTC(tradeDate),
		OpenPrice:        numberValue(row.Values["open"]),
		HighPrice:        numberValue(row.Values["high"]),
		LowPrice:         numberValue(row.Values["low"]),
		ClosePrice:       numberValue(row.Values["close"]),
		PreClosePrice:    numberValue(row.Values["pre_close"]),
		ChangeAmount:     numberValue(row.Values["change"]),
		PctChg:           firstNumberValue(row.Values["pct_chg"], row.Values["pct_change"]),
		Volume:           numberValue(row.Values["vol"]),
		Amount:           numberValue(row.Values["amount"]),
		Source:           MarketDataSourceTushare,
		TushareContent:   raw,
		ConfigVersion:    configVersion,
	}, nil
}

func associateStockDailyProviderRows(
	ctx context.Context,
	syncRunID int64,
	securities []db_model.SecurityMaster,
	tradeDate time.Time,
	rows []stockDailyProviderRow,
	initialMissing []db_model.MarketDataSyncMissingItem,
	configVersion int64,
	concurrency int,
) ([]db_model.StockDailyQuote, []db_model.MarketDataSyncMissingItem, int, error) {
	securityByTSCode := make(map[string]db_model.SecurityMaster, len(securities))
	for _, security := range securities {
		securityByTSCode[security.TSCode] = security
	}

	returned := map[string]struct{}{}
	missing := append([]db_model.MarketDataSyncMissingItem(nil), initialMissing...)
	var quotes []db_model.StockDailyQuote
	providerErrors := 0

	var resultMu sync.Mutex
	if len(rows) > 0 {
		if err := runConcurrent(ctx, rows, concurrency, func(item stockDailyProviderRow) error {
			tsCode := stringValue(item.row.Values["ts_code"])
			security, ok := securityByTSCode[tsCode]
			if !ok || !marketDataAssetTypeMatches(security.AssetType, item.assetType) {
				resultMu.Lock()
				providerErrors++
				missing = append(missing, unknownProviderMissingItem(syncRunID, tsCode, item.assetType, tradeDate))
				resultMu.Unlock()
				return nil
			}
			quote, err := stockDailyQuoteFromProviderRow(security, tradeDate, item.row, configVersion)
			resultMu.Lock()
			defer resultMu.Unlock()
			if err != nil {
				providerErrors++
				reason := MissingReasonProviderError
				if errors.Is(err, marketdata.ErrInvalidDailyIdentity) {
					reason = MissingReasonInvalidIdentity
				}
				missing = append(missing, missingItem(syncRunID, security, tradeDate, reason, err.Error()))
				return nil
			}
			quotes = append(quotes, *quote)
			returned[security.TSCode] = struct{}{}
			return nil
		}); err != nil {
			return nil, nil, 0, err
		}
	}

	for _, security := range securities {
		if _, ok := returned[security.TSCode]; ok {
			continue
		}
		if hasMissing(missing, security.TSCode) {
			continue
		}
		missing = append(missing, missingItem(syncRunID, security, tradeDate, MissingReasonNotReturned, "provider result did not include local security"))
	}
	// Concurrent row conversion must not randomize the database lock order.
	sort.Slice(quotes, func(i, j int) bool { return quotes[i].TSCode < quotes[j].TSCode })
	return quotes, missing, providerErrors, ctx.Err()
}

func normalizeStockDailySyncRequestFromAPI(request StockDailySyncRequest) (stockDailySyncRequest, error) {
	raw := strings.TrimSpace(request.TradeDate)
	if raw == "" {
		return stockDailySyncRequest{}, fmt.Errorf("trade_date is required")
	}
	tradeDate, err := time.Parse(time.DateOnly, raw)
	if err != nil {
		return stockDailySyncRequest{}, fmt.Errorf("invalid trade_date %q, expected YYYY-MM-DD", raw)
	}
	return normalizeStockDailySyncRequest(stockDailySyncRequest{TradeDate: tradeDate})
}

func stockDailySyncResponse(model *db_model.MarketDataSyncRun, deduped bool) *StockDailySyncResponse {
	message := "stock daily sync task queued"
	if deduped {
		message = "stock daily sync task already queued or running"
	}
	return &StockDailySyncResponse{
		SyncRunID: model.ID,
		SyncType:  model.SyncType,
		TradeDate: dateOnlyUTC(model.TradeDate).Format(time.DateOnly),
		Status:    model.Status,
		Deduped:   deduped,
		Message:   message,
	}
}

func fetchRowsWithTokens(ctx context.Context, tokens []config.TushareTokenConfig, tradeDate time.Time, fields []string, fetch func(context.Context, string, time.Time, []string) ([]marketdata.ProviderRow, error)) ([]marketdata.ProviderRow, string, error) {
	var lastErr error
	for _, token := range tokens {
		rows, err := fetch(ctx, strings.TrimSpace(token.Token), tradeDate, fields)
		if err == nil {
			return rows, strings.TrimSpace(token.Alias), nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no enabled token")
	}
	return nil, "", lastErr
}

func marketDataAssetTypeMatches(securityAssetType string, providerAssetType string) bool {
	securityAssetType = strings.ToUpper(strings.TrimSpace(securityAssetType))
	providerAssetType = strings.ToUpper(strings.TrimSpace(providerAssetType))
	if securityAssetType == providerAssetType {
		return true
	}
	return isMarketDataStockAssetType(securityAssetType) && isMarketDataStockAssetType(providerAssetType)
}

func isMarketDataStockAssetType(value string) bool {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "STOCK", "A_SHARE", "ASHARE":
		return true
	default:
		return false
	}
}

func runConcurrent[T any](ctx context.Context, items []T, concurrency int, handle func(T) error) error {
	if len(items) == 0 {
		return ctx.Err()
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > len(items) {
		concurrency = len(items)
	}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error

	for _, item := range items {
		select {
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(item T) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := handle(item); err != nil {
				once.Do(func() { firstErr = err })
			}
		}(item)
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

func retryRetryableDBError(ctx context.Context, maxAttempts int, logger *slog.Logger, op func() error) error {
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := op(); err != nil {
			lastErr = err
			if !isRetryableDBTransactionError(err) || attempt == maxAttempts {
				return err
			}
			if logger != nil {
				logger.WarnContext(ctx, "retrying after transient db error", "attempt", attempt, "max_attempts", maxAttempts, "error", err)
			}
			select {
			case <-time.After(time.Duration(attempt*attempt) * 50 * time.Millisecond):
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}
		return nil
	}
	return lastErr
}

func enabledTushareTokens(tokens []config.TushareTokenConfig) []config.TushareTokenConfig {
	result := make([]config.TushareTokenConfig, 0, len(tokens))
	seenAliases := map[string]int{}
	for _, token := range tokens {
		if token.Enabled && strings.TrimSpace(token.Token) != "" {
			alias := strings.TrimSpace(token.Alias)
			if alias == "" {
				alias = fmt.Sprintf("token_%d", len(result)+1)
			}
			seenAliases[alias]++
			if seenAliases[alias] > 1 {
				alias = fmt.Sprintf("%s#%d", alias, seenAliases[alias])
			}
			token.Alias = alias
			result = append(result, token)
		}
	}
	return result
}

func missingItemsForAssetType(syncRunID int64, securities []db_model.SecurityMaster, tradeDate time.Time, assetType string, reason string, message string) []db_model.MarketDataSyncMissingItem {
	items := make([]db_model.MarketDataSyncMissingItem, 0)
	for _, security := range securities {
		if marketDataAssetTypeMatches(security.AssetType, assetType) {
			items = append(items, missingItem(syncRunID, security, tradeDate, reason, message))
		}
	}
	return items
}

func missingItem(syncRunID int64, security db_model.SecurityMaster, tradeDate time.Time, reason string, message string) db_model.MarketDataSyncMissingItem {
	securityMasterID := security.ID
	return db_model.MarketDataSyncMissingItem{
		SyncRunID:        syncRunID,
		SecurityMasterID: &securityMasterID,
		TSCode:           security.TSCode,
		Symbol:           security.Symbol,
		SecurityName:     security.Name,
		TradeDate:        dateOnlyUTC(tradeDate),
		Reason:           reason,
		Message:          message,
	}
}

func unknownProviderMissingItem(syncRunID int64, tsCode string, assetType string, tradeDate time.Time) db_model.MarketDataSyncMissingItem {
	tsCode = strings.ToUpper(strings.TrimSpace(tsCode))
	symbol := tsCode
	if dot := strings.Index(symbol, "."); dot > 0 {
		symbol = symbol[:dot]
	}
	return db_model.MarketDataSyncMissingItem{
		SyncRunID:    syncRunID,
		TSCode:       tsCode,
		Symbol:       symbol,
		TradeDate:    dateOnlyUTC(tradeDate),
		Reason:       MissingReasonUnknownSymbol,
		Message:      fmt.Sprintf("provider returned %s row not present in security_master for the requested trade date", assetType),
		SecurityName: "",
	}
}

func hasMissing(items []db_model.MarketDataSyncMissingItem, tsCode string) bool {
	for _, item := range items {
		if item.TSCode == tsCode {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func dateOnlyUTC(value time.Time) time.Time {
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func numberValue(value any) float64 {
	switch typed := value.(type) {
	case nil:
		return 0
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return 0
		}
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case int32:
		return float64(typed)
	case json.Number:
		parsed, _ := typed.Float64()
		return parsed
	case string:
		parsed, _ := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed
	default:
		return 0
	}
}

func firstNumberValue(values ...any) float64 {
	for _, value := range values {
		if value == nil {
			continue
		}
		text := stringValue(value)
		if text == "" || text == "<nil>" {
			continue
		}
		return numberValue(value)
	}
	return 0
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}
