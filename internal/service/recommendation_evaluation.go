package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	"finance-sys/internal/evaluation"

	"gorm.io/gorm"
)

const (
	RecommendationEvaluationRunTypeManual    = "MANUAL"
	RecommendationEvaluationRunTypeScheduled = "SCHEDULED"

	RecommendationEvaluationRunStatusQueued        = "QUEUED"
	RecommendationEvaluationRunStatusRunning       = "RUNNING"
	RecommendationEvaluationRunStatusSucceeded     = "SUCCEEDED"
	RecommendationEvaluationRunStatusPartialFailed = "PARTIAL_FAILED"
	RecommendationEvaluationRunStatusFailed        = "FAILED"
)

type RecommendationEvaluationService struct {
	db      *gorm.DB
	runtime *config.Runtime
	logger  *slog.Logger
}

type RecommendationEvaluationRequest struct {
	DateFrom     string   `json:"date_from"`
	DateTo       string   `json:"date_to"`
	BloggerIDs   []int64  `json:"blogger_ids"`
	Symbols      []string `json:"symbols"`
	EventIDs     []int64  `json:"event_ids"`
	Windows      []int    `json:"windows"`
	ForceRebuild bool     `json:"force_rebuild"`
	OnlyActive   *bool    `json:"only_active,omitempty"`
}

type RecommendationEvaluationRunParams struct {
	DateFrom     string   `json:"date_from,omitempty"`
	DateTo       string   `json:"date_to,omitempty"`
	BloggerIDs   []int64  `json:"blogger_ids,omitempty"`
	Symbols      []string `json:"symbols,omitempty"`
	EventIDs     []int64  `json:"event_ids,omitempty"`
	Windows      []int    `json:"windows"`
	ForceRebuild bool     `json:"force_rebuild"`
	OnlyActive   bool     `json:"only_active"`
}

type RecommendationEvaluationRunResponse struct {
	RunID   int64  `json:"run_id"`
	Status  string `json:"status"`
	RunType string `json:"run_type"`
	Message string `json:"message"`
}

type RecommendationEvaluationRunQuery struct {
	Status string
	Limit  int
}

type RecommendationEvaluationRunView struct {
	ID                  int64                             `json:"id"`
	RunType             string                            `json:"run_type"`
	Status              string                            `json:"status"`
	RequestParams       RecommendationEvaluationRunParams `json:"request_params"`
	TargetEventCount    int32                             `json:"target_event_count"`
	EvaluatedEventCount int32                             `json:"evaluated_event_count"`
	WindowMetricCount   int32                             `json:"window_metric_count"`
	PendingCount        int32                             `json:"pending_count"`
	IncompleteCount     int32                             `json:"incomplete_count"`
	FailedCount         int32                             `json:"failed_count"`
	WorkerID            string                            `json:"worker_id"`
	QueuedAt            time.Time                         `json:"queued_at"`
	StartedAt           *time.Time                        `json:"started_at"`
	FinishedAt          *time.Time                        `json:"finished_at"`
	ErrorCode           string                            `json:"error_code"`
	ErrorMessage        string                            `json:"error_message"`
	ConfigVersion       int64                             `json:"config_version"`
	CreatedAt           time.Time                         `json:"created_at"`
	UpdatedAt           time.Time                         `json:"updated_at"`
}

type evaluationProgress struct {
	targetEvents    int
	evaluatedEvents int
	windowMetrics   int
	pending         int
	incomplete      int
	failed          int
}

func NewRecommendationEvaluationService(db *gorm.DB, runtime *config.Runtime, logger *slog.Logger) *RecommendationEvaluationService {
	return &RecommendationEvaluationService{db: db, runtime: runtime, logger: logger}
}

func (s *RecommendationEvaluationService) CreateRun(ctx context.Context, request RecommendationEvaluationRequest) (*RecommendationEvaluationRunResponse, error) {
	return s.createRun(ctx, RecommendationEvaluationRunTypeManual, request)
}

func (s *RecommendationEvaluationService) CreateScheduledRun(ctx context.Context, request RecommendationEvaluationRequest) (*RecommendationEvaluationRunResponse, error) {
	return s.createRun(ctx, RecommendationEvaluationRunTypeScheduled, request)
}

