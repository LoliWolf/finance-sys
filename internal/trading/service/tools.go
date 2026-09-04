package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	tradingdomain "finance-sys/internal/trading/domain"
	"finance-sys/internal/trading/instrument"
)

func (s *Service) RecommendationCandidates(ctx context.Context, asOf time.Time, afterID int64, limit int) (*tradingdomain.CandidateList, error) {
	cfg := s.runtime.Config()
	if cfg == nil {
		return nil, fmt.Errorf("runtime config unavailable")
	}
	if asOf.IsZero() {
		asOf = time.Now()
	}
	limit = boundedLimit(limit)
	if limit > cfg.Trading.Decision.MaxCandidatesPerRun {
		limit = cfg.Trading.Decision.MaxCandidatesPerRun
	}
	events, err := dal.RecommendationEvents.QueryForTrading(ctx, s.db, asOf, cfg.Trading.Decision.MinRecommendationConfidence, afterID, limit)
	if err != nil {
		return nil, err
	}
	items := make([]tradingdomain.RecommendationCandidate, 0, len(events))
	nextCursor := ""
	for _, event := range events {
		security, securityErr := instrument.Parse(event.Symbol, event.Market, event.AssetType)
		if securityErr != nil {
			continue
		}
		item := tradingdomain.RecommendationCandidate{
			RecommendationEventID: event.ID, CandidatePlanID: event.PlanID, BloggerID: event.BloggerID,
			Symbol: security.Symbol, TSCode: security.TSCode, Market: security.Market, AssetType: security.AssetType,
			BoardType: security.BoardType, EastmoneySymbol: security.EastmoneySymbol, Direction: event.Direction,
			RecommendDate: event.RecommendDate.Format(time.DateOnly), ReferencePrice: strconv.FormatFloat(event.ReferencePrice, 'f', 6, 64),
			EntryPrice: strconv.FormatFloat(event.ReferencePrice, 'f', 6, 64), PositionRatio: "0.03000000",
			Confidence: strconv.FormatFloat(event.Confidence, 'f', 8, 64), RuleVersion: event.RuleVersion,
			EvidenceRefs: []string{fmt.Sprintf("recommendation_event:%d", event.ID)}, ObservedAt: event.UpdatedAt,
		}
		if master, masterErr := dal.SecurityMasters.QueryByTSCode(ctx, s.db, security.TSCode); masterErr == nil && master.ListDate != nil {
			if master.ListDate.Before(asOf.AddDate(0, 0, -14)) {
				item.ListingTradingDays = 6
			} else {
				dates, datesErr := dal.StockDailyQuotes.QueryTradingDates(ctx, s.db, "TUSHARE", *master.ListDate, asOf)
				if datesErr == nil {
					item.ListingTradingDays = len(dates)
				}
			}
			item.NoPriceLimitPeriod = security.AssetType == "STOCK" && item.ListingTradingDays > 0 && item.ListingTradingDays <= 5
		}
		if cfg.Trading.Risk.ExcludeNoPriceLimit && item.NoPriceLimitPeriod {
			continue
		}
		if event.PlanID != nil {
			if plan, planErr := dal.TradeCandidatePlans.QueryByID(ctx, s.db, *event.PlanID); planErr == nil {
				item.EntryPrice = strconv.FormatFloat(plan.EntryPrice, 'f', 6, 64)
				item.PositionRatio = strconv.FormatFloat(plan.PositionPct, 'f', 8, 64)
				item.RuleVersion = plan.RuleVersion
				item.EvidenceRefs = append(item.EvidenceRefs, fmt.Sprintf("candidate_plan:%d", plan.ID))
			}
		}
		items = append(items, item)
		nextCursor = strconv.FormatInt(event.ID, 10)
	}
	return &tradingdomain.CandidateList{SchemaVersion: cfg.Trading.Decision.ToolContractVersion, AsOfTime: asOf, NextCursor: nextCursor, Items: items}, nil
}

func (s *Service) BloggerPerformance(ctx context.Context, bloggerID int64, windowDays int) (*BloggerPerformance, error) {
	if windowDays <= 0 {
		windowDays = 30
	}
	type aggregate struct {
		EvaluableCount   int64
		WinCount         int64
		AverageReturn    float64
		UnevaluableCount int64
	}
	var value aggregate
	err := s.db.WithContext(ctx).Model(&db_model.RecommendationEventWindowMetric{}).
		Select("SUM(CASE WHEN status = 'READY' THEN 1 ELSE 0 END) AS evaluable_count, SUM(CASE WHEN status = 'READY' AND win_flag = 1 THEN 1 ELSE 0 END) AS win_count, COALESCE(AVG(CASE WHEN status = 'READY' THEN direction_return_ratio END), 0) AS average_return, SUM(CASE WHEN status <> 'READY' THEN 1 ELSE 0 END) AS unevaluable_count").
		Where("blogger_id = ? AND window_days = ?", bloggerID, windowDays).
		Scan(&value).Error
	if err != nil {
		return nil, err
	}
	winRate := float64(0)
	if value.EvaluableCount > 0 {
		winRate = float64(value.WinCount) / float64(value.EvaluableCount)
	}
	return &BloggerPerformance{BloggerID: bloggerID, WindowDays: windowDays, EvaluableCount: value.EvaluableCount, WinCount: value.WinCount, WinRate: winRate, AverageReturn: value.AverageReturn, UnevaluableCount: value.UnevaluableCount}, nil
}