func (s *RecommendationEvaluationService) createRun(ctx context.Context, runType string, request RecommendationEvaluationRequest) (*RecommendationEvaluationRunResponse, error) {
	performanceConfig, configVersion, err := s.currentPerformanceConfig()
	if err != nil {
		return nil, err
	}
	params, err := normalizeRecommendationEvaluationRequest(request, performanceConfig)
	if err != nil {
		return nil, err
	}
	rawParams, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	model := &db_model.RecommendationEvaluationRun{
		RunType:           runType,
		Status:            RecommendationEvaluationRunStatusQueued,
		RequestParamsJSON: rawParams,
		QueuedAt:          time.Now().UTC(),
		ConfigVersion:     configVersion,
	}
	if err := dal.RecommendationEvaluationRuns.Create(ctx, s.db, model); err != nil {
		return nil, err
	}
	return &RecommendationEvaluationRunResponse{
		RunID:   model.ID,
		Status:  model.Status,
		RunType: model.RunType,
		Message: "recommendation evaluation task queued",
	}, nil
}

func (s *RecommendationEvaluationService) GetRun(ctx context.Context, id int64) (*RecommendationEvaluationRunView, error) {
	model, err := dal.RecommendationEvaluationRuns.QueryByID(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	return recommendationEvaluationRunView(*model)
}

func (s *RecommendationEvaluationService) ListRuns(ctx context.Context, query RecommendationEvaluationRunQuery) ([]RecommendationEvaluationRunView, error) {
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
	if status := strings.ToUpper(strings.TrimSpace(query.Status)); status != "" {
		param.Where = append(param.Where, dal.Eq("status", status))
	}
	models, err := dal.RecommendationEvaluationRuns.QueryByParam(ctx, s.db, param)
	if err != nil {
		return nil, err
	}
	views := make([]RecommendationEvaluationRunView, 0, len(models))
	for _, model := range models {
		view, viewErr := recommendationEvaluationRunView(model)
		if viewErr != nil {
			return nil, viewErr
		}
		views = append(views, *view)
	}
	return views, nil
}

func (s *RecommendationEvaluationService) ClaimAndExecuteNext(ctx context.Context, workerID string) (bool, error) {
	performanceConfig, _, err := s.currentPerformanceConfig()
	if err != nil {
		return false, err
	}
	claimTimeout := time.Duration(performanceConfig.AsyncWorker.ClaimTimeoutMS) * time.Millisecond
	run, err := dal.RecommendationEvaluationRuns.ClaimNextQueued(ctx, s.db, workerID, time.Now().UTC(), claimTimeout)
	if errors.Is(err, dal.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := s.ExecuteRun(ctx, run.ID); err != nil {
		return true, err
	}
	return true, nil
}

func (s *RecommendationEvaluationService) ExecuteRun(ctx context.Context, runID int64) error {
	run, err := dal.RecommendationEvaluationRuns.QueryByID(ctx, s.db, runID)
	if err != nil {
		return err
	}
	var params RecommendationEvaluationRunParams
	if err := json.Unmarshal(run.RequestParamsJSON, &params); err != nil {
		return s.failRun(ctx, runID, "INVALID_REQUEST_PARAMS", err)
	}
	performanceConfig, configVersion, err := s.currentPerformanceConfig()
	if err != nil {
		return s.failRun(ctx, runID, "CONFIG_ERROR", err)
	}
	filter, err := evaluationFilterFromParams(params)
	if err != nil {
		return s.failRun(ctx, runID, "INVALID_REQUEST_PARAMS", err)
	}
	batchSize := performanceConfig.AsyncWorker.BatchSize
	if batchSize <= 0 {
		batchSize = 500
	}
	events, err := s.loadEvaluationEvents(ctx, filter, batchSize)
	if err != nil {
		return s.failRun(ctx, runID, "EVENT_QUERY_FAILED", err)
	}
	progress := evaluationProgress{targetEvents: len(events)}
	if err := s.persistProgress(ctx, runID, progress); err != nil {
		return s.failRun(ctx, runID, "PROGRESS_UPDATE_FAILED", err)
	}
	if len(events) == 0 {
		return s.finishRun(ctx, runID, progress, RecommendationEvaluationRunStatusSucceeded)
	}

	latestMarketDate, err := dal.StockDailyQuotes.QueryLatestTradeDate(ctx, s.db, performanceConfig.QuoteSource)
	if err != nil {
		return s.failRun(ctx, runID, "QUOTE_QUERY_FAILED", err)
	}
	minRecommendDate := events[0].RecommendDate
	for _, event := range events[1:] {
		if event.RecommendDate.Before(minRecommendDate) {
			minRecommendDate = event.RecommendDate
		}
	}
	tradingDates, err := dal.StockDailyQuotes.QueryTradingDates(ctx, s.db, performanceConfig.QuoteSource, minRecommendDate, latestMarketDate)
	if err != nil {
		return s.failRun(ctx, runID, "TRADING_DATE_QUERY_FAILED", err)
	}

	securityCache := make(map[string]*db_model.SecurityMaster)
	quoteCache := make(map[string][]db_model.StockDailyQuote)
	eventIDs := make([]int64, 0, len(events))
	for _, event := range events {
		eventIDs = append(eventIDs, event.ID)
	}
	existingMetrics, err := dal.RecommendationEventWindowMetrics.QueryByEventIDsAndSource(ctx, s.db, eventIDs, performanceConfig.QuoteSource)
	if err != nil {
		return s.failRun(ctx, runID, "METRIC_QUERY_FAILED", err)
	}
	existingByEvent := make(map[int64]map[int]db_model.RecommendationEventWindowMetric)
	for _, metric := range existingMetrics {
		byWindow := existingByEvent[metric.RecommendationEventID]
		if byWindow == nil {
			byWindow = make(map[int]db_model.RecommendationEventWindowMetric)
			existingByEvent[metric.RecommendationEventID] = byWindow
		}
		byWindow[int(metric.WindowDays)] = metric
	}
	var pendingUpserts []db_model.RecommendationEventWindowMetric
	flush := func() error {
		if len(pendingUpserts) == 0 {
			return nil
		}
		if err := dal.RecommendationEventWindowMetrics.UpsertBatch(ctx, s.db, pendingUpserts); err != nil {
			return err
		}
		pendingUpserts = pendingUpserts[:0]
		return s.persistProgress(ctx, runID, progress)
	}

	for _, event := range events {
		if err := ctx.Err(); err != nil {
			return s.failRun(ctx, runID, "CONTEXT_CANCELLED", err)
		}
		security, resolveErr := s.resolveEvaluationSecurity(ctx, event, securityCache)
		if resolveErr != nil {
			return s.failRun(ctx, runID, "SECURITY_QUERY_FAILED", resolveErr)
		}

		existingByWindow := existingByEvent[event.ID]

		var quotes []db_model.StockDailyQuote
		if security != nil && evaluationSecuritySupported(*security, event.Direction) {
			quotes, err = s.quotesForSecurity(ctx, security.TSCode, performanceConfig.QuoteSource, minRecommendDate, latestMarketDate, quoteCache)
			if err != nil {
				return s.failRun(ctx, runID, "QUOTE_QUERY_FAILED", err)
			}
		}

		for _, window := range params.Windows {
			if existing, ok := existingByWindow[window]; ok && !params.ForceRebuild && existing.Status == string(evaluation.StatusReady) && existing.CalcVersion == performanceConfig.CalcVersion {
				progress.windowMetrics++
				continue
			}
			metric := buildEvaluationMetric(event, security, window, runID, performanceConfig, configVersion, quotes, tradingDates, latestMarketDate)
			pendingUpserts = append(pendingUpserts, metric)
			progress.windowMetrics++
			switch evaluation.Status(metric.Status) {
			case evaluation.StatusPending:
				progress.pending++
			case evaluation.StatusIncomplete, evaluation.StatusNoSecurity, evaluation.StatusUnsupported:
				progress.incomplete++
			case evaluation.StatusFailed:
				progress.failed++
			}
		}
		progress.evaluatedEvents++
		if len(pendingUpserts) >= batchSize*len(params.Windows) {
			if err := flush(); err != nil {
				return s.failRun(ctx, runID, "METRIC_UPSERT_FAILED", err)
			}
		}
	}
	if err := flush(); err != nil {
		return s.failRun(ctx, runID, "METRIC_UPSERT_FAILED", err)
	}

	status := RecommendationEvaluationRunStatusSucceeded
	if progress.failed > 0 {
		status = RecommendationEvaluationRunStatusPartialFailed
		if progress.failed == progress.windowMetrics {
			status = RecommendationEvaluationRunStatusFailed
		}
	}
	return s.finishRun(ctx, runID, progress, status)
}

func (s *RecommendationEvaluationService) loadEvaluationEvents(ctx context.Context, filter dal.RecommendationEventEvaluationFilter, batchSize int) ([]db_model.RecommendationEvent, error) {
	filter.Limit = batchSize
	var result []db_model.RecommendationEvent
	for {
		batch, err := dal.RecommendationEvents.QueryForEvaluation(ctx, s.db, filter)
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}
		result = append(result, batch...)
		filter.AfterID = batch[len(batch)-1].ID
		if len(batch) < batchSize {
			break
		}
	}
	return result, nil
}

func (s *RecommendationEvaluationService) resolveEvaluationSecurity(ctx context.Context, event db_model.RecommendationEvent, cache map[string]*db_model.SecurityMaster) (*db_model.SecurityMaster, error) {
	cacheKey := strings.ToUpper(strings.TrimSpace(event.Symbol)) + "|" + strings.ToUpper(strings.TrimSpace(event.Market)) + "|" + strings.ToUpper(strings.TrimSpace(event.AssetType))
	if cached, ok := cache[cacheKey]; ok {
		return cached, nil
	}

	candidates := evaluationTSCodes(event.Symbol, event.Market)
	for _, tsCode := range candidates {
		security, err := dal.SecurityMasters.QueryByTSCode(ctx, s.db, tsCode)
		if err == nil {
			cache[cacheKey] = security
			return security, nil
		}
		if !errors.Is(err, dal.ErrNotFound) {
			return nil, err
		}
	}

	symbol := strings.TrimSpace(event.Symbol)
	if dot := strings.Index(symbol, "."); dot > 0 {
		symbol = symbol[:dot]
	}
	securities, err := dal.SecurityMasters.QueryBySymbol(ctx, s.db, symbol)
	if err != nil {
		return nil, err
	}
	for index := range securities {
		security := &securities[index]
		if securityMatchesEvent(*security, event) {
			copy := *security
			cache[cacheKey] = &copy
			return &copy, nil
		}
	}
	if len(securities) == 1 {
		copy := securities[0]
		cache[cacheKey] = &copy
		return &copy, nil
	}
	cache[cacheKey] = nil
	return nil, nil
}

func (s *RecommendationEvaluationService) quotesForSecurity(ctx context.Context, tsCode string, source string, dateFrom time.Time, dateTo time.Time, cache map[string][]db_model.StockDailyQuote) ([]db_model.StockDailyQuote, error) {
	if cached, ok := cache[tsCode]; ok {
		return cached, nil
	}
	quotes, err := dal.StockDailyQuotes.QueryByTSCodeRange(ctx, s.db, tsCode, source, dateFrom, dateTo)
	if err != nil {
		return nil, err
	}
	cache[tsCode] = quotes
	return quotes, nil
}

func (s *RecommendationEvaluationService) persistProgress(ctx context.Context, runID int64, progress evaluationProgress) error {
	return dal.RecommendationEvaluationRuns.UpdateByID(ctx, s.db, runID, map[string]any{
		"target_event_count":    progress.targetEvents,
		"evaluated_event_count": progress.evaluatedEvents,
		"window_metric_count":   progress.windowMetrics,
		"pending_count":         progress.pending,
		"incomplete_count":      progress.incomplete,
		"failed_count":          progress.failed,
	})
}

func (s *RecommendationEvaluationService) finishRun(ctx context.Context, runID int64, progress evaluationProgress, status string) error {
	now := time.Now().UTC()
	return dal.RecommendationEvaluationRuns.UpdateByID(ctx, s.db, runID, map[string]any{
		"status":                status,
		"target_event_count":    progress.targetEvents,
		"evaluated_event_count": progress.evaluatedEvents,
		"window_metric_count":   progress.windowMetrics,
		"pending_count":         progress.pending,
		"incomplete_count":      progress.incomplete,
		"failed_count":          progress.failed,
		"finished_at":           now,
		"error_code":            "",
		"error_message":         "",
	})
}

func (s *RecommendationEvaluationService) failRun(ctx context.Context, runID int64, code string, runErr error) error {
	now := time.Now().UTC()
	_ = dal.RecommendationEvaluationRuns.UpdateByID(context.WithoutCancel(ctx), s.db, runID, map[string]any{
		"status":        RecommendationEvaluationRunStatusFailed,
		"error_code":    code,
		"error_message": runErr.Error(),
		"finished_at":   now,
	})
	return runErr
}

func (s *RecommendationEvaluationService) currentPerformanceConfig() (config.RecommendationPerformanceConfig, int64, error) {
	cfg := s.runtime.Config()
	if cfg == nil {
		return config.RecommendationPerformanceConfig{}, 0, fmt.Errorf("config runtime unavailable")
	}
	if !cfg.Evaluation.Enabled || !cfg.Evaluation.RecommendationPerformance.Enabled {
		return config.RecommendationPerformanceConfig{}, cfg.Meta.ConfigVersion, fmt.Errorf("evaluation.recommendation_performance is disabled")
	}
	return cfg.Evaluation.RecommendationPerformance, cfg.Meta.ConfigVersion, nil
}

func normalizeRecommendationEvaluationRequest(request RecommendationEvaluationRequest, cfg config.RecommendationPerformanceConfig) (RecommendationEvaluationRunParams, error) {
	params := RecommendationEvaluationRunParams{
		DateFrom:     strings.TrimSpace(request.DateFrom),
		DateTo:       strings.TrimSpace(request.DateTo),
		BloggerIDs:   uniquePositiveInt64s(request.BloggerIDs),
		Symbols:      normalizeEvaluationSymbols(request.Symbols),
		EventIDs:     uniquePositiveInt64s(request.EventIDs),
		Windows:      append([]int(nil), request.Windows...),
		ForceRebuild: request.ForceRebuild,
		OnlyActive:   true,
	}
	if request.OnlyActive != nil {
		params.OnlyActive = *request.OnlyActive
	}
	if params.DateFrom != "" {
		if _, err := time.Parse(time.DateOnly, params.DateFrom); err != nil {
			return params, fmt.Errorf("invalid date_from %q, expected YYYY-MM-DD", params.DateFrom)
		}
	}
	if params.DateTo != "" {
		if _, err := time.Parse(time.DateOnly, params.DateTo); err != nil {
			return params, fmt.Errorf("invalid date_to %q, expected YYYY-MM-DD", params.DateTo)
		}
	}
	if params.DateFrom != "" && params.DateTo != "" && params.DateFrom > params.DateTo {
		return params, fmt.Errorf("date_from must not be after date_to")
	}
	configuredWindows := make(map[int]struct{}, len(cfg.Windows))
	for _, window := range cfg.Windows {
		configuredWindows[window] = struct{}{}
	}
	if len(params.Windows) == 0 {
		params.Windows = append([]int(nil), cfg.Windows...)
	}
	seenWindows := make(map[int]struct{}, len(params.Windows))
	normalizedWindows := make([]int, 0, len(params.Windows))
	for _, window := range params.Windows {
		if _, ok := configuredWindows[window]; !ok {
			return params, fmt.Errorf("window %d is not configured", window)
		}
		if _, duplicated := seenWindows[window]; duplicated {
			continue
		}
		seenWindows[window] = struct{}{}
		normalizedWindows = append(normalizedWindows, window)
	}
	sort.Ints(normalizedWindows)
	params.Windows = normalizedWindows
	return params, nil
}

func evaluationFilterFromParams(params RecommendationEvaluationRunParams) (dal.RecommendationEventEvaluationFilter, error) {
	filter := dal.RecommendationEventEvaluationFilter{
		BloggerIDs: params.BloggerIDs,
		Symbols:    params.Symbols,
		EventIDs:   params.EventIDs,
		OnlyActive: params.OnlyActive,
	}
	if params.DateFrom != "" {
		value, err := time.Parse(time.DateOnly, params.DateFrom)
		if err != nil {
			return filter, err
		}
		filter.DateFrom = &value
	}
	if params.DateTo != "" {
		value, err := time.Parse(time.DateOnly, params.DateTo)
		if err != nil {
			return filter, err
		}
		filter.DateTo = &value
	}
	return filter, nil
}

func buildEvaluationMetric(event db_model.RecommendationEvent, security *db_model.SecurityMaster, window int, runID int64, cfg config.RecommendationPerformanceConfig, configVersion int64, quotes []db_model.StockDailyQuote, tradingDates []time.Time, latestMarketDate time.Time) db_model.RecommendationEventWindowMetric {
	now := time.Now().UTC()
	metric := db_model.RecommendationEventWindowMetric{
		RecommendationEventID: event.ID,
		BloggerID:             event.BloggerID,
		Symbol:                event.Symbol,
		AssetType:             event.AssetType,
		Market:                event.Market,
		Direction:             strings.ToUpper(strings.TrimSpace(event.Direction)),
		RecommendDate:         event.RecommendDate,
		WindowDays:            int32(window),
		QuoteSource:           cfg.QuoteSource,
		ExpectedQuoteCount:    int32(window),
		EvaluationRunID:       runID,
		CalcVersion:           cfg.CalcVersion,
		ConfigVersion:         configVersion,
		CalculatedAt:          now,
	}
	if security == nil {
		metric.Status = string(evaluation.StatusNoSecurity)
		metric.ReasonCode = "SECURITY_NOT_FOUND"
		metric.ReasonMessage = "recommendation event could not be associated with security_master"
		return metric
	}
	metric.SecurityMasterID = security.ID
	metric.TSCode = security.TSCode
	metric.Symbol = security.Symbol
	metric.SecurityName = security.Name
	metric.AssetType = security.AssetType
	metric.Market = security.Market
	metric.Industry = security.Industry
	if !evaluationSecuritySupported(*security, event.Direction) {
		metric.Status = string(evaluation.StatusUnsupported)
		metric.ReasonCode = "UNSUPPORTED_SECURITY"
		metric.ReasonMessage = fmt.Sprintf("asset_type %q or direction %q is unsupported", security.AssetType, event.Direction)
		return metric
	}

	evaluationQuotes := make([]evaluation.Quote, 0, len(quotes))
	for _, quote := range quotes {
		evaluationQuotes = append(evaluationQuotes, evaluation.Quote{
			TradeDate: quote.TradeDate,
			Open:      quote.OpenPrice,
			High:      quote.HighPrice,
			Low:       quote.LowPrice,
			Close:     quote.ClosePrice,
		})
	}
	result := evaluation.EvaluateWindow(evaluation.Input{
		RecommendDate:         event.RecommendDate,
		Direction:             event.Direction,
		WindowDays:            window,
		Quotes:                evaluationQuotes,
		MarketTradingDates:    tradingDates,
		LatestMarketDate:      latestMarketDate,
		WinThresholdRatio:     cfg.WinThresholdRatio,
		MinQuoteCoverageRatio: cfg.MinQuoteCoverageRatio,
	})
	metric.Status = string(result.Status)
	metric.ReasonCode = result.ReasonCode
	metric.ReasonMessage = result.ReasonMessage
	metric.BaseDate = result.BaseDate
	metric.BaseClosePrice = result.BaseClosePrice
	metric.EntryDate = result.EntryDate
	metric.EntryPrice = result.EntryPrice
	metric.ExitDate = result.ExitDate
	metric.ExitClosePrice = result.ExitClosePrice
	metric.ExpectedQuoteCount = int32(result.ExpectedQuoteCount)
	metric.ActualQuoteCount = int32(result.ActualQuoteCount)
	metric.MissingQuoteCount = int32(result.MissingQuoteCount)
	metric.RawReturnRatio = result.RawReturnRatio
	metric.DirectionReturnRatio = result.DirectionReturnRatio
	metric.MaxFavorableReturnRatio = result.MaxFavorableReturnRatio
	metric.MaxAdverseReturnRatio = result.MaxAdverseReturnRatio
	metric.MaxDrawdownRatio = result.MaxDrawdownRatio
	metric.WinFlag = result.WinFlag
	metric.BestTradeDate = result.BestTradeDate
	metric.WorstTradeDate = result.WorstTradeDate
	return metric
}

func recommendationEvaluationRunView(model db_model.RecommendationEvaluationRun) (*RecommendationEvaluationRunView, error) {
	var params RecommendationEvaluationRunParams
	if err := json.Unmarshal(model.RequestParamsJSON, &params); err != nil {
		return nil, fmt.Errorf("decode evaluation run %d request params: %w", model.ID, err)
	}
	return &RecommendationEvaluationRunView{
		ID:                  model.ID,
		RunType:             model.RunType,
		Status:              model.Status,
		RequestParams:       params,
		TargetEventCount:    model.TargetEventCount,
		EvaluatedEventCount: model.EvaluatedEventCount,
		WindowMetricCount:   model.WindowMetricCount,
		PendingCount:        model.PendingCount,
		IncompleteCount:     model.IncompleteCount,
		FailedCount:         model.FailedCount,
		WorkerID:            model.WorkerID,
		QueuedAt:            model.QueuedAt,
		StartedAt:           model.StartedAt,
		FinishedAt:          model.FinishedAt,
		ErrorCode:           model.ErrorCode,
		ErrorMessage:        model.ErrorMessage,
		ConfigVersion:       model.ConfigVersion,
		CreatedAt:           model.CreatedAt,
		UpdatedAt:           model.UpdatedAt,
	}, nil
}

func evaluationTSCodes(symbol string, market string) []string {
	value := strings.ToUpper(strings.TrimSpace(symbol))
	if value == "" {
		return nil
	}
	if strings.Contains(value, ".") {
		return []string{value}
	}
	var suffixes []string
	switch strings.ToUpper(strings.TrimSpace(market)) {
	case "SH", "SSE", "SHSE":
		suffixes = []string{"SH"}
	case "SZ", "SZSE":
		suffixes = []string{"SZ"}
	case "BJ", "BSE":
		suffixes = []string{"BJ"}
	default:
		suffixes = []string{"SH", "SZ", "BJ"}
	}
	result := make([]string, 0, len(suffixes))
	for _, suffix := range suffixes {
		result = append(result, value+"."+suffix)
	}
	return result
}

func securityMatchesEvent(security db_model.SecurityMaster, event db_model.RecommendationEvent) bool {
	market := strings.ToUpper(strings.TrimSpace(event.Market))
	if market != "" {
		suffix := ""
		if dot := strings.LastIndex(security.TSCode, "."); dot >= 0 {
			suffix = strings.ToUpper(security.TSCode[dot+1:])
		}
		if suffix != market && !(market == "SSE" && suffix == "SH") && !(market == "SZSE" && suffix == "SZ") {
			return false
		}
	}
	return evaluationAssetTypeSupported(security.AssetType) && evaluationAssetTypeSupported(event.AssetType)
}

func evaluationSecuritySupported(security db_model.SecurityMaster, direction string) bool {
	direction = strings.ToUpper(strings.TrimSpace(direction))
	return evaluationAssetTypeSupported(security.AssetType) && (direction == "LONG" || direction == "SHORT")
}

func evaluationAssetTypeSupported(value string) bool {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "STOCK", "A_SHARE", "ASHARE", "ETF":
		return true
	default:
		return false
	}
}

func normalizeEvaluationSymbols(values []string) []string {
	seen := map[string]struct{}{}
	var result []string
	for _, raw := range values {
		value := strings.ToUpper(strings.TrimSpace(raw))
		if value == "" {
			continue
		}
		candidates := []string{value}
		if !strings.Contains(value, ".") {
			candidates = append(candidates, value+".SH", value+".SZ", value+".BJ")
		}
		for _, candidate := range candidates {
			if _, exists := seen[candidate]; exists {
				continue
			}
			seen[candidate] = struct{}{}
			result = append(result, candidate)
		}
	}
	return result
}

func uniquePositiveInt64s(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