func (s *Service) MarketSnapshots(ctx context.Context, symbols []string, asOf time.Time) ([]MarketSnapshot, error) {
	if asOf.IsZero() {
		asOf = time.Now()
	}
	securities := make([]instrument.Canonical, 0, len(symbols))
	for _, symbol := range symbols {
		security, err := instrument.Parse(symbol, "", "")
		if err == nil {
			securities = append(securities, security)
		}
	}
	realtimeBySymbol := make(map[string]tradingdomain.QuoteSnapshot)
	now := time.Now()
	cfg := s.runtime.Config()
	if cfg != nil && sessionWindowOpen(asOf, cfg.Trading.Scheduler) && !asOf.Before(now.Add(-5*time.Minute)) && !asOf.After(now.Add(time.Minute)) {
		bridgeSymbols := make([]string, 0, len(securities))
		for _, security := range securities {
			bridgeSymbols = append(bridgeSymbols, security.EastmoneySymbol)
		}
		if quotes, err := s.refreshBridgeQuotes(ctx, bridgeSymbols, asOf); err == nil {
			for _, quote := range quotes {
				if security, parseErr := instrument.Parse(quote.EastmoneySymbol, "", ""); parseErr == nil {
					realtimeBySymbol[security.Symbol] = quote
				}
			}
		}
	}
	items := make([]MarketSnapshot, 0, len(symbols))
	for _, raw := range symbols {
		security, parseErr := instrument.Parse(raw, "", "")
		if parseErr != nil {
			items = append(items, MarketSnapshot{Symbol: raw, ObservedAt: asOf, Source: "LOCAL", MissingReason: "INVALID_SYMBOL"})
			continue
		}
		if quote, ok := realtimeBySymbol[security.Symbol]; ok && quote.Price != "" {
			items = append(items, MarketSnapshot{Symbol: security.Symbol, TSCode: security.TSCode, BoardType: security.BoardType, Price: quote.Price, TradeDate: quote.ObservedAt.Format(time.DateOnly), ObservedAt: quote.ObservedAt, Source: quote.Source})
			continue
		}
		quote, err := dal.StockDailyQuotes.QueryLatestBySymbolAt(ctx, s.db, security.Symbol, asOf)
		if errors.Is(err, dal.ErrNotFound) {
			items = append(items, MarketSnapshot{Symbol: security.Symbol, TSCode: security.TSCode, BoardType: security.BoardType, ObservedAt: asOf, Source: "TUSHARE", MissingReason: "QUOTE_UNAVAILABLE"})
			continue
		}
		if err != nil {
			return nil, err
		}
		price := ""
		if quote.ClosePrice > 0 {
			price = strconv.FormatFloat(quote.ClosePrice, 'f', 4, 64)
		}
		item := MarketSnapshot{Symbol: security.Symbol, TSCode: security.TSCode, BoardType: security.BoardType, Price: price, TradeDate: quote.TradeDate.Format(time.DateOnly), ObservedAt: quote.UpdatedAt, Source: quote.Source}
		if price == "" {
			item.MissingReason = "QUOTE_UNAVAILABLE"
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Service) DailyHistory(ctx context.Context, symbols []string, asOf time.Time, limit int) ([]DailyHistoryItem, error) {
	if asOf.IsZero() {
		asOf = time.Now()
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 120 {
		limit = 120
	}
	items := make([]DailyHistoryItem, 0, len(symbols)*limit)
	for _, raw := range symbols {
		security, err := instrument.Parse(raw, "", "")
		if err != nil {
			continue
		}
		quotes, err := dal.StockDailyQuotes.QueryByTSCodeRange(ctx, s.db, security.TSCode, "TUSHARE", asOf.AddDate(0, 0, -240), asOf)
		if err != nil {
			return nil, err
		}
		if len(quotes) > limit {
			quotes = quotes[len(quotes)-limit:]
		}
		for _, quote := range quotes {
			items = append(items, DailyHistoryItem{Symbol: security.Symbol, TSCode: security.TSCode, TradeDate: quote.TradeDate.Format(time.DateOnly), OpenPrice: quote.OpenPrice, HighPrice: quote.HighPrice, LowPrice: quote.LowPrice, ClosePrice: quote.ClosePrice, PctChg: quote.PctChg, Volume: quote.Volume, Source: quote.Source})
		}
	}
	return items, nil
}

func (s *Service) refreshBridgeQuotes(ctx context.Context, symbols []string, requestedAt time.Time) ([]tradingdomain.QuoteSnapshot, error) {
	if s.bridge == nil {
		return nil, fmt.Errorf("trading Bridge unavailable")
	}
	normalized := uniqueSymbols(symbols)
	if len(normalized) == 0 {
		return []tradingdomain.QuoteSnapshot{}, nil
	}
	key := hashParts("quote-refresh", strings.Join(normalized, ","), requestedAt.UTC().Format(time.RFC3339Nano))
	if _, err := s.bridge.RefreshQuotes(ctx, normalized, key); err != nil {
		return nil, err
	}
	wait := 5 * time.Second
	if cfg := s.runtime.Config(); cfg != nil && cfg.Trading.Bridge.RequestTimeoutMS > 0 {
		wait = time.Duration(cfg.Trading.Bridge.RequestTimeoutMS) * time.Millisecond
	}
	if wait < 500*time.Millisecond {
		wait = 500 * time.Millisecond
	}
	if wait > 10*time.Second {
		wait = 10 * time.Second
	}
	maxQuoteAge := 15 * time.Second
	if cfg := s.runtime.Config(); cfg != nil && cfg.Trading.Risk.MaxSnapshotAgeSeconds > 0 {
		maxQuoteAge = time.Duration(cfg.Trading.Risk.MaxSnapshotAgeSeconds) * time.Second
	}
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		quotes, err := s.bridge.Quotes(ctx, normalized)
		if err == nil && quotesFreshFor(quotes, normalized, requestedAt.Add(-maxQuoteAge)) {
			return quotes, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, fmt.Errorf("timed out waiting for fresh Bridge quotes")
		case <-ticker.C:
		}
	}
}

func quotesFreshFor(quotes []tradingdomain.QuoteSnapshot, symbols []string, minimumObservedAt time.Time) bool {
	available := make(map[string]bool, len(quotes))
	for _, quote := range quotes {
		if quote.Price != "" && !quote.ObservedAt.Before(minimumObservedAt) {
			if security, err := instrument.Parse(quote.EastmoneySymbol, "", ""); err == nil {
				available[security.Symbol] = true
				available[security.EastmoneySymbol] = true
			}
		}
	}
	for _, symbol := range symbols {
		security, err := instrument.Parse(symbol, "", "")
		if err != nil || (!available[security.Symbol] && !available[security.EastmoneySymbol]) {
			return false
		}
	}
	return true
}

func uniqueSymbols(symbols []string) []string {
	seen := make(map[string]struct{}, len(symbols))
	result := make([]string, 0, len(symbols))
	for _, value := range symbols {
		security, err := instrument.Parse(value, "", "")
		if err != nil {
			continue
		}
		symbol := security.EastmoneySymbol
		if _, ok := seen[symbol]; ok {
			continue
		}
		seen[symbol] = struct{}{}
		result = append(result, symbol)
	}
	sort.Strings(result)
	return result
}

func (s *Service) Portfolio(ctx context.Context) (*PortfolioView, error) {
	account, err := s.LatestAccount(ctx)
	if errors.Is(err, dal.ErrNotFound) {
		return &PortfolioView{Positions: []db_model.TradingPositionSnapshot{}, Cycles: []db_model.TradingPositionCycle{}, OpenOrders: []db_model.TradingOrder{}}, nil
	}
	if err != nil {
		return nil, err
	}
	positions, err := dal.TradingPositionSnapshots.ByAccountSnapshot(ctx, s.db, account.ID)
	if err != nil {
		return nil, err
	}
	orders, err := dal.TradingOrders.QueryOpen(ctx, s.db)
	if err != nil {
		return nil, err
	}
	cycles, err := dal.TradingPositionCycles.ListOpen(ctx, s.db, account.AccountID)
	if err != nil {
		return nil, err
	}
	return &PortfolioView{Account: account, Positions: positions, Cycles: cycles, OpenOrders: orders}, nil
}

func (s *Service) RiskBudget(ctx context.Context) (*RiskBudgetView, error) {
	cfg := s.runtime.Config()
	if cfg == nil {
		return nil, fmt.Errorf("runtime config unavailable")
	}
	runtimeKill := true
	if control, err := dal.TradingRuntimeControls.KillSwitch(ctx, s.db); err == nil {
		runtimeKill = control.Enabled
	}
	return &RiskBudgetView{
		TradingEnabled: cfg.Trading.Enabled, NacosKillSwitch: cfg.Trading.KillSwitch, RuntimeKillSwitch: runtimeKill,
		MaxTotalRatio: cfg.Trading.Risk.MaxTotalPositionRatio, MaxSymbolRatio: cfg.Trading.Risk.MaxSymbolPositionRatio,
		MaxSingleOrderRatio: cfg.Trading.Risk.MaxSingleOrderRatio, RiskVersion: cfg.Trading.Risk.Version, ConfigVersion: cfg.Meta.ConfigVersion,
	}, nil
}

func marshalJSON(value any) []byte {
	raw, _ := json.Marshal(value)
	return raw
}
